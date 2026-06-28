package governance

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// ============================================================================
// Error Governance
//
// Prevents raw upstream error messages from reaching downstream clients.
// Upstream errors are classified by keyword/code/type/param/status-code into a
// fixed rule table; the matched rule's Status/Type/Code/Param/Message replace
// the original upstream detail in the client response.
//
// CRITICAL: governance produces a *client response view* (SafeErrorPayload),
// never a replacement for the original *types.NewAPIError. The original error
// must stay intact for retry decisions, channel auto-disable, and billing.
// See controller/relay.go central exit point for the wiring.
// ============================================================================

const RelayErrorGovernanceRuleVersion = 1

// Rule codes — the canonical classification vocabulary.
const (
	RelayRuleInsufficientUserQuota = "insufficient_user_quota"
	RelayRuleInvalidMaxTokens      = "invalid_max_tokens"
	RelayRuleMaxTokensNeedStream   = "max_tokens_requires_stream"
	RelayRuleInvalidBudgetTokens   = "invalid_budget_tokens"
	RelayRuleInvalidStreamOptions  = "invalid_stream_options"
	RelayRuleInvalidMessageRole    = "invalid_message_role"
	RelayRuleInvalidImageURL       = "invalid_image_url"
	RelayRuleContextLengthExceeded = "context_length_exceeded"
	RelayRuleContentFiltered       = "content_filtered"
	RelayRuleModelNotFound         = "model_not_found"
	RelayRuleNoAvailableChannel    = "no_available_channel"
	RelayRuleModelNotPermitted     = "model_not_permitted"
	RelayRuleRiskControlRestricted = "risk_control_restricted"
	RelayRuleBehaviorBanned        = "behavior_banned"
	RelayRuleUpstreamRateLimited   = "upstream_rate_limited"
	RelayRuleUpstreamTimeout       = "upstream_timeout"
	RelayRuleUpstreamBadResponse   = "upstream_bad_response"
	RelayRuleUpstreamUnavailable   = "upstream_unavailable"
	RelayRuleStreamInterrupted     = "stream_interrupted"
	RelayRuleInternalError         = "internal_error"
)

// Match sources — how a rule was matched.
const (
	RelayMatchSourceExactCode    = "exact_code"
	RelayMatchSourceExactType    = "exact_type"
	RelayMatchSourceExactParam   = "exact_param"
	RelayMatchSourceKeyword      = "keyword"
	RelayMatchSourceStatusCode   = "status_code"
	RelayMatchSourceCustomRule   = "custom_rule"
	RelayMatchSourceFallback     = "fallback"
	RelayMatchSourceDisabledRule = "disabled_rule_fallback"
)

// Unmatched reasons — why no rule matched.
const (
	RelayUnmatchedReasonNone                 = "none"
	RelayUnmatchedReasonOpaqueUpstreamError  = "opaque_upstream_error"
	RelayUnmatchedReasonLocalSystemError     = "local_system_error"
	RelayUnmatchedReasonEmptyError           = "empty_error"
	RelayUnmatchedReasonDisabledRuleFallback = "disabled_rule_fallback"
)

var relayRuleOrder = []string{
	RelayRuleInsufficientUserQuota,
	RelayRuleInvalidBudgetTokens,
	RelayRuleMaxTokensNeedStream,
	RelayRuleInvalidMaxTokens,
	RelayRuleInvalidStreamOptions,
	RelayRuleInvalidMessageRole,
	RelayRuleInvalidImageURL,
	RelayRuleContextLengthExceeded,
	RelayRuleContentFiltered,
	RelayRuleModelNotPermitted,
	RelayRuleModelNotFound,
	RelayRuleNoAvailableChannel,
	RelayRuleRiskControlRestricted,
	RelayRuleBehaviorBanned,
	RelayRuleUpstreamRateLimited,
	RelayRuleUpstreamTimeout,
	RelayRuleUpstreamBadResponse,
	RelayRuleStreamInterrupted,
	RelayRuleUpstreamUnavailable,
	RelayRuleInternalError,
}

