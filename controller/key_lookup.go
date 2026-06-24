package controller

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// LookupByKey 通过 API key 反查令牌及归属用户（只读，AdminAuth 路由）。
// GET /api/key_lookup?key=...
//
// 处理流程：
//  1. 取 key 查询参数，trim 后非空；剥离可选的 sk- 前缀。
//  2. model.GetTokenByKey 查询令牌；未找到返回 token not found；其他错误透传。
//  3. model.GetUserById 查询归属用户；错误透传。
//  4. 调用方角色必须能管理目标用户角色（canManageTargetRole），否则拒绝。
//  5. 返回用户与令牌摘要；令牌 key 仅以 masked 形式返回，禁止明文外泄。
func LookupByKey(c *gin.Context) {
	rawKey := strings.TrimSpace(c.Query("key"))
	if rawKey == "" {
		common.ApiErrorMsg(c, "key is required")
		return
	}
	key := strings.TrimPrefix(rawKey, "sk-")
	if key == "" {
		common.ApiErrorMsg(c, "key is required")
		return
	}

	token, err := model.GetTokenByKey(key, false)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiErrorMsg(c, "token not found")
			return
		}
		common.ApiError(c, err)
		return
	}

	user, err := model.GetUserById(token.UserId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	if !canManageTargetRole(c.GetInt("role"), user.Role) {
		common.ApiErrorMsg(c, "no permission to access this token's user")
		return
	}

	common.ApiSuccess(c, gin.H{
		"user":       user,
		"token":      buildMaskedTokenResponse(token),
		"key_masked": token.GetMaskedKey(),
	})
}
