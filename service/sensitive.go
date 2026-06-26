package service

import (
	"errors"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"
)

func CheckSensitiveMessages(messages []dto.Message) ([]string, error) {
	if len(messages) == 0 {
		return nil, nil
	}

	for _, message := range messages {
		arrayContent := message.ParseContent()
		for _, m := range arrayContent {
			if m.Type == "image_url" {
				// TODO: check image url
				continue
			}
			// 检查 text 是否为空
			if m.Text == "" {
				continue
			}
			if ok, words := SensitiveWordContains(m.Text); ok {
				return words, errors.New("sensitive words detected")
			}
		}
	}
	return nil, nil
}

func CheckSensitiveText(text string) (bool, []string) {
	return SensitiveWordContains(text)
}

// SensitiveWordContains 是否包含敏感词，返回是否包含敏感词和敏感词列表
func SensitiveWordContains(text string) (bool, []string) {
	if len(setting.SensitiveWords) == 0 {
		return false, nil
	}
	if len(text) == 0 {
		return false, nil
	}
	checkText := strings.ToLower(text)
	return AcSearch(checkText, setting.SensitiveWords, true)
}

// SensitiveWordReplace 敏感词替换，返回是否包含敏感词和替换后的文本
func SensitiveWordReplace(text string, returnImmediately bool) (bool, []string, string) {
	if len(setting.SensitiveWords) == 0 {
		return false, nil, text
	}
	checkText := strings.ToLower(text)
	m := getOrBuildAC(setting.SensitiveWords)
	hits := m.MultiPatternSearch([]rune(checkText), returnImmediately)
	if len(hits) > 0 {
		words := make([]string, 0, len(hits))
		var builder strings.Builder
		builder.Grow(len(text))
		lastPos := 0

		for _, hit := range hits {
			pos := hit.Pos
			word := string(hit.Word)
			builder.WriteString(text[lastPos:pos])
			builder.WriteString("**###**")
			lastPos = pos + len(word)
			words = append(words, word)
		}
		builder.WriteString(text[lastPos:])
		return true, words, builder.String()
	}
	return false, nil, text
}

// SensitiveRuleHit describes a single prompt/UA regex interception match,
// ready to be turned into a blocked error response.
//
// NOTE: AutoBanSync (gy's per-rule "auto joint ban" flag) is intentionally NOT
// present — ban_sync is deprecated for this branch. AutoBan is the local
// auto-ban config flag (disable tokens + mark user). The relay chain triggers
// it via the prompt/UA block-log builders. Does not depend on ban_sync.
type SensitiveRuleHit struct {
	Pattern        string
	RuleName       string
	Message        string
	ErrorCode      types.ErrorCode
	HTTPStatusCode int
	AutoBan        bool
	// MatchMode is the classification the caller used ("rule" vs "empty_ua" vs
	// "blocked_regex"). Recorded on block logs so admins can filter; not used
	// for any control flow in this subphase.
	MatchMode string
}

// MatchSensitivePromptRule returns the first prompt regex rule that matches
// `text`, or (nil, false) if none match. Matching is case-insensitive.
// A nil/empty text yields no hit.
func MatchSensitivePromptRule(text string) (*SensitiveRuleHit, bool) {
	return matchSensitiveRule(text, setting.SensitivePromptRegexRules, "rule")
}

// MatchSensitiveUARule returns a SensitiveRuleHit for the UA `userAgent`:
//   - if CheckSensitiveOnEmptyUAEnabled and the UA is empty, returns an
//     "<empty_ua>" synthetic hit with the empty-UA fallbacks applied;
//   - otherwise returns the first UA regex rule that matches.
//
// Returns (nil, false) if nothing matches.
func MatchSensitiveUARule(userAgent string, groups ...string) (*SensitiveRuleHit, bool) {
	if setting.CheckSensitiveOnEmptyUAEnabled && strings.TrimSpace(userAgent) == "" {
		emptyUAMessage := strings.TrimSpace(setting.SensitiveEmptyUABlockedMessage)
		if emptyUAMessage == "" {
			emptyUAMessage = setting.SensitiveUABlockedMessage
		}
		emptyUAStatusCode := setting.SensitiveEmptyUABlockedHTTPStatusCode
		if emptyUAStatusCode < 100 || emptyUAStatusCode > 599 {
			emptyUAStatusCode = setting.DefaultSensitiveStatusCode
		}
		emptyUAErrorCode := strings.TrimSpace(setting.SensitiveEmptyUABlockedErrorCode)
		if emptyUAErrorCode == "" {
			emptyUAErrorCode = setting.DefaultSensitiveErrorCode
		}
		return &SensitiveRuleHit{
			Pattern:        "<empty_ua>",
			RuleName:       "",
			Message:        emptyUAMessage,
			ErrorCode:      types.ErrorCode(emptyUAErrorCode),
			HTTPStatusCode: emptyUAStatusCode,
			AutoBan:        setting.CheckSensitiveOnEmptyUAAutoBanEnabled,
			MatchMode:      "empty_ua",
		}, true
	}
	if hit, ok := matchSensitiveRule(userAgent, setting.SensitiveUARegexRules, "rule"); ok {
		return hit, true
	}
	for _, group := range groups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		if hit, ok := matchSensitiveRule(userAgent, setting.SensitiveUAGroupRegexRules[group], "group_rule"); ok {
			return hit, true
		}
	}
	return nil, false
}

