package controller

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupSubscriptionGateTestDB wires an isolated in-memory SQLite DB carrying
// the tables the subscription creation paths and the Creem wallet topup path
// touch. The creation-gate tests prove that no pending SubscriptionOrder /
// TopUp is inserted (or left pending) when the gate or a downstream link
// generator fails, so SubscriptionOrder and TopUp are the tables whose counts
// actually matter.
func setupSubscriptionGateTestDB(t *testing.T) *gorm.DB {
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

	require.NoError(t, db.AutoMigrate(&model.User{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.TopUp{}))

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

// callSubscriptionHandler builds an authenticated gin context carrying a JSON
// body and dispatches it to the supplied handler. The caller controls user id
// and body so the same helper covers Epay, Stripe, Creem, and Waffo Pancake
// subscription request flows.
func callSubscriptionHandler(t *testing.T, handler gin.HandlerFunc, userID int, body any) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := common.Marshal(body)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(payload))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("id", userID)
	handler(ctx)
	return recorder
}

// decodeGateResponse returns the success flag + message from an ApiError*
// response so tests can assert the gate fired with the expected error.
func decodeGateResponse(t *testing.T, recorder *httptest.ResponseRecorder) (bool, string) {
	t.Helper()
	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response.Success, response.Message
}

// TestSubscriptionRequestEpay_NoOrderWhenWebhookGateFails proves the Epay
// subscription creation gate refuses before the pending SubscriptionOrder is
// inserted: with compliance confirmed but the Epay verification config
// missing (isEpayWebhookEnabled false), the handler returns an error and the
// SubscriptionOrder table stays empty.
func TestSubscriptionRequestEpay_NoOrderWhenWebhookGateFails(t *testing.T) {
	setupSubscriptionGateTestDB(t)
	confirmPaymentComplianceForTest(t)

	originalPayAddress := operation_setting.PayAddress
	originalEpayID := operation_setting.EpayId
	originalEpayKey := operation_setting.EpayKey
	originalPayMethods := operation_setting.PayMethods
	t.Cleanup(func() {
		operation_setting.PayAddress = originalPayAddress
		operation_setting.EpayId = originalEpayID
		operation_setting.EpayKey = originalEpayKey
		operation_setting.PayMethods = originalPayMethods
	})
	operation_setting.PayAddress = ""
	operation_setting.EpayId = ""
	operation_setting.EpayKey = ""
	operation_setting.PayMethods = nil

	require.False(t, isEpayWebhookEnabled(), "test precondition: Epay webhook gate must be off")

	recorder := callSubscriptionHandler(t, SubscriptionRequestEpay, 1, SubscriptionEpayPayRequest{
		PlanId:        1,
		PaymentMethod: "alipay",
	})

	success, _ := decodeGateResponse(t, recorder)
	assert.False(t, success, "gate failure must surface as a non-success response")

	var orderCount int64
	require.NoError(t, model.DB.Model(&model.SubscriptionOrder{}).Count(&orderCount).Error)
	assert.Zero(t, orderCount, "no pending SubscriptionOrder must be inserted when the Epay webhook gate fails")
}

// TestSubscriptionRequestCreemPay_NoOrderWhenApiKeyMissing proves the Creem
// subscription creation gate checks CreemApiKey BEFORE inserting the pending
// SubscriptionOrder: with compliance confirmed, a plan carrying a
// CreemProductId, and webhook configured (test mode), a missing API key must
// fail early and leave the SubscriptionOrder table empty.
//
// This is the risky path called out in Phase 8B: previously genCreemLink was
// the only place that checked the API key, which meant the pending order was
// already written by the time creation failed.
func TestSubscriptionRequestCreemPay_NoOrderWhenApiKeyMissing(t *testing.T) {
	db := setupSubscriptionGateTestDB(t)
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
	// Webhook gate satisfied via test mode; API key deliberately empty.
	setting.CreemApiKey = ""
	setting.CreemProducts = "[]"
	setting.CreemWebhookSecret = ""
	setting.CreemTestMode = true

	require.True(t, isCreemWebhookEnabled(), "test precondition: Creem webhook gate must be on via test mode")

	plan := &model.SubscriptionPlan{
		Id:             42,
		Title:          "creem-gate-plan",
		PriceAmount:    9.99,
		Currency:       "USD",
		DurationUnit:   "month",
		Enabled:        true,
		CreemProductId: "prod_creem_42",
	}
	plan.NormalizeDefaults()
	require.NoError(t, db.Create(plan).Error)
	t.Cleanup(func() { model.InvalidateSubscriptionPlanCache(plan.Id) })

	recorder := callSubscriptionHandler(t, SubscriptionRequestCreemPay, 1, SubscriptionCreemPayRequest{
		PlanId: plan.Id,
	})

	success, _ := decodeGateResponse(t, recorder)
	assert.False(t, success, "gate failure must surface as a non-success response")

	var orderCount int64
	require.NoError(t, model.DB.Model(&model.SubscriptionOrder{}).Count(&orderCount).Error)
	assert.Zero(t, orderCount, "no pending SubscriptionOrder must be inserted when CreemApiKey is missing")
}

