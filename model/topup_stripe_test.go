package model

import (
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// insertStripeTopUpForTest creates a pending Stripe wallet topup row with the
// given amount (recharge units), money (USD snapshot), and (optionally) an
// overridden status. The user must already exist (or be omitted to exercise the
// missing-user rollback path).
func insertStripeTopUpForTest(t *testing.T, tradeNo string, userID int, amount int64, money float64, status string) {
	t.Helper()
	topUp := &TopUp{
		UserId:          userID,
		Amount:          amount,
		Money:           money,
		TradeNo:         tradeNo,
		PaymentMethod:   PaymentMethodStripe,
		PaymentProvider: PaymentProviderStripe,
		Status:          status,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())
}

func insertStripeUserForTest(t *testing.T, id int, quota int, stripeCustomer string) {
	t.Helper()
	user := &User{
		Id:             id,
		Username:       "stripe_user",
		Status:         common.UserStatusEnabled,
		Quota:          quota,
		StripeCustomer: stripeCustomer,
	}
	require.NoError(t, DB.Create(user).Error)
}

func getUserQuotaForStripeTest(t *testing.T, userID int) int {
	t.Helper()
	var user User
	require.NoError(t, DB.Select("quota").Where("id = ?", userID).First(&user).Error)
	return user.Quota
}

func getStripeCustomerForTest(t *testing.T, userID int) string {
	t.Helper()
	var user User
	require.NoError(t, DB.Select("stripe_customer").Where("id = ?", userID).First(&user).Error)
	return user.StripeCustomer
}

func getTopUpForStripeTest(t *testing.T, tradeNo string) *TopUp {
	t.Helper()
	topUp := GetTopUpByTradeNo(tradeNo)
	require.NotNil(t, topUp)
	return topUp
}

// stripeHappyCallbackArgs returns the verified-webhook field set that matches
// the given stored snapshot (money USD -> cents, customer). Centralized so the
// happy path and the idempotent duplicate reuse the exact same values.
func stripeHappyCallbackArgs(money float64, customer string) (customerID, amountTotal, currency, checkoutStatus, paymentStatus, mode string) {
	return customer, centsString(money), "USD", "complete", "paid", "payment"
}

// centsString formats a USD money value as Stripe minor-units (cents) string.
func centsString(money float64) string {
	// money * 100 rounded to cents, integer-formatted (Stripe sends cents).
	d := decimal.NewFromFloat(money).Mul(decimal.NewFromInt(100)).Round(0)
	return d.String()
}

// TestCompleteStripeTopUp_HappyPath verifies a correct pending Stripe wallet
// topup is transitioned to success, quota is incremented from Amount (not
// Money), the stripe_customer is set on first completion, and Completed is
// true.
func TestCompleteStripeTopUp_HappyPath(t *testing.T) {
	truncateTables(t)
	insertStripeUserForTest(t, 801, 0, "")
	// Money snapshot = 9.99 USD -> amount_total cents = 999. Amount = 2 ->
	// quota = 2 * QuotaPerUnit (NOT Money * QuotaPerUnit).
	insertStripeTopUpForTest(t, "stripe-happy", 801, 2, 9.99, common.TopUpStatusPending)

	customerID, amountTotal, currency, status, paymentStatus, mode := stripeHappyCallbackArgs(9.99, "cus_happy_801")
	completion, err := CompleteStripeTopUp("stripe-happy", customerID, amountTotal, currency, status, paymentStatus, mode, "127.0.0.1")
	require.NoError(t, err)
	require.NotNil(t, completion)
	assert.True(t, completion.Completed)
	assert.Equal(t, 801, completion.UserId)
	assert.Equal(t, PaymentMethodStripe, completion.PaymentMethod)
	assert.InDelta(t, 9.99, completion.PayMoney, 0.0001)
	assert.Equal(t, "cus_happy_801", completion.CustomerID)

	expectedQuota := int(float64(2) * common.QuotaPerUnit)
	assert.Equal(t, expectedQuota, completion.QuotaToAdd)
	assert.Equal(t, expectedQuota, getUserQuotaForStripeTest(t, 801))
	assert.Equal(t, "cus_happy_801", getStripeCustomerForTest(t, 801))

	topUp := getTopUpForStripeTest(t, "stripe-happy")
	assert.Equal(t, common.TopUpStatusSuccess, topUp.Status)
	assert.NotZero(t, topUp.CompleteTime)
	assert.GreaterOrEqual(t, topUp.CompleteTime, topUp.CreateTime)
}

// TestCompleteStripeTopUp_DuplicateSuccessIsIdempotent verifies that a second
// completion on an already-success order with the same verified payload returns
// nil with Completed=false and does NOT re-add quota, rewrite status/CompleteTime,
// or clobber stripe_customer.
func TestCompleteStripeTopUp_DuplicateSuccessIsIdempotent(t *testing.T) {
	truncateTables(t)
	insertStripeUserForTest(t, 802, 0, "")
	insertStripeTopUpForTest(t, "stripe-dup", 802, 1, 5.50, common.TopUpStatusPending)

	customerID, amountTotal, currency, status, paymentStatus, mode := stripeHappyCallbackArgs(5.50, "cus_dup_802")
	first, err := CompleteStripeTopUp("stripe-dup", customerID, amountTotal, currency, status, paymentStatus, mode, "127.0.0.1")
	require.NoError(t, err)
	require.True(t, first.Completed)
	expectedQuota := int(float64(1) * common.QuotaPerUnit)
	assert.Equal(t, expectedQuota, getUserQuotaForStripeTest(t, 802))
	assert.Equal(t, "cus_dup_802", getStripeCustomerForTest(t, 802))

	afterFirst := getTopUpForStripeTest(t, "stripe-dup")
	afterFirstCompleteTime := afterFirst.CompleteTime

	second, err := CompleteStripeTopUp("stripe-dup", customerID, amountTotal, currency, status, paymentStatus, mode, "127.0.0.1")
	require.NoError(t, err)
	require.NotNil(t, second)
	assert.False(t, second.Completed)
	assert.Equal(t, expectedQuota, getUserQuotaForStripeTest(t, 802), "quota must not double on idempotent dup")
	assert.Equal(t, "cus_dup_802", getStripeCustomerForTest(t, 802), "stripe_customer must not be clobbered on dup")

	afterSecond := getTopUpForStripeTest(t, "stripe-dup")
	assert.Equal(t, common.TopUpStatusSuccess, afterSecond.Status)
	assert.Equal(t, afterFirstCompleteTime, afterSecond.CompleteTime, "CompleteTime must not be rewritten on idempotent dup")
}

// TestCompleteStripeTopUp_AmountMismatchKeepsPending verifies an amount_total
// mismatch (Stripe cents != snapshot Money*100) leaves the order pending and
// quota untouched.
func TestCompleteStripeTopUp_AmountMismatchKeepsPending(t *testing.T) {
	truncateTables(t)
	insertStripeUserForTest(t, 803, 100, "")
	insertStripeTopUpForTest(t, "stripe-amount", 803, 2, 9.99, common.TopUpStatusPending)

	// Stored Money=9.99 -> expected 999 cents; webhook claims 888.
	_, _, currency, status, paymentStatus, mode := stripeHappyCallbackArgs(9.99, "cus_amount_803")
	_, err := CompleteStripeTopUp("stripe-amount", "cus_amount_803", "888", currency, status, paymentStatus, mode, "127.0.0.1")
	require.ErrorIs(t, err, ErrPaidMoneyMismatch)

	topUp := getTopUpForStripeTest(t, "stripe-amount")
	assert.Equal(t, common.TopUpStatusPending, topUp.Status)
	assert.Zero(t, topUp.CompleteTime)
	assert.Equal(t, 100, getUserQuotaForStripeTest(t, 803))
	assert.Equal(t, "", getStripeCustomerForTest(t, 803), "stripe_customer must not be set on rejected completion")
}

// TestCompleteStripeTopUp_CurrencyMismatchKeepsPending verifies a non-USD
// currency is rejected (no multi-currency schema) and the order stays pending.
func TestCompleteStripeTopUp_CurrencyMismatchKeepsPending(t *testing.T) {
	truncateTables(t)
	insertStripeUserForTest(t, 804, 50, "")
	insertStripeTopUpForTest(t, "stripe-currency", 804, 2, 9.99, common.TopUpStatusPending)

	_, amountTotal, _, status, paymentStatus, mode := stripeHappyCallbackArgs(9.99, "cus_currency_804")
	_, err := CompleteStripeTopUp("stripe-currency", "cus_currency_804", amountTotal, "EUR", status, paymentStatus, mode, "127.0.0.1")
	require.ErrorIs(t, err, ErrStripeCurrencyMismatch)

	topUp := getTopUpForStripeTest(t, "stripe-currency")
	assert.Equal(t, common.TopUpStatusPending, topUp.Status)
	assert.Equal(t, 50, getUserQuotaForStripeTest(t, 804))
}

// TestCompleteStripeTopUp_ProviderMismatchKeepsPending verifies a non-Stripe
// provider topup cannot be claimed by a Stripe webhook callback.
func TestCompleteStripeTopUp_ProviderMismatchKeepsPending(t *testing.T) {
	truncateTables(t)
	insertStripeUserForTest(t, 805, 50, "")
	// Stored as an Epay provider topup; a Stripe callback must not claim it.
	topUp := &TopUp{
		UserId:          805,
		Amount:          2,
		Money:           9.99,
		TradeNo:         "stripe-provider",
		PaymentMethod:   "alipay",
		PaymentProvider: PaymentProviderEpay,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())

	_, amountTotal, currency, status, paymentStatus, mode := stripeHappyCallbackArgs(9.99, "cus_provider_805")
	_, err := CompleteStripeTopUp("stripe-provider", "cus_provider_805", amountTotal, currency, status, paymentStatus, mode, "127.0.0.1")
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)

	refreshed := getTopUpForStripeTest(t, "stripe-provider")
	assert.Equal(t, common.TopUpStatusPending, refreshed.Status)
	assert.Equal(t, PaymentProviderEpay, refreshed.PaymentProvider)
	assert.Equal(t, 50, getUserQuotaForStripeTest(t, 805))
}