// matchSensitiveRule is the shared first-match loop for prompt/UA regex rules.
// matchMode is recorded on the returned hit (see SensitiveRuleHit.MatchMode).
func matchSensitiveRule(text string, rules []setting.SensitiveRegexRule, matchMode string) (*SensitiveRuleHit, bool) {
	if len(rules) == 0 || strings.TrimSpace(text) == "" {
		return nil, false
	}
	for _, rule := range rules {
		pattern := strings.TrimSpace(rule.Pattern)
		if pattern == "" {
			continue
		}
		re, err := regexp.Compile("(?i)" + pattern)
		if err != nil {
			continue
		}
		if !re.MatchString(text) {
			continue
		}
		status := rule.HTTPStatusCode
		if status < 100 || status > 599 {
			status = setting.DefaultSensitiveStatusCode
		}
		code := strings.TrimSpace(rule.ErrorCode)
		if code == "" {
			code = setting.DefaultSensitiveErrorCode
		}
		msg := strings.TrimSpace(rule.Message)
		// Per-rule empty message is intentionally left empty: the caller's
		// fallbackMessage (BuildSensitiveBlockedError / BuildPromptBlockedErrorAndRecord
		// / BuildUABlockedErrorAndRecord) supplies the appropriate default
		// (SensitivePromptBlockedMessage for prompt rules, SensitiveUABlockedMessage
		// for UA rules), so a UA rule never accidentally returns the prompt message.
		return &SensitiveRuleHit{
			Pattern:        pattern,
			RuleName:       strings.TrimSpace(rule.RuleName),
			Message:        msg,
			ErrorCode:      types.ErrorCode(code),
			HTTPStatusCode: status,
			AutoBan:        rule.AutoBan,
			MatchMode:      matchMode,
		}, true
	}
	return nil, false
}

// CheckSensitiveUA reports whether the request UA should be blocked and the
// list of matching UA blocked-regex patterns (one per line in
// setting.UABlockedRegexes). An empty UA is only blocked when
// CheckSensitiveOnEmptyUAEnabled is set; in that case it returns
// (true, ["<empty_ua>"]).
func CheckSensitiveUA(userAgent string) (bool, []string) {
	if setting.CheckSensitiveOnEmptyUAEnabled && strings.TrimSpace(userAgent) == "" {
		return true, []string{"<empty_ua>"}
	}
	return SensitiveRegexContains(userAgent, setting.UABlockedRegexes)
}

// SensitiveRegexContains tests `text` against a list of "one regex per line"
// patterns (case-insensitive). Returns (true, hits) when one or more patterns
// match; (false, nil) otherwise. Empty patterns or empty text never match.
func SensitiveRegexContains(text string, patterns []string) (bool, []string) {
	if len(patterns) == 0 {
		return false, nil
	}
	if len(strings.TrimSpace(text)) == 0 {
		return false, nil
	}
	hits := make([]string, 0)
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		re, err := regexp.Compile("(?i)" + pattern)
		if err != nil {
			continue
		}
		if re.MatchString(text) {
			hits = append(hits, pattern)
		}
	}
	if len(hits) == 0 {
		return false, nil
	}
	return true, hits
}

// SensitiveBlockedError is a small DTO carrying the fields a relay handler
// needs to synthesize a blocked response (HTTP status, error code, message).
//
// This subphase intentionally returns a plain DTO rather than a
// types.NewAPIError value: BuildSensitiveBlockedError does not know which
// relay adapter will consume it, and constructing a full APIError here would
// couple the sensitive-matching layer to the relay error envelope. Callers
// (a future subphase that wires the relay chain) convert this DTO into the
// appropriate relay error.
type SensitiveBlockedError struct {
	HTTPStatusCode int
	ErrorCode      string
	Message        string
}

// BuildSensitiveBlockedError derives the (status, code, message) triple to
// return for a blocked request. When `hit` is nil, the fallback message and the
// default status/code are used. Empty fields on the hit fall back to the
// setting defaults.
//
// The returned error wraps the resolved message; SensitiveBlockedError carries
// the structured fields. This subphase does NOT construct a types.NewAPIError
// here (see SensitiveBlockedError doc).
func BuildSensitiveBlockedError(hit *SensitiveRuleHit, fallbackMessage string) (status int, code types.ErrorCode, errMsg error) {
	if hit == nil {
		return setting.DefaultSensitiveStatusCode, types.ErrorCodeSensitiveWordsDetected, errors.New(fallbackMessage)
	}
	status = hit.HTTPStatusCode
	if status < 100 || status > 599 {
		status = setting.DefaultSensitiveStatusCode
	}
	code = hit.ErrorCode
	if strings.TrimSpace(string(code)) == "" {
		code = types.ErrorCode(setting.DefaultSensitiveErrorCode)
	}
	msg := strings.TrimSpace(hit.Message)
	if msg == "" {
		msg = fallbackMessage
	}
	return status, code, errors.New(msg)
}

// BuildSensitiveBlockedErrorDTO is the DTO variant of BuildSensitiveBlockedError
// that returns a SensitiveBlockedError struct (no error wrapping). Use this when
// the caller wants the structured triple without an error value (e.g. to log
// or to forward to a relay adapter that builds its own error).
func BuildSensitiveBlockedErrorDTO(hit *SensitiveRuleHit, fallbackMessage string) SensitiveBlockedError {
	status, code, err := BuildSensitiveBlockedError(hit, fallbackMessage)
	return SensitiveBlockedError{
		HTTPStatusCode: status,
		ErrorCode:      string(code),
		Message:        err.Error(),
	}
}

// ValidateSensitiveRegexOptions delegates to setting.ValidateSensitiveRegexOptions.
// Kept as a thin wrapper for backward compatibility with controller/option.go
// callers. The canonical validator lives in the setting package so
// model.UpdateOption can call it without an import cycle.
func ValidateSensitiveRegexOptions(key string, value string) error {
	return setting.ValidateSensitiveRegexOptions(key, value)
}

// firstNonEmptyLine is retained for backward compat; delegates to setting.
func firstNonEmptyLine(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}
