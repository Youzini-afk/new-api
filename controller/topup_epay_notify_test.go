package controller

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupEpayNotifyTestDB wires an isolated in-memory SQLite DB for the Epay
// webhook controller tests, migrating the tables the completion + log path
// touches.
func setupEpayNotifyTestDB(t *testing.T) *gorm.DB {
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

	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}, &model.Option{}))

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

// configureEpayForNotifyTest enables the Epay webhook with a known key and
// restores the originals on cleanup.
func configureEpayForNotifyTest(t *testing.T) {
	t.Helper()
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
	operation_setting.PayAddress = "https://pay.example.com"
	operation_setting.EpayId = "1001"
	operation_setting.EpayKey = "test-epay-key"
	operation_setting.PayMethods = []map[string]string{{"type": "alipay"}, {"type": "wxpay"}}
}

// signEpayNotifyParams builds a GET request carrying a validly signed Epay
// notify payload for the given trade. The caller controls trade_status / type /
// money so the same helper covers happy, mismatch, and non-success-event cases.
func signEpayNotifyParams(t *testing.T, tradeNo string, tradeStatus string, paymentType string, money string) *http.Request {
	t.Helper()
	params := map[string]string{
		"pid":          operation_setting.EpayId,
		"type":         paymentType,
		"out_trade_no": tradeNo,
		"trade_status": tradeStatus,
		"name":         "TUC2",
		"money":        money,
	}
	signed := epay.GenerateParams(params, operation_setting.EpayKey)

	values := url.Values{}
	for k, v := range signed {
		values.Set(k, v)
	}
	target := "/api/user/epay/notify?" + values.Encode()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	return req
}

func callEpayNotify(t *testing.T, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = req
	EpayNotify(ctx)
	return recorder
}

// TestEpayNotify_AcksSuccessAfterCompletion verifies the core fix: the
// webhook acks "success" ONLY after the topup completion transaction commits,
// and the user actually receives quota.
func TestEpayNotify_AcksSuccessAfterCompletion(t *testing.T) {
	setupEpayNotifyTestDB(t)
	configureEpayForNotifyTest(t)

	user := &model.User{Username: "epay_notify_user", Password: "pw12345678", Status: common.UserStatusEnabled, Group: "default", Quota: 0}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId:          user.Id,
		Amount:          2,
		Money:           9.99,
		TradeNo:         "USR1NOnotify-ok",
		PaymentMethod:   "alipay",
		PaymentProvider: model.PaymentProviderEpay,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}
	require.NoError(t, topUp.Insert())

	recorder := callEpayNotify(t, signEpayNotifyParams(t, "USR1NOnotify-ok", epay.StatusTradeSuccess, "alipay", "9.99"))
	assert.Equal(t, "success", recorder.Body.String())

	refreshed := model.GetTopUpByTradeNo("USR1NOnotify-ok")
	require.NotNil(t, refreshed)
	assert.Equal(t, common.TopUpStatusSuccess, refreshed.Status)
	assert.NotZero(t, refreshed.CompleteTime)

	var reloaded model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", user.Id).First(&reloaded).Error)
	assert.Equal(t, int(float64(2)*common.QuotaPerUnit), reloaded.Quota)
}

// TestEpayNotify_DuplicateSuccessAcksSuccessNoDoubleQuota verifies a repeated
// success notify is idempotent: still acks "success" but does not re-add quota.
func TestEpayNotify_DuplicateSuccessAcksSuccessNoDoubleQuota(t *testing.T) {
	setupEpayNotifyTestDB(t)
	configureEpayForNotifyTest(t)

	user := &model.User{Username: "epay_notify_dup", Password: "pw12345678", Status: common.UserStatusEnabled, Group: "default", Quota: 0}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId:          user.Id,
		Amount:          1,
		Money:           5.50,
		TradeNo:         "USR1NOnotify-dup",
		PaymentMethod:   "wxpay",
		PaymentProvider: model.PaymentProviderEpay,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}
	require.NoError(t, topUp.Insert())
	expectedQuota := int(float64(1) * common.QuotaPerUnit)

	first := callEpayNotify(t, signEpayNotifyParams(t, "USR1NOnotify-dup", epay.StatusTradeSuccess, "wxpay", "5.50"))
	assert.Equal(t, "success", first.Body.String())

	second := callEpayNotify(t, signEpayNotifyParams(t, "USR1NOnotify-dup", epay.StatusTradeSuccess, "wxpay", "5.50"))
	assert.Equal(t, "success", second.Body.String(), "idempotent duplicate must still ack success")

	var reloaded model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", user.Id).First(&reloaded).Error)
	assert.Equal(t, expectedQuota, reloaded.Quota, "quota must not be doubled on duplicate notify")
}