var relayRules = map[string]relayRule{
	RelayRuleInsufficientUserQuota: {Code: RelayRuleInsufficientUserQuota, Status: http.StatusPaymentRequired, Type: "insufficient_quota_error", Message: "账户额度不足。"},
	RelayRuleInvalidMaxTokens:      {Code: RelayRuleInvalidMaxTokens, Status: http.StatusBadRequest, Type: "invalid_request_error", Param: "max_tokens", Message: "max_tokens 参数无效，请调整后重试。"},
	RelayRuleMaxTokensNeedStream:   {Code: RelayRuleMaxTokensNeedStream, Status: http.StatusBadRequest, Type: "invalid_request_error", Param: "stream", Message: "max_tokens 超过 4096 时必须启用 stream=true。"},
	RelayRuleInvalidBudgetTokens:   {Code: RelayRuleInvalidBudgetTokens, Status: http.StatusBadRequest, Type: "invalid_request_error", Param: "reasoning.budget_tokens", Message: "reasoning.budget_tokens 参数无效，请在允许范围内调整后重试。"},
	RelayRuleInvalidStreamOptions:  {Code: RelayRuleInvalidStreamOptions, Status: http.StatusBadRequest, Type: "invalid_request_error", Param: "stream_options", Message: "stream_options 参数无效，请检查后重试。"},
	RelayRuleInvalidMessageRole:    {Code: RelayRuleInvalidMessageRole, Status: http.StatusBadRequest, Type: "invalid_request_error", Param: "messages.role", Message: "消息角色无效，请检查 messages 中的 role，仅支持 system、user、assistant 或 tool。"},
	RelayRuleInvalidImageURL:       {Code: RelayRuleInvalidImageURL, Status: http.StatusBadRequest, Type: "invalid_request_error", Param: "image_url", Message: "图片链接无法访问或格式不受支持，请更换后重试。"},
	RelayRuleContextLengthExceeded: {Code: RelayRuleContextLengthExceeded, Status: http.StatusBadRequest, Type: "invalid_request_error", Message: "请求超过模型上下文或 token 限制，请缩短输入或降低 max_tokens。"},
	RelayRuleContentFiltered:       {Code: RelayRuleContentFiltered, Status: http.StatusBadRequest, Type: "content_policy_violation", Message: "您提供的内容或模型输出被上游安全策略屏蔽，请调整后重试。"},
	RelayRuleModelNotFound:         {Code: RelayRuleModelNotFound, Status: http.StatusNotFound, Type: "model_not_found", Message: "请求的模型不存在。"},
	RelayRuleNoAvailableChannel:    {Code: RelayRuleNoAvailableChannel, Status: http.StatusServiceUnavailable, Type: "service_unavailable", Message: "当前模型或能力暂无可用通道，请稍后重试。"},
	RelayRuleModelNotPermitted:     {Code: RelayRuleModelNotPermitted, Status: http.StatusForbidden, Type: "permission_denied", Message: "当前令牌未授权使用此模型。"},
	RelayRuleRiskControlRestricted: {Code: RelayRuleRiskControlRestricted, Status: http.StatusTooManyRequests, Type: "risk_control_error", Message: "账户活跃度受限，请稍后再试或联系管理员。"},
	RelayRuleBehaviorBanned:        {Code: RelayRuleBehaviorBanned, Status: http.StatusForbidden, Type: "behavior_banned", Message: "账户因异常行为已被限制，请联系管理员。"},
	RelayRuleUpstreamRateLimited:   {Code: RelayRuleUpstreamRateLimited, Status: http.StatusServiceUnavailable, Type: "service_unavailable", Message: "上游服务当前负载较高，请稍后重试。"},
	RelayRuleUpstreamTimeout:       {Code: RelayRuleUpstreamTimeout, Status: http.StatusGatewayTimeout, Type: "service_unavailable", Message: "上游服务响应超时，请稍后重试。"},
	RelayRuleUpstreamBadResponse:   {Code: RelayRuleUpstreamBadResponse, Status: http.StatusBadGateway, Type: "service_unavailable", Message: "上游响应格式异常，请稍后重试；如持续出现，可尝试开启流式响应。"},
	RelayRuleUpstreamUnavailable:   {Code: RelayRuleUpstreamUnavailable, Status: http.StatusServiceUnavailable, Type: "service_unavailable", Message: "上游服务暂时不可用，请稍后重试。"},
	RelayRuleStreamInterrupted:     {Code: RelayRuleStreamInterrupted, Status: http.StatusServiceUnavailable, Type: "service_unavailable", Message: "流式响应中断，请稍后重试。"},
	RelayRuleInternalError:         {Code: RelayRuleInternalError, Status: http.StatusInternalServerError, Type: "system_error", Message: "服务处理请求时发生错误，请稍后重试。"},
}

type relayRule struct {
	Code    string
	Status  int
	Type    string
	Param   string
	Message string
}

// Classification holds the result of classifying an error against the rule
// table. It is used both for client response generation (EG-1) and for error
// insight analytics (EG-2).
type Classification struct {
	AdviceCode      string
	StatusCode      int
	Type            string
	Code            string
	Param           string
	RuleMatched     bool
	MatchSource     string
	UnmatchedReason string
	RuleVersion     int
}

// RelayErrorAnalysis extends Classification with analytics fingerprint data.
// Used by EG-2 error insight recording.
type RelayErrorAnalysis struct {
	Classification
	ErrorSource         string
	ErrorStage          string
	NormalizedMessage   string
	NormalizedSignature string
}

// ErrorFingerprint holds the normalized message and HMAC signature used for
// admin error insight aggregation (EG-2).
type ErrorFingerprint struct {
	NormalizedMessage   string
	NormalizedSignature string
}

type relayErrorMatch struct {
	Code            string
	RuleMatched     bool
	MatchSource     string
	UnmatchedReason string
}

// --- Input extraction ---

