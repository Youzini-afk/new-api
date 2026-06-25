package controller

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v81"
	"gorm.io/gorm"
)

// setupStripeTopUpTestDB wires an isolated in-memory SQLite DB for the Stripe
// wallet topup controller tests, migrating the tables the completion / creation
// paths touch.
func setupStripeTopUpTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)

	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainType := common.MainDatabaseType()
	originalLogType := common.LogDatabaseType()
	originalRedisEnabled := common.RedisEnabled

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	model.InitColumnNames()

	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db

	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}, &model.Option{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.UserSubscription{}))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainType, originalLogType)
		common.RedisEnabled = originalRedisEnabled
		model.InitColumnNames()
	})
	return db
}

// configureStripeForTopUpTest turns the Stripe wallet topup creation gate on
// (compliance + sk_ API secret + webhook secret + wallet StripePriceId) and
// restores the originals on cleanup. The unit price / min topup are pinned so
// the quote snapshot is deterministic.
func configureStripeForTopUpTest(t *testing.T) {
	t.Helper()
	confirmPaymentComplianceForTest(t)
	originalAPISecret := setting.StripeApiSecret
	originalWebhookSecret := setting.StripeWebhookSecret
	originalPriceID := setting.StripePriceId
	originalUnitPrice := setting.StripeUnitPrice
	originalMinTopUp := setting.StripeMinTopUp
	originalPromotion := setting.StripePromotionCodesEnabled
	t.Cleanup(func() {
		setting.StripeApiSecret = originalAPISecret
		setting.StripeWebhookSecret = originalWebhookSecret
		setting.StripePriceId = originalPriceID
		setting.StripeUnitPrice = originalUnitPrice
		setting.StripeMinTopUp = originalMinTopUp
		setting.StripePromotionCodesEnabled = originalPromotion
	})
	setting.StripeApiSecret = "sk_test_stripe_topup"
	setting.StripeWebhookSecret = "whsec_test_stripe_topup"
	setting.StripePriceId = "price_test_topup"
	setting.StripeUnitPrice = 8.0
	setting.StripeMinTopUp = 1
	setting.StripePromotionCodesEnabled = false
}

// overrideGenStripeLink replaces genStripeLink with a deterministic stub that
// records the line-item quantity and returns a fake URL, so tests avoid the
// network and can assert on the captured quantity. Restores the real function
// on cleanup.
func overrideGenStripeLink(t *testing.T, captured *int64) {
	t.Helper()
	originalGenStripeLink := genStripeLink
	t.Cleanup(func() { genStripeLink = originalGenStripeLink })
	genStripeLink = func(_ string, _ string, _ string, amount int64, _ string, _ string) (string, error) {
		if captured != nil {
			*captured = amount
		}
		return "https://checkout.stripe.com/test-session", nil
	}
}

// callStripePay builds an authenticated gin context carrying a JSON body and
// dispatches it to RequestStripePay. The caller controls user id and body.
func callStripePay(t *testing.T, userID int, body any) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := common.Marshal(body)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(payload))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("id", userID)
	RequestStripePay(ctx)
	return recorder
}

