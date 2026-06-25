package model

import (
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// insertEpayTopUpForTest creates a pending Epay topup row with the given
// payment method, paid money, amount, and (optionally) an overridden status.
// The user must already exist (or be omitted to exercise the missing-user
// rollback path).
func insertEpayTopUpForTest(t *testing.T, tradeNo string, userID int, paymentMethod string, money float64, amount int64, status string) {
	t.Helper()
	topUp := &TopUp{
		UserId:          userID,
		Amount:          amount,
		Money:           money,
		TradeNo:         tradeNo,
		PaymentMethod:   paymentMethod,
		PaymentProvider: PaymentProviderEpay,
		Status:          status,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())
}

func insertEpayUserForTest(t *testing.T, id int, quota int) {
	t.Helper()
	user := &User{
		Id:       id,
		Username: "epay_user",
		Status:   common.UserStatusEnabled,
		Quota:    quota,
	}
	require.NoError(t, DB.Create(user).Error)
}

func getUserQuotaForEpayTest(t *testing.T, userID int) int {
	t.Helper()
	var user User
	require.NoError(t, DB.Select("quota").Where("id = ?", userID).First(&user).Error)
	return user.Quota
}

func getTopUpForEpayTest(t *testing.T, tradeNo string) *TopUp {
	t.Helper()
	topUp := GetTopUpByTradeNo(tradeNo)
	require.NotNil(t, topUp)
	return topUp
}

// TestCompleteEpayTopUp_HappyPath verifies a correct pending Epay topup is
// transitioned to success, quota is incremented, and Completed is true.
func TestCompleteEpayTopUp_HappyPath(t *testing.T) {
	truncateTables(t)
	insertEpayUserForTest(t, 701, 0)
	insertEpayTopUpForTest(t, "epay-happy", 701, "alipay", 9.99, 2, common.TopUpStatusPending)

	completion, err := CompleteEpayTopUp("epay-happy", "alipay", "9.99", "127.0.0.1")
	require.NoError(t, err)
	require.NotNil(t, completion)
	assert.True(t, completion.Completed)
	assert.Equal(t, 701, completion.UserId)
	assert.Equal(t, "alipay", completion.PaymentMethod)
	assert.InDelta(t, 9.99, completion.PayMoney, 0.0001)
	expectedQuota := int(float64(2) * common.QuotaPerUnit)
	assert.Equal(t, expectedQuota, completion.QuotaToAdd)
	assert.Equal(t, expectedQuota, getUserQuotaForEpayTest(t, 701))

	topUp := getTopUpForEpayTest(t, "epay-happy")
	assert.Equal(t, common.TopUpStatusSuccess, topUp.Status)
	assert.NotZero(t, topUp.CompleteTime)
	assert.GreaterOrEqual(t, topUp.CompleteTime, topUp.CreateTime)
}

// TestCompleteEpayTopUp_DuplicateSuccessIsIdempotent verifies that a second
// completion on an already-success order returns nil with Completed=false and
// does NOT re-add quota or rewrite status/CompleteTime.
func TestCompleteEpayTopUp_DuplicateSuccessIsIdempotent(t *testing.T) {
	truncateTables(t)
	insertEpayUserForTest(t, 702, 0)
	insertEpayTopUpForTest(t, "epay-dup", 702, "wxpay", 5.50, 1, common.TopUpStatusPending)

	first, err := CompleteEpayTopUp("epay-dup", "wxpay", "5.50", "127.0.0.1")
	require.NoError(t, err)
	require.True(t, first.Completed)
	expectedQuota := int(float64(1) * common.QuotaPerUnit)
	assert.Equal(t, expectedQuota, getUserQuotaForEpayTest(t, 702))

	afterFirst := getTopUpForEpayTest(t, "epay-dup")
	afterFirstCompleteTime := afterFirst.CompleteTime

	second, err := CompleteEpayTopUp("epay-dup", "wxpay", "5.50", "127.0.0.1")
	require.NoError(t, err)
	require.NotNil(t, second)
	assert.False(t, second.Completed)
	assert.Equal(t, expectedQuota, getUserQuotaForEpayTest(t, 702))

	afterSecond := getTopUpForEpayTest(t, "epay-dup")
	assert.Equal(t, common.TopUpStatusSuccess, afterSecond.Status)
	assert.Equal(t, afterFirstCompleteTime, afterSecond.CompleteTime, "CompleteTime must not be rewritten on idempotent dup")
}

// TestCompleteEpayTopUp_AmountMismatchKeepsPending verifies a paid-money
// mismatch leaves the order pending and quota untouched.
func TestCompleteEpayTopUp_AmountMismatchKeepsPending(t *testing.T) {
	truncateTables(t)
	insertEpayUserForTest(t, 703, 100)
	insertEpayTopUpForTest(t, "epay-amount", 703, "alipay", 9.99, 2, common.TopUpStatusPending)

	_, err := CompleteEpayTopUp("epay-amount", "alipay", "8.88", "127.0.0.1")
	require.ErrorIs(t, err, ErrPaidMoneyMismatch)

	topUp := getTopUpForEpayTest(t, "epay-amount")
	assert.Equal(t, common.TopUpStatusPending, topUp.Status)
	assert.Zero(t, topUp.CompleteTime)
	assert.Equal(t, 100, getUserQuotaForEpayTest(t, 703))
}

// TestCompleteEpayTopUp_PaymentMethodMismatchKeepsPending verifies a callback
// payment method that differs from the stored method is rejected (no silent
// rewrite) and the order stays pending.
func TestCompleteEpayTopUp_PaymentMethodMismatchKeepsPending(t *testing.T) {
	truncateTables(t)
	insertEpayUserForTest(t, 704, 50)
	insertEpayTopUpForTest(t, "epay-method", 704, "alipay", 9.99, 2, common.TopUpStatusPending)

	_, err := CompleteEpayTopUp("epay-method", "wxpay", "9.99", "127.0.0.1")
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)

	topUp := getTopUpForEpayTest(t, "epay-method")
	assert.Equal(t, common.TopUpStatusPending, topUp.Status)
	assert.Equal(t, "alipay", topUp.PaymentMethod, "stored method must not be silently rewritten")
	assert.Equal(t, 50, getUserQuotaForEpayTest(t, 704))
}

