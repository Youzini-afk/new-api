package setting

import (
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// DefaultSensitiveErrorCode / DefaultSensitiveStatusCode are the fallback
// values used when a UA/prompt rule does not override the error code or the
// HTTP status code (or supplies an out-of-range value).
const (
	DefaultSensitiveErrorCode  = "sensitive_words_detected"
	DefaultSensitiveStatusCode = 400
)

// SensitiveRegexRule represents a single prompt/UA regex interception rule.
// Each rule can independently override the blocked message, error code and HTTP
// status code returned to the client.
//
// NOTE: AutoBanSync (gy's "auto joint ban" per-rule flag) is intentionally NOT
// migrated — ban_sync is deprecated for this branch. AutoBan is the local
// auto-ban config flag (disable tokens + mark user); the relay chain triggers
// it via the prompt/UA block-log builders.
type SensitiveRegexRule struct {
	Pattern        string `json:"pattern"`
	RuleName       string `json:"rule_name,omitempty"`
	Message        string `json:"message"`
	HTTPStatusCode int    `json:"http_status_code"`
	ErrorCode      string `json:"error_code"`
	// AutoBan toggles local auto-ban on hit (disable tokens + mark user). The
	// relay chain triggers this via the prompt/UA block-log builders. Does not
	// depend on ban_sync.
	AutoBan bool `json:"auto_ban"`
}

var CheckSensitiveEnabled = true
var CheckSensitiveOnPromptEnabled = true

// CheckSensitiveOnUAEnabled enables request UA interception checks (runs in
// parallel with prompt interception). The relay chain acts on this flag.
var CheckSensitiveOnUAEnabled = false

// CheckSensitiveOnEmptyUAEnabled enables blocking requests whose User-Agent
// header is the empty string.
var CheckSensitiveOnEmptyUAEnabled = false

// CheckSensitiveOnEmptyUAAutoBanEnabled toggles local auto-ban after an empty-UA
// hit (independent switch). Does not depend on ban_sync.
var CheckSensitiveOnEmptyUAAutoBanEnabled = false

//var CheckSensitiveOnCompletionEnabled = true

// StopOnSensitiveEnabled 如果检测到敏感词，是否立刻停止生成，否则替换敏感词
var StopOnSensitiveEnabled = true

// StreamCacheQueueLength 流模式缓存队列长度，0表示无缓存
var StreamCacheQueueLength = 0

// SensitiveWords 敏感词
// var SensitiveWords []string
var SensitiveWords = []string{
	"test_sensitive",
}

// UABlockedRegexes holds UA interception rules (one regex per line).
var UABlockedRegexes = []string{}

// SensitivePromptRegexRules holds prompt interception regex rules.
var SensitivePromptRegexRules = []SensitiveRegexRule{}

// SensitiveUARegexRules holds UA interception regex rules.
var SensitiveUARegexRules = []SensitiveRegexRule{}

// SensitiveUAGroupRegexRules holds additional UA interception rules scoped by
// user/token group. Existing SensitiveUARegexRules remain global rules.
var SensitiveUAGroupRegexRules = map[string][]SensitiveRegexRule{}

// SensitivePromptBlockedMessage is the default message returned when a prompt
// interception rule hits.
var SensitivePromptBlockedMessage = "请求包含违规内容，已被系统拦截"

// SensitiveUABlockedMessage is the default message returned when a UA
// interception rule hits.
var SensitiveUABlockedMessage = "当前请求来源已被系统策略拦截"

// SensitiveEmptyUABlockedMessage is the message returned on an empty-UA hit.
// When empty, callers fall back to SensitiveUABlockedMessage.
var SensitiveEmptyUABlockedMessage = ""

// SensitiveEmptyUABlockedHTTPStatusCode is the HTTP status returned on an
// empty-UA hit; out-of-range values fall back to DefaultSensitiveStatusCode.
var SensitiveEmptyUABlockedHTTPStatusCode = DefaultSensitiveStatusCode

// SensitiveEmptyUABlockedErrorCode is the error code returned on an empty-UA
// hit; an empty value falls back to DefaultSensitiveErrorCode.
var SensitiveEmptyUABlockedErrorCode = DefaultSensitiveErrorCode

func SensitiveWordsToString() string {
	return strings.Join(SensitiveWords, "\n")
}

func SensitiveWordsFromString(s string) {
	SensitiveWords = []string{}
	sw := strings.Split(s, "\n")
	for _, w := range sw {
		w = strings.TrimSpace(w)
		if w != "" {
			SensitiveWords = append(SensitiveWords, w)
		}
	}
}

// UABlockedRegexesToString serializes UA regex lines back to the newline-joined
// option form.
func UABlockedRegexesToString() string {
	return strings.Join(UABlockedRegexes, "\n")
}

// UABlockedRegexesFromString parses the newline-joined option form into the
// in-memory slice, dropping blank/whitespace-only lines.
func UABlockedRegexesFromString(s string) {
	UABlockedRegexes = splitRegexLines(s)
}

// ParseSensitiveRegexRules parses a JSON array of regex rules. Invalid JSON
// yields an error; individual rules are trimmed. An empty input returns an
// empty (non-nil) slice.
func ParseSensitiveRegexRules(raw string) ([]SensitiveRegexRule, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []SensitiveRegexRule{}, nil
	}
	var rules []SensitiveRegexRule
	if err := common.UnmarshalJsonStr(raw, &rules); err != nil {
		return nil, err
	}
	for i := range rules {
		rules[i].Pattern = strings.TrimSpace(rules[i].Pattern)
		rules[i].RuleName = strings.TrimSpace(rules[i].RuleName)
		rules[i].Message = strings.TrimSpace(rules[i].Message)
		rules[i].ErrorCode = strings.TrimSpace(rules[i].ErrorCode)
	}
	return rules, nil
}

