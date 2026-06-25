package controller

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/shopspring/decimal"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupCreemTopUpTestDB wires an isolated in-memory SQLite DB for the Creem
// wallet topup controller tests, migrating the tables the completion / creation
// paths touch (mirrors setupStripeTopUpTestDB plus the subscription tables for
// the subscription-first webhook test).
func setupCreemTopUpTestDB(t *testing.T) *gorm.DB {
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

// configureCreemWebhookForTest turns the Creem webhook + topup creation gate on
// via test mode (so signature verification is skipped and no real secret is
// needed) and restores the originals on cleanup.
func configureCreemWebhookForTest(t *testing.T) {
	t.Helper()
	confirmPaymentComplianceForTest(t)
	originalAPIKey := setting.CreemApiKey
	originalProducts := setting.CreemProducts
	originalWebhookSecret := setting.CreemWebhookSecret
	originalTestMode := setting.CreemTestMode
	t.Cleanup(func() {
		setting.CreemApiKey = originalAPIKey
		setting.CreemProducts = originalProducts
		setting.CreemWebhookSecret = originalWebhookSecret
		setting.CreemTestMode = originalTestMode
	})
	setting.CreemApiKey = "creem_api_key"
	setting.CreemWebhookSecret = ""
	setting.CreemTestMode = true
}

// buildCreemWebhookEvent constructs a checkout.completed CreemWebhookEvent
// whose payload matches the given stored snapshot (money -> minor cents,
// quota, product/customer/currency). The caller can mutate individual fields
// to exercise mismatch / status / type cases.
func buildCreemWebhookEvent(referenceId, productId, customerId, currency string, money float64, quota int64) CreemWebhookEvent {
	expectedMinor := int(decimal.NewFromFloat(money).Mul(decimal.NewFromInt(100)).Round(0).IntPart())
	event := CreemWebhookEvent{
		Id:        "evt_test_" + referenceId,
		EventType: "checkout.completed",
		CreatedAt: time.Now().Unix(),
	}
	event.Object.Id = "checkout_" + referenceId
	event.Object.Object = "checkout"
	event.Object.RequestId = referenceId
	event.Object.Status = "completed"
	event.Object.Order.Id = "order_" + referenceId
	event.Object.Order.Customer = customerId
	event.Object.Order.Product = productId
	event.Object.Order.Amount = expectedMinor
	event.Object.Order.Currency = currency
	event.Object.Order.Status = "paid"
	event.Object.Order.Type = "onetime"
	event.Object.Product.Id = productId
	event.Object.Product.Price = expectedMinor
	event.Object.Product.Currency = currency
	event.Object.Product.Name = "wallet topup"
	event.Object.Customer.Id = customerId
	event.Object.Customer.Email = "creem@example.com"
	event.Object.Customer.Name = "Creem Customer"
	event.Object.Metadata = map[string]string{
		"product_id":   productId,
		"reference_id": referenceId,
		"quota":        fmt.Sprintf("%d", quota),
		"price_minor":  fmt.Sprintf("%d", expectedMinor),
		"currency":     currency,
	}
	return event
}

// callCreemWebhookWithBody dispatches a raw body + signature to CreemWebhook
// and returns the recorder. The caller controls the signature so the same
// helper covers signed, unsigned, and tampered-signature cases.
func callCreemWebhookWithBody(t *testing.T, body []byte, signature string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/creem/webhook", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	if signature != "" {
		ctx.Request.Header.Set(CreemSignatureHeader, signature)
	}
	CreemWebhook(ctx)
	return recorder
}

// signAndCallCreemWebhook marshals the event, signs it with the given secret,
// and dispatches it to CreemWebhook. In test mode the secret is empty and the
// signature is skipped, so any non-empty signature header is accepted.
func signAndCallCreemWebhook(t *testing.T, event CreemWebhookEvent, secret string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := common.Marshal(event)
	require.NoError(t, err)
	signature := generateCreemSignature(string(body), secret)
	if signature == "" {
		signature = "test-mode-sig"
	}
	return callCreemWebhookWithBody(t, body, signature)
}

