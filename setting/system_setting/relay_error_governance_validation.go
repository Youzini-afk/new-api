package system_setting

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	RelayErrorGovernanceTokenMaxLength    = 64
	RelayErrorGovernanceMessageMaxLength  = 500
	relayErrorGovernancePatternMaxLength  = 1000
	relayErrorGovernanceCategoryMaxLength = 128
)

var relayErrorGovernanceTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

var relayErrorGovernanceBuiltinRuleCodes = map[string]struct{}{
	RelayErrorRuleInsufficientUserQuota: {},
	RelayErrorRuleInvalidMaxTokens:      {},
	RelayErrorRuleMaxTokensNeedStream:   {},
	RelayErrorRuleInvalidBudgetTokens:   {},
	RelayErrorRuleInvalidStreamOptions:  {},
	RelayErrorRuleInvalidMessageRole:    {},
	RelayErrorRuleInvalidImageURL:       {},
	RelayErrorRuleContextLengthExceeded: {},
	RelayErrorRuleContentFiltered:       {},
	RelayErrorRuleModelNotFound:         {},
	RelayErrorRuleNoAvailableChannel:    {},
	RelayErrorRuleModelNotPermitted:     {},
	RelayErrorRuleRiskControlRestricted: {},
	RelayErrorRuleBehaviorBanned:        {},
	RelayErrorRuleUpstreamRateLimited:   {},
	RelayErrorRuleUpstreamTimeout:       {},
	RelayErrorRuleUpstreamBadResponse:   {},
	RelayErrorRuleUpstreamUnavailable:   {},
	RelayErrorRuleStreamInterrupted:     {},
	RelayErrorRuleInternalError:         {},
}

func IsBuiltinRelayErrorGovernanceRuleCode(code string) bool {
	_, ok := relayErrorGovernanceBuiltinRuleCodes[strings.TrimSpace(code)]
	return ok
}

func NormalizeRelayErrorGovernanceCustomRule(input RelayErrorGovernanceCustomRuleConfig) (RelayErrorGovernanceCustomRuleConfig, error) {
	ruleCode := strings.TrimSpace(input.RuleCode)
	if err := validateRelayErrorGovernanceToken("rule_code", ruleCode, true); err != nil {
		return RelayErrorGovernanceCustomRuleConfig{}, err
	}
	if IsBuiltinRelayErrorGovernanceRuleCode(ruleCode) {
		return RelayErrorGovernanceCustomRuleConfig{}, fmt.Errorf("custom rule_code %q conflicts with a built-in rule", ruleCode)
	}

	matchType := strings.ToLower(strings.TrimSpace(input.MatchType))
	if matchType != "contains" && matchType != "regex" {
		return RelayErrorGovernanceCustomRuleConfig{}, errors.New("match_type must be contains or regex")
	}
	matchPattern := strings.TrimSpace(input.MatchPattern)
	if matchPattern == "" {
		return RelayErrorGovernanceCustomRuleConfig{}, errors.New("match_pattern is required")
	}
	if utf8.RuneCountInString(matchPattern) > relayErrorGovernancePatternMaxLength {
		return RelayErrorGovernanceCustomRuleConfig{}, fmt.Errorf("match_pattern must not exceed %d characters", relayErrorGovernancePatternMaxLength)
	}
	if matchType == "regex" {
		if _, err := regexp.Compile(matchPattern); err != nil {
			return RelayErrorGovernanceCustomRuleConfig{}, fmt.Errorf("match_pattern must be a valid Go regex: %w", err)
		}
	}

	category := strings.TrimSpace(input.Category)
	if utf8.RuneCountInString(category) > relayErrorGovernanceCategoryMaxLength {
		category = truncateRelayErrorGovernanceText(category, relayErrorGovernanceCategoryMaxLength)
	}

	safeCode := strings.TrimSpace(input.SafeErrorCode)
	if safeCode == "" {
		safeCode = ruleCode
	}
	if err := validateRelayErrorGovernanceToken("safe_error_code", safeCode, true); err != nil {
		return RelayErrorGovernanceCustomRuleConfig{}, err
	}

	statusCode := input.StatusCode
	if statusCode == 0 {
		statusCode = InferRelayErrorGovernanceStatusCode(RelayErrorGovernanceCustomRuleConfig{
			RuleCode:         ruleCode,
			Category:         category,
			MatchPattern:     matchPattern,
			SafeErrorCode:    safeCode,
			SafeErrorType:    strings.TrimSpace(input.SafeErrorType),
			SafeErrorMessage: strings.TrimSpace(input.SafeErrorMessage),
		})
	}
	if statusCode < http.StatusBadRequest || statusCode > 599 {
		return RelayErrorGovernanceCustomRuleConfig{}, errors.New("status_code must be between 400 and 599")
	}

	safeType := strings.TrimSpace(input.SafeErrorType)
	if safeType == "" {
		safeType = inferRelayErrorGovernanceSafeType(statusCode)
	}
	if err := validateRelayErrorGovernanceToken("safe_error_type", safeType, true); err != nil {
		return RelayErrorGovernanceCustomRuleConfig{}, err
	}

	safeMessage, err := normalizeRelayErrorGovernanceMessage(input.SafeErrorMessage, false)
	if err != nil {
		return RelayErrorGovernanceCustomRuleConfig{}, err
	}

	return RelayErrorGovernanceCustomRuleConfig{
		Enabled:          input.Enabled,
		RuleCode:         ruleCode,
		Category:         category,
		MatchType:        matchType,
		MatchPattern:     matchPattern,
		SafeErrorCode:    safeCode,
		SafeErrorType:    safeType,
		SafeErrorMessage: safeMessage,
		StatusCode:       statusCode,
	}, nil
}

