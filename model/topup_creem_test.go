package model

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// insertCreemTopUpForTest creates a pending Creem wallet topup row with the
// given amount (direct product quota), money (paid-money snapshot), and
// (optionally) an overridden status. The user must already exist (or be
// omitted to exercise the missing-user rollback path).
func insertCreemTopUpForTest(t *testing.T, tradeNo string, userID int, amount int64, money float64, status string) {
	t.Helper()
	topUp := &TopUp{
		UserId:          userID,
		Amount:          amount,
		Money:           money,
		TradeNo:         tradeNo,
		PaymentMethod:   PaymentMethodCreem,
		PaymentProvider: PaymentProviderCreem,
		Status:          status,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())
}

func insertCreemUserForTest(t *testing.T, id int, quota int, email string) {
	t.Helper()
	user := &User{
		Id:       id,
		Username: "creem_user",
		Status:   common.UserStatusEnabled,
		Quota:    quota,
		Email:    email,
	}
	require.NoError(t, DB.Create(user).Error)
}

func getUserQuotaForCreemTest(t *testing.T, userID int) int {
	t.Helper()
	var user User
	require.NoError(t, DB.Select("quota").Where("id = ?", userID).First(&user).Error)
	return user.Quota
}

func getUserEmailForCreemTest(t *testing.T, userID int) string {
	t.Helper()
	var user User
	require.NoError(t, DB.Select("email").Where("id = ?", userID).First(&user).Error)
	return user.Email
}

func getTopUpForCreemTest(t *testing.T, tradeNo string) *TopUp {
	t.Helper()
	topUp := GetTopUpByTradeNo(tradeNo)
	require.NotNil(t, topUp)
	return topUp
}

// creemHappyPayload returns a verified-webhook payload that matches the given
// stored snapshot (money -> minor cents, quota, product/customer/currency).
// Centralized so the happy path and the idempotent duplicate reuse the exact
// same values.
func creemHappyPayload(tradeNo string, money float64, quota int64, productId, customerId, currency string) CreemTopUpWebhookPayload {
	expectedMinor := decimal.NewFromFloat(money).Mul(decimal.NewFromInt(100)).Round(0).IntPart()
	return CreemTopUpWebhookPayload{
		TradeNo:         tradeNo,
		CheckoutStatus:  "completed",
		OrderStatus:     "paid",
		OrderType:       "onetime",
		OrderProduct:    productId,
		OrderAmount:     int(expectedMinor),
		OrderCurrency:   currency,
		ProductId:       productId,
		ProductPrice:    int(expectedMinor),
		ProductCurrency: currency,
		CustomerId:      customerId,
		OrderCustomer:   customerId,
		CustomerEmail:   "creem@example.com",
		CustomerName:    "Creem Customer",
		Metadata: map[string]string{
			"product_id":   productId,
			"reference_id": tradeNo,
			"quota":        fmt.Sprintf("%d", quota),
			"price_minor":  fmt.Sprintf("%d", expectedMinor),
			"currency":     currency,
		},
	}
}

// TestCompleteCreemTopUp_HappyPath verifies a correct pending Creem wallet
// topup is transitioned to success, quota is incremented by TopUp.Amount
// directly (not Amount * QuotaPerUnit), the user email is filled when empty,
// and Completed is true.
func TestCompleteCreemTopUp_HappyPath(t *testing.T) {
	truncateTables(t)
	insertCreemUserForTest(t, 901, 0, "")
	// Money snapshot = 9.99 -> minor cents = 999. Amount = 1000 -> quota =
	// 1000 directly (Creem product quota, NOT Amount * QuotaPerUnit).
	insertCreemTopUpForTest(t, "creem-happy", 901, 1000, 9.99, common.TopUpStatusPending)

	payload := creemHappyPayload("creem-happy", 9.99, 1000, "prod_happy_901", "cus_happy_901", "USD")
	completion, err := CompleteCreemTopUp(payload, "127.0.0.1")
	require.NoError(t, err)
	require.NotNil(t, completion)
	assert.True(t, completion.Completed)
	assert.Equal(t, 901, completion.UserId)
	assert.Equal(t, PaymentMethodCreem, completion.PaymentMethod)
	assert.InDelta(t, 9.99, completion.PayMoney, 0.0001)
	assert.Equal(t, "cus_happy_901", completion.CustomerID)
	assert.Equal(t, int64(1000), completion.QuotaToAdd, "quota must equal TopUp.Amount directly")
	assert.Equal(t, 1000, getUserQuotaForCreemTest(t, 901))
	assert.Equal(t, "creem@example.com", getUserEmailForCreemTest(t, 901), "empty email must be filled from webhook")

	topUp := getTopUpForCreemTest(t, "creem-happy")
	assert.Equal(t, common.TopUpStatusSuccess, topUp.Status)
	assert.NotZero(t, topUp.CompleteTime)
	assert.GreaterOrEqual(t, topUp.CompleteTime, topUp.CreateTime)
}