// TestCreemWebhook_SignedHappyPathCompletesQuota verifies a signed
// checkout.completed webhook for a pending Creem wallet topup completes the
// order, credits TopUp.Amount directly as quota, fills the empty user email,
// and returns 200.
func TestCreemWebhook_SignedHappyPathCompletesQuota(t *testing.T) {
	setupCreemTopUpTestDB(t)
	configureCreemWebhookForTest(t)

	require.True(t, isCreemWebhookEnabled(), "test precondition: Creem webhook gate must be on")

	user := &model.User{Id: 1, Username: "creem_wh_user", Status: common.UserStatusEnabled, Group: "default", Quota: 0, Email: ""}
	require.NoError(t, model.DB.Create(user).Error)
	// Money=9.99 -> minor cents=999. Amount=1000 -> quota=1000 directly.
	topUp := &model.TopUp{
		UserId:          user.Id,
		Amount:          1000,
		Money:           9.99,
		TradeNo:         "creem-wh-happy",
		PaymentMethod:   model.PaymentMethodCreem,
		PaymentProvider: model.PaymentProviderCreem,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}
	require.NoError(t, topUp.Insert())

	event := buildCreemWebhookEvent("creem-wh-happy", "prod_wh_happy", "cus_wh_happy", "USD", 9.99, 1000)
	recorder := signAndCallCreemWebhook(t, event, setting.CreemWebhookSecret)
	assert.Equal(t, http.StatusOK, recorder.Code)

	refreshed := model.GetTopUpByTradeNo("creem-wh-happy")
	require.NotNil(t, refreshed)
	assert.Equal(t, common.TopUpStatusSuccess, refreshed.Status)
	assert.NotZero(t, refreshed.CompleteTime)

	var reloaded model.User
	require.NoError(t, model.DB.Select("quota", "email").Where("id = ?", user.Id).First(&reloaded).Error)
	assert.Equal(t, 1000, reloaded.Quota, "quota must equal TopUp.Amount directly")
	assert.Equal(t, "creem@example.com", reloaded.Email, "empty email must be filled from webhook")
}

// TestCreemWebhook_AmountMismatchLeavesPendingReturns200 verifies a verified
// webhook whose order.amount differs from the snapshot leaves the order
// pending, credits no quota, and still acks 200 (verified mismatch leaves
// pending for reconciliation, mirroring Stripe behavior).
func TestCreemWebhook_AmountMismatchLeavesPendingReturns200(t *testing.T) {
	setupCreemTopUpTestDB(t)
	configureCreemWebhookForTest(t)

	user := &model.User{Id: 2, Username: "creem_wh_amount", Status: common.UserStatusEnabled, Group: "default", Quota: 7, Email: ""}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId:          user.Id,
		Amount:          1000,
		Money:           9.99,
		TradeNo:         "creem-wh-amount",
		PaymentMethod:   model.PaymentMethodCreem,
		PaymentProvider: model.PaymentProviderCreem,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}
	require.NoError(t, topUp.Insert())

	// Stored Money=9.99 -> expected 999; webhook claims 888.
	event := buildCreemWebhookEvent("creem-wh-amount", "prod_wh_amount", "cus_wh_amount", "USD", 9.99, 1000)
	event.Object.Order.Amount = 888
	recorder := signAndCallCreemWebhook(t, event, setting.CreemWebhookSecret)
	assert.Equal(t, http.StatusOK, recorder.Code, "verified mismatch must ack 200")

	refreshed := model.GetTopUpByTradeNo("creem-wh-amount")
	require.NotNil(t, refreshed)
	assert.Equal(t, common.TopUpStatusPending, refreshed.Status, "amount mismatch must leave the order pending")
	assert.Zero(t, refreshed.CompleteTime)

	var reloaded model.User
	require.NoError(t, model.DB.Select("quota", "email").Where("id = ?", user.Id).First(&reloaded).Error)
	assert.Equal(t, 7, reloaded.Quota, "quota must not change on amount mismatch")
	assert.Equal(t, "", reloaded.Email, "email must not be filled on rejected completion")
}

