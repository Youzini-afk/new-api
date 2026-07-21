package governance

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// Error insight recording: bridges governance classification with the
// model.ErrorLog async queue. Called from relay exit points after the client
// response has been built. Never mutates the original *types.NewAPIError.

var (
	adminRawAuthorizationPattern = regexp.MustCompile(`(?i)(authorization\s*[:=]\s*)(bearer\s+)?[^\s,;]+`)
	adminRawSecretPattern        = regexp.MustCompile(`(?i)((?:api[_ -]?key|cookie|token|secret)\s*[:=]\s*)[^\s,;]+`)
)

const errorInsightRecordedKey = "error_insight_recorded"

// RecordRelayErrorInsight records an error insight event for admin
// debugging. It classifies the original error, generates a fingerprint, and
// enqueues an async DB write. Idempotent per-request (guarded by a gin
// context flag). Safe to call from any relay exit point.
func RecordRelayErrorInsight(c *gin.Context, originalErr *types.NewAPIError, safe SafeErrorPayload, source, stage string, requestTime, retryCount int) {
	if originalErr == nil {
		return
	}
	if c != nil && c.GetBool(errorInsightRecordedKey) {
		return
	}
	if c != nil {
		c.Set(errorInsightRecordedKey, true)
	}

	in := ExtractRelayErrorInput(originalErr)
	analysis := AnalyzeRelayError(in, source, stage)

	event := model.ErrorLogEvent{
		CreatedAt:   time.Now().Unix(),
		RequestId:   contextString(c, common.RequestIdKey),
		UserId:      contextInt(c, "id"),
		Username:    contextString(c, "username"),
		Group:       contextString(c, "group"),
		TokenId:     contextInt(c, "token_id"),
		TokenName:   contextString(c, "token_name"),
		ChannelId:   contextInt(c, "channel_id"),
		ModelName:   relayLogModelName(c),
		RequestPath: requestPath(c),
		IsStream:    c != nil && c.GetBool("is_stream"),

		ErrorSource: analysis.ErrorSource,
		ErrorStage:  analysis.ErrorStage,

		ClientStatusCode:   safe.StatusCode,
		UpstreamStatusCode: in.StatusCode,

		RuleCode:        analysis.AdviceCode,
		RuleMatched:     analysis.RuleMatched,
		MatchSource:     analysis.MatchSource,
		UnmatchedReason: analysis.UnmatchedReason,
		RuleVersion:     analysis.RuleVersion,

		SafeErrorCode:    sanitizeAnalyticsToken(fmt.Sprint(safe.OpenAIError.Code)),
		SafeErrorType:    sanitizeAnalyticsToken(safe.OpenAIError.Type),
		SafeErrorMessage: truncateString(safe.OpenAIError.Message, 500),

		OriginalErrorCode:       sanitizeAnalyticsToken(in.Code),
		OriginalErrorType:       sanitizeAnalyticsToken(in.Type),
		OriginalErrorMessage:    sanitizeAdminRawErrorMessage(in.Message),
		OriginalErrorStatusCode: in.StatusCode,

		NormalizedSignature: analysis.NormalizedSignature,
		NormalizedMessage:   analysis.NormalizedMessage,

		RequestTime: requestTime,
		RetryCount:  retryCount,
	}

	model.EnqueueErrorLog(event)
}

func contextString(c *gin.Context, key string) string {
	if c == nil {
		return ""
	}
	return c.GetString(key)
}

func contextInt(c *gin.Context, key string) int {
	if c == nil {
		return 0
	}
	return c.GetInt(key)
}

func relayLogModelName(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if modelName := c.GetString("original_model"); modelName != "" {
		return modelName
	}
	if modelName := c.GetString("new_model"); modelName != "" {
		return modelName
	}
	if modelName := c.GetString("model_name"); modelName != "" {
		return modelName
	}
	return ""
}

func requestPath(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return ""
	}
	return c.Request.URL.Path
}

func truncateString(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen]
	}
	return s
}

// sanitizeAdminRawErrorMessage applies light sanitization to the original
// upstream error message for admin-visible logs. Replaces secrets,
// authorization headers, and API keys. Truncates to 2000 chars.
func sanitizeAdminRawErrorMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	message = analyticsSecretPattern.ReplaceAllString(message, "<secret>")
	message = adminRawAuthorizationPattern.ReplaceAllString(message, "$1<secret>")
	message = adminRawSecretPattern.ReplaceAllString(message, "$1<secret>")
	if len(message) > 2000 {
		message = message[:1997] + "…"
	}
	return message
}