// TestCompleteCreemTopUp_DuplicateSuccessIsIdempotent verifies that a second
// completion on an already-success order with the same verified payload returns
// nil with Completed=false and does NOT re-add quota, rewrite status/CompleteTime,
// or clobber the user email.
func TestCompleteCreemTopUp_DuplicateSuccessIsIdempotent(t *testing.T) {
	truncateTables(t)
	insertCreemUserForTest(t, 902, 0, "existing@example.com")
	insertCreemTopUpForTest(t, "creem-dup", 902, 500, 5.50, common.TopUpStatusPending)

	payload := creemHappyPayload("creem-dup", 5.50, 500, "prod_dup_902", "cus_dup_902", "USD")
	first, err := CompleteCreemTopUp(payload, "127.0.0.1")
	require.NoError(t, err)
	require.True(t, first.Completed)
	assert.Equal(t, 500, getUserQuotaForCreemTest(t, 902))
	assert.Equal(t, "existing@example.com", getUserEmailForCreemTest(t, 902), "existing email must not be overwritten")

	afterFirst := getTopUpForCreemTest(t, "creem-dup")
	afterFirstCompleteTime := afterFirst.CompleteTime

	second, err := CompleteCreemTopUp(payload, "127.0.0.1")
	require.NoError(t, err)
	require.NotNil(t, second)
	assert.False(t, second.Completed)
	assert.Equal(t, 500, getUserQuotaForCreemTest(t, 902), "quota must not double on idempotent dup")
	assert.Equal(t, "existing@example.com", getUserEmailForCreemTest(t, 902), "email must not be clobbered on dup")

	afterSecond := getTopUpForCreemTest(t, "creem-dup")
	assert.Equal(t, common.TopUpStatusSuccess, afterSecond.Status)
	assert.Equal(t, afterFirstCompleteTime, afterSecond.CompleteTime, "CompleteTime must not be rewritten on idempotent dup")
}

// TestCompleteCreemTopUp_AmountMismatchKeepsPending verifies an order.amount
// mismatch (minor cents != snapshot Money*100) leaves the order pending and
// quota untouched.
func TestCompleteCreemTopUp_AmountMismatchKeepsPending(t *testing.T) {
	truncateTables(t)
	insertCreemUserForTest(t, 903, 100, "")
	insertCreemTopUpForTest(t, "creem-amount", 903, 1000, 9.99, common.TopUpStatusPending)

	// Stored Money=9.99 -> expected 999 cents; webhook claims 888.
	payload := creemHappyPayload("creem-amount", 9.99, 1000, "prod_amount_903", "cus_amount_903", "USD")
	payload.OrderAmount = 888
	_, err := CompleteCreemTopUp(payload, "127.0.0.1")
	require.ErrorIs(t, err, ErrPaidMoneyMismatch)

	topUp := getTopUpForCreemTest(t, "creem-amount")
	assert.Equal(t, common.TopUpStatusPending, topUp.Status)
	assert.Zero(t, topUp.CompleteTime)
	assert.Equal(t, 100, getUserQuotaForCreemTest(t, 903))
	assert.Equal(t, "", getUserEmailForCreemTest(t, 903), "email must not be filled on rejected completion")
}

// TestCompleteCreemTopUp_ProductPriceMismatchKeepsPending verifies a
// product.price mismatch (minor cents != snapshot Money*100) leaves the order
// pending, even when order.amount is correct.
func TestCompleteCreemTopUp_ProductPriceMismatchKeepsPending(t *testing.T) {
	truncateTables(t)
	insertCreemUserForTest(t, 904, 50, "")
	insertCreemTopUpForTest(t, "creem-product-price", 904, 1000, 9.99, common.TopUpStatusPending)

	payload := creemHappyPayload("creem-product-price", 9.99, 1000, "prod_pp_904", "cus_pp_904", "USD")
	payload.ProductPrice = 888
	_, err := CompleteCreemTopUp(payload, "127.0.0.1")
	require.ErrorIs(t, err, ErrPaidMoneyMismatch)

	topUp := getTopUpForCreemTest(t, "creem-product-price")
	assert.Equal(t, common.TopUpStatusPending, topUp.Status)
	assert.Equal(t, 50, getUserQuotaForCreemTest(t, 904))
}