func NormalizeRelayErrorGovernanceCustomRules(input []RelayErrorGovernanceCustomRuleConfig) ([]RelayErrorGovernanceCustomRuleConfig, error) {
	if input == nil {
		return nil, nil
	}
	normalized := make([]RelayErrorGovernanceCustomRuleConfig, 0, len(input))
	seen := make(map[string]struct{}, len(input))
	for i, custom := range input {
		rule, err := NormalizeRelayErrorGovernanceCustomRule(custom)
		if err != nil {
			return nil, fmt.Errorf("custom_rules[%d]: %w", i, err)
		}
		if _, ok := seen[rule.RuleCode]; ok {
			return nil, fmt.Errorf("duplicate custom rule_code %q", rule.RuleCode)
		}
		seen[rule.RuleCode] = struct{}{}
		normalized = append(normalized, rule)
	}
	return normalized, nil
}

func NormalizeRelayErrorGovernanceSetting(input RelayErrorGovernanceSetting) (RelayErrorGovernanceSetting, error) {
	normalized := RelayErrorGovernanceSetting{Enabled: input.Enabled}
	if input.Rules != nil {
		normalized.Rules = make(map[string]RelayErrorGovernanceRuleConfig, len(input.Rules))
		for rawCode, override := range input.Rules {
			code := strings.TrimSpace(rawCode)
			if !IsBuiltinRelayErrorGovernanceRuleCode(code) {
				return RelayErrorGovernanceSetting{}, fmt.Errorf("rules contains unknown built-in rule code %q", code)
			}
			message, err := normalizeRelayErrorGovernanceMessage(override.Message, true)
			if err != nil {
				return RelayErrorGovernanceSetting{}, fmt.Errorf("rules[%s]: %w", code, err)
			}
			normalized.Rules[code] = RelayErrorGovernanceRuleConfig{Enabled: override.Enabled, Message: message}
		}
	}
	customRules, err := NormalizeRelayErrorGovernanceCustomRules(input.CustomRules)
	if err != nil {
		return RelayErrorGovernanceSetting{}, err
	}
	normalized.CustomRules = customRules
	return normalized, nil
}

func ParseAndValidateRelayErrorGovernanceSetting(value string) (RelayErrorGovernanceSetting, string, error) {
	var input RelayErrorGovernanceSetting
	if err := json.Unmarshal([]byte(value), &input); err != nil {
		return RelayErrorGovernanceSetting{}, "", errors.New("中继错误治理配置必须是合法 JSON")
	}
	normalized, err := NormalizeRelayErrorGovernanceSetting(input)
	if err != nil {
		return RelayErrorGovernanceSetting{}, "", err
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return RelayErrorGovernanceSetting{}, "", err
	}
	return normalized, string(data), nil
}

func NormalizeRelayErrorGovernanceOption(key string, value string) (string, error) {
	switch key {
	case RelayErrorGovernanceOptionKey:
		_, normalized, err := ParseAndValidateRelayErrorGovernanceSetting(value)
		return normalized, err
	case RelayErrorGovernanceEnabledOptionKey:
		enabled, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return "", fmt.Errorf("%s must be a boolean", RelayErrorGovernanceEnabledOptionKey)
		}
		return strconv.FormatBool(enabled), nil
	case RelayErrorGovernanceRulesOptionKey:
		var rules map[string]RelayErrorGovernanceRuleConfig
		if err := json.Unmarshal([]byte(value), &rules); err != nil {
			return "", fmt.Errorf("%s must be valid JSON", RelayErrorGovernanceRulesOptionKey)
		}
		normalized, err := NormalizeRelayErrorGovernanceSetting(RelayErrorGovernanceSetting{Rules: rules})
		if err != nil {
			return "", err
		}
		data, err := json.Marshal(normalized.Rules)
		return string(data), err
	case RelayErrorGovernanceCustomRulesOptionKey:
		var rules []RelayErrorGovernanceCustomRuleConfig
		if err := json.Unmarshal([]byte(value), &rules); err != nil {
			return "", fmt.Errorf("%s must be valid JSON", RelayErrorGovernanceCustomRulesOptionKey)
		}
		normalized, err := NormalizeRelayErrorGovernanceCustomRules(rules)
		if err != nil {
			return "", err
		}
		data, err := json.Marshal(normalized)
		return string(data), err
	default:
		if strings.HasPrefix(key, RelayErrorGovernanceOptionKey+".") {
			return "", fmt.Errorf("unknown relay error governance option %q", key)
		}
		return value, nil
	}
}