// TestCompleteEpayTopUp_ProviderMismatchKeepsPending verifies a non-Epay
// provider is rejected with ErrPaymentMethodMismatch and no quota change.
func TestCompleteEpayTopUp_ProviderMismatchKeepsPending(t *testing.T) {
	truncateTables(t)
	insertEpayUserForTest(t, 705, 50)
	// Stored as a Stripe provider topup; an Epay callback must not claim it.
	topUp := &TopUp{
		UserId:          705,
		Amount:          2,
		Money:           9.99,
		TradeNo:         "epay-provider",
		PaymentMethod:   "alipay",
		PaymentProvider: PaymentProviderStripe,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())

	_, err := CompleteEpayTopUp("epay-provider", "alipay", "9.99", "127.0.0.1")
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)

	refreshed := getTopUpForEpayTest(t, "epay-provider")
	assert.Equal(t, common.TopUpStatusPending, refreshed.Status)
	assert.Equal(t, PaymentProviderStripe, refreshed.PaymentProvider)
	assert.Equal(t, 50, getUserQuotaForEpayTest(t, 705))
}

// TestCompleteEpayTopUp_NonPendingStatusRejected verifies that failed/expired
// orders cannot be completed and return ErrTopUpStatusInvalid with no quota
// change.
func TestCompleteEpayTopUp_NonPendingStatusRejected(t *testing.T) {
	truncateTables(t)
	insertEpayUserForTest(t, 706, 0)
	insertEpayTopUpForTest(t, "epay-failed", 706, "alipay", 9.99, 2, common.TopUpStatusFailed)
	insertEpayTopUpForTest(t, "epay-expired", 706, "alipay", 9.99, 2, common.TopUpStatusExpired)

	for _, tradeNo := range []string{"epay-failed", "epay-expired"} {
		_, err := CompleteEpayTopUp(tradeNo, "alipay", "9.99", "127.0.0.1")
		require.ErrorIs(t, err, ErrTopUpStatusInvalid, "tradeNo=%s", tradeNo)
		assert.Equal(t, 0, getUserQuotaForEpayTest(t, 706))
	}
}

