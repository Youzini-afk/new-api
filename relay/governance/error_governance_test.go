package governance

import (
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
)

func TestClassifyRelayError(t *testing.T) {
	tests := []struct {
		name        string
		in          RelayErrorInput
		wantCode    string
		wantMatched bool
	}{
		{
			name:     "insufficient_user_quota by code",
			in:       RelayErrorInput{Code: RelayRuleInsufficientUserQuota, Message: "quota exceeded"},
			wantCode: RelayRuleInsufficientUserQuota, wantMatched: true,
		},
		{
			name:     "context_length_exceeded by keyword",
			in:       RelayErrorInput{Message: "This model's maximum context length is 128000 tokens."},
			wantCode: RelayRuleContextLengthExceeded, wantMatched: true,
		},
		{
			name:     "content_filtered by keyword",
			in:       RelayErrorInput{Message: "content filter blocked the response"},
			wantCode: RelayRuleContentFiltered, wantMatched: true,
		},
		{
			name:     "upstream rate limited by status code",
			in:       RelayErrorInput{StatusCode: http.StatusTooManyRequests, ErrorCode: types.ErrorCodeBadResponseStatusCode, Message: "rate limited"},
			wantCode: RelayRuleUpstreamRateLimited, wantMatched: true,
		},
		{
			name:     "upstream timeout by status code",
			in:       RelayErrorInput{StatusCode: http.StatusGatewayTimeout, ErrorCode: types.ErrorCodeBadResponseStatusCode, Message: "gateway timeout"},
			wantCode: RelayRuleUpstreamTimeout, wantMatched: true,
		},
		{
			name:     "upstream timeout by keyword",
			in:       RelayErrorInput{ErrorCode: types.ErrorCodeBadResponseStatusCode, Message: "context deadline exceeded", StatusCode: 500},
			wantCode: RelayRuleUpstreamTimeout, wantMatched: true,
		},
		{
			name:     "max_tokens needs stream keyword",
			in:       RelayErrorInput{Message: "requests with max_tokens > 4096 must have stream=true"},
			wantCode: RelayRuleMaxTokensNeedStream, wantMatched: true,
		},
		{
			name:     "invalid message role by param",
			in:       RelayErrorInput{Param: "messages.role", Message: "bad role"},
			wantCode: RelayRuleInvalidMessageRole, wantMatched: true,
		},
		{
			name:     "empty error unmatched",
			in:       RelayErrorInput{},
			wantCode: RelayRuleInternalError, wantMatched: false,
		},
		{
			name:     "opaque upstream error unmatched",
			in:       RelayErrorInput{ErrorCode: types.ErrorCodeBadResponseStatusCode, Message: "some weird upstream error", StatusCode: 502},
			wantCode: RelayRuleInternalError, wantMatched: false,
		},
		{
			name:     "local system error unmatched",
			in:       RelayErrorInput{Message: "some local error", ErrorCode: types.ErrorCodeInvalidRequest, StatusCode: 400},
			wantCode: RelayRuleInternalError, wantMatched: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cls := ClassifyRelayError(tt.in)
			assert.Equal(t, tt.wantCode, cls.Code)
			assert.Equal(t, tt.wantMatched, cls.RuleMatched)
		})
	}
}