func IsRelayErrorGovernanceOptionKey(key string) bool {
	return key == RelayErrorGovernanceOptionKey || strings.HasPrefix(key, RelayErrorGovernanceOptionKey+".")
}

func InferRelayErrorGovernanceStatusCode(rule RelayErrorGovernanceCustomRuleConfig) int {
	semantic := strings.ToLower(strings.Join([]string{
		rule.RuleCode,
		rule.Category,
		rule.SafeErrorCode,
		rule.SafeErrorType,
		rule.MatchPattern,
		rule.SafeErrorMessage,
	}, " "))
	has := func(values ...string) bool {
		for _, value := range values {
			if strings.Contains(semantic, value) {
				return true
			}
		}
		return false
	}
	upstream := has("upstream", "provider", "gateway", "channel", "relay", "worker", "account_pool")

	if has("model_not_found", "model not found", "model不存在", "模型不存在") {
		return http.StatusNotFound
	}
	if has("timeout", "timed out", "deadline", "超时") {
		return http.StatusGatewayTimeout
	}
	if has("bad_gateway", "bad gateway", "bad_response", "bad response", "broken pipe", "unexpected_eof", "unexpected eof", "tls", "ssl") {
		return http.StatusBadGateway
	}
	if upstream && has("rate_limit", "rate limit", "too many requests", "cooling", "quota", "balance", "credit", "authentication", "unauthorized", "forbidden", "permission", "not_found", "not found", "unavailable", "overload", "busy") {
		return http.StatusServiceUnavailable
	}
	if has("insufficient_quota", "insufficient quota", "payment_required", "余额不足", "额度不足") {
		return http.StatusPaymentRequired
	}
	if has("content_policy", "content policy", "content_safety", "content safety", "content_filter", "content filter", "moderation", "sensitive content", "内容安全", "敏感内容", "敏感词") {
		return http.StatusBadRequest
	}
	if has("permission_denied", "access_denied", "forbidden", "behavior_banned", "ua_blocked", "system_policy_blocked", "禁止访问", "封禁") {
		return http.StatusForbidden
	}
	if has("authentication_error", "unauthorized", "invalid_token", "invalid token", "认证") {
		return http.StatusUnauthorized
	}
	if has("not_found", "not found", "不存在") {
		return http.StatusNotFound
	}
	if has("rate_limit", "rate limit", "too many requests", "risk_control", "并发", "限流") {
		return http.StatusTooManyRequests
	}
	if has("invalid", "validation", "parameter", "unsupported", "bad_request", "bad request", "client_error", "user_error", "格式错误", "参数错误", "不支持") {
		return http.StatusBadRequest
	}
	if upstream || has("service_unavailable", "unavailable", "overload", "cooling", "busy", "资源不足") {
		return http.StatusServiceUnavailable
	}
	if has("internal", "system_error", "system error") {
		return http.StatusInternalServerError
	}
	return http.StatusInternalServerError
}

func inferRelayErrorGovernanceSafeType(statusCode int) string {
	switch statusCode {
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusPaymentRequired:
		return "insufficient_quota_error"
	case http.StatusForbidden:
		return "permission_denied"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	case http.StatusInternalServerError:
		return "system_error"
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return "service_unavailable"
	default:
		if statusCode >= 400 && statusCode < 500 {
			return "invalid_request_error"
		}
		return "service_unavailable"
	}
}

func validateRelayErrorGovernanceToken(field string, value string, required bool) error {
	if value == "" {
		if required {
			return fmt.Errorf("%s is required", field)
		}
		return nil
	}
	if len(value) > RelayErrorGovernanceTokenMaxLength || !relayErrorGovernanceTokenPattern.MatchString(value) {
		return fmt.Errorf("%s must contain only letters, numbers, dot, underscore, or dash and must not exceed %d characters", field, RelayErrorGovernanceTokenMaxLength)
	}
	return nil
}

func normalizeRelayErrorGovernanceMessage(value string, allowEmpty bool) (string, error) {
	message := strings.TrimSpace(value)
	if message == "" {
		if allowEmpty {
			return "", nil
		}
		return "", errors.New("safe_error_message is required")
	}
	lower := strings.ToLower(message)
	if strings.Contains(lower, "{original") || strings.Contains(lower, "{upstream") {
		return "", errors.New("safe error messages cannot contain original/upstream placeholders")
	}
	return truncateRelayErrorGovernanceText(message, RelayErrorGovernanceMessageMaxLength), nil
}

func truncateRelayErrorGovernanceText(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes])
}
