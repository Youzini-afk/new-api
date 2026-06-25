package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newOptionUpdateContext builds a gin.Context whose request body is a JSON
// OptionUpdateRequest for the given key/value. It mimics an admin caller.
func newOptionUpdateContext(t *testing.T, key, value string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	body := `{"key":"` + key + `","value":` + jsonQuote(value) + `}`
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPut, "/api/option/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	ctx.Set("id", 1)
	ctx.Set("role", common.RoleRootUser)
	ctx.Set("username", "root")
	return ctx, recorder
}

// jsonQuote produces a JSON-quoted string value. Used to embed string option
// values in the test request body without pulling encoding/json into helpers.
func jsonQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// TestUpdateOption_SensitiveRegexValidation verifies the controller wires
// service.ValidateSensitiveRegexOptions for the Phase 5 sensitive regex keys:
// valid values are persisted; invalid values are rejected with a message.
func TestUpdateOption_SensitiveRegexValidation(t *testing.T) {
	setupLogScreeningTestDB(t)
	// Initialize OptionMap so updateOptionMap can write into it (otherwise it
	// panics on the nil map). The Phase 5 sensitive keys are registered here.
	model.InitOptionMap()

	// Snapshot + restore the settings touched.
	origUARegexes := setting.UABlockedRegexes
	origPromptRules := setting.SensitivePromptRegexRules
	origEmptyStatus := setting.SensitiveEmptyUABlockedHTTPStatusCode
	origEmptyCode := setting.SensitiveEmptyUABlockedErrorCode
	t.Cleanup(func() {
		setting.UABlockedRegexes = origUARegexes
		setting.SensitivePromptRegexRules = origPromptRules
		setting.SensitiveEmptyUABlockedHTTPStatusCode = origEmptyStatus
		setting.SensitiveEmptyUABlockedErrorCode = origEmptyCode
	})

	// Valid UA blocked regexes -> persisted + applied.
	ctx, recorder := newOptionUpdateContext(t, "SensitiveUABlockedRegexes", "curl/.*\npython/.*")
	UpdateOption(ctx)
	resp := decodeLogScreeningResponse(t, recorder)
	require.True(t, resp.Success, "valid UA regexes should persist: %s", resp.Message)
	assert.Equal(t, []string{"curl/.*", "python/.*"}, setting.UABlockedRegexes)

	// Invalid UA blocked regex (unclosed group) -> rejected.
	ctx, recorder = newOptionUpdateContext(t, "SensitiveUABlockedRegexes", "good\n(unclosed\n")
	UpdateOption(ctx)
	resp = decodeLogScreeningResponse(t, recorder)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "非法正则")
	// Setting unchanged (still the previously-applied valid value).
	assert.Equal(t, []string{"curl/.*", "python/.*"}, setting.UABlockedRegexes)

	// Valid prompt regex rules JSON -> persisted.
	validRules := `[{"pattern":"foo","rule_name":"r1","message":"blocked","http_status_code":403,"error_code":"ua_blocked"}]`
	ctx, recorder = newOptionUpdateContext(t, "SensitivePromptRegexRules", validRules)
	UpdateOption(ctx)
	resp = decodeLogScreeningResponse(t, recorder)
	require.True(t, resp.Success, resp.Message)
	require.Len(t, setting.SensitivePromptRegexRules, 1)
	assert.Equal(t, "foo", setting.SensitivePromptRegexRules[0].Pattern)

	// Invalid prompt JSON -> rejected.
	ctx, recorder = newOptionUpdateContext(t, "SensitivePromptRegexRules", "{not-json")
	UpdateOption(ctx)
	resp = decodeLogScreeningResponse(t, recorder)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "JSON")

	// Prompt rule with auto_ban but no rule_name -> rejected.
	ctx, recorder = newOptionUpdateContext(t, "SensitivePromptRegexRules", `[{"pattern":"foo","auto_ban":true}]`)
	UpdateOption(ctx)
	resp = decodeLogScreeningResponse(t, recorder)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "rule_name")

	// Empty-UA HTTP status code: valid.
	ctx, recorder = newOptionUpdateContext(t, "SensitiveEmptyUABlockedHTTPStatusCode", "418")
	UpdateOption(ctx)
	resp = decodeLogScreeningResponse(t, recorder)
	require.True(t, resp.Success, resp.Message)
	assert.Equal(t, 418, setting.SensitiveEmptyUABlockedHTTPStatusCode)

	// Out-of-range -> rejected.
	ctx, recorder = newOptionUpdateContext(t, "SensitiveEmptyUABlockedHTTPStatusCode", "999")
	UpdateOption(ctx)
	resp = decodeLogScreeningResponse(t, recorder)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "100-599")
	assert.Equal(t, 418, setting.SensitiveEmptyUABlockedHTTPStatusCode, "rejected value must not mutate setting")

	// Empty-UA error code: valid.
	ctx, recorder = newOptionUpdateContext(t, "SensitiveEmptyUABlockedErrorCode", "custom_err")
	UpdateOption(ctx)
	resp = decodeLogScreeningResponse(t, recorder)
	require.True(t, resp.Success, resp.Message)
	assert.Equal(t, "custom_err", setting.SensitiveEmptyUABlockedErrorCode)

	// Empty error code -> rejected by validator.
	ctx, recorder = newOptionUpdateContext(t, "SensitiveEmptyUABlockedErrorCode", "   ")
	UpdateOption(ctx)
	resp = decodeLogScreeningResponse(t, recorder)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "不能为空")
}