// TestCompleteCreemTopUp_CurrencyMismatchKeepsPending verifies a currency
// mismatch between order.currency and product.currency is rejected and the
// order stays pending.
func TestCompleteCreemTopUp_CurrencyMismatchKeepsPending(t *testing.T) {
	truncateTables(t)
	insertCreemUserForTest(t, 905, 50, "")
	insertCreemTopUpForTest(t, "creem-currency", 905, 1000, 9.99, common.TopUpStatusPending)

	payload := creemHappyPayload("creem-currency", 9.99, 1000, "prod_cur_905", "cus_cur_905", "USD")
	payload.ProductCurrency = "EUR"
	_, err := CompleteCreemTopUp(payload, "127.0.0.1")
	require.ErrorIs(t, err, ErrCreemCurrencyMismatch)

	topUp := getTopUpForCreemTest(t, "creem-currency")
	assert.Equal(t, common.TopUpStatusPending, topUp.Status)
	assert.Equal(t, 50, getUserQuotaForCreemTest(t, 905))
}

// TestCompleteCreemTopUp_EmptyCurrencyRejected verifies that an empty currency
// in either order or product is rejected.
func TestCompleteCreemTopUp_EmptyCurrencyRejected(t *testing.T) {
	truncateTables(t)
	insertCreemUserForTest(t, 906, 0, "")
	insertCreemTopUpForTest(t, "creem-empty-cur", 906, 1000, 9.99, common.TopUpStatusPending)

	payload := creemHappyPayload("creem-empty-cur", 9.99, 1000, "prod_ec_906", "cus_ec_906", "USD")
	payload.OrderCurrency = ""
	_, err := CompleteCreemTopUp(payload, "127.0.0.1")
	require.ErrorIs(t, err, ErrCreemCurrencyMismatch)

	topUp := getTopUpForCreemTest(t, "creem-empty-cur")
	assert.Equal(t, common.TopUpStatusPending, topUp.Status)
}

// TestCompleteCreemTopUp_ProductMismatchKeepsPending verifies a product id
// mismatch between order.product and product.id is rejected.
func TestCompleteCreemTopUp_ProductMismatchKeepsPending(t *testing.T) {
	truncateTables(t)
	insertCreemUserForTest(t, 907, 50, "")
	insertCreemTopUpForTest(t, "creem-product", 907, 1000, 9.99, common.TopUpStatusPending)

	payload := creemHappyPayload("creem-product", 9.99, 1000, "prod_a_907", "cus_prod_907", "USD")
	payload.OrderProduct = "prod_b_907"
	_, err := CompleteCreemTopUp(payload, "127.0.0.1")
	require.ErrorIs(t, err, ErrCreemProductMismatch)

	topUp := getTopUpForCreemTest(t, "creem-product")
	assert.Equal(t, common.TopUpStatusPending, topUp.Status)
	assert.Equal(t, 50, getUserQuotaForCreemTest(t, 907))
}

// TestCompleteCreemTopUp_EmptyProductRejected verifies an empty product id is
// rejected.
func TestCompleteCreemTopUp_EmptyProductRejected(t *testing.T) {
	truncateTables(t)
	insertCreemUserForTest(t, 908, 0, "")
	insertCreemTopUpForTest(t, "creem-empty-prod", 908, 1000, 9.99, common.TopUpStatusPending)

	payload := creemHappyPayload("creem-empty-prod", 9.99, 1000, "prod_ep_908", "cus_ep_908", "USD")
	payload.ProductId = ""
	_, err := CompleteCreemTopUp(payload, "127.0.0.1")
	require.ErrorIs(t, err, ErrCreemProductMismatch)

	topUp := getTopUpForCreemTest(t, "creem-empty-prod")
	assert.Equal(t, common.TopUpStatusPending, topUp.Status)
}

// TestCompleteCreemTopUp_ProviderMismatchKeepsPending verifies a non-Creem
// provider topup cannot be claimed by a Creem webhook callback.
func TestCompleteCreemTopUp_ProviderMismatchKeepsPending(t *testing.T) {
	truncateTables(t)
	insertCreemUserForTest(t, 909, 50, "")
	// Stored as a Stripe provider topup; a Creem callback must not claim it.
	topUp := &TopUp{
		UserId:          909,
		Amount:          1000,
		Money:           9.99,
		TradeNo:         "creem-provider",
		PaymentMethod:   PaymentMethodCreem,
		PaymentProvider: PaymentProviderStripe,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())

	payload := creemHappyPayload("creem-provider", 9.99, 1000, "prod_prov_909", "cus_prov_909", "USD")
	_, err := CompleteCreemTopUp(payload, "127.0.0.1")
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)

	refreshed := getTopUpForCreemTest(t, "creem-provider")
	assert.Equal(t, common.TopUpStatusPending, refreshed.Status)
	assert.Equal(t, PaymentProviderStripe, refreshed.PaymentProvider)
	assert.Equal(t, 50, getUserQuotaForCreemTest(t, 909))
}