// RelayErrorInput holds the fields extracted from *types.NewAPIError that the
// classification engine needs. It decouples governance from NewAPIError's
// unexported fields (skipRetry/errorType/errorCode).
type RelayErrorInput struct {
	Message    string
	Type       string
	Code       string
	Param      string
	StatusCode int
	ErrorCode  types.ErrorCode
	ErrorType  types.ErrorType
}

// ExtractRelayErrorInput builds a RelayErrorInput from a *types.NewAPIError by
// calling its exported accessors. ToOpenAIError() already applies
// common.MaskSensitiveInfo to the message, which is safe for classification.
func ExtractRelayErrorInput(err *types.NewAPIError) RelayErrorInput {
	if err == nil {
		return RelayErrorInput{}
	}
	oai := err.ToOpenAIError()
	return RelayErrorInput{
		Message:    oai.Message,
		Type:       oai.Type,
		Code:       fmt.Sprint(oai.Code),
		Param:      oai.Param,
		StatusCode: err.StatusCode,
		ErrorCode:  err.GetErrorCode(),
		ErrorType:  err.GetErrorType(),
	}
}

// isUpstreamErrorInput returns true when the error originated from an upstream
// HTTP response or connection, as opposed to a local validation/billing/system
// error. This determines the unmatched-reason classification.
func isUpstreamErrorInput(in RelayErrorInput) bool {
	switch in.ErrorCode {
	case types.ErrorCodeBadResponseStatusCode, types.ErrorCodeDoRequestFailed:
		return true
	}
	return false
}

// --- Client response view ---

// SafeErrorPayload is a client-facing relay error view. It carries ONLY the
// governance-classified status code and a safe OpenAIError — never the
// original upstream detail. The original *types.NewAPIError stays intact for
// retry / channel-disable / billing decisions.
type SafeErrorPayload struct {
	StatusCode  int
	OpenAIError types.OpenAIError
}

// ClaudeError converts the safe payload to the Claude error format for
// Claude-format relay responses.
func (s SafeErrorPayload) ClaudeError() types.ClaudeError {
	return types.ClaudeError{
		Type:    s.OpenAIError.Type,
		Message: s.OpenAIError.Message,
	}
}

// SanitizeRelayErrorForClient classifies the original error against the rule
// table and returns a SafeErrorPayload containing only the governance-classified
// status/type/code/param/message. The original *types.NewAPIError is never
// mutated and remains usable for retry, channel-disable, and billing.
//
// When governance is disabled (RELAY_ERROR_GOVERNANCE_ENABLED=false), a minimal
// fallback is used: the error is still masked (ToOpenAIError already applies
// MaskSensitiveInfo) and the status code is preserved, but no rule-based
// message replacement happens. This ensures governance-off never leaks more
// than the pre-existing behavior.
func SanitizeRelayErrorForClient(c *gin.Context, err *types.NewAPIError) SafeErrorPayload {
	if err == nil {
		return SafeErrorPayload{
			StatusCode: http.StatusInternalServerError,
			OpenAIError: types.OpenAIError{
				Message: relayRules[RelayRuleInternalError].Message,
				Type:    relayRules[RelayRuleInternalError].Type,
				Code:    RelayRuleInternalError,
			},
		}
	}

	in := ExtractRelayErrorInput(err)

	if !governanceEnabled() {
		// Governance disabled: preserve pre-existing behavior (masked message +
		// original status), but strip upstream request-ids and attach local one.
		oai := err.ToOpenAIError()
		oai.Message = withLocalRequestID(c, oai.Message)
		return SafeErrorPayload{StatusCode: normalizedStatusFallback(in), OpenAIError: oai}
	}

	rules := effectiveRules()
	cls := classifyWithRules(in, rules)
	effectiveCode := firstEnabledAdviceCode(cls.Code, rules)

	oai := types.OpenAIError{
		Message: withLocalRequestID(c, effectiveMessage(effectiveCode, rules)),
		Type:    ruleType(effectiveCode, rules),
		Code:    effectiveCode,
		Param:   ruleParam(effectiveCode, rules),
	}
	return SafeErrorPayload{
		StatusCode:  normalizedStatusFor(effectiveCode, in.StatusCode, cls.StatusCode, rules),
		OpenAIError: oai,
	}
}

// firstEnabledAdviceCode mirrors nashiyard's disabled-rule fallback: if the
// classified rule code is disabled, fall back to internal_error, then to the
// first enabled rule in rule order. This ensures that even if an admin
// disables a specific rule, a safe message is always returned.
func firstEnabledAdviceCode(adviceCode string, rules map[string]effectiveRelayRule) string {
	if adviceCode != "" && rules[adviceCode].Enabled {
		return adviceCode
	}
	if rules[RelayRuleInternalError].Enabled {
		return RelayRuleInternalError
	}
	for _, code := range relayRuleOrder {
		if rules[code].Enabled {
			return code
		}
	}
	return RelayRuleInternalError
}

