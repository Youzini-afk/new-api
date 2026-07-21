package system_setting

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validRelayErrorGovernanceCustomRule() RelayErrorGovernanceCustomRuleConfig {
	return RelayErrorGovernanceCustomRuleConfig{
		Enabled:          true,
		RuleCode:         "custom_validation_rule",
		Category:         "parameter_validation",
		MatchType:        "contains",
		MatchPattern:     "invalid parameter",
		SafeErrorMessage: "请求参数无效。",
	}
}

func TestNormalizeRelayErrorGovernanceCustomRuleInfersStatusAndType(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*RelayErrorGovernanceCustomRuleConfig)
		wantStatus int
		wantType   string
	}{
		{name: "parameter validation", wantStatus: http.StatusBadRequest, wantType: "invalid_request_error"},
		{name: "upstream rate limit", mutate: func(rule *RelayErrorGovernanceCustomRuleConfig) {
			rule.RuleCode = "provider_rate_limit"
			rule.Category = "upstream_rate_limit"
			rule.MatchPattern = "provider rate limit exceeded"
		}, wantStatus: http.StatusServiceUnavailable, wantType: "service_unavailable"},
		{name: "upstream timeout", mutate: func(rule *RelayErrorGovernanceCustomRuleConfig) {
			rule.RuleCode = "provider_timeout"
			rule.Category = "upstream_timeout"
			rule.MatchPattern = "deadline exceeded"
		}, wantStatus: http.StatusGatewayTimeout, wantType: "service_unavailable"},
		{name: "bad upstream response", mutate: func(rule *RelayErrorGovernanceCustomRuleConfig) {
			rule.RuleCode = "provider_bad_response"
			rule.Category = "upstream_response"
			rule.MatchPattern = "unexpected eof"
		}, wantStatus: http.StatusBadGateway, wantType: "service_unavailable"},
		{name: "quota", mutate: func(rule *RelayErrorGovernanceCustomRuleConfig) {
			rule.RuleCode = "custom_quota"
			rule.Category = "quota"
			rule.SafeErrorCode = "insufficient_quota"
		}, wantStatus: http.StatusPaymentRequired, wantType: "insufficient_quota_error"},
		{name: "access denied", mutate: func(rule *RelayErrorGovernanceCustomRuleConfig) {
			rule.RuleCode = "custom_access_denied"
			rule.Category = "access_control"
			rule.SafeErrorCode = "access_denied"
		}, wantStatus: http.StatusForbidden, wantType: "permission_denied"},
		{name: "content safety block", mutate: func(rule *RelayErrorGovernanceCustomRuleConfig) {
			rule.RuleCode = "custom_content_safety"
			rule.Category = "content_policy"
			rule.MatchPattern = "request blocked by content safety"
		}, wantStatus: http.StatusBadRequest, wantType: "invalid_request_error"},
		{name: "ua blocked", mutate: func(rule *RelayErrorGovernanceCustomRuleConfig) {
			rule.RuleCode = "custom_ua_blocked"
			rule.Category = "access_control"
			rule.MatchPattern = "ua_blocked_lobster_client"
		}, wantStatus: http.StatusForbidden, wantType: "permission_denied"},
		{name: "local rate limit", mutate: func(rule *RelayErrorGovernanceCustomRuleConfig) {
			rule.RuleCode = "user_rate_limit"
			rule.Category = "rate_limit"
			rule.MatchPattern = "per-user rate limit"
		}, wantStatus: http.StatusTooManyRequests, wantType: "rate_limit_error"},
		{name: "unknown system error", mutate: func(rule *RelayErrorGovernanceCustomRuleConfig) {
			rule.RuleCode = "unknown_failure"
			rule.Category = "unknown"
			rule.MatchPattern = "opaque failure"
		}, wantStatus: http.StatusInternalServerError, wantType: "system_error"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rule := validRelayErrorGovernanceCustomRule()
			if test.mutate != nil {
				test.mutate(&rule)
			}
			normalized, err := NormalizeRelayErrorGovernanceCustomRule(rule)
			require.NoError(t, err)
			assert.Equal(t, test.wantStatus, normalized.StatusCode)
			assert.Equal(t, test.wantType, normalized.SafeErrorType)
			assert.NotEmpty(t, normalized.SafeErrorCode)
		})
	}
}

