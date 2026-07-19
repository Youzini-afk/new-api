package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runRiskGuardRequest(t *testing.T, role int, action string, until int64, limit int) *httptest.ResponseRecorder {
	return runRiskGuardRequestWithMessage(t, role, action, until, limit, "")
}

func runRiskGuardRequestWithMessage(t *testing.T, role int, action string, until int64, limit int, message string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Set("id", 101)
	common.SetContextKey(ctx, constant.ContextKeyUserRole, role)
	common.SetContextKey(ctx, constant.ContextKeyRiskAction, action)
	common.SetContextKey(ctx, constant.ContextKeyRiskUntil, until)
	common.SetContextKey(ctx, constant.ContextKeyRiskActionId, int64(77))
	common.SetContextKey(ctx, constant.ContextKeyRiskRequestLimit, limit)
	common.SetContextKey(ctx, constant.ContextKeyRiskMessage, message)
	ActiveRiskGuard()(ctx)
	return recorder
}

func TestActiveRiskGuardUsesCachedUserMessage(t *testing.T) {
	recorder := runRiskGuardRequestWithMessage(t, common.RoleCommonUser, model.RiskActionTemporaryBlock, common.GetTimestamp()+60, 0, "请联系公益站管理员复核")
	assert.Equal(t, http.StatusForbidden, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "请联系公益站管理员复核")
}

func TestActiveRiskGuardTemporaryBlockAndExpiry(t *testing.T) {
	originalRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = originalRedis })

	recorder := runRiskGuardRequest(t, common.RoleCommonUser, model.RiskActionTemporaryBlock, common.GetTimestamp()+60, 0)
	assert.Equal(t, http.StatusForbidden, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "risk_control_restricted")

	expired := runRiskGuardRequest(t, common.RoleCommonUser, model.RiskActionTemporaryBlock, common.GetTimestamp()-1, 0)
	assert.Equal(t, http.StatusOK, expired.Code)
}

func TestActiveRiskGuardRateLimitUsesCachedLimit(t *testing.T) {
	originalRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = originalRedis })

	first := runRiskGuardRequest(t, common.RoleCommonUser, model.RiskActionRateLimit, common.GetTimestamp()+60, 1)
	require.Equal(t, http.StatusOK, first.Code)
	second := runRiskGuardRequest(t, common.RoleCommonUser, model.RiskActionRateLimit, common.GetTimestamp()+60, 1)
	assert.Equal(t, http.StatusTooManyRequests, second.Code)

	admin := runRiskGuardRequest(t, common.RoleAdminUser, model.RiskActionTemporaryBlock, common.GetTimestamp()+60, 0)
	assert.Equal(t, http.StatusOK, admin.Code)
}