func SensitivePromptRegexRulesToString() string {
	b, err := common.Marshal(SensitivePromptRegexRules)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func SensitivePromptRegexRulesFromString(raw string) {
	rules, err := ParseSensitiveRegexRules(raw)
	if err != nil {
		SensitivePromptRegexRules = []SensitiveRegexRule{}
		return
	}
	SensitivePromptRegexRules = rules
}

func SensitiveUARegexRulesToString() string {
	b, err := common.Marshal(SensitiveUARegexRules)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func SensitiveUARegexRulesFromString(raw string) {
	rules, err := ParseSensitiveRegexRules(raw)
	if err != nil {
		SensitiveUARegexRules = []SensitiveRegexRule{}
		return
	}
	SensitiveUARegexRules = rules
}

// ParseSensitiveRegexRuleGroups parses a JSON object whose keys are group names
// and whose values are SensitiveRegexRule arrays. Blank group names are ignored
// when loading persisted options; validation rejects them before persistence.
func ParseSensitiveRegexRuleGroups(raw string) (map[string][]SensitiveRegexRule, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string][]SensitiveRegexRule{}, nil
	}
	var groups map[string][]SensitiveRegexRule
	if err := common.UnmarshalJsonStr(raw, &groups); err != nil {
		return nil, err
	}
	normalized := make(map[string][]SensitiveRegexRule, len(groups))
	for group, rules := range groups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		for i := range rules {
			rules[i].Pattern = strings.TrimSpace(rules[i].Pattern)
			rules[i].RuleName = strings.TrimSpace(rules[i].RuleName)
			rules[i].Message = strings.TrimSpace(rules[i].Message)
			rules[i].ErrorCode = strings.TrimSpace(rules[i].ErrorCode)
		}
		normalized[group] = rules
	}
	return normalized, nil
}

func SensitiveUAGroupRegexRulesToString() string {
	b, err := common.Marshal(SensitiveUAGroupRegexRules)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func SensitiveUAGroupRegexRulesFromString(raw string) {
	groups, err := ParseSensitiveRegexRuleGroups(raw)
	if err != nil {
		SensitiveUAGroupRegexRules = map[string][]SensitiveRegexRule{}
		return
	}
	SensitiveUAGroupRegexRules = groups
}

func splitRegexLines(s string) []string {
	lines := strings.Split(s, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		result = append(result, line)
	}
	return result
}

// ValidateRegexLines 校验“每行一个正则”文本，返回第一条非法规则与错误。
func ValidateRegexLines(raw string) (string, error) {
	for _, line := range splitRegexLines(raw) {
		if _, err := regexp.Compile("(?i)" + line); err != nil {
			return line, err
		}
	}
	return "", nil
}

func ShouldCheckPromptSensitive() bool {
	return CheckSensitiveEnabled && CheckSensitiveOnPromptEnabled
}

// ShouldCheckUASensitive reports whether UA interception is enabled. The relay
// chain consumes this flag to decide whether to run UA checks.
func ShouldCheckUASensitive() bool {
	return CheckSensitiveEnabled && CheckSensitiveOnUAEnabled
}

//func ShouldCheckCompletionSensitive() bool {
//	return CheckSensitiveEnabled && CheckSensitiveOnCompletionEnabled
//}