// TestUpdateOption_DiscordRegisterGateRoleMatch verifies the controller
// persistence path for the nested "discord.register_gate" contract: an illegal
// rule-level role_match value like "foo" must be rejected before it reaches the
// DB, while valid values ("any"/"all"/empty) are accepted.
func TestUpdateOption_DiscordRegisterGateRoleMatch(t *testing.T) {
	setupLogScreeningTestDB(t)
	model.InitOptionMap()

	// "foo" -> rejected by ParseAndValidate before persistence.
	ctx, recorder := newOptionUpdateContext(t, "discord.register_gate", `{"groups":[{"rules":[{"guild_id":"g1","role_ids":["r1"],"role_match":"foo"}]}]}`)
	UpdateOption(ctx)
	resp := decodeLogScreeningResponse(t, recorder)
	assert.False(t, resp.Success, "illegal role_match 'foo' must be rejected")
	assert.Contains(t, resp.Message, "role_match must be")

	// Not persisted.
	var count int64
	require.NoError(t, model.DB.Model(&model.Option{}).Where("key = ?", "discord.register_gate").Count(&count).Error)
	assert.Equal(t, int64(0), count, "rejected role_match must not be persisted")

	// Empty role_match -> accepted (normalized to "any").
	ctx, recorder = newOptionUpdateContext(t, "discord.register_gate", `{"groups":[{"rules":[{"guild_id":"g1","role_ids":["r1"],"role_match":""}]}]}`)
	UpdateOption(ctx)
	resp = decodeLogScreeningResponse(t, recorder)
	require.True(t, resp.Success, "empty role_match should be accepted: %s", resp.Message)

	// "any" -> accepted.
	ctx, recorder = newOptionUpdateContext(t, "discord.register_gate", `{"groups":[{"rules":[{"guild_id":"g1","role_ids":["r1"],"role_match":"any"}]}]}`)
	UpdateOption(ctx)
	resp = decodeLogScreeningResponse(t, recorder)
	require.True(t, resp.Success, "role_match 'any' should be accepted: %s", resp.Message)

	// "all" -> accepted.
	ctx, recorder = newOptionUpdateContext(t, "discord.register_gate", `{"groups":[{"rules":[{"guild_id":"g1","role_ids":["r1","r2"],"role_match":"all"}]}]}`)
	UpdateOption(ctx)
	resp = decodeLogScreeningResponse(t, recorder)
	require.True(t, resp.Success, "role_match 'all' should be accepted: %s", resp.Message)
}

// TestUpdateOption_RejectsBanSyncKeysAtController verifies that even if a
// ban_sync legacy key reaches the controller, model.UpdateOption silently
// rejects it (no DB row, no OptionMap entry) without surfacing an error.
func TestUpdateOption_RejectsBanSyncKeysAtController(t *testing.T) {
	setupLogScreeningTestDB(t)
	model.InitOptionMap()

	for _, key := range []string{"CheckSensitiveAutoBanSyncEnabled", "AutoBanSync", "ban_sync.enabled"} {
		ctx, recorder := newOptionUpdateContext(t, key, "true")
		UpdateOption(ctx)
		resp := decodeLogScreeningResponse(t, recorder)
		assert.True(t, resp.Success, "UpdateOption must succeed (silent reject) for banned key %q: %s", key, resp.Message)

		// Not persisted.
		var count int64
		require.NoError(t, model.DB.Model(&model.Option{}).Where("key = ?", key).Count(&count).Error)
		assert.Equal(t, int64(0), count, "banned key %q must not be persisted", key)

		// Not in OptionMap.
		common.OptionMapRWMutex.RLock()
		_, present := common.OptionMap[key]
		common.OptionMapRWMutex.RUnlock()
		assert.False(t, present, "banned key %q must not enter OptionMap", key)
	}
}