// --- Classification engine ---

func ClassifyRelayError(in RelayErrorInput) Classification {
	return classifyWithRules(in, effectiveRules())
}

func AnalyzeRelayError(in RelayErrorInput, source string, stage string) RelayErrorAnalysis {
	cls := classifyWithContext(in, source, stage)
	fp := NormalizeRelayErrorForAnalytics(in, cls.Code, source, stage)
	return RelayErrorAnalysis{
		Classification:      cls,
		ErrorSource:         source,
		ErrorStage:          stage,
		NormalizedMessage:   fp.NormalizedMessage,
		NormalizedSignature: fp.NormalizedSignature,
	}
}

func NormalizeRelayErrorForAnalytics(in RelayErrorInput, ruleCode string, source string, stage string) ErrorFingerprint {
	normalized := normalizeRelayErrorMessage(in)
	data := strings.Join([]string{source, stage, fmt.Sprint(in.StatusCode), ruleCode, normalized}, "|")
	mac := hmac.New(sha256.New, []byte(errorAnalyticsSignatureSecret()))
	_, _ = mac.Write([]byte(data))
	return ErrorFingerprint{
		NormalizedMessage:   normalized,
		NormalizedSignature: hex.EncodeToString(mac.Sum(nil)),
	}
}

func classifyWithRules(in RelayErrorInput, rules map[string]effectiveRelayRule) Classification {
	if match, ok := matchCustomRelayError(in, rules); ok {
		return byMatchWithRules(match, rules)
	}
	match := classifyMatchWithContext(in, "", "")
	if rule, ok := rules[match.Code]; ok && !rule.Enabled {
		match = relayErrorMatch{Code: RelayRuleInternalError, RuleMatched: false, MatchSource: RelayMatchSourceDisabledRule, UnmatchedReason: RelayUnmatchedReasonDisabledRuleFallback}
	}
	return byMatchWithRules(match, rules)
}

func classifyWithContext(in RelayErrorInput, source string, stage string) Classification {
	rules := effectiveRules()
	if match, ok := matchCustomRelayError(in, rules); ok {
		return byMatchWithRules(match, rules)
	}
	match := classifyMatchWithContext(in, source, stage)
	if _, ok := relayRules[match.Code]; !ok {
		match = relayErrorMatch{Code: RelayRuleInternalError, RuleMatched: false, MatchSource: RelayMatchSourceFallback, UnmatchedReason: RelayUnmatchedReasonLocalSystemError}
	}
	return byMatchWithRules(match, rules)
}

func matchCustomRelayError(in RelayErrorInput, rules map[string]effectiveRelayRule) (relayErrorMatch, bool) {
	msg := lowerJoin(in.Message, in.Param, in.Code, in.Type)
	if strings.TrimSpace(msg) == "" {
		return relayErrorMatch{}, false
	}
	for code, rule := range rules {
		if !rule.Custom || !rule.Enabled || rule.MatchPattern == "" {
			continue
		}
		switch rule.MatchType {
		case "contains":
			if strings.Contains(msg, strings.ToLower(rule.MatchPattern)) {
				return matched(code, RelayMatchSourceCustomRule), true
			}
		case "regex":
			re, err := regexp.Compile(rule.MatchPattern)
			if err == nil && re.MatchString(msg) {
				return matched(code, RelayMatchSourceCustomRule), true
			}
		}
	}
	return relayErrorMatch{}, false
}