// TestSubscriptionRequestStripe_NoOrderWhenWebhookSecretIsWhitespace proves
// the Stripe subscription creation gate trims StripeWebhookSecret consistently
// with the webhook gate: a whitespace-only secret ("   ") passes the legacy
// `== ""` check but must be rejected here so a freshly-created order can
// actually be fulfilled later. The gate must fire before the pending
// SubscriptionOrder is inserted.
func TestSubscriptionRequestStripe_NoOrderWhenWebhookSecretIsWhitespace(t *testing.T) {
	db := setupSubscriptionGateTestDB(t)
	confirmPaymentComplianceForTest(t)

	originalAPISecret := setting.StripeApiSecret
	originalWebhookSecret := setting.StripeWebhookSecret
	t.Cleanup(func() {
		setting.StripeApiSecret = originalAPISecret
		setting.StripeWebhookSecret = originalWebhookSecret
	})
	setting.StripeApiSecret = "sk_test_123"
	// Whitespace-only: the old `== ""` check would let this through, but the
	// shared webhook gate (TrimSpace) rejects it, so the creation gate must
	// also reject it to avoid stranding a pending order.
	setting.StripeWebhookSecret = "   "

	require.False(t, isStripeWebhookEnabled(), "test precondition: whitespace-only webhook secret must fail the webhook gate")

	plan := &model.SubscriptionPlan{
		Id:            77,
		Title:         "stripe-gate-plan",
		PriceAmount:   9.99,
		Currency:      "USD",
		DurationUnit:  "month",
		Enabled:       true,
		StripePriceId: "price_stripe_77",
	}
	plan.NormalizeDefaults()
	require.NoError(t, db.Create(plan).Error)
	t.Cleanup(func() { model.InvalidateSubscriptionPlanCache(plan.Id) })

	recorder := callSubscriptionHandler(t, SubscriptionRequestStripePay, 1, SubscriptionStripePayRequest{
		PlanId: plan.Id,
	})

	success, _ := decodeGateResponse(t, recorder)
	assert.False(t, success, "whitespace-only StripeWebhookSecret must fail the creation gate")

	var orderCount int64
	require.NoError(t, model.DB.Model(&model.SubscriptionOrder{}).Count(&orderCount).Error)
	assert.Zero(t, orderCount, "no pending SubscriptionOrder must be inserted when StripeWebhookSecret is whitespace-only")
}