func TestNormalizeRelayErrorGovernanceCustomRulePreservesExplicitStatus(t *testing.T) {
	rule := validRelayErrorGovernanceCustomRule()
	rule.StatusCode = http.StatusTeapot
	rule.SafeErrorType = "custom_error"

	normalized, err := NormalizeRelayErrorGovernanceCustomRule(rule)
	require.NoError(t, err)
	assert.Equal(t, http.StatusTeapot, normalized.StatusCode)
	assert.Equal(t, "custom_error", normalized.SafeErrorType)
}

func TestNormalizeRelayErrorGovernanceCustomRuleRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*RelayErrorGovernanceCustomRuleConfig)
		match  string
	}{
		{name: "built-in conflict", mutate: func(rule *RelayErrorGovernanceCustomRuleConfig) { rule.RuleCode = RelayErrorRuleInternalError }, match: "built-in"},
		{name: "invalid regex", mutate: func(rule *RelayErrorGovernanceCustomRuleConfig) { rule.MatchType = "regex"; rule.MatchPattern = "(" }, match: "valid Go regex"},
		{name: "invalid status", mutate: func(rule *RelayErrorGovernanceCustomRuleConfig) { rule.StatusCode = 200 }, match: "400 and 599"},
		{name: "invalid safe code", mutate: func(rule *RelayErrorGovernanceCustomRuleConfig) { rule.SafeErrorCode = "invalid code" }, match: "safe_error_code"},
		{name: "unsafe message", mutate: func(rule *RelayErrorGovernanceCustomRuleConfig) { rule.SafeErrorMessage = "failed: {original_error}" }, match: "placeholders"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rule := validRelayErrorGovernanceCustomRule()
			test.mutate(&rule)
			_, err := NormalizeRelayErrorGovernanceCustomRule(rule)
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.match)
		})
	}
}

func TestNormalizeRelayErrorGovernanceSettingRejectsDuplicatesAndUnknownOverrides(t *testing.T) {
	rule := validRelayErrorGovernanceCustomRule()
	_, err := NormalizeRelayErrorGovernanceSetting(RelayErrorGovernanceSetting{
		Enabled:     true,
		CustomRules: []RelayErrorGovernanceCustomRuleConfig{rule, rule},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")

	_, err = NormalizeRelayErrorGovernanceSetting(RelayErrorGovernanceSetting{
		Enabled: true,
		Rules: map[string]RelayErrorGovernanceRuleConfig{
			"unknown_rule": {Message: "safe"},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown built-in")
}

func TestParseAndValidateRelayErrorGovernanceSettingNormalizesCustomRule(t *testing.T) {
	input := `{"enabled":true,"custom_rules":[{"enabled":true,"rule_code":"custom_rule","category":"parameter_validation","match_type":"CONTAINS","match_pattern":" invalid parameter ","safe_error_code":"","safe_error_type":"","safe_error_message":" 请求参数无效。 "}]}`
	cfg, normalizedJSON, err := ParseAndValidateRelayErrorGovernanceSetting(input)
	require.NoError(t, err)
	require.Len(t, cfg.CustomRules, 1)
	assert.Equal(t, "contains", cfg.CustomRules[0].MatchType)
	assert.Equal(t, "invalid parameter", cfg.CustomRules[0].MatchPattern)
	assert.Equal(t, "custom_rule", cfg.CustomRules[0].SafeErrorCode)
	assert.Equal(t, http.StatusBadRequest, cfg.CustomRules[0].StatusCode)
	assert.Contains(t, normalizedJSON, `"status_code":400`)
}

func TestDefaultErrorGovernancePromptsRequireStatusClassification(t *testing.T) {
	assert.Contains(t, DefaultErrorInsightAIPromptTemplate, "400-599")
	assert.Contains(t, DefaultErrorInsightAIPromptTemplate, "不得为了方便把所有规则统一写成 503")
	assert.Contains(t, DefaultErrorGovernanceAIPromptTemplate, "400-599")
	assert.Contains(t, DefaultErrorGovernanceAIPromptTemplate, "rule_code 必须唯一")
}