func classifyMatchWithContext(in RelayErrorInput, source string, stage string) relayErrorMatch {
	statusCode := in.StatusCode
	if statusCode == 0 {
		statusCode = http.StatusInternalServerError
	}
	upstream := isUpstreamErrorInput(in)

	if strings.TrimSpace(in.Message) == "" && strings.TrimSpace(in.Code) == "" && strings.TrimSpace(in.Type) == "" && strings.TrimSpace(in.Param) == "" {
		return unmatched(RelayUnmatchedReasonEmptyError)
	}
	msg := lowerJoin(in.Message, in.Param, in.Code, in.Type)
	if !upstream && isUpstreamErrorInsightContext(source, stage) && isBareEOFErrorMessage(in.Message) {
		return matched(RelayRuleStreamInterrupted, RelayMatchSourceKeyword)
	}
	if containsAny(msg, "requests with max_tokens > 4096 must have stream=true") {
		return matched(RelayRuleMaxTokensNeedStream, RelayMatchSourceKeyword)
	}

	switch in.Code {
	case RelayRuleInsufficientUserQuota:
		return matched(RelayRuleInsufficientUserQuota, RelayMatchSourceExactCode)
	case "budget_tokens_too_small", "budget_tokens_too_large":
		return matched(RelayRuleInvalidBudgetTokens, RelayMatchSourceExactCode)
	case "image_url_invalid":
		return matched(RelayRuleInvalidImageURL, RelayMatchSourceExactCode)
	case RelayRuleStreamInterrupted:
		return matched(RelayRuleStreamInterrupted, RelayMatchSourceExactCode)
	case "risk_control_error":
		return matched(RelayRuleRiskControlRestricted, RelayMatchSourceExactCode)
	case "behavior_banned", "behavior_detection_error":
		return matched(RelayRuleBehaviorBanned, RelayMatchSourceExactCode)
	case RelayRuleModelNotFound:
		return matched(RelayRuleModelNotFound, RelayMatchSourceExactCode)
	case RelayRuleUpstreamUnavailable:
		return matched(RelayRuleUpstreamUnavailable, RelayMatchSourceExactCode)
	}

	switch in.Type {
	case "insufficient_quota_error":
		if !upstream {
			return matched(RelayRuleInsufficientUserQuota, RelayMatchSourceExactType)
		}
	case "risk_control_error":
		return matched(RelayRuleRiskControlRestricted, RelayMatchSourceExactType)
	case "behavior_banned":
		return matched(RelayRuleBehaviorBanned, RelayMatchSourceExactType)
	case "content_policy_violation", "content_filter":
		return matched(RelayRuleContentFiltered, RelayMatchSourceExactType)
	}

	switch in.Param {
	case "max_tokens":
		if !containsAny(msg, "budget_token") {
			return matched(RelayRuleInvalidMaxTokens, RelayMatchSourceExactParam)
		}
	case "stream_options":
		return matched(RelayRuleInvalidStreamOptions, RelayMatchSourceExactParam)
	case "messages.role", "message.role", "role":
		return matched(RelayRuleInvalidMessageRole, RelayMatchSourceExactParam)
	case "reasoning.budget_tokens", "budget_tokens":
		return matched(RelayRuleInvalidBudgetTokens, RelayMatchSourceExactParam)
	case "image_url":
		return matched(RelayRuleInvalidImageURL, RelayMatchSourceExactParam)
	}

	if containsAny(msg, "content_filter", "content filter", "safety", "blocked by safety", "content policy", "policy violation", "prohibited", "unsafe content", "content you provided", "machine outputted is blocked", "provided or machine outputted is blocked", "outputted is blocked", "内容安全", "安全策略拦截", "内容被屏蔽", "输出被屏蔽", "审查屏蔽") {
		return matched(RelayRuleContentFiltered, RelayMatchSourceKeyword)
	}
	if containsAny(msg, "input tag", "found using 'role'", "found using role", "expected tags", "does not match any of the expected tags") && containsAny(msg, "system", "user", "assistant", "tool", "role") {
		return matched(RelayRuleInvalidMessageRole, RelayMatchSourceKeyword)
	}
	if containsAny(msg, "context length", "context_length", "context window", "token limit", "maximum context", "too many tokens", "tokens exceeds", "exceeded token", "number of input tokens", "max_prompt_tokens", "input tokens", "prompt tokens", "上下文超限", "超过模型上下文", "token 限制") && containsAny(msg, "context", "token", "tokens", "max_prompt_tokens", "输入") {
		return matched(RelayRuleContextLengthExceeded, RelayMatchSourceKeyword)
	}
	if containsAny(msg, "is not supported for current token", "no available models configured for current token") {
		return matched(RelayRuleModelNotPermitted, RelayMatchSourceKeyword)
	}
	if containsAny(msg, "no available channel", "no channel", "无可用渠道", "当前分组", "能力无通道", "数据库一致性已被破坏") {
		return matched(RelayRuleNoAvailableChannel, RelayMatchSourceKeyword)
	}
	if containsAny(msg, "model not available", "model unavailable", "model_not_found", "model not found", "model disabled", "unsupported model", "模型不可用") {
		return matched(RelayRuleModelNotFound, RelayMatchSourceKeyword)
	}
	if containsAny(msg, "stream interrupted", "stream closed", "stream reset", "eof during stream", "client disconnected") {
		return matched(RelayRuleStreamInterrupted, RelayMatchSourceKeyword)
	}

	if upstream {
		if isBareEOFErrorMessage(in.Message) {
			return matched(RelayRuleStreamInterrupted, RelayMatchSourceKeyword)
		}
		if containsAny(msg,
			"invalid character",
			"looking for beginning of value",
			"cannot unmarshal",
			"json: cannot unmarshal",
			"unexpected end of json input",
			"unexpected eof",
			"decode_response_failed",
			"decode response failed",
			"bad_response_status_code",
		) && containsAny(msg,
			"looking for beginning of value",
			"cannot unmarshal",
			"unexpected end of json input",
			"unexpected eof",
			"decode_response_failed",
			"decode response failed",
			"bad_response_status_code",
		) {
			return matched(RelayRuleUpstreamBadResponse, RelayMatchSourceKeyword)
		}
		if containsAny(msg,
			"not enough credits", "insufficient credits", "insufficient credit", "no credits",
			"credit balance", "credits exhausted", "quota exhausted", "billing quota",
			"insufficient quota", "exceeded your current quota", "account balance",
			"balance insufficient", "余额不足", "额度不足",
			"provider api error: openai_error",
		) {
			return matched(RelayRuleUpstreamUnavailable, RelayMatchSourceKeyword)
		}
		if statusCode == http.StatusTooManyRequests {
			return matched(RelayRuleUpstreamRateLimited, RelayMatchSourceStatusCode)
		}
		if statusCode == http.StatusRequestTimeout || statusCode == http.StatusGatewayTimeout || statusCode == 524 {
			return matched(RelayRuleUpstreamTimeout, RelayMatchSourceStatusCode)
		}
		if containsAny(msg, "timeout", "timed out", "deadline exceeded", "context deadline", "i/o timeout") {
			return matched(RelayRuleUpstreamTimeout, RelayMatchSourceKeyword)
		}
		return relayErrorMatch{Code: RelayRuleInternalError, RuleMatched: false, MatchSource: RelayMatchSourceFallback, UnmatchedReason: RelayUnmatchedReasonOpaqueUpstreamError}
	}

	return relayErrorMatch{Code: RelayRuleInternalError, RuleMatched: false, MatchSource: RelayMatchSourceFallback, UnmatchedReason: RelayUnmatchedReasonLocalSystemError}
}