// TestEpayNotify_DuplicateWrongMethodAcksFail verifies that even when an order
// is already successful, a verified duplicate callback with a different payment
// method is not silently acked as success.
func TestEpayNotify_DuplicateWrongMethodAcksFail(t *testing.T) {
	setupEpayNotifyTestDB(t)
	configureEpayForNotifyTest(t)

	user := &model.User{Username: "epay_notify_dup_method", Password: "pw12345678", Status: common.UserStatusEnabled, Group: "default", Quota: 0}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId:          user.Id,
		Amount:          1,
		Money:           5.50,
		TradeNo:         "USR1NOnotify-dup-method",
		PaymentMethod:   "wxpay",
		PaymentProvider: model.PaymentProviderEpay,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}
	require.NoError(t, topUp.Insert())
	expectedQuota := int(float64(1) * common.QuotaPerUnit)

	first := callEpayNotify(t, signEpayNotifyParams(t, "USR1NOnotify-dup-method", epay.StatusTradeSuccess, "wxpay", "5.50"))
	assert.Equal(t, "success", first.Body.String())
	duplicate := callEpayNotify(t, signEpayNotifyParams(t, "USR1NOnotify-dup-method", epay.StatusTradeSuccess, "alipay", "5.50"))
	assert.Equal(t, "fail", duplicate.Body.String())

	var reloaded model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", user.Id).First(&reloaded).Error)
	assert.Equal(t, expectedQuota, reloaded.Quota)
}

// TestEpayNotify_DuplicateWrongMoneyAcksFail verifies that a verified duplicate
// callback with different paid money is rejected even after the order is
// already successful.
func TestEpayNotify_DuplicateWrongMoneyAcksFail(t *testing.T) {
	setupEpayNotifyTestDB(t)
	configureEpayForNotifyTest(t)

	user := &model.User{Username: "epay_notify_dup_money", Password: "pw12345678", Status: common.UserStatusEnabled, Group: "default", Quota: 0}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId:          user.Id,
		Amount:          1,
		Money:           5.50,
		TradeNo:         "USR1NOnotify-dup-money",
		PaymentMethod:   "wxpay",
		PaymentProvider: model.PaymentProviderEpay,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}
	require.NoError(t, topUp.Insert())
	expectedQuota := int(float64(1) * common.QuotaPerUnit)

	first := callEpayNotify(t, signEpayNotifyParams(t, "USR1NOnotify-dup-money", epay.StatusTradeSuccess, "wxpay", "5.50"))
	assert.Equal(t, "success", first.Body.String())
	duplicate := callEpayNotify(t, signEpayNotifyParams(t, "USR1NOnotify-dup-money", epay.StatusTradeSuccess, "wxpay", "6.50"))
	assert.Equal(t, "fail", duplicate.Body.String())

	var reloaded model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", user.Id).First(&reloaded).Error)
	assert.Equal(t, expectedQuota, reloaded.Quota)
}

// TestEpayNotify_AmountMismatchAcksFail verifies a paid-money mismatch leaves
// the order pending and the webhook acks "fail" so the gateway can retry.
func TestEpayNotify_AmountMismatchAcksFail(t *testing.T) {
	setupEpayNotifyTestDB(t)
	configureEpayForNotifyTest(t)

	user := &model.User{Username: "epay_notify_amount", Password: "pw12345678", Status: common.UserStatusEnabled, Group: "default", Quota: 7}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId:          user.Id,
		Amount:          2,
		Money:           9.99,
		TradeNo:         "USR1NOnotify-amount",
		PaymentMethod:   "alipay",
		PaymentProvider: model.PaymentProviderEpay,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}
	require.NoError(t, topUp.Insert())

	recorder := callEpayNotify(t, signEpayNotifyParams(t, "USR1NOnotify-amount", epay.StatusTradeSuccess, "alipay", "8.88"))
	assert.Equal(t, "fail", recorder.Body.String())

	refreshed := model.GetTopUpByTradeNo("USR1NOnotify-amount")
	require.NotNil(t, refreshed)
	assert.Equal(t, common.TopUpStatusPending, refreshed.Status)
	assert.Zero(t, refreshed.CompleteTime)

	var reloaded model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", user.Id).First(&reloaded).Error)
	assert.Equal(t, 7, reloaded.Quota)
}

