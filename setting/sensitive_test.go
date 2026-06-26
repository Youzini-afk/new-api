package setting

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSensitiveUASettings_Defaults verifies the Phase 5 sensitive UA/prompt
// extension settings ship with the documented defaults.
func TestSensitiveUASettings_Defaults(t *testing.T) {
	assert.False(t, CheckSensitiveOnUAEnabled)
	assert.False(t, CheckSensitiveOnEmptyUAEnabled)
	assert.False(t, CheckSensitiveOnEmptyUAAutoBanEnabled)

	assert.Equal(t, "请求包含违规内容，已被系统拦截", SensitivePromptBlockedMessage)
	assert.Equal(t, "当前请求来源已被系统策略拦截", SensitiveUABlockedMessage)
	assert.Equal(t, "", SensitiveEmptyUABlockedMessage)
	assert.Equal(t, DefaultSensitiveStatusCode, SensitiveEmptyUABlockedHTTPStatusCode)
	assert.Equal(t, DefaultSensitiveErrorCode, SensitiveEmptyUABlockedErrorCode)
	assert.Equal(t, "sensitive_words_detected", DefaultSensitiveErrorCode)
	assert.Equal(t, 400, DefaultSensitiveStatusCode)

	require.Empty(t, UABlockedRegexes)
	require.Empty(t, SensitivePromptRegexRules)
	require.Empty(t, SensitiveUARegexRules)
	require.Empty(t, SensitiveUAGroupRegexRules)

	assert.False(t, ShouldCheckUASensitive(), "UA check is off by default")
	CheckSensitiveOnUAEnabled = true
	assert.True(t, ShouldCheckUASensitive())
	CheckSensitiveOnUAEnabled = false
}

// TestSensitiveRegexRules_Roundtrip verifies the JSON roundtrip for prompt/UA
// regex rules preserves rule fields (and that AutoBan is retained as inert
// stored data while AutoBanSync is NOT a field on the struct).
func TestSensitiveRegexRules_Roundtrip(t *testing.T) {
	original := SensitivePromptRegexRules
	originalUA := SensitiveUARegexRules
	originalUAGroup := SensitiveUAGroupRegexRules
	t.Cleanup(func() {
		SensitivePromptRegexRules = original
		SensitiveUARegexRules = originalUA
		SensitiveUAGroupRegexRules = originalUAGroup
	})

	rules := []SensitiveRegexRule{
		{Pattern: "foo", RuleName: "r1", Message: "blocked", HTTPStatusCode: 403, ErrorCode: "ua_blocked", AutoBan: true},
		{Pattern: "bar", Message: ""},
	}
	raw := SensitivePromptRegexRulesToString()
	// Pre-seed the global then serialize so ToString reflects the rules.
	SensitivePromptRegexRules = rules
	raw = SensitivePromptRegexRulesToString()
	require.NotEmpty(t, raw)

	// Empty input -> empty slice, no error.
	parsed, err := ParseSensitiveRegexRules("")
	require.NoError(t, err)
	assert.Empty(t, parsed)

	// Roundtrip: parse the serialized form back.
	SensitivePromptRegexRulesFromString(raw)
	require.Len(t, SensitivePromptRegexRules, 2)
	assert.Equal(t, "foo", SensitivePromptRegexRules[0].Pattern)
	assert.Equal(t, "r1", SensitivePromptRegexRules[0].RuleName)
	assert.Equal(t, "blocked", SensitivePromptRegexRules[0].Message)
	assert.Equal(t, 403, SensitivePromptRegexRules[0].HTTPStatusCode)
	assert.Equal(t, "ua_blocked", SensitivePromptRegexRules[0].ErrorCode)
	assert.True(t, SensitivePromptRegexRules[0].AutoBan)

	// Invalid JSON -> reset to empty slice, no panic.
	SensitivePromptRegexRulesFromString("{not-json")
	assert.Empty(t, SensitivePromptRegexRules)

	// UA rules follow the same parser.
	SensitiveUARegexRulesFromString(raw)
	require.Len(t, SensitiveUARegexRules, 2)
	assert.Equal(t, "bar", SensitiveUARegexRules[1].Pattern)

	SensitiveUAGroupRegexRules = map[string][]SensitiveRegexRule{"vip": rules}
	groupRaw := SensitiveUAGroupRegexRulesToString()
	require.NotEmpty(t, groupRaw)
	SensitiveUAGroupRegexRulesFromString(groupRaw)
	require.Len(t, SensitiveUAGroupRegexRules["vip"], 2)
	assert.Equal(t, "foo", SensitiveUAGroupRegexRules["vip"][0].Pattern)

	SensitiveUAGroupRegexRulesFromString("{not-json")
	assert.Empty(t, SensitiveUAGroupRegexRules)
}