// TestCompleteCreemTopUp_PaymentMethodMismatchKeepsPending verifies a stored
// payment method that differs from PaymentMethodCreem is rejected.
func TestCompleteCreemTopUp_PaymentMethodMismatchKeepsPending(t *testing.T) {
	truncateTables(t)
	insertCreemUserForTest(t, 910, 50, "")
	// Provider is Creem but method is stripe (mismatched method).
	topUp := &TopUp{
		UserId:          910,
		Amount:          1000,
		Money:           9.99,
		TradeNo:         "creem-method",
		PaymentMethod:   PaymentMethodStripe,
		PaymentProvider: PaymentProviderCreem,
		Status:          common.TopUpStatusPending,
		CreateTime:      time.Now().Unix(),
	}
	require.NoError(t, topUp.Insert())

	payload := creemHappyPayload("creem-method", 9.99, 1000, "prod_pm_910", "cus_pm_910", "USD")
	_, err := CompleteCreemTopUp(payload, "127.0.0.1")
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)

	refreshed := getTopUpForCreemTest(t, "creem-method")
	assert.Equal(t, common.TopUpStatusPending, refreshed.Status)
	assert.Equal(t, PaymentMethodStripe, refreshed.PaymentMethod, "stored method must not be silently rewritten")
	assert.Equal(t, 50, getUserQuotaForCreemTest(t, 910))
}

// TestCompleteCreemTopUp_CustomerMismatchKeepsPending verifies a customer id
// mismatch between order.customer and customer.id is rejected.
func TestCompleteCreemTopUp_CustomerMismatchKeepsPending(t *testing.T) {
	truncateTables(t)
	insertCreemUserForTest(t, 911, 50, "")
	insertCreemTopUpForTest(t, "creem-customer", 911, 1000, 9.99, common.TopUpStatusPending)

	payload := creemHappyPayload("creem-customer", 9.99, 1000, "prod_cus_911", "cus_a_911", "USD")
	payload.OrderCustomer = "cus_b_911"
	_, err := CompleteCreemTopUp(payload, "127.0.0.1")
	require.ErrorIs(t, err, ErrCreemCustomerMismatch)

	topUp := getTopUpForCreemTest(t, "creem-customer")
	assert.Equal(t, common.TopUpStatusPending, topUp.Status)
	assert.Equal(t, 50, getUserQuotaForCreemTest(t, 911))
}

// TestCompleteCreemTopUp_EmptyCustomerRejected verifies a webhook with an
// empty customer is rejected and the order stays pending.
func TestCompleteCreemTopUp_EmptyCustomerRejected(t *testing.T) {
	truncateTables(t)
	insertCreemUserForTest(t, 912, 0, "")
	insertCreemTopUpForTest(t, "creem-empty-customer", 912, 1000, 9.99, common.TopUpStatusPending)

	payload := creemHappyPayload("creem-empty-customer", 9.99, 1000, "prod_ec2_912", "cus_ec2_912", "USD")
	payload.CustomerId = ""
	_, err := CompleteCreemTopUp(payload, "127.0.0.1")
	require.ErrorIs(t, err, ErrCreemCustomerMismatch)

	refreshed := getTopUpForCreemTest(t, "creem-empty-customer")
	assert.Equal(t, common.TopUpStatusPending, refreshed.Status)
	assert.Equal(t, 0, getUserQuotaForCreemTest(t, 912))
}

// TestCompleteCreemTopUp_InvalidStatusOrTypeRejected verifies the model
// enforces checkout status==completed, order status==paid, and order
// type==onetime, leaving the order pending.
func TestCompleteCreemTopUp_InvalidStatusOrTypeRejected(t *testing.T) {
	truncateTables(t)
	insertCreemUserForTest(t, 913, 0, "")
	insertCreemTopUpForTest(t, "creem-status-type", 913, 1000, 9.99, common.TopUpStatusPending)

	cases := []struct {
		name           string
		checkoutStatus string
		orderStatus    string
		orderType      string
	}{
		{name: "incomplete checkout status", checkoutStatus: "open", orderStatus: "paid", orderType: "onetime"},
		{name: "unpaid order status", checkoutStatus: "completed", orderStatus: "pending", orderType: "onetime"},
		{name: "subscription order type", checkoutStatus: "completed", orderStatus: "paid", orderType: "subscription"},
		{name: "empty checkout status", checkoutStatus: "", orderStatus: "paid", orderType: "onetime"},
		{name: "empty order type", checkoutStatus: "completed", orderStatus: "paid", orderType: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := creemHappyPayload("creem-status-type", 9.99, 1000, "prod_st_913", "cus_st_913", "USD")
			payload.CheckoutStatus = tc.checkoutStatus
			payload.OrderStatus = tc.orderStatus
			payload.OrderType = tc.orderType
			_, err := CompleteCreemTopUp(payload, "127.0.0.1")
			require.Error(t, err, "must reject when %s", tc.name)
			refreshed := getTopUpForCreemTest(t, "creem-status-type")
			assert.Equal(t, common.TopUpStatusPending, refreshed.Status)
		})
	}
}

