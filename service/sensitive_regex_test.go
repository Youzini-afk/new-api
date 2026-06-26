package service

import (
	"reflect"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reflectTypeOf returns the reflect.Type of a struct value (helper to keep the
// call sites readable).
func reflectTypeOf[T any](v T) reflect.Type {
	return reflect.TypeOf(v)
}

// snapshotSensitiveGlobals captures and restores the sensitive settings touched
// by the matcher tests so each test runs in isolation.
func snapshotSensitiveGlobals(t *testing.T) {
	t.Helper()
	origPromptRules := setting.SensitivePromptRegexRules
	origUARules := setting.SensitiveUARegexRules
	origUAGroupRules := setting.SensitiveUAGroupRegexRules
	origUABlocked := setting.UABlockedRegexes
	origOnEmptyUA := setting.CheckSensitiveOnEmptyUAEnabled
	origOnEmptyAutoBan := setting.CheckSensitiveOnEmptyUAAutoBanEnabled
	origEmptyMsg := setting.SensitiveEmptyUABlockedMessage
	origUAMsg := setting.SensitiveUABlockedMessage
	origPromptMsg := setting.SensitivePromptBlockedMessage
	origEmptyStatus := setting.SensitiveEmptyUABlockedHTTPStatusCode
	origEmptyCode := setting.SensitiveEmptyUABlockedErrorCode
	t.Cleanup(func() {
		setting.SensitivePromptRegexRules = origPromptRules
		setting.SensitiveUARegexRules = origUARules
		setting.SensitiveUAGroupRegexRules = origUAGroupRules
		setting.UABlockedRegexes = origUABlocked
		setting.CheckSensitiveOnEmptyUAEnabled = origOnEmptyUA
		setting.CheckSensitiveOnEmptyUAAutoBanEnabled = origOnEmptyAutoBan
		setting.SensitiveEmptyUABlockedMessage = origEmptyMsg
		setting.SensitiveUABlockedMessage = origUAMsg
		setting.SensitivePromptBlockedMessage = origPromptMsg
		setting.SensitiveEmptyUABlockedHTTPStatusCode = origEmptyStatus
		setting.SensitiveEmptyUABlockedErrorCode = origEmptyCode
	})
}

// TestMatchSensitivePromptRule_Hit verifies the prompt regex matcher returns
// the first matching rule with per-rule fields (and AutoBan preserved as local
// config, AutoBanSync absent).
func TestMatchSensitivePromptRule_Hit(t *testing.T) {
	snapshotSensitiveGlobals(t)
	setting.SensitivePromptRegexRules = []setting.SensitiveRegexRule{
		{Pattern: "forbidden", RuleName: "r1", Message: "blocked by r1", HTTPStatusCode: 403, ErrorCode: "prompt_blocked", AutoBan: true},
		{Pattern: "other", Message: ""},
	}

	hit, ok := MatchSensitivePromptRule("this is forbidden content")
	require.True(t, ok)
	require.NotNil(t, hit)
	assert.Equal(t, "forbidden", hit.Pattern)
	assert.Equal(t, "r1", hit.RuleName)
	assert.Equal(t, "blocked by r1", hit.Message)
	assert.Equal(t, 403, hit.HTTPStatusCode)
	assert.Equal(t, types.ErrorCode("prompt_blocked"), hit.ErrorCode)
	assert.True(t, hit.AutoBan, "AutoBan must be preserved as local config")
	assert.Equal(t, "rule", hit.MatchMode)

	// No match.
	hit, ok = MatchSensitivePromptRule("clean text")
	assert.False(t, ok)
	assert.Nil(t, hit)

	// Empty text never matches.
	hit, ok = MatchSensitivePromptRule("")
	assert.False(t, ok)
	assert.Nil(t, hit)

	// Empty rules -> no match.
	setting.SensitivePromptRegexRules = nil
	hit, ok = MatchSensitivePromptRule("forbidden")
	assert.False(t, ok)
	assert.Nil(t, hit)
}

// TestMatchSensitivePromptRule_Fallbacks verifies empty per-rule fields leave
// the hit Message empty (the caller's fallbackMessage in
// BuildSensitiveBlockedError supplies the default), status -> default, code ->
// default.
func TestMatchSensitivePromptRule_Fallbacks(t *testing.T) {
	snapshotSensitiveGlobals(t)
	setting.SensitivePromptBlockedMessage = "默认拦截消息"
	// Rule with empty message/status/code and an out-of-range status.
	setting.SensitivePromptRegexRules = []setting.SensitiveRegexRule{
		{Pattern: "bad", HTTPStatusCode: 999},
	}
	hit, ok := MatchSensitivePromptRule("bad text")
	require.True(t, ok)
	require.NotNil(t, hit)
	// Per-rule empty Message is left empty; the caller's fallback is applied in
	// BuildSensitiveBlockedError (so a UA rule never gets the prompt message).
	assert.Empty(t, hit.Message, "empty rule message stays empty; caller supplies fallback")
	assert.Equal(t, setting.DefaultSensitiveStatusCode, hit.HTTPStatusCode, "out-of-range status falls back to default")
	assert.Equal(t, types.ErrorCode(setting.DefaultSensitiveErrorCode), hit.ErrorCode, "empty error code falls back to default")

	// BuildSensitiveBlockedError applies the prompt fallback message.
	_, _, errMsg := BuildSensitiveBlockedError(hit, setting.SensitivePromptBlockedMessage)
	assert.Equal(t, "默认拦截消息", errMsg.Error(), "BuildSensitiveBlockedError applies the prompt fallback")
}

// TestMatchSensitiveUARule_EmptyUA verifies the empty-UA synthetic hit applies
// the empty-UA fallbacks (message -> SensitiveEmptyUABlockedMessage or
// SensitiveUABlockedMessage, status/code clamped to defaults).
func TestMatchSensitiveUARule_EmptyUA(t *testing.T) {
	snapshotSensitiveGlobals(t)
	setting.CheckSensitiveOnEmptyUAEnabled = true
	setting.CheckSensitiveOnEmptyUAAutoBanEnabled = true
	setting.SensitiveEmptyUABlockedMessage = "空UA被拦截"
	setting.SensitiveEmptyUABlockedHTTPStatusCode = 418
	setting.SensitiveEmptyUABlockedErrorCode = "empty_ua_blocked"

	hit, ok := MatchSensitiveUARule("   ")
	require.True(t, ok)
	require.NotNil(t, hit)
	assert.Equal(t, "<empty_ua>", hit.Pattern)
	assert.Equal(t, "空UA被拦截", hit.Message)
	assert.Equal(t, 418, hit.HTTPStatusCode)
	assert.Equal(t, types.ErrorCode("empty_ua_blocked"), hit.ErrorCode)
	assert.True(t, hit.AutoBan, "AutoBan reflects CheckSensitiveOnEmptyUAAutoBanEnabled")
	assert.Equal(t, "empty_ua", hit.MatchMode)

	// Empty-UA message empty -> falls back to SensitiveUABlockedMessage.
	setting.SensitiveEmptyUABlockedMessage = ""
	setting.SensitiveUABlockedMessage = "UA回退消息"
	hit, ok = MatchSensitiveUARule("")
	require.True(t, ok)
	require.NotNil(t, hit)
	assert.Equal(t, "UA回退消息", hit.Message)

	// Out-of-range empty-UA status -> default.
	setting.SensitiveEmptyUABlockedHTTPStatusCode = 50
	setting.SensitiveEmptyUABlockedErrorCode = ""
	hit, ok = MatchSensitiveUARule("")
	require.True(t, ok)
	require.NotNil(t, hit)
	assert.Equal(t, setting.DefaultSensitiveStatusCode, hit.HTTPStatusCode)
	assert.Equal(t, types.ErrorCode(setting.DefaultSensitiveErrorCode), hit.ErrorCode)

	// When CheckSensitiveOnEmptyUAEnabled is false, empty UA does NOT hit.
	setting.CheckSensitiveOnEmptyUAEnabled = false
	hit, ok = MatchSensitiveUARule("")
	assert.False(t, ok)
	assert.Nil(t, hit)
}

// TestMatchSensitiveUARule_RegexHit verifies UA regex rules match and return
// per-rule fields.
func TestMatchSensitiveUARule_RegexHit(t *testing.T) {
	snapshotSensitiveGlobals(t)
	setting.CheckSensitiveOnEmptyUAEnabled = false
	setting.SensitiveUARegexRules = []setting.SensitiveRegexRule{
		{Pattern: "curl/.*", RuleName: "ua-curl", Message: "curl blocked", HTTPStatusCode: 403, ErrorCode: "ua_blocked"},
	}
	hit, ok := MatchSensitiveUARule("Mozilla curl/8.0")
	require.True(t, ok)
	require.NotNil(t, hit)
	assert.Equal(t, "curl/.*", hit.Pattern)
	assert.Equal(t, "ua-curl", hit.RuleName)
	assert.Equal(t, "curl blocked", hit.Message)
	assert.Equal(t, 403, hit.HTTPStatusCode)
	assert.Equal(t, types.ErrorCode("ua_blocked"), hit.ErrorCode)
	assert.Equal(t, "rule", hit.MatchMode)
}

func TestMatchSensitiveUARule_GroupRules(t *testing.T) {
	snapshotSensitiveGlobals(t)
	setting.CheckSensitiveOnEmptyUAEnabled = false
	setting.SensitiveUARegexRules = []setting.SensitiveRegexRule{
		{Pattern: "curl/.*", RuleName: "global-curl", Message: "global blocked", HTTPStatusCode: 403, ErrorCode: "global_ua_blocked"},
	}
	setting.SensitiveUAGroupRegexRules = map[string][]setting.SensitiveRegexRule{
		"vip": {
			{Pattern: "vipbot/.*", RuleName: "vip-bot", Message: "vip blocked", HTTPStatusCode: 451, ErrorCode: "vip_ua_blocked"},
			{Pattern: "curl/.*", RuleName: "vip-curl", Message: "vip curl blocked", HTTPStatusCode: 452, ErrorCode: "vip_curl_blocked"},
		},
	}

	hit, ok := MatchSensitiveUARule("Mozilla vipbot/1.0", "vip")
	require.True(t, ok)
	require.NotNil(t, hit)
	assert.Equal(t, "vipbot/.*", hit.Pattern)
	assert.Equal(t, "vip-bot", hit.RuleName)
	assert.Equal(t, "vip blocked", hit.Message)
	assert.Equal(t, 451, hit.HTTPStatusCode)
	assert.Equal(t, types.ErrorCode("vip_ua_blocked"), hit.ErrorCode)
	assert.Equal(t, "group_rule", hit.MatchMode)

	hit, ok = MatchSensitiveUARule("Mozilla vipbot/1.0", "default")
	assert.False(t, ok)
	assert.Nil(t, hit)

	// Global rules are evaluated before group rules so a global block remains
	// global even when a group has a more specific replacement rule.
	hit, ok = MatchSensitiveUARule("Mozilla curl/8.0", "vip")
	require.True(t, ok)
	require.NotNil(t, hit)
	assert.Equal(t, "global-curl", hit.RuleName)
	assert.Equal(t, "global blocked", hit.Message)
	assert.Equal(t, "rule", hit.MatchMode)
}

// TestMatchSensitiveUARule_RegexEmptyMessageFallsBackToUAMessage verifies a UA
// regex rule with an empty Message leaves the hit Message empty, and
// BuildSensitiveBlockedError falls back to SensitiveUABlockedMessage (NOT
// SensitivePromptBlockedMessage).
func TestMatchSensitiveUARule_RegexEmptyMessageFallsBackToUAMessage(t *testing.T) {
	snapshotSensitiveGlobals(t)
	setting.CheckSensitiveOnEmptyUAEnabled = false
	setting.SensitiveUABlockedMessage = "UA回退消息"
	setting.SensitivePromptBlockedMessage = "Prompt默认消息"
	setting.SensitiveUARegexRules = []setting.SensitiveRegexRule{
		{Pattern: "curl/.*"}, // empty Message
	}
	hit, ok := MatchSensitiveUARule("Mozilla curl/8.0")
	require.True(t, ok)
	require.NotNil(t, hit)
	assert.Empty(t, hit.Message, "empty rule Message stays empty; caller supplies fallback")

	// BuildSensitiveBlockedError with the UA fallback returns the UA message.
	_, _, errMsg := BuildSensitiveBlockedError(hit, setting.SensitiveUABlockedMessage)
	assert.Equal(t, "UA回退消息", errMsg.Error(), "UA empty message must fall back to SensitiveUABlockedMessage, not SensitivePromptBlockedMessage")
	assert.NotEqual(t, "Prompt默认消息", errMsg.Error(), "UA rule must NOT fall back to the prompt message")
}

// TestCheckSensitiveUA verifies the UA blocked-regex-lines helper: empty UA
// only hits when the toggle is on; otherwise per-line regex matching.
func TestCheckSensitiveUA(t *testing.T) {
	snapshotSensitiveGlobals(t)
	setting.CheckSensitiveOnEmptyUAEnabled = false
	setting.UABlockedRegexes = []string{"curl/.*", "python/.*"}

	// Match.
	blocked, hits := CheckSensitiveUA("Mozilla curl/8.0")
	assert.True(t, blocked)
	assert.Equal(t, []string{"curl/.*"}, hits)

	// Multiple matches.
	blocked, hits = CheckSensitiveUA("curl/8.0 python/3.11")
	assert.True(t, blocked)
	assert.Len(t, hits, 2)

	// No match.
	blocked, hits = CheckSensitiveUA("Mozilla/5.0")
	assert.False(t, blocked)
	assert.Nil(t, hits)

	// Empty UA with toggle off -> no hit.
	blocked, hits = CheckSensitiveUA("")
	assert.False(t, blocked)
	assert.Nil(t, hits)

	// Empty UA with toggle on -> synthetic hit.
	setting.CheckSensitiveOnEmptyUAEnabled = true
	blocked, hits = CheckSensitiveUA("")
	assert.True(t, blocked)
	assert.Equal(t, []string{"<empty_ua>"}, hits)

	// Empty patterns -> never hit.
	setting.UABlockedRegexes = nil
	setting.CheckSensitiveOnEmptyUAEnabled = false
	blocked, hits = CheckSensitiveUA("anything")
	assert.False(t, blocked)
	assert.Nil(t, hits)
}

// TestSensitiveRegexContains verifies the per-line regex matcher (case
// insensitive, skips blank/invalid patterns).
func TestSensitiveRegexContains(t *testing.T) {
	blocked, hits := SensitiveRegexContains("Hello World", []string{"world", "foo"})
	assert.True(t, blocked)
	assert.Equal(t, []string{"world"}, hits)

	// Case-insensitive.
	blocked, hits = SensitiveRegexContains("HELLO WORLD", []string{"world"})
	assert.True(t, blocked)

	// Invalid regex is skipped, valid ones still match.
	blocked, hits = SensitiveRegexContains("bad", []string{"(unclosed", "bad"})
	assert.True(t, blocked)
	assert.Equal(t, []string{"bad"}, hits)

	// Empty text never matches.
	blocked, hits = SensitiveRegexContains("", []string{"foo"})
	assert.False(t, blocked)
	assert.Nil(t, hits)

	// Empty patterns never match.
	blocked, hits = SensitiveRegexContains("foo", nil)
	assert.False(t, blocked)
	assert.Nil(t, hits)
}

// TestBuildSensitiveBlockedError verifies the (status, code, message) triple
// derivation with fallbacks, and that AutoBanSync is not a field on the hit.
func TestBuildSensitiveBlockedError(t *testing.T) {
	snapshotSensitiveGlobals(t)

	// Nil hit -> default status/code + fallback message.
	status, code, err := BuildSensitiveBlockedError(nil, "fallback msg")
	assert.Equal(t, setting.DefaultSensitiveStatusCode, status)
	assert.Equal(t, types.ErrorCodeSensitiveWordsDetected, code)
	require.Error(t, err)
	assert.Equal(t, "fallback msg", err.Error())

	// Hit with all fields set.
	hit := &SensitiveRuleHit{
		Message:        "custom blocked",
		ErrorCode:      types.ErrorCode("custom_code"),
		HTTPStatusCode: 418,
	}
	status, code, err = BuildSensitiveBlockedError(hit, "fallback msg")
	assert.Equal(t, 418, status)
	assert.Equal(t, types.ErrorCode("custom_code"), code)
	require.Error(t, err)
	assert.Equal(t, "custom blocked", err.Error())

	// Hit with empty fields -> fallbacks.
	hit2 := &SensitiveRuleHit{
		Message:        "  ",
		ErrorCode:      "",
		HTTPStatusCode: 0,
	}
	status, code, err = BuildSensitiveBlockedError(hit2, "fallback msg")
	assert.Equal(t, setting.DefaultSensitiveStatusCode, status)
	assert.Equal(t, types.ErrorCode(setting.DefaultSensitiveErrorCode), code)
	require.Error(t, err)
	assert.Equal(t, "fallback msg", err.Error())

	// DTO variant mirrors the triple.
	dto := BuildSensitiveBlockedErrorDTO(hit, "fallback msg")
	assert.Equal(t, 418, dto.HTTPStatusCode)
	assert.Equal(t, "custom_code", dto.ErrorCode)
	assert.Equal(t, "custom blocked", dto.Message)
}

// TestSensitiveRuleHit_NoAutoBanSyncField asserts AutoBanSync is absent from
// the SensitiveRuleHit struct (ban_sync deprecated), while AutoBan is present.
func TestSensitiveRuleHit_NoAutoBanSyncField(t *testing.T) {
	rt := reflectTypeOf(SensitiveRuleHit{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		assert.False(t,
			strings.Contains(strings.ToLower(f.Name), "bansync"),
			"SensitiveRuleHit must not carry any ban_sync field; found %q", f.Name,
		)
	}
	// AutoBan present (local auto-ban config), AutoBanSync absent.
	_, hasAutoBan := rt.FieldByName("AutoBan")
	assert.True(t, hasAutoBan, "AutoBan field should be present as local auto-ban config")
	_, hasAutoBanSync := rt.FieldByName("AutoBanSync")
	assert.False(t, hasAutoBanSync, "AutoBanSync field must NOT exist on SensitiveRuleHit")
}

// TestValidateSensitiveRegexOptions covers the validator for all owned keys.
func TestValidateSensitiveRegexOptions(t *testing.T) {
	// Newline-joined regexes: valid.
	require.NoError(t, ValidateSensitiveRegexOptions("SensitiveWords", "foo\nbar"))
	require.NoError(t, ValidateSensitiveRegexOptions("SensitiveUABlockedRegexes", "curl/.*\npython/.*"))
	// Invalid regex line.
	err := ValidateSensitiveRegexOptions("SensitiveUABlockedRegexes", "good\n(unclosed\n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "非法正则")

	// JSON rule arrays: valid.
	validRules := `[{"pattern":"foo","rule_name":"r1","message":"blocked","http_status_code":403,"error_code":"ua_blocked","auto_ban":true}]`
	require.NoError(t, ValidateSensitiveRegexOptions("SensitivePromptRegexRules", validRules))
	require.NoError(t, ValidateSensitiveRegexOptions("SensitiveUARegexRules", validRules))
	require.NoError(t, ValidateSensitiveRegexOptions("SensitiveUAGroupRegexRules", `{"vip":[{"pattern":"foo","auto_ban":true}]}`))

	// Invalid JSON.
	err = ValidateSensitiveRegexOptions("SensitivePromptRegexRules", "{not-json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JSON")

	// Missing pattern.
	err = ValidateSensitiveRegexOptions("SensitivePromptRegexRules", `[{"message":"x"}]`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "缺少 pattern")

	// Invalid regex in rule.
	err = ValidateSensitiveRegexOptions("SensitivePromptRegexRules", `[{"pattern":"(unclosed"}]`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "正则非法")
	err = ValidateSensitiveRegexOptions("SensitiveUAGroupRegexRules", `{"vip":[{"pattern":"(unclosed"}]}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "分组")
	assert.Contains(t, err.Error(), "正则非法")

	// Prompt rule with auto_ban but no rule_name.
	err = ValidateSensitiveRegexOptions("SensitivePromptRegexRules", `[{"pattern":"foo","auto_ban":true}]`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rule_name")
	// UA rules do NOT require rule_name for auto_ban.
	require.NoError(t, ValidateSensitiveRegexOptions("SensitiveUARegexRules", `[{"pattern":"foo","auto_ban":true}]`))

	// Out-of-range http_status_code (0 is allowed = "use default").
	err = ValidateSensitiveRegexOptions("SensitivePromptRegexRules", `[{"pattern":"foo","http_status_code":999}]`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "http_status_code")
	require.NoError(t, ValidateSensitiveRegexOptions("SensitivePromptRegexRules", `[{"pattern":"foo","http_status_code":0}]`))

	// Empty-UA status code validation.
	require.NoError(t, ValidateSensitiveRegexOptions("SensitiveEmptyUABlockedHTTPStatusCode", "418"))
	err = ValidateSensitiveRegexOptions("SensitiveEmptyUABlockedHTTPStatusCode", "999")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "100-599")
	err = ValidateSensitiveRegexOptions("SensitiveEmptyUABlockedHTTPStatusCode", "not-a-number")
	require.Error(t, err)

	// Empty-UA error code validation.
	require.NoError(t, ValidateSensitiveRegexOptions("SensitiveEmptyUABlockedErrorCode", "custom_err"))
	err = ValidateSensitiveRegexOptions("SensitiveEmptyUABlockedErrorCode", "   ")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不能为空")

	// Unowned key -> nil (no validation).
	require.NoError(t, ValidateSensitiveRegexOptions("SomeOtherKey", "anything"))
}