func TestSanitizeRelayErrorForClientDoesNotLeakUpstreamMessage(t *testing.T) {
	// An opaque upstream error should be replaced by the governance message,
	// never the raw upstream text. The error code "bad_response_status_code"
	// causes classification as upstream_bad_response (502), which is correct:
	// the upstream returned a non-200 status.
	upstreamErr := types.NewOpenAIError(
		typesErrorWithBody("Request xJ4k9s failed: your API key sk-abc123def456 is invalid, account billing_id=acct_987654321"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusBadGateway,
	)

	safe := SanitizeRelayErrorForClient(nil, upstreamErr)
	assert.NotContains(t, safe.OpenAIError.Message, "sk-abc123def456")
	assert.NotContains(t, safe.OpenAIError.Message, "acct_987654321")
	assert.NotContains(t, safe.OpenAIError.Message, "xJ4k9s")
	assert.NotContains(t, safe.OpenAIError.Message, "Request")
	assert.Equal(t, RelayRuleUpstreamBadResponse, safe.OpenAIError.Code)
	assert.Equal(t, http.StatusBadGateway, safe.StatusCode)
}

func TestSanitizeRelayErrorForClientPreservesLocalActionableMessage(t *testing.T) {
	// A local quota error should get the actionable rule message, not a
	// generic "internal error".
	quotaErr := types.NewOpenAIError(
		typesErrorWithBody("user quota exhausted, balance: 0"),
		types.ErrorCodeInsufficientUserQuota,
		http.StatusPaymentRequired,
	)
	quotaErr.SetMessage("user quota exhausted")

	safe := SanitizeRelayErrorForClient(nil, quotaErr)
	assert.Contains(t, safe.OpenAIError.Message, "额度不足")
	assert.Equal(t, http.StatusPaymentRequired, safe.StatusCode)
	assert.Equal(t, RelayRuleInsufficientUserQuota, safe.OpenAIError.Code)
}

func TestSanitizeRelayErrorForClientNilError(t *testing.T) {
	safe := SanitizeRelayErrorForClient(nil, nil)
	assert.Equal(t, http.StatusInternalServerError, safe.StatusCode)
	assert.NotEmpty(t, safe.OpenAIError.Message)
	assert.Contains(t, safe.OpenAIError.Message, "服务处理请求时发生错误")
}

func TestSanitizeRelayErrorForClientStripsUpstreamRequestID(t *testing.T) {
	err := types.NewOpenAIError(
		typesErrorWithBody("upstream error (request id: req_abc123def456)"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusBadGateway,
	)
	safe := SanitizeRelayErrorForClient(nil, err)
	assert.NotContains(t, safe.OpenAIError.Message, "req_abc123def456")
}

func TestNormalizeRelayErrorMessageRedactsSecrets(t *testing.T) {
	in := RelayErrorInput{
		Message: "auth failed for sk-abcd1234efgh5678 at https://api.example.com/v1/chat",
		Code:    "auth_error",
		Type:    "upstream_error",
	}
	normalized := normalizeRelayErrorMessage(in)
	assert.Contains(t, normalized, "<secret>")
	assert.Contains(t, normalized, "<url>")
	assert.NotContains(t, normalized, "sk-abcd1234efgh5678")
	assert.NotContains(t, normalized, "https://api.example.com/v1/chat")
}

func TestNormalizeRelayErrorMessageRedactsSensitiveContext(t *testing.T) {
	in := RelayErrorInput{
		Message: "prompt: the user said something bad",
	}
	normalized := normalizeRelayErrorMessage(in)
	assert.Contains(t, normalized, "<redacted_error_message>")
	assert.NotContains(t, normalized, "the user said something bad")
}

func TestSanitizeRelayErrorForClientClaudeFormat(t *testing.T) {
	err := types.NewOpenAIError(
		typesErrorWithBody("upstream timeout"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusGatewayTimeout,
	)
	safe := SanitizeRelayErrorForClient(nil, err)
	claude := safe.ClaudeError()
	assert.NotEmpty(t, claude.Message)
	assert.NotEmpty(t, claude.Type)
	assert.NotContains(t, claude.Message, "upstream timeout")
}

func TestSanitizedStreamErrorMessage(t *testing.T) {
	msg, code := SanitizedStreamErrorMessage(nil, nil)
	assert.NotEmpty(t, msg)
	assert.NotEmpty(t, code)
	assert.NotContains(t, msg, "upstream")
}

func TestSafeRuleMessageTruncatesUTF8ByRunes(t *testing.T) {
	message := safeRuleMessage(strings.Repeat("测", system_setting.RelayErrorGovernanceMessageMaxLength+1))
	assert.True(t, utf8.ValidString(message))
	assert.Equal(t, system_setting.RelayErrorGovernanceMessageMaxLength, utf8.RuneCountInString(message))
	assert.Empty(t, safeRuleMessage("{Original_Error}"))
}

func TestCustomRuleSafeErrorCodeControlsClientCode(t *testing.T) {
	withRelayErrorGovernanceCustomRules(t, []system_setting.RelayErrorGovernanceCustomRuleConfig{
		{
			Enabled:          true,
			RuleCode:         "ai_overload_rule",
			MatchType:        "contains",
			MatchPattern:     "provider overloaded uniquely",
			SafeErrorCode:    "upstream_overloaded",
			SafeErrorType:    "service_unavailable",
			SafeErrorMessage: "上游服务繁忙。",
			StatusCode:       http.StatusServiceUnavailable,
		},
	})

	in := RelayErrorInput{Message: "provider overloaded uniquely"}
	classification := ClassifyRelayError(in)
	assert.Equal(t, "ai_overload_rule", classification.AdviceCode)
	assert.Equal(t, "upstream_overloaded", classification.Code)

	err := types.NewOpenAIError(typesErrorWithBody(in.Message), types.ErrorCodeBadResponseStatusCode, http.StatusServiceUnavailable)
	safe := SanitizeRelayErrorForClient(nil, err)
	assert.Equal(t, "upstream_overloaded", safe.OpenAIError.Code)
	assert.Equal(t, "service_unavailable", safe.OpenAIError.Type)
	assert.Equal(t, "上游服务繁忙。", safe.OpenAIError.Message)
	assert.Equal(t, http.StatusServiceUnavailable, safe.StatusCode)
}

func TestCustomRuleSafeErrorCodeDoesNotDriveRuleLookup(t *testing.T) {
	withRelayErrorGovernanceCustomRules(t, []system_setting.RelayErrorGovernanceCustomRuleConfig{
		{
			Enabled:          true,
			RuleCode:         "rule_a",
			MatchType:        "contains",
			MatchPattern:     "match rule a uniquely",
			SafeErrorCode:    "rule_b",
			SafeErrorType:    "a_error",
			SafeErrorMessage: "规则 A 的文案。",
			StatusCode:       http.StatusTeapot,
		},
		{
			Enabled:          true,
			RuleCode:         "rule_b",
			MatchType:        "contains",
			MatchPattern:     "match rule b uniquely",
			SafeErrorCode:    "public_b",
			SafeErrorType:    "b_error",
			SafeErrorMessage: "规则 B 的文案。",
			StatusCode:       http.StatusServiceUnavailable,
		},
	})

	err := types.NewOpenAIError(typesErrorWithBody("match rule a uniquely"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)
	safe := SanitizeRelayErrorForClient(nil, err)
	assert.Equal(t, "rule_b", safe.OpenAIError.Code)
	assert.Equal(t, "a_error", safe.OpenAIError.Type)
	assert.Equal(t, "规则 A 的文案。", safe.OpenAIError.Message)
	assert.Equal(t, http.StatusTeapot, safe.StatusCode)
}

func TestCustomRuleMissingSafeErrorCodeFallsBackToRuleCode(t *testing.T) {
	withRelayErrorGovernanceCustomRules(t, []system_setting.RelayErrorGovernanceCustomRuleConfig{
		{
			Enabled:          true,
			RuleCode:         "legacy_custom_rule",
			MatchType:        "contains",
			MatchPattern:     "legacy custom failure",
			SafeErrorMessage: "自定义错误。",
			SafeErrorType:    "custom_error",
			StatusCode:       http.StatusBadRequest,
		},
	})

	classification := ClassifyRelayError(RelayErrorInput{Message: "legacy custom failure"})
	assert.Equal(t, "legacy_custom_rule", classification.AdviceCode)
	assert.Equal(t, "legacy_custom_rule", classification.Code)
}

func TestLegacyCustomRuleCannotOverrideBuiltinRule(t *testing.T) {
	withRelayErrorGovernanceCustomRules(t, []system_setting.RelayErrorGovernanceCustomRuleConfig{
		{
			Enabled:          true,
			RuleCode:         RelayRuleInternalError,
			MatchType:        "contains",
			MatchPattern:     "legacy override attempt",
			SafeErrorCode:    "attacker_controlled_code",
			SafeErrorType:    "attacker_controlled_type",
			SafeErrorMessage: "不应出现的文案。",
			StatusCode:       http.StatusTeapot,
		},
	})

	classification := ClassifyRelayError(RelayErrorInput{Message: "legacy override attempt"})
	assert.Equal(t, RelayRuleInternalError, classification.AdviceCode)
	assert.Equal(t, RelayRuleInternalError, classification.Code)
	assert.False(t, classification.RuleMatched)
}

func TestAnalyzeRelayErrorFingerprintUsesInternalRuleCode(t *testing.T) {
	rules := []system_setting.RelayErrorGovernanceCustomRuleConfig{
		{
			Enabled:          true,
			RuleCode:         "stable_internal_rule",
			MatchType:        "contains",
			MatchPattern:     "stable fingerprint failure",
			SafeErrorCode:    "public_code_v1",
			SafeErrorType:    "custom_error",
			SafeErrorMessage: "安全错误。",
			StatusCode:       http.StatusBadRequest,
		},
	}
	withRelayErrorGovernanceCustomRules(t, rules)
	in := RelayErrorInput{Message: "stable fingerprint failure"}
	first := AnalyzeRelayError(in, "relay", "response")

	rules[0].SafeErrorCode = "public_code_v2"
	system_setting.PublishRelayErrorGovernanceRuntime(system_setting.RelayErrorGovernanceSetting{
		Enabled:     true,
		CustomRules: rules,
	})
	second := AnalyzeRelayError(in, "relay", "response")
	assert.Equal(t, "stable_internal_rule", first.AdviceCode)
	assert.Equal(t, "stable_internal_rule", second.AdviceCode)
	assert.Equal(t, "public_code_v1", first.Code)
	assert.Equal(t, "public_code_v2", second.Code)
	assert.Equal(t, first.NormalizedSignature, second.NormalizedSignature)
}

func withRelayErrorGovernanceCustomRules(t *testing.T, rules []system_setting.RelayErrorGovernanceCustomRuleConfig) {
	t.Helper()
	cfg := system_setting.GetRelayErrorGovernanceSetting()
	original := *cfg
	t.Cleanup(func() {
		system_setting.PublishRelayErrorGovernanceRuntime(original)
	})
	system_setting.PublishRelayErrorGovernanceRuntime(system_setting.RelayErrorGovernanceSetting{
		Enabled:     true,
		CustomRules: rules,
	})
}

// typesErrorWithBody is a minimal error type for test inputs.
type typesErrorWithBody string

func (e typesErrorWithBody) Error() string { return string(e) }