// TestCompleteCreemTopUp_NonPendingStatusRejected verifies that failed/expired
// orders cannot be completed and return ErrTopUpStatusInvalid with no quota
// change. The verified payload still matches so the rejection is purely on
// status (not a mismatch).
func TestCompleteCreemTopUp_NonPendingStatusRejected(t *testing.T) {
	truncateTables(t)
	insertCreemUserForTest(t, 914, 0, "")
	insertCreemTopUpForTest(t, "creem-failed", 914, 1000, 9.99, common.TopUpStatusFailed)
	insertCreemTopUpForTest(t, "creem-expired", 914, 1000, 9.99, common.TopUpStatusExpired)

	for _, tradeNo := range []string{"creem-failed", "creem-expired"} {
		payload := creemHappyPayload(tradeNo, 9.99, 1000, "prod_np_914", "cus_np_914", "USD")
		_, err := CompleteCreemTopUp(payload, "127.0.0.1")
		require.ErrorIs(t, err, ErrTopUpStatusInvalid, "tradeNo=%s", tradeNo)
		assert.Equal(t, 0, getUserQuotaForCreemTest(t, 914))
	}
}

// TestCompleteCreemTopUp_MissingUserRollsBack verifies that when the user row
// does not exist, the quota Update affects 0 rows, the transaction rolls back,
// and the order remains pending (no success status, no quota).
func TestCompleteCreemTopUp_MissingUserRollsBack(t *testing.T) {
	truncateTables(t)
	// No user created.
	insertCreemTopUpForTest(t, "creem-missing-user", 999, 1000, 9.99, common.TopUpStatusPending)

	payload := creemHappyPayload("creem-missing-user", 9.99, 1000, "prod_mu_999", "cus_mu_999", "USD")
	_, err := CompleteCreemTopUp(payload, "127.0.0.1")
	require.Error(t, err)

	topUp := getTopUpForCreemTest(t, "creem-missing-user")
	assert.Equal(t, common.TopUpStatusPending, topUp.Status, "order must stay pending when user is missing")
	assert.Zero(t, topUp.CompleteTime)
}

// TestCompleteCreemTopUp_NotFound verifies a missing trade_no returns
// ErrTopUpNotFound.
func TestCompleteCreemTopUp_NotFound(t *testing.T) {
	truncateTables(t)
	insertCreemUserForTest(t, 916, 0, "")

	payload := creemHappyPayload("creem-no-such-trade", 9.99, 1000, "prod_nf_916", "cus_nf_916", "USD")
	_, err := CompleteCreemTopUp(payload, "127.0.0.1")
	require.ErrorIs(t, err, ErrTopUpNotFound)
}

// TestCompleteCreemTopUp_EmptyTradeNoRejected verifies an empty trade number is
// rejected before touching the DB.
func TestCompleteCreemTopUp_EmptyTradeNoRejected(t *testing.T) {
	truncateTables(t)
	payload := creemHappyPayload("", 9.99, 1000, "prod_et", "cus_et", "USD")
	_, err := CompleteCreemTopUp(payload, "127.0.0.1")
	require.Error(t, err)
}