// TestUpdateOption_ShortMsgExtraBillingValidation verifies the Phase 10C
// server-side guard for `quota_setting.short_msg_extra_billing`: invalid
// JSON / unknown mode / trigger / threshold / fee_quota / model / id /
// response_mode / duplicate rule id are rejected before persistence, while a
// valid config is normalized and applied to the in-memory QuotaSetting.
func TestUpdateOption_ShortMsgExtraBillingValidation(t *testing.T) {
	setupLogScreeningTestDB(t)
	model.InitOptionMap()

	// Snapshot and restore the in-memory QuotaSetting so the test does not
	// leak global state to other tests in the package.
	orig := *operation_setting.GetQuotaSetting()
	t.Cleanup(func() {
		*operation_setting.GetQuotaSetting() = orig
	})

	// Invalid JSON -> rejected, not persisted.
	ctx, recorder := newOptionUpdateContext(t, operation_setting.ShortMsgExtraBillingOptionKey, `{not-json`)
	UpdateOption(ctx)
	resp := decodeLogScreeningResponse(t, recorder)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "无效 JSON")
	var count int64
	require.NoError(t, model.DB.Model(&model.Option{}).Where("key = ?", operation_setting.ShortMsgExtraBillingOptionKey).Count(&count).Error)
	assert.Equal(t, int64(0), count, "invalid config must not be persisted")

	// Unknown mode -> rejected.
	ctx, recorder = newOptionUpdateContext(t, operation_setting.ShortMsgExtraBillingOptionKey, `{"mode":"bogus","rules":[]}`)
	UpdateOption(ctx)
	resp = decodeLogScreeningResponse(t, recorder)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "mode 必须是")

	// Rule with negative threshold -> rejected.
	ctx, recorder = newOptionUpdateContext(t, operation_setting.ShortMsgExtraBillingOptionKey, `{"mode":"shadow","rules":[{"id":"r1","model":"m","trigger":"input_tokens_below","threshold":-1,"fee_quota":1}]}`)
	UpdateOption(ctx)
	resp = decodeLogScreeningResponse(t, recorder)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "threshold 必须 > 0")

	// Rule with unknown response mode -> rejected.
	ctx, recorder = newOptionUpdateContext(t, operation_setting.ShortMsgExtraBillingOptionKey, `{"mode":"shadow","rules":[{"id":"r1","model":"m","trigger":"input_tokens_below","threshold":1,"fee_quota":1,"response_modes":["bogus"]}]}`)
	UpdateOption(ctx)
	resp = decodeLogScreeningResponse(t, recorder)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "response_modes 无效")

	// Duplicate rule id -> rejected.
	ctx, recorder = newOptionUpdateContext(t, operation_setting.ShortMsgExtraBillingOptionKey, `{"mode":"shadow","rules":[{"id":"r1","model":"m","trigger":"input_tokens_below","threshold":1,"fee_quota":1},{"id":"r1","model":"m2","trigger":"input_tokens_below","threshold":1,"fee_quota":1}]}`)
	UpdateOption(ctx)
	resp = decodeLogScreeningResponse(t, recorder)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "重复 rule id")

	// Valid config with whitespace + duplicate response modes -> accepted,
	// normalized, and applied to the in-memory QuotaSetting.
	ctx, recorder = newOptionUpdateContext(t, operation_setting.ShortMsgExtraBillingOptionKey, `{"mode":"shadow","rules":[{"id":"  r1  ","model":"  gpt-4o-mini  ","trigger":"input_tokens_below","threshold":100,"fee_quota":500,"response_modes":["  claude ","claude"," gemini "]}]}`)
	UpdateOption(ctx)
	resp = decodeLogScreeningResponse(t, recorder)
	require.True(t, resp.Success, "valid config should persist: %s", resp.Message)

	qs := operation_setting.GetQuotaSetting()
	require.Equal(t, operation_setting.ShortMsgExtraBillingModeShadow, qs.ShortMsgExtraBilling.Mode)
	require.Len(t, qs.ShortMsgExtraBilling.Rules, 1)
	assert.Equal(t, "r1", qs.ShortMsgExtraBilling.Rules[0].ID, "id should be trimmed in-memory")
	assert.Equal(t, "gpt-4o-mini", qs.ShortMsgExtraBilling.Rules[0].Model, "model should be trimmed in-memory")
	assert.Equal(t, []string{"claude", "gemini"}, qs.ShortMsgExtraBilling.Rules[0].ResponseModes, "response_modes should be trimmed+deduped in-memory")

	// The persisted row holds the normalized JSON string, not the raw input.
	var opt model.Option
	require.NoError(t, model.DB.First(&opt, model.Option{Key: operation_setting.ShortMsgExtraBillingOptionKey}).Error)
	assert.JSONEq(t, `{"mode":"shadow","rules":[{"id":"r1","model":"gpt-4o-mini","trigger":"input_tokens_below","threshold":100,"fee_quota":500,"waive_when_completion_tokens_zero":false,"response_modes":["claude","gemini"]}]}`, opt.Value)
}