// TestCompleteStripeTopUp_NonPendingStatusRejected verifies that failed/expired
// orders cannot be completed and return ErrTopUpStatusInvalid with no quota
// change. The verified payload still matches so the rejection is purely on
// status (not a mismatch).
func TestCompleteStripeTopUp_NonPendingStatusRejected(t *testing.T) {
	truncateTables(t)
	insertStripeUserForTest(t, 806, 0, "")
	insertStripeTopUpForTest(t, "stripe-failed", 806, 2, 9.99, common.TopUpStatusFailed)
	insertStripeTopUpForTest(t, "stripe-expired", 806, 2, 9.99, common.TopUpStatusExpired)

	for _, tradeNo := range []string{"stripe-failed", "stripe-expired"} {
		_, amountTotal, currency, status, paymentStatus, mode := stripeHappyCallbackArgs(9.99, "cus_status_806")
		_, err := CompleteStripeTopUp(tradeNo, "cus_status_806", amountTotal, currency, status, paymentStatus, mode, "127.0.0.1")
		require.ErrorIs(t, err, ErrTopUpStatusInvalid, "tradeNo=%s", tradeNo)
		assert.Equal(t, 0, getUserQuotaForStripeTest(t, 806))
	}
}

// TestCompleteStripeTopUp_DuplicateSuccessWrongAmountRejected verifies that a
// duplicate success notify whose amount_total differs from the stored snapshot
// is rejected with ErrPaidMoneyMismatch and does not change quota.
func TestCompleteStripeTopUp_DuplicateSuccessWrongAmountRejected(t *testing.T) {
	truncateTables(t)
	insertStripeUserForTest(t, 812, 0, "")
	insertStripeTopUpForTest(t, "stripe-dup-amount", 812, 2, 9.99, common.TopUpStatusPending)

	customerID, amountTotal, currency, status, paymentStatus, mode := stripeHappyCallbackArgs(9.99, "cus_dup_amount_812")
	first, err := CompleteStripeTopUp("stripe-dup-amount", customerID, amountTotal, currency, status, paymentStatus, mode, "127.0.0.1")
	require.NoError(t, err)
	require.True(t, first.Completed)
	expectedQuota := int(float64(2) * common.QuotaPerUnit)
	assert.Equal(t, expectedQuota, getUserQuotaForStripeTest(t, 812))

	// Duplicate notify claims a different paid amount than the snapshot.
	_, err = CompleteStripeTopUp("stripe-dup-amount", customerID, "888", currency, status, paymentStatus, mode, "127.0.0.1")
	require.ErrorIs(t, err, ErrPaidMoneyMismatch)
	assert.Equal(t, expectedQuota, getUserQuotaForStripeTest(t, 812), "quota must not change on rejected duplicate")
	refreshed := getTopUpForStripeTest(t, "stripe-dup-amount")
	assert.Equal(t, common.TopUpStatusSuccess, refreshed.Status)
}