// TestCompleteCreemTopUp_ConcurrentCompletionsNoDoubleQuota verifies the CAS
// transition is the authoritative concurrency guard: launching many concurrent
// completions (no controller LockOrder) of the same pending order credits the
// user exactly once — one Completed=true winner and the rest Completed=false
// idempotent duplicates. No sleeps or timing assumptions.
func TestCompleteCreemTopUp_ConcurrentCompletionsNoDoubleQuota(t *testing.T) {
	truncateTables(t)
	insertCreemUserForTest(t, 917, 0, "")
	insertCreemTopUpForTest(t, "creem-concurrent", 917, 1000, 9.99, common.TopUpStatusPending)

	const goroutines = 16
	type result struct {
		completion *CreemTopUpCompletion
		err        error
	}
	results := make([]result, goroutines)

	payload := creemHappyPayload("creem-concurrent", 9.99, 1000, "prod_conc_917", "cus_conc_917", "USD")
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			<-start // release all goroutines simultaneously
			completion, err := CompleteCreemTopUp(payload, "127.0.0.1")
			results[idx] = result{completion: completion, err: err}
		}(i)
	}
	close(start)
	wg.Wait()

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
	assert.Equal(t, 1000, getUserQuotaForCreemTest(t, 917), "quota must be credited exactly once")

	refreshed := getTopUpForCreemTest(t, "creem-concurrent")
	assert.Equal(t, common.TopUpStatusSuccess, refreshed.Status)
	assert.NotZero(t, refreshed.CompleteTime)
}

// TestCompleteCreemTopUp_DuplicateSuccessWrongAmountRejected verifies that a
// duplicate success notify whose amount differs from the stored snapshot is
// rejected with ErrPaidMoneyMismatch and does not change quota.
func TestCompleteCreemTopUp_DuplicateSuccessWrongAmountRejected(t *testing.T) {
	truncateTables(t)
	insertCreemUserForTest(t, 918, 0, "")
	insertCreemTopUpForTest(t, "creem-dup-amount", 918, 1000, 9.99, common.TopUpStatusPending)

	payload := creemHappyPayload("creem-dup-amount", 9.99, 1000, "prod_da_918", "cus_da_918", "USD")
	first, err := CompleteCreemTopUp(payload, "127.0.0.1")
	require.NoError(t, err)
	require.True(t, first.Completed)
	assert.Equal(t, 1000, getUserQuotaForCreemTest(t, 918))

	// Duplicate notify claims a different amount than the snapshot.
	payload.OrderAmount = 888
	_, err = CompleteCreemTopUp(payload, "127.0.0.1")
	require.ErrorIs(t, err, ErrPaidMoneyMismatch)
	assert.Equal(t, 1000, getUserQuotaForCreemTest(t, 918), "quota must not change on rejected duplicate")
	refreshed := getTopUpForCreemTest(t, "creem-dup-amount")
	assert.Equal(t, common.TopUpStatusSuccess, refreshed.Status)
}

// TestCompleteCreemTopUp_NonPendingIgnoresPayloadMismatch verifies that for a
// failed/expired order the status rejection (ErrTopUpStatusInvalid) takes
// precedence over payload validation — a mismatched payload on a non-pending
// order does NOT surface as a mismatch error, mirroring CompleteEpayTopUp /
// CompleteStripeTopUp.
func TestCompleteCreemTopUp_NonPendingIgnoresPayloadMismatch(t *testing.T) {
	truncateTables(t)
	insertCreemUserForTest(t, 919, 0, "")
	insertCreemTopUpForTest(t, "creem-failed-mismatch", 919, 1000, 9.99, common.TopUpStatusFailed)

	// Stored Money=9.99 -> expected 999; webhook claims 888 AND a wrong
	// customer/currency/product. The status rejection must win (not the
	// mismatch errors).
	payload := creemHappyPayload("creem-failed-mismatch", 9.99, 1000, "prod_fm_919", "cus_fm_919", "USD")
	payload.OrderAmount = 888
	payload.ProductCurrency = "EUR"
	payload.OrderProduct = "prod_other"
	payload.CustomerId = "cus_other"
	payload.OrderCustomer = "cus_other"
	payload.CheckoutStatus = "open"
	payload.OrderStatus = "unpaid"
	payload.OrderType = "subscription"
	_, err := CompleteCreemTopUp(payload, "127.0.0.1")
	require.ErrorIs(t, err, ErrTopUpStatusInvalid)

	refreshed := getTopUpForCreemTest(t, "creem-failed-mismatch")
	assert.Equal(t, common.TopUpStatusFailed, refreshed.Status)
	assert.Equal(t, 0, getUserQuotaForCreemTest(t, 919))
}

// TestCompleteCreemTopUp_MetadataProductIdMismatchRejected verifies the
// metadata product_id cross-check: when present, it must equal the webhook
// product.id.
func TestCompleteCreemTopUp_MetadataProductIdMismatchRejected(t *testing.T) {
	truncateTables(t)
	insertCreemUserForTest(t, 920, 0, "")
	insertCreemTopUpForTest(t, "creem-meta-prod", 920, 1000, 9.99, common.TopUpStatusPending)

	payload := creemHappyPayload("creem-meta-prod", 9.99, 1000, "prod_mp_920", "cus_mp_920", "USD")
	payload.Metadata["product_id"] = "prod_other"
	_, err := CompleteCreemTopUp(payload, "127.0.0.1")
	require.ErrorIs(t, err, ErrCreemProductMismatch)

	refreshed := getTopUpForCreemTest(t, "creem-meta-prod")
	assert.Equal(t, common.TopUpStatusPending, refreshed.Status)
}