// TestSubscriptionRequestCreemPay_ExpireOrderOnLinkFailure proves that when
// genCreemLink fails AFTER the pending SubscriptionOrder has been inserted,
// the handler expires the order instead of leaving it pending. A stranded
// pending order could never be redeemed (no checkout link was returned), so
// it must be transitioned out of pending. The genCreemLink failure is
// simulated via the function-variable test seam so the test is deterministic
// and does not depend on network.
func TestSubscriptionRequestCreemPay_ExpireOrderOnLinkFailure(t *testing.T) {
	db := setupSubscriptionGateTestDB(t)
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
	// Creation gate satisfied: compliance confirmed, API key present, plan
	// carrying a CreemProductId, webhook gate on via test mode.
	setting.CreemApiKey = "creem_api_key"
	setting.CreemProducts = "[]"
	setting.CreemWebhookSecret = ""
	setting.CreemTestMode = true

	plan := &model.SubscriptionPlan{
		Id:             43,
		Title:          "creem-link-fail-plan",
		PriceAmount:    9.99,
		Currency:       "USD",
		DurationUnit:   "month",
		Enabled:        true,
		CreemProductId: "prod_creem_43",
	}
	plan.NormalizeDefaults()
	require.NoError(t, db.Create(plan).Error)
	t.Cleanup(func() { model.InvalidateSubscriptionPlanCache(plan.Id) })

	// Seed a user so GetUserById succeeds before the order insert.
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "creem-sub-user", DisplayName: "creem-sub-user"}).Error)

	// Deterministically fail genCreemLink via the function-variable seam.
	originalGenCreemLink := genCreemLink
	t.Cleanup(func() { genCreemLink = originalGenCreemLink })
	genCreemLink = func(_ context.Context, _ string, _ *CreemProduct, _, _ string) (string, error) {
		return "", fmt.Errorf("simulated creem checkout failure")
	}

	recorder := callSubscriptionHandler(t, SubscriptionRequestCreemPay, 1, SubscriptionCreemPayRequest{
		PlanId: plan.Id,
	})

	success, _ := decodeGateResponse(t, recorder)
	assert.False(t, success, "genCreemLink failure must surface as a non-success response")

	var pendingCount int64
	require.NoError(t, model.DB.Model(&model.SubscriptionOrder{}).Where("status = ?", common.TopUpStatusPending).Count(&pendingCount).Error)
	assert.Zero(t, pendingCount, "genCreemLink failure must not leave a pending SubscriptionOrder")

	var expiredCount int64
	require.NoError(t, model.DB.Model(&model.SubscriptionOrder{}).Where("status = ?", common.TopUpStatusExpired).Count(&expiredCount).Error)
	assert.Equal(t, int64(1), expiredCount, "the stranded SubscriptionOrder must be expired")
}

// TestRequestCreemPay_MarkTopUpFailedOnLinkFailure proves the Creem wallet
// topup path marks the pending TopUp failed when genCreemLink fails after the
// order was inserted, instead of stranding a pending order that can never be
// redeemed. The genCreemLink failure is simulated via the function-variable
// test seam so the test is deterministic and does not depend on network.
func TestRequestCreemPay_MarkTopUpFailedOnLinkFailure(t *testing.T) {
	db := setupSubscriptionGateTestDB(t)
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
	// Creation gate satisfied so the pending TopUp is inserted before
	// genCreemLink is called.
	setting.CreemApiKey = "creem_api_key"
	setting.CreemProducts = `[{"productId":"prod_creem_topup","name":"wallet topup","price":1.0,"currency":"USD","quota":1000}]`
	setting.CreemWebhookSecret = ""
	setting.CreemTestMode = true

	require.True(t, isCreemTopUpEnabled(), "test precondition: Creem topup creation gate must be on")

	// Seed a user so GetUserById succeeds inside RequestPay.
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "creem-wallet-user", DisplayName: "creem-wallet-user"}).Error)

	// Deterministically fail genCreemLink via the function-variable seam.
	originalGenCreemLink := genCreemLink
	t.Cleanup(func() { genCreemLink = originalGenCreemLink })
	genCreemLink = func(_ context.Context, _ string, _ *CreemProduct, _, _ string) (string, error) {
		return "", fmt.Errorf("simulated creem checkout failure")
	}

	recorder := callSubscriptionHandler(t, RequestCreemPay, 1, CreemPayRequest{
		ProductId:     "prod_creem_topup",
		PaymentMethod: model.PaymentMethodCreem,
	})

	success, _ := decodeGateResponse(t, recorder)
	assert.False(t, success, "genCreemLink failure must surface as a non-success response")

	var pendingCount int64
	require.NoError(t, model.DB.Model(&model.TopUp{}).Where("status = ?", common.TopUpStatusPending).Count(&pendingCount).Error)
	assert.Zero(t, pendingCount, "genCreemLink failure must not leave a pending TopUp")

	var failedCount int64
	require.NoError(t, model.DB.Model(&model.TopUp{}).Where("status = ?", common.TopUpStatusFailed).Count(&failedCount).Error)
	assert.Equal(t, int64(1), failedCount, "the stranded TopUp must be marked failed")
}