// TestCompleteEpayTopUp_MissingUserRollsBack verifies that when the user row
// does not exist, the quota Update affects 0 rows, the transaction rolls back,
// and the order remains pending (no success status, no quota).
func TestCompleteEpayTopUp_MissingUserRollsBack(t *testing.T) {
	truncateTables(t)
	// No user created.
	insertEpayTopUpForTest(t, "epay-missing-user", 999, "alipay", 9.99, 2, common.TopUpStatusPending)

	_, err := CompleteEpayTopUp("epay-missing-user", "alipay", "9.99", "127.0.0.1")
	require.Error(t, err)

	topUp := getTopUpForEpayTest(t, "epay-missing-user")
	assert.Equal(t, common.TopUpStatusPending, topUp.Status, "order must stay pending when user is missing")
	assert.Zero(t, topUp.CompleteTime)
}

// TestCompleteEpayTopUp_NotFound verifies a missing trade_no returns
// ErrTopUpNotFound.
func TestCompleteEpayTopUp_NotFound(t *testing.T) {
	truncateTables(t)
	insertEpayUserForTest(t, 707, 0)

	_, err := CompleteEpayTopUp("epay-no-such-trade", "alipay", "9.99", "127.0.0.1")
	require.ErrorIs(t, err, ErrTopUpNotFound)
}

// TestCompleteEpayTopUp_EmptyTradeNoRejected verifies an empty trade number is
// rejected before touching the DB.
func TestCompleteEpayTopUp_EmptyTradeNoRejected(t *testing.T) {
	truncateTables(t)
	_, err := CompleteEpayTopUp("", "alipay", "9.99", "127.0.0.1")
	require.Error(t, err)
}

// TestCompleteEpayTopUp_MoneyFloatDriftTolerant verifies that a paid-money
// string with extra trailing precision beyond 2 cents still matches when the
// cent value is identical (decimal equality, not string equality).
func TestCompleteEpayTopUp_MoneyFloatDriftTolerant(t *testing.T) {
	truncateTables(t)
	insertEpayUserForTest(t, 708, 0)
	insertEpayTopUpForTest(t, "epay-drift", 708, "alipay", 9.99, 1, common.TopUpStatusPending)

	// "9.9900" decodes to the same decimal as "9.99"; must match.
	completion, err := CompleteEpayTopUp("epay-drift", "alipay", "9.9900", "127.0.0.1")
	require.NoError(t, err)
	require.True(t, completion.Completed)
	assert.Equal(t, common.TopUpStatusSuccess, getTopUpForEpayTest(t, "epay-drift").Status)
}

// TestCompleteEpayTopUp_DuplicateSuccessWrongMethodRejected verifies that a
// duplicate success notify carrying a payment method that differs from the
// stored method is rejected with ErrPaymentMethodMismatch (not silently ack'd
// as an idempotent duplicate) and does not change quota.
func TestCompleteEpayTopUp_DuplicateSuccessWrongMethodRejected(t *testing.T) {
	truncateTables(t)
	insertEpayUserForTest(t, 711, 0)
	insertEpayTopUpForTest(t, "epay-dup-method", 711, "alipay", 9.99, 2, common.TopUpStatusPending)

	first, err := CompleteEpayTopUp("epay-dup-method", "alipay", "9.99", "127.0.0.1")
	require.NoError(t, err)
	require.True(t, first.Completed)
	expectedQuota := int(float64(2) * common.QuotaPerUnit)
	assert.Equal(t, expectedQuota, getUserQuotaForEpayTest(t, 711))

	// Duplicate notify claims wxpay while the stored method is alipay.
	_, err = CompleteEpayTopUp("epay-dup-method", "wxpay", "9.99", "127.0.0.1")
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)

	// Quota must not change on the rejected duplicate.
	assert.Equal(t, expectedQuota, getUserQuotaForEpayTest(t, 711))
	refreshed := getTopUpForEpayTest(t, "epay-dup-method")
	assert.Equal(t, "alipay", refreshed.PaymentMethod, "stored method must not be rewritten")
	assert.Equal(t, common.TopUpStatusSuccess, refreshed.Status)
}