func isBareEOFErrorMessage(message string) bool {
	normalized := strings.ToLower(strings.TrimSpace(message))
	normalized = strings.Trim(normalized, `"'`)
	if normalized == "eof" {
		return true
	}
	return strings.HasSuffix(normalized, ": eof") && !strings.Contains(normalized, "unexpected eof") && !strings.Contains(normalized, "eof during stream")
}

func isUpstreamErrorInsightContext(source string, stage string) bool {
	ctx := lowerJoin(source, stage)
	return containsAny(ctx, "upstream_response", "upstream_error", "relay_response", "stream")
}

func matched(code string, source string) relayErrorMatch {
	return relayErrorMatch{Code: code, RuleMatched: true, MatchSource: source, UnmatchedReason: RelayUnmatchedReasonNone}
}

func unmatched(reason string) relayErrorMatch {
	return relayErrorMatch{Code: RelayRuleInternalError, RuleMatched: false, MatchSource: RelayMatchSourceFallback, UnmatchedReason: reason}
}

func byMatch(match relayErrorMatch) Classification {
	return byMatchWithRules(match, defaultEffectiveRules())
}

func byMatchWithRules(match relayErrorMatch, rules map[string]effectiveRelayRule) Classification {
	if rule, ok := rules[match.Code]; ok {
		return Classification{
			AdviceCode:      rule.Code,
			StatusCode:      rule.Status,
			Type:            rule.Type,
			Code:            rule.Code,
			Param:           rule.Param,
			RuleMatched:     match.RuleMatched,
			MatchSource:     match.MatchSource,
			UnmatchedReason: match.UnmatchedReason,
			RuleVersion:     RelayErrorGovernanceRuleVersion,
		}
	}
	rule, ok := relayRules[match.Code]
	if !ok {
		rule = relayRules[RelayRuleInternalError]
		match.Code = RelayRuleInternalError
		match.RuleMatched = false
		match.MatchSource = RelayMatchSourceFallback
		match.UnmatchedReason = RelayUnmatchedReasonLocalSystemError
	}
	return Classification{
		AdviceCode:      rule.Code,
		StatusCode:      rule.Status,
		Type:            rule.Type,
		Code:            rule.Code,
		Param:           rule.Param,
		RuleMatched:     match.RuleMatched,
		MatchSource:     match.MatchSource,
		UnmatchedReason: match.UnmatchedReason,
		RuleVersion:     RelayErrorGovernanceRuleVersion,
	}
}

// --- Rule resolution helpers ---

type effectiveRelayRule struct {
	Enabled      bool
	Code         string
	Status       int
	Type         string
	Param        string
	Message      string
	Custom       bool
	MatchType    string
	MatchPattern string
}

func defaultEffectiveRules() map[string]effectiveRelayRule {
	rules := make(map[string]effectiveRelayRule, len(relayRules))
	for _, code := range relayRuleOrder {
		rule, ok := relayRules[code]
		if !ok {
			continue
		}
		rules[code] = effectiveRelayRule{Enabled: true, Code: rule.Code, Status: rule.Status, Type: rule.Type, Param: rule.Param, Message: rule.Message}
	}
	return rules
}

func normalizedStatusFor(code string, fallback int, clsStatus int, rules map[string]effectiveRelayRule) int {
	if rule, ok := rules[code]; ok && rule.Status > 0 {
		return rule.Status
	}
	if fallback != 0 {
		return fallback
	}
	return relayRules[RelayRuleInternalError].Status
}

func normalizedStatusFallback(in RelayErrorInput) int {
	if in.StatusCode != 0 {
		return in.StatusCode
	}
	return relayRules[RelayRuleInternalError].Status
}

func ruleType(code string, rules map[string]effectiveRelayRule) string {
	if rule, ok := rules[code]; ok && rule.Type != "" {
		return rule.Type
	}
	return relayRules[RelayRuleInternalError].Type
}