// decodeStripePayMessage returns the "message" field of the response. The
// Stripe RequestPay handler responds with `{"message":"success",...}` on
// success and `{"message":"error",...}` on failure (there is no `success`
// bool field), so tests assert on the message rather than a boolean.
func decodeStripePayMessage(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var response struct {
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response.Message
}

// stripePaySucceeded returns true when the RequestStripePay response message
// is "success". Wraps decodeStripePayMessage so call sites read naturally as a
// boolean success flag.
func stripePaySucceeded(t *testing.T, recorder *httptest.ResponseRecorder) bool {
	t.Helper()
	return decodeStripePayMessage(t, recorder) == "success"
}

// buildStripeEvent builds a verified stripe.Event for fulfillOrder tests by
// unmarshaling a JSON payload, so event.GetObjectValue works exactly as it
// does for a real webhook.
func buildStripeEvent(t *testing.T, eventType, referenceId, customer, amountTotal, currency, status, paymentStatus, mode string) stripe.Event {
	t.Helper()
	body := fmt.Sprintf(`{
		"type": %q,
		"data": {
			"object": {
				"client_reference_id": %q,
				"customer": %q,
				"amount_total": %s,
				"currency": %q,
				"status": %q,
				"payment_status": %q,
				"mode": %q
			}
		}
	}`, eventType, referenceId, customer, amountTotal, currency, status, paymentStatus, mode)
	var event stripe.Event
	require.NoError(t, common.Unmarshal([]byte(body), &event))
	return event
}

// TestFulfillOrder_AmountMismatchLeavesPending verifies the wallet topup branch
// of fulfillOrder validates the verified event's amount_total against the stored
// snapshot: a mismatch leaves the order pending and quota untouched (no silent
// recharge). The webhook still returns (caller acks 200).
func TestFulfillOrder_AmountMismatchLeavesPending(t *testing.T) {
	setupStripeTopUpTestDB(t)

	user := &model.User{Id: 1, Username: "stripe_fulfill_user", Status: common.UserStatusEnabled, Group: "default", Quota: 7}
	require.NoError(t, model.DB.Create(user).Error)
	// Money snapshot = 9.99 USD -> expected 999 cents. Webhook claims 888.
	topUp := &model.TopUp{
		UserId:          user.Id,
		Amount:          2,
		Money:           9.99,
		TradeNo:         "stripe-fulfill-mismatch",
		PaymentMethod:   model.PaymentMethodStripe,
		PaymentProvider: model.PaymentProviderStripe,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}
	require.NoError(t, topUp.Insert())

	event := buildStripeEvent(t, "checkout.session.completed", "stripe-fulfill-mismatch", "cus_fulfill", "888", "usd", "complete", "paid", "payment")
	fulfillOrder(context.Background(), event, "stripe-fulfill-mismatch", "cus_fulfill", "127.0.0.1")

	refreshed := model.GetTopUpByTradeNo("stripe-fulfill-mismatch")
	require.NotNil(t, refreshed)
	assert.Equal(t, common.TopUpStatusPending, refreshed.Status, "amount mismatch must leave the order pending")
	assert.Zero(t, refreshed.CompleteTime)

	var reloaded model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", user.Id).First(&reloaded).Error)
	assert.Equal(t, 7, reloaded.Quota, "quota must not change on amount mismatch")
	assert.Equal(t, "", reloaded.StripeCustomer, "stripe_customer must not be set on rejected completion")
}

// TestFulfillOrder_SubscriptionNotBlockedByWalletValidation verifies the
// subscription branch runs first: when a SubscriptionOrder exists for the
// trade_no, CompleteSubscriptionOrder handles it and the wallet topup
// amount_total validation (CompleteStripeTopUp) is never reached — so a
// wallet-topup-incompatible amount_total does not block subscription
// fulfillment.
func TestFulfillOrder_SubscriptionNotBlockedByWalletValidation(t *testing.T) {
	db := setupStripeTopUpTestDB(t)

	user := &model.User{Id: 2, Username: "stripe_sub_user", Status: common.UserStatusEnabled, Group: "default", Quota: 0}
	require.NoError(t, db.Create(user).Error)
	plan := &model.SubscriptionPlan{
		Id:            500,
		Title:         "stripe-sub-plan",
		PriceAmount:   19.99,
		Currency:      "USD",
		DurationUnit:  model.SubscriptionDurationMonth,
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
	}
	plan.NormalizeDefaults()
	require.NoError(t, db.Create(plan).Error)
	t.Cleanup(func() { model.InvalidateSubscriptionPlanCache(plan.Id) })

	order := &model.SubscriptionOrder{
		UserId:          user.Id,
		PlanId:          plan.Id,
		Money:           19.99,
		TradeNo:         "stripe-sub-not-blocked",
		PaymentMethod:   model.PaymentMethodStripe,
		PaymentProvider: model.PaymentProviderStripe,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}
	require.NoError(t, order.Insert())

	// amount_total=1 would mismatch any reasonable wallet topup snapshot, but
	// the subscription path does not consult amount_total, and the wallet
	// topup branch is skipped because CompleteSubscriptionOrder succeeds.
	event := buildStripeEvent(t, "checkout.session.completed", "stripe-sub-not-blocked", "cus_sub", "1", "usd", "complete", "paid", "payment")
	fulfillOrder(context.Background(), event, "stripe-sub-not-blocked", "cus_sub", "127.0.0.1")

	refreshed := model.GetSubscriptionOrderByTradeNo("stripe-sub-not-blocked")
	require.NotNil(t, refreshed)
	assert.Equal(t, common.TopUpStatusSuccess, refreshed.Status, "subscription must complete even with a wallet-incompatible amount_total")
	assert.NotZero(t, refreshed.CompleteTime)

	var subCount int64
	require.NoError(t, db.Model(&model.UserSubscription{}).Where("user_id = ?", user.Id).Count(&subCount).Error)
	assert.Equal(t, int64(1), subCount, "a UserSubscription must be created")
}

// TestQuoteStripeTopUp_TokensDisplayRejectsNonMultiple verifies that under
// TOKENS display the requested amount must be an exact multiple of
// QuotaPerUnit, otherwise the quote (and thus order creation) is rejected with
// no pending insert.
func TestQuoteStripeTopUp_TokensDisplayRejectsNonMultiple(t *testing.T) {
	originalDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	t.Cleanup(func() { operation_setting.GetGeneralSetting().QuotaDisplayType = originalDisplayType })
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeTokens

	perUnit := int64(common.QuotaPerUnit)
	require.Greater(t, perUnit, int64(0))

	// Non-multiple: reject.
	_, err := quoteStripeTopUp(perUnit+1, "default")
	require.Error(t, err)

	// Exact multiple: accept and normalize.
	quote, err := quoteStripeTopUp(perUnit*3, "default")
	require.NoError(t, err)
	assert.Equal(t, int64(3), quote.Amount)
	assert.Equal(t, int64(3), quote.CheckoutQuantity)
	assert.InDelta(t, 3*setting.StripeUnitPrice, quote.ExpectedMoney, 0.0001)
}

// TestQuoteStripeTopUp_USDDisplayNoNormalization verifies that under non-TOKENS
// display the requested amount is used directly (no QuotaPerUnit division).
func TestQuoteStripeTopUp_USDDisplayNoNormalization(t *testing.T) {
	originalDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	t.Cleanup(func() { operation_setting.GetGeneralSetting().QuotaDisplayType = originalDisplayType })
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD

	quote, err := quoteStripeTopUp(5, "default")
	require.NoError(t, err)
	assert.Equal(t, int64(5), quote.Amount)
	assert.Equal(t, int64(5), quote.CheckoutQuantity)
	assert.InDelta(t, 5*setting.StripeUnitPrice, quote.ExpectedMoney, 0.0001)
}

// TestRequestStripePay_TokensDisplayNormalizedQuantity verifies the wallet
// topup creation captures the normalized checkout quantity (req.Amount /
// QuotaPerUnit) in the Stripe line item, not the raw token amount.
func TestRequestStripePay_TokensDisplayNormalizedQuantity(t *testing.T) {
	setupStripeTopUpTestDB(t)
	configureStripeForTopUpTest(t)
	originalDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	t.Cleanup(func() { operation_setting.GetGeneralSetting().QuotaDisplayType = originalDisplayType })
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeTokens

	require.True(t, isStripeTopUpEnabled(), "test precondition: Stripe topup gate must be on")

	require.NoError(t, model.DB.Create(&model.User{Id: 3, Username: "stripe_tokens_user", Status: common.UserStatusEnabled, Group: "default"}).Error)

	var capturedQuantity int64
	overrideGenStripeLink(t, &capturedQuantity)

	perUnit := int64(common.QuotaPerUnit)
	recorder := callStripePay(t, 3, StripePayRequest{
		Amount:        perUnit * 4, // 4 USD units
		PaymentMethod: model.PaymentMethodStripe,
	})

	success := stripePaySucceeded(t, recorder)
	require.True(t, success, "tokens-display topup creation must succeed for an exact multiple")
	assert.Equal(t, int64(4), capturedQuantity, "checkout quantity must be the normalized USD amount, not the raw token count")

	var topUp model.TopUp
	require.NoError(t, model.DB.Where("user_id = ?", 3).First(&topUp).Error)
	assert.Equal(t, int64(4), topUp.Amount, "TopUp.Amount must be the normalized recharge amount")
	assert.InDelta(t, 4*setting.StripeUnitPrice, topUp.Money, 0.0001, "TopUp.Money must be the expected paid money snapshot")
	assert.Equal(t, common.TopUpStatusPending, topUp.Status)
}

// TestRequestStripePay_TokensDisplayRejectsNonMultiple verifies a non-multiple
// token amount is rejected before genStripeLink / order insert, leaving no
// pending TopUp.
func TestRequestStripePay_TokensDisplayRejectsNonMultiple(t *testing.T) {
	setupStripeTopUpTestDB(t)
	configureStripeForTopUpTest(t)
	originalDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	t.Cleanup(func() { operation_setting.GetGeneralSetting().QuotaDisplayType = originalDisplayType })
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeTokens

	require.NoError(t, model.DB.Create(&model.User{Id: 4, Username: "stripe_tokens_reject_user", Status: common.UserStatusEnabled, Group: "default"}).Error)

	var capturedQuantity int64
	overrideGenStripeLink(t, &capturedQuantity)

	perUnit := int64(common.QuotaPerUnit)
	recorder := callStripePay(t, 4, StripePayRequest{
		Amount:        perUnit + 1, // non-multiple
		PaymentMethod: model.PaymentMethodStripe,
	})

	success := stripePaySucceeded(t, recorder)
	assert.False(t, success, "non-multiple token amount must be rejected")
	assert.Equal(t, int64(0), capturedQuantity, "genStripeLink must not be called when the quote is rejected")

	var count int64
	require.NoError(t, model.DB.Model(&model.TopUp{}).Where("user_id = ?", 4).Count(&count).Error)
	assert.Zero(t, count, "no pending TopUp must be inserted when the quote is rejected")
}

// TestRequestStripePay_RejectsGroupRatioBeforeInsert verifies a user whose
// topup group ratio is not 1 is rejected before genStripeLink / order insert
// (a fixed PriceId + Quantity cannot express the ratio safely).
func TestRequestStripePay_RejectsGroupRatioBeforeInsert(t *testing.T) {
	setupStripeTopUpTestDB(t)
	configureStripeForTopUpTest(t)

	originalRatio := common.TopupGroupRatio2JSONString()
	t.Cleanup(func() { require.NoError(t, common.UpdateTopupGroupRatioByJSONString(originalRatio)) })
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":1,"vip":1.2}`))

	require.NoError(t, model.DB.Create(&model.User{Id: 5, Username: "stripe_ratio_user", Status: common.UserStatusEnabled, Group: "vip"}).Error)

	var capturedQuantity int64
	overrideGenStripeLink(t, &capturedQuantity)

	recorder := callStripePay(t, 5, StripePayRequest{
		Amount:        2,
		PaymentMethod: model.PaymentMethodStripe,
	})

	success := stripePaySucceeded(t, recorder)
	assert.False(t, success, "group ratio != 1 must reject Stripe topup creation")
	assert.Equal(t, int64(0), capturedQuantity, "genStripeLink must not be called when the ratio is unsupported")

	var count int64
	require.NoError(t, model.DB.Model(&model.TopUp{}).Where("user_id = ?", 5).Count(&count).Error)
	assert.Zero(t, count, "no pending TopUp must be inserted when the ratio is unsupported")
}

// TestRequestStripePay_RejectsAmountDiscountBeforeInsert verifies a preset
// AmountDiscount for the requested amount is rejected before genStripeLink /
// order insert (a fixed PriceId + Quantity cannot express the discount
// safely).
func TestRequestStripePay_RejectsAmountDiscountBeforeInsert(t *testing.T) {
	setupStripeTopUpTestDB(t)
	configureStripeForTopUpTest(t)

	paymentSetting := operation_setting.GetPaymentSetting()
	originalDiscounts := make(map[int]float64, len(paymentSetting.AmountDiscount))
	for k, v := range paymentSetting.AmountDiscount {
		originalDiscounts[k] = v
	}
	t.Cleanup(func() { paymentSetting.AmountDiscount = originalDiscounts })
	paymentSetting.AmountDiscount = map[int]float64{2: 0.9}

	require.NoError(t, model.DB.Create(&model.User{Id: 6, Username: "stripe_discount_user", Status: common.UserStatusEnabled, Group: "default"}).Error)

	var capturedQuantity int64
	overrideGenStripeLink(t, &capturedQuantity)

	recorder := callStripePay(t, 6, StripePayRequest{
		Amount:        2,
		PaymentMethod: model.PaymentMethodStripe,
	})

	success := stripePaySucceeded(t, recorder)
	assert.False(t, success, "configured AmountDiscount must reject Stripe topup creation")
	assert.Equal(t, int64(0), capturedQuantity, "genStripeLink must not be called when a discount is configured")

	var count int64
	require.NoError(t, model.DB.Model(&model.TopUp{}).Where("user_id = ?", 6).Count(&count).Error)
	assert.Zero(t, count, "no pending TopUp must be inserted when a discount is configured")
}

// TestRequestStripePay_PromotionCodesDoNotBreakCreation verifies that when the
// operator has enabled StripePromotionCodesEnabled, wallet topup creation still
// succeeds: the wallet checkout forces AllowPromotionCodes=false (hardcoded in
// genStripeLink) so promotion codes cannot diverge the paid amount from the
// snapshot. The captured checkout quantity proves the flow reached
// genStripeLink with the correct normalized quantity.
func TestRequestStripePay_PromotionCodesDoNotBreakCreation(t *testing.T) {
	setupStripeTopUpTestDB(t)
	configureStripeForTopUpTest(t)
	// Deliberately enable promotion codes; the wallet path must ignore this.
	setting.StripePromotionCodesEnabled = true

	require.NoError(t, model.DB.Create(&model.User{Id: 7, Username: "stripe_promo_user", Status: common.UserStatusEnabled, Group: "default"}).Error)

	var capturedQuantity int64
	overrideGenStripeLink(t, &capturedQuantity)

	recorder := callStripePay(t, 7, StripePayRequest{
		Amount:        3,
		PaymentMethod: model.PaymentMethodStripe,
	})

	success := stripePaySucceeded(t, recorder)
	assert.True(t, success, "promotion-codes setting must not break wallet topup creation (forced false)")
	assert.Equal(t, int64(3), capturedQuantity, "checkout quantity must be the requested amount")

	var topUp model.TopUp
	require.NoError(t, model.DB.Where("user_id = ?", 7).First(&topUp).Error)
	assert.Equal(t, int64(3), topUp.Amount)
	assert.InDelta(t, 3*setting.StripeUnitPrice, topUp.Money, 0.0001)
}

// TestRequestStripePay_HappyPathCreatesPendingSnapshot is the end-to-end happy
// path: USD display, default group, no discount -> pending TopUp inserted with
// Amount/Money/quantity all derived from the quote.
func TestRequestStripePay_HappyPathCreatesPendingSnapshot(t *testing.T) {
	setupStripeTopUpTestDB(t)
	configureStripeForTopUpTest(t)

	require.NoError(t, model.DB.Create(&model.User{Id: 8, Username: "stripe_happy_user", Status: common.UserStatusEnabled, Group: "default"}).Error)

	var capturedQuantity int64
	overrideGenStripeLink(t, &capturedQuantity)

	recorder := callStripePay(t, 8, StripePayRequest{
		Amount:        5,
		PaymentMethod: model.PaymentMethodStripe,
	})

	success := stripePaySucceeded(t, recorder)
	require.True(t, success)
	assert.Equal(t, int64(5), capturedQuantity)

	var topUp model.TopUp
	require.NoError(t, model.DB.Where("user_id = ?", 8).First(&topUp).Error)
	assert.Equal(t, int64(5), topUp.Amount)
	assert.InDelta(t, 5*setting.StripeUnitPrice, topUp.Money, 0.0001)
	assert.Equal(t, model.PaymentMethodStripe, topUp.PaymentMethod)
	assert.Equal(t, model.PaymentProviderStripe, topUp.PaymentProvider)
	assert.Equal(t, common.TopUpStatusPending, topUp.Status)
}

// TestRequestStripePay_ConcurrentCreationsNoDuplicateQuota sanity-checks that
// concurrent RequestPay calls each create their own pending order without
// colliding (the reference id is unique per call). This guards the
// reference-id generation under concurrency.
func TestRequestStripePay_ConcurrentCreationsNoDuplicateTradeNo(t *testing.T) {
	setupStripeTopUpTestDB(t)
	configureStripeForTopUpTest(t)

	require.NoError(t, model.DB.Create(&model.User{Id: 9, Username: "stripe_concurrent_user", Status: common.UserStatusEnabled, Group: "default"}).Error)

	overrideGenStripeLink(t, nil)

	const goroutines = 8
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			recorder := callStripePay(t, 9, StripePayRequest{
				Amount:        1,
				PaymentMethod: model.PaymentMethodStripe,
			})
			success := stripePaySucceeded(t, recorder)
			assert.True(t, success)
		}()
	}
	wg.Wait()

	var count int64
	require.NoError(t, model.DB.Model(&model.TopUp{}).Where("user_id = ?", 9).Count(&count).Error)
	assert.Equal(t, int64(goroutines), count, "each concurrent call must create its own order with a unique trade_no")
}