// TestCompleteStripeTopUp_DuplicateSuccessWrongCustomerRejected verifies that a
// duplicate success notify whose webhook customer differs from the already-set
// stripe_customer is rejected with ErrStripeCustomerMismatch.
func TestCompleteStripeTopUp_DuplicateSuccessWrongCustomerRejected(t *testing.T) {
	truncateTables(t)
	insertStripeUserForTest(t, 813, 0, "")
	insertStripeTopUpForTest(t, "stripe-dup-customer", 813, 2, 9.99, common.TopUpStatusPending)

	customerID, amountTotal, currency, status, paymentStatus, mode := stripeHappyCallbackArgs(9.99, "cus_original_813")
	first, err := CompleteStripeTopUp("stripe-dup-customer", customerID, amountTotal, currency, status, paymentStatus, mode, "127.0.0.1")
	require.NoError(t, err)
	require.True(t, first.Completed)
	assert.Equal(t, "cus_original_813", getStripeCustomerForTest(t, 813))

	// Duplicate notify claims a different customer than the one stored.
	_, err = CompleteStripeTopUp("stripe-dup-customer", "cus_other_813", amountTotal, currency, status, paymentStatus, mode, "127.0.0.1")
	require.ErrorIs(t, err, ErrStripeCustomerMismatch)
	refreshed := getTopUpForStripeTest(t, "stripe-dup-customer")
	assert.Equal(t, common.TopUpStatusSuccess, refreshed.Status)
	assert.Equal(t, "cus_original_813", getStripeCustomerForTest(t, 813), "stored customer must not be rewritten on rejected duplicate")
}

