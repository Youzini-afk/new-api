package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const riskRateLimitWindowSeconds int64 = 60

var riskInMemoryRateLimiter common.InMemoryRateLimiter

// ActiveRiskGuard enforces the current risk summary already loaded by
// TokenAuth. It performs no user lookup and therefore adds no DB/Redis read on
// the common path. Admin/root traffic is always exempt.
func ActiveRiskGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		role := common.GetContextKeyInt(c, constant.ContextKeyUserRole)
		if role >= common.RoleAdminUser {
			c.Next()
			return
		}

		action := strings.TrimSpace(common.GetContextKeyString(c, constant.ContextKeyRiskAction))
		if action == "" || action == "none" || action == "observe" || action == "manual_review" {
			c.Next()
			return
		}
		until, _ := common.GetContextKeyType[int64](c, constant.ContextKeyRiskUntil)
		if until > 0 && until <= common.GetTimestamp() {
			c.Next()
			return
		}

		switch action {
		case "rate_limit":
			limit := common.GetContextKeyInt(c, constant.ContextKeyRiskRequestLimit)
			if limit <= 0 {
				limit = 10
			}
			if riskRateLimitAllowed(c, limit) {
				c.Next()
				return
			}
			message := strings.TrimSpace(common.GetContextKeyString(c, constant.ContextKeyRiskMessage))
			if message == "" {
				message = "请求频率已被风控策略临时限制，请稍后再试"
			}
			abortWithOpenAiMessage(c, http.StatusTooManyRequests, message, types.ErrorCodeRiskControlRestricted)
		case "temporary_block", "permanent_ban":
			message := strings.TrimSpace(common.GetContextKeyString(c, constant.ContextKeyRiskMessage))
			if message == "" {
				message = "当前 API 访问已被风控策略限制，请联系管理员核查"
			}
			abortWithOpenAiMessage(c, http.StatusForbidden, message, types.ErrorCodeRiskControlRestricted)
		default:
			// Unknown or history-only actions fail open. Persisted action types are
			// validated by the action service before they reach the user summary.
			c.Next()
		}
	}
}

func riskRateLimitAllowed(c *gin.Context, limit int) bool {
	userID := c.GetInt("id")
	if userID <= 0 || limit <= 0 {
		return true
	}
	window := time.Now().Unix() / riskRateLimitWindowSeconds
	actionID, _ := common.GetContextKeyType[int64](c, constant.ContextKeyRiskActionId)
	key := fmt.Sprintf("risk:rate:%d:%d:%d", userID, actionID, window)
	if common.RedisEnabled && common.RDB != nil {
		count, err := common.RDB.Incr(c.Request.Context(), key).Result()
		if err == nil {
			if count == 1 {
				_ = common.RDB.Expire(c.Request.Context(), key, 2*time.Minute).Err()
			}
			return count <= int64(limit)
		}
		// Redis degradation should not silently disable an already-authorized
		// safety action. Fall back to the process-local limiter; on multi-node
		// deployments this is less strict than Redis but still bounded per node.
		common.SysLog("risk rate limit redis check failed, using in-memory fallback: " + err.Error())
	}
	riskInMemoryRateLimiter.Init(2 * time.Minute)
	return riskInMemoryRateLimiter.Request(key, limit, riskRateLimitWindowSeconds)
}

type sensitiveUABlockContext struct {
	ctx *gin.Context
}

func (u *sensitiveUABlockContext) RequestContext() context.Context {
	if u == nil || u.ctx == nil || u.ctx.Request == nil {
		return context.Background()
	}
	return u.ctx.Request.Context()
}

func (u *sensitiveUABlockContext) UserID() int {
	if u == nil || u.ctx == nil {
		return 0
	}
	return u.ctx.GetInt("id")
}

func (u *sensitiveUABlockContext) Username() string {
	if u == nil || u.ctx == nil {
		return ""
	}
	return u.ctx.GetString("username")
}

func (u *sensitiveUABlockContext) ClientIP() string {
	if u == nil || u.ctx == nil {
		return ""
	}
	return u.ctx.ClientIP()
}

func (u *sensitiveUABlockContext) RequestPath() string {
	if u == nil || u.ctx == nil || u.ctx.Request == nil || u.ctx.Request.URL == nil {
		return ""
	}
	return u.ctx.Request.URL.Path
}

func (u *sensitiveUABlockContext) UserAgent() string {
	if u == nil || u.ctx == nil || u.ctx.Request == nil {
		return ""
	}
	return u.ctx.Request.UserAgent()
}

func (u *sensitiveUABlockContext) RequestHeadersRaw() string {
	if u == nil || u.ctx == nil {
		return ""
	}
	return service.BuildRawRequestHeadersForInterceptLog(u.ctx)
}

func (u *sensitiveUABlockContext) RequestParamsRaw() string {
	if u == nil || u.ctx == nil {
		return ""
	}
	return service.BuildRawRequestParamsForInterceptLog(u.ctx, nil)
}

// SensitiveUAGuard moves UA enforcement in front of request parsing and
// channel distribution. It uses only context populated by TokenAuth and keeps
// the existing audit/auto-ban implementation unchanged.
func SensitiveUAGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !setting.ShouldCheckUASensitive() {
			c.Next()
			return
		}
		common.SetContextKey(c, constant.ContextKeySensitiveUAChecked, true)
		if common.GetContextKeyInt(c, constant.ContextKeyUserRole) >= common.RoleAdminUser {
			c.Next()
			return
		}

		group := strings.TrimSpace(common.GetContextKeyString(c, constant.ContextKeyTokenGroup))
		if group == "" {
			group = strings.TrimSpace(common.GetContextKeyString(c, constant.ContextKeyUsingGroup))
		}
		userAgent := c.Request.UserAgent()
		hit, matched := service.MatchSensitiveUARule(userAgent, group)
		if !matched || hit == nil {
			if contains, patterns := service.CheckSensitiveUA(userAgent); contains {
				hit = &service.SensitiveRuleHit{
					Pattern:        strings.Join(patterns, ","),
					Message:        setting.SensitiveUABlockedMessage,
					ErrorCode:      types.ErrorCodeSensitiveWordsDetected,
					HTTPStatusCode: http.StatusBadRequest,
					MatchMode:      "blocked_regex",
				}
				matched = true
			}
		}
		if !matched || hit == nil {
			c.Next()
			return
		}

		logger.LogWarn(c, fmt.Sprintf("user agent blocked before relay: %s", hit.Pattern))
		status, code, errMsg := service.BuildUABlockedErrorAndRecord(&sensitiveUABlockContext{ctx: c}, hit)
		abortWithOpenAiMessage(c, status, errMsg.Error(), code)
	}
}