// TestCreemWebhook_SubscriptionCompletesBeforeWalletValidation verifies the
// subscription branch runs first: when a SubscriptionOrder exists for the
// trade_no, CompleteSubscriptionOrder handles it and the wallet topup amount
// validation (CompleteCreemTopUp) is never reached — so a wallet-topup-
// incompatible order.amount does not block subscription fulfillment.
func TestCreemWebhook_SubscriptionCompletesBeforeWalletValidation(t *testing.T) {
	db := setupCreemTopUpTestDB(t)
	configureCreemWebhookForTest(t)

	user := &model.User{Id: 3, Username: "creem_wh_sub", Status: common.UserStatusEnabled, Group: "default", Quota: 0}
	require.NoError(t, db.Create(user).Error)
	plan := &model.SubscriptionPlan{
		Id:            600,
		Title:         "creem-sub-plan",
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
		TradeNo:         "creem-wh-sub",
		PaymentMethod:   model.PaymentMethodCreem,
		PaymentProvider: model.PaymentProviderCreem,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}
	require.NoError(t, order.Insert())

	// order.amount=1 would mismatch any wallet topup snapshot, but the
	// subscription path does not consult order.amount, and the wallet topup
	// branch is skipped because CompleteSubscriptionOrder succeeds.
	event := buildCreemWebhookEvent("creem-wh-sub", "prod_wh_sub", "cus_wh_sub", "USD", 19.99, 1000)
	event.Object.Order.Amount = 1
	recorder := signAndCallCreemWebhook(t, event, setting.CreemWebhookSecret)
	assert.Equal(t, http.StatusOK, recorder.Code)

	refreshed := model.GetSubscriptionOrderByTradeNo("creem-wh-sub")
	require.NotNil(t, refreshed)
	assert.Equal(t, common.TopUpStatusSuccess, refreshed.Status, "subscription must complete even with a wallet-incompatible order.amount")
	assert.NotZero(t, refreshed.CompleteTime)

	var subCount int64
	require.NoError(t, db.Model(&model.UserSubscription{}).Where("user_id = ?", user.Id).Count(&subCount).Error)
	assert.Equal(t, int64(1), subCount, "a UserSubscription must be created")
}

// TestCreemWebhook_SubscriptionUnpaidEventLeavesPendingReturns200 verifies
// the controller-side payment status gate restored in handleCheckoutCompleted:
// a signed checkout.completed webhook whose checkout status is not
// "completed" or whose order status is not "paid" must NOT be handed to
// model.CompleteSubscriptionOrder — CompleteSubscriptionOrder does not
// understand the Creem payload's amount/status semantics, so completing a
// SubscriptionOrder on a verified-but-not-paid event would provision a
// UserSubscription for an unpaid order. The webhook acks 200, the pending
// SubscriptionOrder stays pending, and no UserSubscription is created.
func TestCreemWebhook_SubscriptionUnpaidEventLeavesPendingReturns200(t *testing.T) {
	cases := []struct {
		name           string
		checkoutStatus string
		orderStatus    string
	}{
		{name: "incomplete checkout status", checkoutStatus: "open", orderStatus: "paid"},
		{name: "unpaid order status", checkoutStatus: "completed", orderStatus: "pending"},
		{name: "both incomplete and unpaid", checkoutStatus: "processing", orderStatus: "unpaid"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupCreemTopUpTestDB(t)
			configureCreemWebhookForTest(t)

			userId := 200 + i
			require.NoError(t, db.Create(&model.User{Id: userId, Username: fmt.Sprintf("creem_wh_unpaid_%d", i), Status: common.UserStatusEnabled, Group: "default", Quota: 0}).Error)
			plan := &model.SubscriptionPlan{
				Id:            700 + i,
				Title:         fmt.Sprintf("creem-unpaid-plan-%d", i),
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

			tradeNo := fmt.Sprintf("creem-wh-unpaid-%d", i)
			order := &model.SubscriptionOrder{
				UserId:          userId,
				PlanId:          plan.Id,
				Money:           19.99,
				TradeNo:         tradeNo,
				PaymentMethod:   model.PaymentMethodCreem,
				PaymentProvider: model.PaymentProviderCreem,
				Status:          common.TopUpStatusPending,
				CreateTime:      common.GetTimestamp(),
			}
			require.NoError(t, order.Insert())

			// Signed checkout.completed whose status fields indicate the
			// payment has not settled. The order.amount is intentionally
			// wallet-incompatible to prove the subscription branch is never
			// reached (and neither is the wallet fallback).
			event := buildCreemWebhookEvent(tradeNo, fmt.Sprintf("prod_unpaid_%d", i), fmt.Sprintf("cus_unpaid_%d", i), "USD", 19.99, 1000)
			event.Object.Status = tc.checkoutStatus
			event.Object.Order.Status = tc.orderStatus
			event.Object.Order.Amount = 1
			recorder := signAndCallCreemWebhook(t, event, setting.CreemWebhookSecret)
			assert.Equal(t, http.StatusOK, recorder.Code, "verified-but-not-paid event must ack 200")

			refreshed := model.GetSubscriptionOrderByTradeNo(tradeNo)
			require.NotNil(t, refreshed)
			assert.Equal(t, common.TopUpStatusPending, refreshed.Status, "subscription must stay pending on a non-paid event: %s", tc.name)
			assert.Zero(t, refreshed.CompleteTime, "complete time must not be set on a non-paid event: %s", tc.name)

			var subCount int64
			require.NoError(t, db.Model(&model.UserSubscription{}).Where("user_id = ?", userId).Count(&subCount).Error)
			assert.Equal(t, int64(0), subCount, "no UserSubscription must be created on a non-paid event: %s", tc.name)
		})
	}
}