func ruleParam(code string, rules map[string]effectiveRelayRule) string {
	if rule, ok := rules[code]; ok {
		return rule.Param
	}
	return ""
}

func effectiveMessage(code string, rules map[string]effectiveRelayRule) string {
	if rule, ok := rules[code]; ok && rule.Message != "" {
		return rule.Message
	}
	if rule, ok := relayRules[code]; ok {
		return rule.Message
	}
	return relayRules[RelayRuleInternalError].Message
}

// --- Request ID handling ---

var upstreamRequestIDPattern = regexp.MustCompile(`(?i)\s*\(?\b(?:request|req|trace|correlation|x-request)[-_ ]?id\s*[:=]\s*[^\s\)\]\}]+\)?`)

func withLocalRequestID(c *gin.Context, message string) string {
	message = upstreamRequestIDPattern.ReplaceAllString(message, "")
	message = strings.TrimSpace(message)
	if c == nil {
		return message
	}
	return common.MessageWithRequestId(message, c.GetString(common.RequestIdKey))
}

// --- Analytics normalization (for EG-2 error insight) ---

var analyticsURLPattern = regexp.MustCompile(`(?i)\b(?:https?|wss?|ftp)://[^\s\)\]\}"']+`)
var analyticsEmailPattern = regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`)
var analyticsSecretPattern = regexp.MustCompile(`(?i)\b(?:bearer\s+)?(?:sk|rk|ak|api[_-]?key|secret)[-_a-z0-9]{8,}\b`)
var analyticsRequestIDPattern = regexp.MustCompile(`(?i)\b(?:request|req|trace|correlation|x-request)[-_ ]?id\s*[:=]?\s*[a-z0-9][a-z0-9._:\-]{5,}`)
var analyticsResourcePathPattern = regexp.MustCompile(`(?i)\b(?:accounts|projects|organizations|orgs)/[^\s\)\]\}"']+`)
var analyticsUUIDPattern = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
var analyticsLongHexPattern = regexp.MustCompile(`(?i)\b[0-9a-f]{16,}\b`)
var analyticsIPPattern = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
var analyticsNumberPattern = regexp.MustCompile(`\b\d+(?:\.\d+)?\b`)
var analyticsWhitespacePattern = regexp.MustCompile(`\s+`)
var sensitiveContextPattern = regexp.MustCompile(`(?i)\b(?:prompt|messages|request\s+body|body|payload|input|content|authorization|bearer|api\s*key|cookie)\b`)
var safeAnalyticsTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_.\-]+$`)

const safeAnalyticsTokenMaxLen = 100

func normalizeRelayErrorMessage(in RelayErrorInput) string {
	msg := strings.TrimSpace(in.Message)
	if msg != "" && sensitiveContextPattern.MatchString(msg) {
		parts := make([]string, 0, 3)
		if code := sanitizeAnalyticsToken(in.Code); code != "" {
			parts = append(parts, "code="+code)
		}
		if typ := sanitizeAnalyticsToken(in.Type); typ != "" {
			parts = append(parts, "type="+typ)
		}
		if param := sanitizeAnalyticsToken(in.Param); param != "" {
			parts = append(parts, "param="+param)
		}
		if len(parts) == 0 {
			return "<redacted_error_message>"
		}
		return "<redacted_error_message> " + strings.Join(parts, " ")
	}

	parts := make([]string, 0, 4)
	if msg != "" {
		parts = append(parts, msg)
	}
	if code := sanitizeAnalyticsToken(in.Code); code != "" {
		parts = append(parts, "code="+code)
	}
	if typ := sanitizeAnalyticsToken(in.Type); typ != "" {
		parts = append(parts, "type="+typ)
	}
	if param := sanitizeAnalyticsToken(in.Param); param != "" {
		parts = append(parts, "param="+param)
	}
	if len(parts) == 0 {
		return "<empty_error>"
	}

	normalized := strings.Join(parts, " ")
	replacements := []struct {
		pattern *regexp.Regexp
		value   string
	}{
		{analyticsURLPattern, "<url>"},
		{analyticsEmailPattern, "<email>"},
		{analyticsSecretPattern, "<secret>"},
		{analyticsRequestIDPattern, "<request_id>"},
		{analyticsResourcePathPattern, "<resource_path>"},
		{analyticsUUIDPattern, "<id>"},
		{analyticsLongHexPattern, "<id>"},
		{analyticsIPPattern, "<ip>"},
		{analyticsNumberPattern, "<num>"},
	}
	for _, replacement := range replacements {
		normalized = replacement.pattern.ReplaceAllString(normalized, replacement.value)
	}
	normalized = analyticsWhitespacePattern.ReplaceAllString(normalized, " ")
	normalized = strings.TrimSpace(normalized)
	if normalized == "" {
		normalized = "<empty_error>"
	}
	const maxNormalizedMessageLen = 1000
	if len(normalized) > maxNormalizedMessageLen {
		normalized = strings.TrimSpace(normalized[:maxNormalizedMessageLen])
	}
	return normalized
}

func sanitizeAnalyticsToken(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) > safeAnalyticsTokenMaxLen {
		s = s[:safeAnalyticsTokenMaxLen]
	}
	if !safeAnalyticsTokenPattern.MatchString(s) {
		return "<redacted>"
	}
	return s
}

func errorAnalyticsSignatureSecret() string {
	secret := strings.TrimSpace(common.SessionSecret)
	if secret == "" {
		return "new-api-error-analytics"
	}
	return secret
}

func lowerJoin(parts ...string) string {
	return strings.ToLower(strings.Join(parts, " "))
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

// --- Config toggle ---

// governanceEnabled returns the global governance toggle from the system
// setting. When false, governance falls back to masked original messages
// (pre-existing behavior). Default is true: upstream error details must not
// reach downstream clients.
//
// The RELAY_ERROR_GOVERNANCE_ENABLED=false env var still overrides to disable
// for emergency rollback scenarios.
func governanceEnabled() bool {
	if os.Getenv("RELAY_ERROR_GOVERNANCE_ENABLED") == "false" {
		return false
	}
	cfg := system_setting.GetRelayErrorGovernanceSetting()
	return cfg.Enabled
}

// effectiveRules merges the built-in default rules with admin-configured
// overrides from the system setting. Only Enabled and Message can be
// overridden — Status/Type/Code/Param are fixed in code for security.
func effectiveRules() map[string]effectiveRelayRule {
	rules := defaultEffectiveRules()
	cfg := system_setting.GetRelayErrorGovernanceSetting()
	if cfg == nil {
		return rules
	}
	if cfg.Rules != nil {
		for code, override := range cfg.Rules {
			rule, ok := rules[code]
			if !ok {
				continue
			}
			if override.Enabled != nil {
				rule.Enabled = *override.Enabled
			}
			if msg := safeRuleMessage(override.Message); msg != "" {
				rule.Message = msg
			}
			rules[code] = rule
		}
	}
	for _, custom := range cfg.CustomRules {
		code := safeCustomRuleCode(custom.RuleCode)
		if code == "" || strings.TrimSpace(custom.MatchPattern) == "" {
			continue
		}
		matchType := strings.TrimSpace(custom.MatchType)
		if matchType != "contains" && matchType != "regex" {
			continue
		}
		if matchType == "regex" {
			if _, err := regexp.Compile(custom.MatchPattern); err != nil {
				continue
			}
		}
		message := safeRuleMessage(custom.SafeErrorMessage)
		if message == "" {
			continue
		}
		status := custom.StatusCode
		if status < 400 || status > 599 {
			status = http.StatusServiceUnavailable
		}
		rules[code] = effectiveRelayRule{
			Enabled:      custom.Enabled,
			Code:         code,
			Status:       status,
			Type:         safeCustomErrorToken(custom.SafeErrorType, relayRules[RelayRuleInternalError].Type),
			Message:      message,
			Custom:       true,
			MatchType:    matchType,
			MatchPattern: strings.TrimSpace(custom.MatchPattern),
		}
	}
	return rules
}

func safeCustomRuleCode(code string) string {
	code = strings.TrimSpace(code)
	if code == "" || len(code) > 80 {
		return ""
	}
	if !safeAnalyticsTokenPattern.MatchString(code) {
		return ""
	}
	return code
}

func safeCustomErrorToken(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > safeAnalyticsTokenMaxLen || !safeAnalyticsTokenPattern.MatchString(value) {
		return fallback
	}
	return value
}

// safeRuleMessage validates that an admin-provided message override is safe:
// non-empty, reasonable length, and does not contain template placeholders
// that could leak original error text.
func safeRuleMessage(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ""
	}
	if len(msg) > 500 {
		msg = msg[:500]
	}
	// Reject messages containing template placeholders that could inject
	// original error content into the client response.
	if strings.Contains(msg, "{original") || strings.Contains(msg, "{upstream") {
		return ""
	}
	return msg
}

// --- Stream error sanitization ---

// SanitizedStreamErrorMessage classifies a stream-mid error and returns the
// safe message + rule code. Used by stream handlers that need to emit an SSE
// error chunk after headers are already sent.
func SanitizedStreamErrorMessage(c *gin.Context, err error) (message, code string) {
	in := RelayErrorInput{
		Message:    "",
		Type:       relayRules[RelayRuleStreamInterrupted].Type,
		Code:       RelayRuleStreamInterrupted,
		StatusCode: http.StatusServiceUnavailable,
	}
	if err != nil {
		in.Message = err.Error()
	}
	cls := ClassifyRelayError(in)
	codeStr := cls.Code
	if codeStr == "" {
		codeStr = RelayRuleStreamInterrupted
	}
	return withLocalRequestID(c, effectiveMessage(codeStr, effectiveRules())), codeStr
}