// TestCompleteEpayTopUp_DuplicateSuccessWrongMoneyRejected verifies that a
// duplicate success notify whose paid money differs from the stored money is
// rejected with ErrPaidMoneyMismatch and does not change quota.
func TestCompleteEpayTopUp_DuplicateSuccessWrongMoneyRejected(t *testing.T) {
	truncateTables(t)
	insertEpayUserForTest(t, 712, 0)
	insertEpayTopUpForTest(t, "epay-dup-money", 712, "alipay", 9.99, 2, common.TopUpStatusPending)

	first, err := CompleteEpayTopUp("epay-dup-money", "alipay", "9.99", "127.0.0.1")
	require.NoError(t, err)
	require.True(t, first.Completed)
	expectedQuota := int(float64(2) * common.QuotaPerUnit)
	assert.Equal(t, expectedQuota, getUserQuotaForEpayTest(t, 712))

	// Duplicate notify claims a different paid amount than what was stored.
	_, err = CompleteEpayTopUp("epay-dup-money", "alipay", "8.88", "127.0.0.1")
	require.ErrorIs(t, err, ErrPaidMoneyMismatch)

	// Quota must not change on the rejected duplicate.
	assert.Equal(t, expectedQuota, getUserQuotaForEpayTest(t, 712))
	refreshed := getTopUpForEpayTest(t, "epay-dup-money")
	assert.Equal(t, common.TopUpStatusSuccess, refreshed.Status)
}

// TestCompleteEpayTopUp_ConcurrentCompletionsNoDoubleQuota verifies the CAS
// transition is the authoritative concurrency guard: launching many concurrent
// completions (no controller LockOrder) of the same pending order credits the
// user exactly once — one Completed=true winner and the rest Completed=false
// idempotent duplicates. No sleeps or timing assumptions.
//
// The model test DB uses SetMaxOpenConns(1), so transactions serialize at the
// connection layer; the start barrier still forces goroutines to overlap in Go
// scheduling. The outcome is timing-independent regardless: the conditional
// `UPDATE ... WHERE status=pending` can match at most once, and every loser
// resolves as an idempotent duplicate (Completed=false).
func TestCompleteEpayTopUp_ConcurrentCompletionsNoDoubleQuota(t *testing.T) {
	truncateTables(t)
	insertEpayUserForTest(t, 713, 0)
	insertEpayTopUpForTest(t, "epay-concurrent", 713, "alipay", 9.99, 2, common.TopUpStatusPending)

	const goroutines = 16
	type result struct {
		completion *EpayTopUpCompletion
		err        error
	}
	results := make([]result, goroutines)

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			<-start // release all goroutines simultaneously
			completion, err := CompleteEpayTopUp("epay-concurrent", "alipay", "9.99", "127.0.0.1")
			results[idx] = result{completion: completion, err: err}
		}(i)
	}
	close(start)
	wg.Wait()

	expectedQuota := int(float64(2) * common.QuotaPerUnit)
	completedCount := 0
	for i, r := range results {
		require.NoError(t, r.err, "goroutine %d", i)
		require.NotNil(t, r.completion, "goroutine %d", i)
		if r.completion.Completed {
			completedCount++
		} else {
			assert.False(t, r.completion.Completed, "goroutine %d: loser must report Completed=false", i)
		}
	}
	assert.Equal(t, 1, completedCount, "exactly one goroutine must transition pending->success")
	assert.Equal(t, expectedQuota, getUserQuotaForEpayTest(t, 713), "quota must be credited exactly once")

	refreshed := getTopUpForEpayTest(t, "epay-concurrent")
	assert.Equal(t, common.TopUpStatusSuccess, refreshed.Status)
	assert.NotZero(t, refreshed.CompleteTime)
}
