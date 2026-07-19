package model

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestUserBaseWriteContextIncludesRoleAndRiskSummary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	user := &UserBase{
		Id:               7,
		Role:             common.RoleAdminUser,
		Group:            "default",
		Status:           common.UserStatusEnabled,
		RiskAction:       RiskActionRateLimit,
		RiskUntil:        12345,
		RiskScore:        87,
		RiskCaseId:       31,
		RiskActionId:     42,
		RiskRequestLimit: 9,
		RiskMessage:      "custom restriction message",
	}
	user.WriteContext(ctx)

	assert.Equal(t, common.RoleAdminUser, common.GetContextKeyInt(ctx, constant.ContextKeyUserRole))
	assert.Equal(t, RiskActionRateLimit, common.GetContextKeyString(ctx, constant.ContextKeyRiskAction))
	until, _ := common.GetContextKeyType[int64](ctx, constant.ContextKeyRiskUntil)
	assert.Equal(t, int64(12345), until)
	assert.Equal(t, 9, common.GetContextKeyInt(ctx, constant.ContextKeyRiskRequestLimit))
	assert.Equal(t, "custom restriction message", common.GetContextKeyString(ctx, constant.ContextKeyRiskMessage))
}