// TestCompleteStripeTopUp_EmptyCustomerRejected verifies a webhook with an
// empty customer is rejected (customer must be present) and the order stays
// pending.
func TestCompleteStripeTopUp_EmptyCustomerRejected(t *testing.T) {
	truncateTables(t)
	insertStripeUserForTest(t, 814, 0, "")
	insertStripeTopUpForTest(t, "stripe-empty-customer", 814, 2, 9.99, common.TopUpStatusPending)

	_, amountTotal, currency, status, paymentStatus, mode := stripeHappyCallbackArgs(9.99, "cus_814")
	_, err := CompleteStripeTopUp("stripe-empty-customer", "", amountTotal, currency, status, paymentStatus, mode, "127.0.0.1")
	require.ErrorIs(t, err, ErrStripeCustomerMismatch)

	refreshed := getTopUpForStripeTest(t, "stripe-empty-customer")
	assert.Equal(t, common.TopUpStatusPending, refreshed.Status)
	assert.Equal(t, 0, getUserQuotaForStripeTest(t, 814))
}

// TestCompleteStripeTopUp_ModeOrStatusInvalidRejected verifies the model
// enforces checkout status==complete, payment_status==paid, and mode==payment
// (the async-payment-succeeded path relies on this), leaving the order
// pending.
func TestCompleteStripeTopUp_ModeOrStatusInvalidRejected(t *testing.T) {
	truncateTables(t)
	insertStripeUserForTest(t, 815, 0, "")
	insertStripeTopUpForTest(t, "stripe-mode", 815, 2, 9.99, common.TopUpStatusPending)

	cases := []struct {
		name           string
		checkoutStatus string
		paymentStatus  string
		mode           string
	}{
		{name: "incomplete checkout status", checkoutStatus: "open", paymentStatus: "paid", mode: "payment"},
		{name: "unpaid", checkoutStatus: "complete", paymentStatus: "unpaid", mode: "payment"},
		{name: "subscription mode", checkoutStatus: "complete", paymentStatus: "paid", mode: "subscription"},
		{name: "empty mode", checkoutStatus: "complete", paymentStatus: "paid", mode: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, amountTotal, currency, _, _, _ := stripeHappyCallbackArgs(9.99, "cus_mode_815")
			_, err := CompleteStripeTopUp("stripe-mode", "cus_mode_815", amountTotal, currency, tc.checkoutStatus, tc.paymentStatus, tc.mode, "127.0.0.1")
			require.Error(t, err, "must reject when %s", tc.name)
			// Order must stay pending across all sub-cases (truncate is per-test
			// only at the top level, so re-assert the status didn't flip).
			refreshed := getTopUpForStripeTest(t, "stripe-mode")
			assert.Equal(t, common.TopUpStatusPending, refreshed.Status)
		})
	}
}

