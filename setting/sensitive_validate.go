package setting

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// ValidateSensitiveRegexOptions validates the sensitive option values for the
// regex/message/status keys. Returns nil for keys it does not own.
//
// This is the canonical validator, living in the `setting` package so
// `model.UpdateOption` can call it without an import cycle (model → setting,
// but model → service would cycle since service → model).
//
// Owned keys:
//   - "SensitiveUABlockedRegexes": newline-joined regexes; each line must compile.
//   - "SensitivePromptRegexRules", "SensitiveUARegexRules": JSON arrays of
//     SensitiveRegexRule; each rule needs a compilable pattern. For prompt
//     rules, a rule with AutoBan=true must set a rule_name.
//   - "SensitiveUAGroupRegexRules": JSON object {group: SensitiveRegexRule[]}.
//   - "SensitiveEmptyUABlockedHTTPStatusCode": integer in [100,599].
//   - "SensitiveEmptyUABlockedErrorCode": trimmed non-empty string.
//
// ban_sync keys are NOT validated here — they are rejected upstream in
// model.UpdateOption and never reach this function.
func ValidateSensitiveRegexOptions(key string, value string) error {
	switch key {
	case "SensitiveUABlockedRegexes":
		if rule, err := ValidateRegexLines(value); err != nil {
			if rule == "" {
				rule = firstNonEmptyLine(value)
			}
			return fmt.Errorf("第一个非法正则为 %q: %w", rule, err)
		}
	case "SensitivePromptRegexRules", "SensitiveUARegexRules":
		rules, err := ParseSensitiveRegexRules(value)
		if err != nil {
			return fmt.Errorf("规则 JSON 解析失败: %w", err)
		}
		if err := validateSensitiveRegexRuleList(key, "", rules); err != nil {
			return err
		}
	case "SensitiveUAGroupRegexRules":
		groups, err := parseSensitiveRegexRuleGroupsForValidation(value)
		if err != nil {
			return fmt.Errorf("分组规则 JSON 解析失败: %w", err)
		}
		for group, rules := range groups {
			group = strings.TrimSpace(group)
			if group == "" {
				return fmt.Errorf("分组规则包含空 group 名称")
			}
			if err := validateSensitiveRegexRuleList(key, group, rules); err != nil {
				return err
			}
		}
	case "SensitiveEmptyUABlockedHTTPStatusCode":
		statusCode, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("SensitiveEmptyUABlockedHTTPStatusCode 必须是整数")
		}
		if statusCode < 100 || statusCode > 599 {
			return fmt.Errorf("SensitiveEmptyUABlockedHTTPStatusCode 必须在 100-599 之间")
		}
	case "SensitiveEmptyUABlockedErrorCode":
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("SensitiveEmptyUABlockedErrorCode 不能为空")
		}
	}
	return nil
}

func validateSensitiveRegexRuleList(key string, group string, rules []SensitiveRegexRule) error {
	for idx, rule := range rules {
		prefix := fmt.Sprintf("第 %d 条规则", idx+1)
		if group != "" {
			prefix = fmt.Sprintf("分组 %q 的第 %d 条规则", group, idx+1)
		}
		pattern := strings.TrimSpace(rule.Pattern)
		if pattern == "" {
			return fmt.Errorf("%s 缺少 pattern", prefix)
		}
		if _, compileErr := regexp.Compile("(?i)" + pattern); compileErr != nil {
			return fmt.Errorf("%s 正则非法 %q: %w", prefix, pattern, compileErr)
		}
		// NOTE: no AutoBanSync cross-check — AutoBanSync does not exist on
		// SensitiveRegexRule here.
		if key == "SensitivePromptRegexRules" && rule.AutoBan && strings.TrimSpace(rule.RuleName) == "" {
			return fmt.Errorf("%s 启用 auto_ban 时必须设置 rule_name", prefix)
		}
		status := rule.HTTPStatusCode
		if status != 0 && (status < 100 || status > 599) {
			return fmt.Errorf("%s http_status_code 非法: %s", prefix, strconv.Itoa(status))
		}
	}
	return nil
}

func parseSensitiveRegexRuleGroupsForValidation(raw string) (map[string][]SensitiveRegexRule, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string][]SensitiveRegexRule{}, nil
	}
	var groups map[string][]SensitiveRegexRule
	if err := common.UnmarshalJsonStr(raw, &groups); err != nil {
		return nil, err
	}
	return groups, nil
}

func firstNonEmptyLine(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}