// TestCompleteCreemTopUp_MetadataReferenceIdMismatchRejected verifies the
// metadata reference_id cross-check: when present, it must equal the tradeNo.
func TestCompleteCreemTopUp_MetadataReferenceIdMismatchRejected(t *testing.T) {
	truncateTables(t)
	insertCreemUserForTest(t, 921, 0, "")
	insertCreemTopUpForTest(t, "creem-meta-ref", 921, 1000, 9.99, common.TopUpStatusPending)

	payload := creemHappyPayload("creem-meta-ref", 9.99, 1000, "prod_mr_921", "cus_mr_921", "USD")
	payload.Metadata["reference_id"] = "ref_other"
	_, err := CompleteCreemTopUp(payload, "127.0.0.1")
	require.Error(t, err)

	refreshed := getTopUpForCreemTest(t, "creem-meta-ref")
	assert.Equal(t, common.TopUpStatusPending, refreshed.Status)
}

// TestCompleteCreemTopUp_MetadataQuotaMismatchRejected verifies the metadata
// quota cross-check: when present, it must equal TopUp.Amount.
func TestCompleteCreemTopUp_MetadataQuotaMismatchRejected(t *testing.T) {
	truncateTables(t)
	insertCreemUserForTest(t, 922, 0, "")
	insertCreemTopUpForTest(t, "creem-meta-quota", 922, 1000, 9.99, common.TopUpStatusPending)

	payload := creemHappyPayload("creem-meta-quota", 9.99, 1000, "prod_mq_922", "cus_mq_922", "USD")
	payload.Metadata["quota"] = "999"
	_, err := CompleteCreemTopUp(payload, "127.0.0.1")
	require.Error(t, err)

	refreshed := getTopUpForCreemTest(t, "creem-meta-quota")
	assert.Equal(t, common.TopUpStatusPending, refreshed.Status)
}

// TestCompleteCreemTopUp_MetadataPriceMinorMismatchRejected verifies the
// metadata price_minor cross-check: when present, it must equal the expected
// minor cents.
func TestCompleteCreemTopUp_MetadataPriceMinorMismatchRejected(t *testing.T) {
	truncateTables(t)
	insertCreemUserForTest(t, 923, 0, "")
	insertCreemTopUpForTest(t, "creem-meta-price", 923, 1000, 9.99, common.TopUpStatusPending)

	payload := creemHappyPayload("creem-meta-price", 9.99, 1000, "prod_mpm_923", "cus_mpm_923", "USD")
	payload.Metadata["price_minor"] = "888"
	_, err := CompleteCreemTopUp(payload, "127.0.0.1")
	require.ErrorIs(t, err, ErrPaidMoneyMismatch)

	refreshed := getTopUpForCreemTest(t, "creem-meta-price")
	assert.Equal(t, common.TopUpStatusPending, refreshed.Status)
}

// TestCompleteCreemTopUp_MetadataCurrencyMismatchRejected verifies the
// metadata currency cross-check: when present, it must equal the order/product
// currency.
func TestCompleteCreemTopUp_MetadataCurrencyMismatchRejected(t *testing.T) {
	truncateTables(t)
	insertCreemUserForTest(t, 924, 0, "")
	insertCreemTopUpForTest(t, "creem-meta-currency", 924, 1000, 9.99, common.TopUpStatusPending)

	payload := creemHappyPayload("creem-meta-currency", 9.99, 1000, "prod_mc_924", "cus_mc_924", "USD")
	payload.Metadata["currency"] = "EUR"
	_, err := CompleteCreemTopUp(payload, "127.0.0.1")
	require.ErrorIs(t, err, ErrCreemCurrencyMismatch)

	refreshed := getTopUpForCreemTest(t, "creem-meta-currency")
	assert.Equal(t, common.TopUpStatusPending, refreshed.Status)
}

// TestCompleteCreemTopUp_MetadataAbsentIsAccepted verifies that when the
// webhook carries no metadata at all, the completion still succeeds (metadata
// cross-checks are optional).
func TestCompleteCreemTopUp_MetadataAbsentIsAccepted(t *testing.T) {
	truncateTables(t)
	insertCreemUserForTest(t, 925, 0, "")
	insertCreemTopUpForTest(t, "creem-meta-absent", 925, 1000, 9.99, common.TopUpStatusPending)

	payload := creemHappyPayload("creem-meta-absent", 9.99, 1000, "prod_ma_925", "cus_ma_925", "USD")
	payload.Metadata = nil
	completion, err := CompleteCreemTopUp(payload, "127.0.0.1")
	require.NoError(t, err)
	require.True(t, completion.Completed)
	assert.Equal(t, 1000, getUserQuotaForCreemTest(t, 925))
}