// TestCreemWebhook_MissingRequestIdReturns400 verifies a verified webhook
// with an empty request_id returns 400 and does not mutate any TopUp.
func TestCreemWebhook_MissingRequestIdReturns400(t *testing.T) {
	setupCreemTopUpTestDB(t)
	configureCreemWebhookForTest(t)

	user := &model.User{Id: 4, Username: "creem_wh_noref", Status: common.UserStatusEnabled, Group: "default", Quota: 3}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId:          user.Id,
		Amount:          1000,
		Money:           9.99,
		TradeNo:         "creem-wh-noref",
		PaymentMethod:   model.PaymentMethodCreem,
		PaymentProvider: model.PaymentProviderCreem,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}
	require.NoError(t, topUp.Insert())

	event := buildCreemWebhookEvent("creem-wh-noref", "prod_wh_noref", "cus_wh_noref", "USD", 9.99, 1000)
	event.Object.RequestId = ""
	recorder := signAndCallCreemWebhook(t, event, setting.CreemWebhookSecret)
	assert.Equal(t, http.StatusBadRequest, recorder.Code, "missing request_id must return 400")

	refreshed := model.GetTopUpByTradeNo("creem-wh-noref")
	require.NotNil(t, refreshed)
	assert.Equal(t, common.TopUpStatusPending, refreshed.Status, "missing request_id must not mutate the order")

	var reloaded model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", user.Id).First(&reloaded).Error)
	assert.Equal(t, 3, reloaded.Quota, "quota must not change on missing request_id")
}

// TestCreemWebhook_InvalidSignatureReturns401 verifies a webhook with a
// signature that does not match the configured secret is rejected with 401
// and does not touch any TopUp. Uses non-test mode with a real secret so the
// HMAC check is actually enforced.
func TestCreemWebhook_InvalidSignatureReturns401(t *testing.T) {
	setupCreemTopUpTestDB(t)
	confirmPaymentComplianceForTest(t)

	originalWebhookSecret := setting.CreemWebhookSecret
	originalTestMode := setting.CreemTestMode
	t.Cleanup(func() {
		setting.CreemWebhookSecret = originalWebhookSecret
		setting.CreemTestMode = originalTestMode
	})
	setting.CreemWebhookSecret = "whsec_real_test_creem"
	setting.CreemTestMode = false

	require.True(t, isCreemWebhookEnabled(), "test precondition: Creem webhook gate must be on with real secret")

	user := &model.User{Id: 5, Username: "creem_wh_badsig", Status: common.UserStatusEnabled, Group: "default", Quota: 5}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId:          user.Id,
		Amount:          1000,
		Money:           9.99,
		TradeNo:         "creem-wh-badsig",
		PaymentMethod:   model.PaymentMethodCreem,
		PaymentProvider: model.PaymentProviderCreem,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}
	require.NoError(t, topUp.Insert())

	event := buildCreemWebhookEvent("creem-wh-badsig", "prod_wh_badsig", "cus_wh_badsig", "USD", 9.99, 1000)
	body, err := common.Marshal(event)
	require.NoError(t, err)

	recorder := callCreemWebhookWithBody(t, body, "tampered-signature-does-not-match")
	assert.Equal(t, http.StatusUnauthorized, recorder.Code, "invalid signature must return 401")

	refreshed := model.GetTopUpByTradeNo("creem-wh-badsig")
	require.NotNil(t, refreshed)
	assert.Equal(t, common.TopUpStatusPending, refreshed.Status, "invalid signature must not mutate the order")

	var reloaded model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", user.Id).First(&reloaded).Error)
	assert.Equal(t, 5, reloaded.Quota, "quota must not change on invalid signature")
}