// TestEpayNotify_VerifyFailureAcksFail verifies a tampered signature acks fail
// and does NOT touch the order.
func TestEpayNotify_VerifyFailureAcksFail(t *testing.T) {
	setupEpayNotifyTestDB(t)
	configureEpayForNotifyTest(t)

	user := &model.User{Username: "epay_notify_bad_sig", Password: "pw12345678", Status: common.UserStatusEnabled, Group: "default", Quota: 3}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId:          user.Id,
		Amount:          2,
		Money:           9.99,
		TradeNo:         "USR1NOnotify-badsig",
		PaymentMethod:   "alipay",
		PaymentProvider: model.PaymentProviderEpay,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}
	require.NoError(t, topUp.Insert())

	req := signEpayNotifyParams(t, "USR1NOnotify-badsig", epay.StatusTradeSuccess, "alipay", "9.99")
	// Tamper with the money after signing so the signature no longer matches.
	urlVals := req.URL.Query()
	urlVals.Set("money", "1.00")
	req.URL.RawQuery = urlVals.Encode()

	recorder := callEpayNotify(t, req)
	assert.Equal(t, "fail", recorder.Body.String())

	refreshed := model.GetTopUpByTradeNo("USR1NOnotify-badsig")
	require.NotNil(t, refreshed)
	assert.Equal(t, common.TopUpStatusPending, refreshed.Status)

	var reloaded model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", user.Id).First(&reloaded).Error)
	assert.Equal(t, 3, reloaded.Quota)
}

// TestEpayNotify_NonSuccessEventAcksSuccess verifies that a verified non-success
// trade status (e.g. TRADE_CLOSED) acks success to stop retries without
// processing completion.
func TestEpayNotify_NonSuccessEventAcksSuccess(t *testing.T) {
	setupEpayNotifyTestDB(t)
	configureEpayForNotifyTest(t)

	user := &model.User{Username: "epay_notify_closed", Password: "pw12345678", Status: common.UserStatusEnabled, Group: "default", Quota: 11}
	require.NoError(t, model.DB.Create(user).Error)
	topUp := &model.TopUp{
		UserId:          user.Id,
		Amount:          2,
		Money:           9.99,
		TradeNo:         "USR1NOnotify-closed",
		PaymentMethod:   "alipay",
		PaymentProvider: model.PaymentProviderEpay,
		Status:          common.TopUpStatusPending,
		CreateTime:      common.GetTimestamp(),
	}
	require.NoError(t, topUp.Insert())

	recorder := callEpayNotify(t, signEpayNotifyParams(t, "USR1NOnotify-closed", "TRADE_CLOSED", "alipay", "9.99"))
	assert.Equal(t, "success", recorder.Body.String())

	refreshed := model.GetTopUpByTradeNo("USR1NOnotify-closed")
	require.NotNil(t, refreshed)
	assert.Equal(t, common.TopUpStatusPending, refreshed.Status, "non-success event must not complete the order")
	assert.Zero(t, refreshed.CompleteTime)

	var reloaded model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", user.Id).First(&reloaded).Error)
	assert.Equal(t, 11, reloaded.Quota)
}

// TestEpayNotify_WebhookDisabledAcksFail guards the gate: when the Epay webhook
// is not configured/enabled, the handler refuses early.
func TestEpayNotify_WebhookDisabledAcksFail(t *testing.T) {
	setupEpayNotifyTestDB(t)
	confirmPaymentComplianceForTest(t)
	// Leave PayAddress / EpayId / EpayKey / PayMethods unset -> disabled.
	originalPayAddress := operation_setting.PayAddress
	originalEpayID := operation_setting.EpayId
	originalEpayKey := operation_setting.EpayKey
	originalPayMethods := operation_setting.PayMethods
	operation_setting.PayAddress = ""
	operation_setting.EpayId = ""
	operation_setting.EpayKey = ""
	operation_setting.PayMethods = nil
	t.Cleanup(func() {
		operation_setting.PayAddress = originalPayAddress
		operation_setting.EpayId = originalEpayID
		operation_setting.EpayKey = originalEpayKey
		operation_setting.PayMethods = originalPayMethods
	})

	recorder := callEpayNotify(t, signEpayNotifyParams(t, "USR1NOnotify-disabled", epay.StatusTradeSuccess, "alipay", "9.99"))
	assert.Equal(t, "fail", recorder.Body.String())
}