// TestCompleteStripeTopUp_MissingUserRollsBack verifies that when the user row
// does not exist, the quota Update affects 0 rows, the transaction rolls back,
// and the order remains pending (no success status, no quota, no customer set).
func TestCompleteStripeTopUp_MissingUserRollsBack(t *testing.T) {
	truncateTables(t)
	// No user created.
	insertStripeTopUpForTest(t, "stripe-missing-user", 999, 2, 9.99, common.TopUpStatusPending)

	_, amountTotal, currency, status, paymentStatus, mode := stripeHappyCallbackArgs(9.99, "cus_missing_999")
	_, err := CompleteStripeTopUp("stripe-missing-user", "cus_missing_999", amountTotal, currency, status, paymentStatus, mode, "127.0.0.1")
	require.Error(t, err)

	topUp := getTopUpForStripeTest(t, "stripe-missing-user")
	assert.Equal(t, common.TopUpStatusPending, topUp.Status, "order must stay pending when user is missing")
	assert.Zero(t, topUp.CompleteTime)
}

// TestCompleteStripeTopUp_NonPendingIgnoresPayloadMismatch verifies that for a
// failed/expired order the status rejection (ErrTopUpStatusInvalid) takes
// precedence over payload validation — a mismatched payload on a non-pending
// order does NOT surface as a mismatch error, mirroring CompleteEpayTopUp.
func TestCompleteStripeTopUp_NonPendingIgnoresPayloadMismatch(t *testing.T) {
	truncateTables(t)
	insertStripeUserForTest(t, 819, 0, "")
	insertStripeTopUpForTest(t, "stripe-failed-mismatch", 819, 2, 9.99, common.TopUpStatusFailed)

	// Stored Money=9.99 -> expected 999 cents; webhook claims 888 AND a wrong
	// customer. The status rejection must win (not ErrPaidMoneyMismatch /
	// ErrStripeCustomerMismatch).
	_, err := CompleteStripeTopUp("stripe-failed-mismatch", "cus_wrong", "888", "EUR", "open", "unpaid", "subscription", "127.0.0.1")
	require.ErrorIs(t, err, ErrTopUpStatusInvalid)

	refreshed := getTopUpForStripeTest(t, "stripe-failed-mismatch")
	assert.Equal(t, common.TopUpStatusFailed, refreshed.Status)
	assert.Equal(t, 0, getUserQuotaForStripeTest(t, 819))
}