// TestCompleteCreemTopUp_EmailFillOnlyWhenEmpty verifies the webhook email
// fills user.Email only when it is empty, and never overwrites an existing
// email — both on the first completion and on a duplicate.
func TestCompleteCreemTopUp_EmailFillOnlyWhenEmpty(t *testing.T) {
	truncateTables(t)
	insertCreemUserForTest(t, 926, 0, "")
	insertCreemTopUpForTest(t, "creem-email-fill", 926, 1000, 9.99, common.TopUpStatusPending)

	// First completion: user email empty -> filled from webhook.
	payload := creemHappyPayload("creem-email-fill", 9.99, 1000, "prod_ef_926", "cus_ef_926", "USD")
	payload.CustomerEmail = "  webhook@example.com  " // trimmed before storage
	first, err := CompleteCreemTopUp(payload, "127.0.0.1")
	require.NoError(t, err)
	require.True(t, first.Completed)
	assert.Equal(t, "webhook@example.com", getUserEmailForCreemTest(t, 926), "empty email must be filled (trimmed)")

	// Duplicate: user email now set -> must not be overwritten even if webhook
	// claims a different email.
	payload.CustomerEmail = "other@example.com"
	second, err := CompleteCreemTopUp(payload, "127.0.0.1")
	require.NoError(t, err)
	require.False(t, second.Completed)
	assert.Equal(t, "webhook@example.com", getUserEmailForCreemTest(t, 926), "existing email must not be overwritten on dup")
}

// TestCompleteCreemTopUp_EmailNeverOverwritesExisting verifies that on the
// first completion (pending->success) an existing user email is never
// overwritten by the webhook email.
func TestCompleteCreemTopUp_EmailNeverOverwritesExisting(t *testing.T) {
	truncateTables(t)
	insertCreemUserForTest(t, 927, 0, "preset@example.com")
	insertCreemTopUpForTest(t, "creem-email-keep", 927, 1000, 9.99, common.TopUpStatusPending)

	payload := creemHappyPayload("creem-email-keep", 9.99, 1000, "prod_ek_927", "cus_ek_927", "USD")
	payload.CustomerEmail = "webhook@example.com"
	completion, err := CompleteCreemTopUp(payload, "127.0.0.1")
	require.NoError(t, err)
	require.True(t, completion.Completed)
	assert.Equal(t, "preset@example.com", getUserEmailForCreemTest(t, 927), "existing email must not be overwritten on first completion")
}

// TestCompleteCreemTopUp_MoneyFloatDriftTolerant verifies that the minor-cents
// computation tolerates float drift (decimal rounding), so 9.99 -> 999 cents
// even though 9.99 cannot be represented exactly in float64.
func TestCompleteCreemTopUp_MoneyFloatDriftTolerant(t *testing.T) {
	truncateTables(t)
	insertCreemUserForTest(t, 928, 0, "")
	insertCreemTopUpForTest(t, "creem-drift", 928, 1000, 9.99, common.TopUpStatusPending)

	payload := creemHappyPayload("creem-drift", 9.99, 1000, "prod_dr_928", "cus_dr_928", "USD")
	completion, err := CompleteCreemTopUp(payload, "127.0.0.1")
	require.NoError(t, err)
	require.True(t, completion.Completed)
	assert.Equal(t, common.TopUpStatusSuccess, getTopUpForCreemTest(t, "creem-drift").Status)
}

// TestCompleteCreemTopUp_NilMetadataMapIsSafe verifies a nil metadata map does
// not panic and the completion succeeds (the metadata cross-check loop is a
// no-op over a nil map).
func TestCompleteCreemTopUp_NilMetadataMapIsSafe(t *testing.T) {
	truncateTables(t)
	insertCreemUserForTest(t, 929, 0, "")
	insertCreemTopUpForTest(t, "creem-nil-meta", 929, 1000, 9.99, common.TopUpStatusPending)

	payload := creemHappyPayload("creem-nil-meta", 9.99, 1000, "prod_nm_929", "cus_nm_929", "USD")
	payload.Metadata = nil
	completion, err := CompleteCreemTopUp(payload, "127.0.0.1")
	require.NoError(t, err)
	require.True(t, completion.Completed)
}