// TestCreemWebhook_SubscriptionNotFoundWalletTopUpBranch verifies that when
// the trade_no has no SubscriptionOrder, the webhook falls through to the
// wallet topup branch (CompleteCreemTopUp). This complements the subscription-
// first test by proving the fallback path is reached.
func TestCreemWebhook_SubscriptionNotFoundWalletTopUpBranch(t *testing.T) {
	setupCreemTopUpTestDB(t)
	configureCreemWebhookForTest(t)

	user := &model.User{Id: 6, Username: "creem_wh_fallback", Status: common.UserStatusEnabled, Group: "default", Quota: 0}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId:          user.Id,
		Amount:          2000,
		Money:           5.00,
		TradeNo:         "creem-wh-fallback",
		PaymentMethod:   model.PaymentMethodCreem,
		PaymentProvider: model.PaymentProviderCreem,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}
	require.NoError(t, topUp.Insert())

	// No SubscriptionOrder exists for this trade_no, so the webhook falls
	// through to CompleteCreemTopUp and credits quota.
	event := buildCreemWebhookEvent("creem-wh-fallback", "prod_wh_fallback", "cus_wh_fallback", "USD", 5.00, 2000)
	recorder := signAndCallCreemWebhook(t, event, setting.CreemWebhookSecret)
	assert.Equal(t, http.StatusOK, recorder.Code)

	refreshed := model.GetTopUpByTradeNo("creem-wh-fallback")
	require.NotNil(t, refreshed)
	assert.Equal(t, common.TopUpStatusSuccess, refreshed.Status)

	var reloaded model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", user.Id).First(&reloaded).Error)
	assert.Equal(t, 2000, reloaded.Quota, "quota must equal TopUp.Amount directly")
}