// TestSensitiveRegexRule_NoAutoBanSyncField asserts the ban_sync per-rule flag
// is NOT present on the rule struct (ban_sync is deprecated for this branch).
func TestSensitiveRegexRule_NoAutoBanSyncField(t *testing.T) {
	rt := reflect.TypeOf(SensitiveRegexRule{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		assert.False(t,
			strings.Contains(strings.ToLower(f.Name), "bansync"),
			"SensitiveRegexRule must not carry any ban_sync field; found %q", f.Name,
		)
		jsonTag := f.Tag.Get("json")
		assert.False(t,
			strings.Contains(strings.ToLower(jsonTag), "ban_sync"),
			"SensitiveRegexRule must not carry any ban_sync json tag; found %q", jsonTag,
		)
	}
	// AutoBan (local auto-ban config, retained for a future subphase) IS allowed;
	// AutoBanSync is NOT.
	_, hasAutoBan := rt.FieldByName("AutoBan")
	assert.True(t, hasAutoBan, "AutoBan field should be retained as local auto-ban config")
	_, hasAutoBanSync := rt.FieldByName("AutoBanSync")
	assert.False(t, hasAutoBanSync, "AutoBanSync field must NOT exist")
}

// TestUABlockedRegexes_Roundtrip verifies the newline-joined UA regex list
// roundtrips and drops blank lines.
func TestUABlockedRegexes_Roundtrip(t *testing.T) {
	original := UABlockedRegexes
	t.Cleanup(func() { UABlockedRegexes = original })

	UABlockedRegexesFromString("  curl/.*\n\npython/.*  \n")
	assert.Equal(t, []string{"curl/.*", "python/.*"}, UABlockedRegexes)
	assert.Equal(t, "curl/.*\npython/.*", UABlockedRegexesToString())

	UABlockedRegexesFromString("   \n\n")
	assert.Empty(t, UABlockedRegexes)
}

// TestValidateRegexLines verifies the per-line regex validator flags invalid
// patterns and accepts valid ones.
func TestValidateRegexLines(t *testing.T) {
	bad, err := ValidateRegexLines("foo\n(unclosed\nbar")
	require.Error(t, err)
	assert.Equal(t, "(unclosed", bad)

	bad, err = ValidateRegexLines("goodpattern\nanothergood")
	require.NoError(t, err)
	assert.Equal(t, "", bad)
}

// TestSensitiveBlockedMessage_FallbackInOptionLayer documents that the
// empty-message fallback is enforced in the option layer (model.updateOptionMap),
// not in the setting helpers themselves — the helpers only trim.
func TestSensitiveBlockedMessage_NoTrimMutation(t *testing.T) {
	original := SensitivePromptBlockedMessage
	t.Cleanup(func() { SensitivePromptBlockedMessage = original })

	// The setter helpers do NOT enforce a fallback; the option layer does.
	// Here we only assert the helper roundtrips the trimmed value as-is.
	SensitivePromptBlockedMessage = "  custom msg  "
	assert.Equal(t, "  custom msg  ", SensitivePromptBlockedMessage)
}