// TestCompleteStripeTopUp_NotFound verifies a missing trade_no returns
// ErrTopUpNotFound.
func TestCompleteStripeTopUp_NotFound(t *testing.T) {
	truncateTables(t)
	insertStripeUserForTest(t, 816, 0, "")

	_, amountTotal, currency, status, paymentStatus, mode := stripeHappyCallbackArgs(9.99, "cus_notfound_816")
	_, err := CompleteStripeTopUp("stripe-no-such-trade", "cus_notfound_816", amountTotal, currency, status, paymentStatus, mode, "127.0.0.1")
	require.ErrorIs(t, err, ErrTopUpNotFound)
}

// TestCompleteStripeTopUp_EmptyTradeNoRejected verifies an empty trade number
// is rejected before touching the DB.
func TestCompleteStripeTopUp_EmptyTradeNoRejected(t *testing.T) {
	truncateTables(t)
	_, err := CompleteStripeTopUp("", "cus_empty", "999", "USD", "complete", "paid", "payment", "127.0.0.1")
	require.Error(t, err)
}

// TestCompleteStripeTopUp_ConcurrentCompletionsNoDoubleQuota verifies the CAS
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
func TestCompleteStripeTopUp_ConcurrentCompletionsNoDoubleQuota(t *testing.T) {
	truncateTables(t)
	insertStripeUserForTest(t, 817, 0, "")
	insertStripeTopUpForTest(t, "stripe-concurrent", 817, 2, 9.99, common.TopUpStatusPending)

	const goroutines = 16
	type result struct {
		completion *StripeTopUpCompletion
		err        error
	}
	results := make([]result, goroutines)

	customerID, amountTotal, currency, status, paymentStatus, mode := stripeHappyCallbackArgs(9.99, "cus_concurrent_817")
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			<-start // release all goroutines simultaneously
			completion, err := CompleteStripeTopUp("stripe-concurrent", customerID, amountTotal, currency, status, paymentStatus, mode, "127.0.0.1")
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
	assert.Equal(t, expectedQuota, getUserQuotaForStripeTest(t, 817), "quota must be credited exactly once")
	assert.Equal(t, "cus_concurrent_817", getStripeCustomerForTest(t, 817), "stripe_customer must be set exactly once")

	refreshed := getTopUpForStripeTest(t, "stripe-concurrent")
	assert.Equal(t, common.TopUpStatusSuccess, refreshed.Status)
	assert.NotZero(t, refreshed.CompleteTime)
}

// TestManualCompleteTopUp_StripeUsesAmountNotMoney verifies the admin manual
// completion now calculates quota from TopUp.Amount (not Money) for Stripe
// orders, matching the new controller snapshot semantics.
func TestManualCompleteTopUp_StripeUsesAmountNotMoney(t *testing.T) {
	truncateTables(t)
	insertStripeUserForTest(t, 818, 0, "")
	// Amount=2, Money=9.99. Old behavior credited Money*QuotaPerUnit; new
	// behavior credits Amount*QuotaPerUnit.
	insertStripeTopUpForTest(t, "stripe-manual", 818, 2, 9.99, common.TopUpStatusPending)

	require.NoError(t, ManualCompleteTopUp("stripe-manual", "127.0.0.1"))

	expectedQuota := int(float64(2) * common.QuotaPerUnit)
	assert.Equal(t, expectedQuota, getUserQuotaForStripeTest(t, 818), "manual completion must use Amount, not Money")
	refreshed := getTopUpForStripeTest(t, "stripe-manual")
	assert.Equal(t, common.TopUpStatusSuccess, refreshed.Status)
}