// TestRequestCreemPay_SnapshotAndMetadata verifies the RequestCreemPay handler
// captures the correct TopUp snapshot (Amount=Quota, Money=Price, provider/
// method=creem, trimmed product id/currency) and that the genCreemLink metadata
// contains the product_id / currency / price_minor cross-check fields the
// webhook will validate. genCreemLink is stubbed so no network call is made.
func TestRequestCreemPay_SnapshotAndMetadata(t *testing.T) {
	setupCreemTopUpTestDB(t)
	configureCreemWebhookForTest(t)

	// Products with whitespace around the product id and currency to prove the
	// snapshot trims them before insert / genCreemLink.
	originalProducts := setting.CreemProducts
	t.Cleanup(func() { setting.CreemProducts = originalProducts })
	setting.CreemProducts = `[{"productId":"  prod_creem_topup  ","name":"wallet topup","price":9.99,"currency":"  USD  ","quota":1000}]`

	require.True(t, isCreemTopUpEnabled(), "test precondition: Creem topup creation gate must be on")

	require.NoError(t, model.DB.Create(&model.User{Id: 10, Username: "creem-pay-user", Status: common.UserStatusEnabled, Group: "default"}).Error)

	var capturedRef string
	var capturedProduct *CreemProduct
	originalGenCreemLink := genCreemLink
	t.Cleanup(func() { genCreemLink = originalGenCreemLink })
	genCreemLink = func(_ context.Context, referenceId string, product *CreemProduct, _, _ string) (string, error) {
		capturedRef = referenceId
		capturedProduct = product
		return "https://checkout.creem.io/test-session", nil
	}

	payload, err := common.Marshal(CreemPayRequest{
		ProductId:     "  prod_creem_topup  ",
		PaymentMethod: model.PaymentMethodCreem,
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(payload))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("id", 10)
	RequestCreemPay(ctx)

	var response struct {
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "success", response.Message, "RequestCreemPay must succeed with a valid product")

	require.NotNil(t, capturedProduct, "genCreemLink must be called")
	assert.Equal(t, "prod_creem_topup", capturedProduct.ProductId, "product id must be trimmed")
	assert.Equal(t, "USD", capturedProduct.Currency, "currency must be trimmed")

	var topUp model.TopUp
	require.NoError(t, model.DB.Where("user_id = ?", 10).First(&topUp).Error)
	assert.Equal(t, int64(1000), topUp.Amount, "TopUp.Amount must equal product Quota")
	assert.InDelta(t, 9.99, topUp.Money, 0.0001, "TopUp.Money must equal product Price")
	assert.Equal(t, model.PaymentMethodCreem, topUp.PaymentMethod)
	assert.Equal(t, model.PaymentProviderCreem, topUp.PaymentProvider)
	assert.Equal(t, common.TopUpStatusPending, topUp.Status)
	assert.Equal(t, capturedRef, topUp.TradeNo)

	// Verify the metadata contents the webhook will cross-check, built from
	// the same captured product + reference id.
	metadata := creemCheckoutMetadata(capturedRef, capturedProduct, "creem-pay-user")
	assert.Equal(t, "prod_creem_topup", metadata["product_id"], "metadata product_id must match trimmed product id")
	assert.Equal(t, "USD", metadata["currency"], "metadata currency must match trimmed currency")
	assert.Equal(t, capturedRef, metadata["reference_id"], "metadata reference_id must match the trade no")
	assert.Equal(t, "1000", metadata["quota"], "metadata quota must match product Quota")
	expectedPriceMinor := decimal.NewFromFloat(9.99).Mul(decimal.NewFromInt(100)).Round(0).String()
	assert.Equal(t, expectedPriceMinor, metadata["price_minor"], "metadata price_minor must be round(Price*100)")
}

// TestRequestCreemPay_RejectsInvalidProductBeforeInsert verifies a product
// with a non-positive quota / price / empty currency / empty product id is
// rejected before the pending TopUp is inserted (no stranded pending order).
func TestRequestCreemPay_RejectsInvalidProductBeforeInsert(t *testing.T) {
	setupCreemTopUpTestDB(t)
	configureCreemWebhookForTest(t)

	originalProducts := setting.CreemProducts
	t.Cleanup(func() { setting.CreemProducts = originalProducts })

	cases := []struct {
		name     string
		products string
	}{
		{name: "empty product id", products: `[{"productId":"","name":"topup","price":9.99,"currency":"USD","quota":1000}]`},
		{name: "non-positive quota", products: `[{"productId":"prod_x","name":"topup","price":9.99,"currency":"USD","quota":0}]`},
		{name: "non-positive price", products: `[{"productId":"prod_x","name":"topup","price":0,"currency":"USD","quota":1000}]`},
		{name: "empty currency", products: `[{"productId":"prod_x","name":"topup","price":9.99,"currency":"","quota":1000}]`},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setting.CreemProducts = tc.products
			userID := 110 + i
			require.NoError(t, model.DB.Create(&model.User{Id: userID, Username: "creem-pay-invalid-" + tc.name, AffCode: fmt.Sprintf("aff%d", userID), Status: common.UserStatusEnabled, Group: "default"}).Error)

			originalGenCreemLink := genCreemLink
			t.Cleanup(func() { genCreemLink = originalGenCreemLink })
			genCreemLink = func(context.Context, string, *CreemProduct, string, string) (string, error) {
				t.Fatal("genCreemLink must not be called when the product snapshot is invalid")
				return "", nil
			}

			payload, err := common.Marshal(CreemPayRequest{
				ProductId:     "prod_x",
				PaymentMethod: model.PaymentMethodCreem,
			})
			require.NoError(t, err)

			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(payload))
			ctx.Request.Header.Set("Content-Type", "application/json")
			ctx.Set("id", userID)
			RequestCreemPay(ctx)

			var response struct {
				Message string `json:"message"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.NotEqual(t, "success", response.Message, "invalid product must be rejected: %s", tc.name)

			var count int64
			require.NoError(t, model.DB.Model(&model.TopUp{}).Where("user_id = ?", userID).Count(&count).Error)
			assert.Zero(t, count, "no pending TopUp must be inserted when the product is invalid: %s", tc.name)
		})
	}
}
