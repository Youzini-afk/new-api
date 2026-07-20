package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

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

func TestGetOptions_IncludesRelayErrorGovernanceWhenFullKeyMissing(t *testing.T) {
	setupLogScreeningTestDB(t)
	model.InitOptionMap()

	cfg := system_setting.GetRelayErrorGovernanceSetting()
	originalEnabled := cfg.Enabled
	originalRules := cfg.Rules
	originalCustomRules := append([]system_setting.RelayErrorGovernanceCustomRuleConfig(nil), cfg.CustomRules...)
	t.Cleanup(func() {
		cfg.Enabled = originalEnabled
		cfg.Rules = originalRules
		cfg.CustomRules = originalCustomRules
	})

	cfg.Enabled = true
	cfg.CustomRules = []system_setting.RelayErrorGovernanceCustomRuleConfig{
		{
			Enabled:          true,
			RuleCode:         "ai_test_rule",
			MatchType:        "contains",
			MatchPattern:     "upstream overloaded",
			SafeErrorCode:    "upstream_overloaded",
			SafeErrorType:    "upstream_error",
			SafeErrorMessage: "Upstream is overloaded.",
		},
	}
	common.OptionMapRWMutex.Lock()
	delete(common.OptionMap, "relay_error_governance")
	common.OptionMapRWMutex.Unlock()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/option/", nil)
	GetOptions(ctx)

	resp := decodeLogScreeningResponse(t, recorder)
	require.True(t, resp.Success, resp.Message)
	var options []model.Option
	require.NoError(t, json.Unmarshal(resp.Data, &options))
	found := false
	for _, option := range options {
		if option.Key != "relay_error_governance" {
			continue
		}
		found = true
		var parsed system_setting.RelayErrorGovernanceSetting
		require.NoError(t, json.Unmarshal([]byte(option.Value), &parsed))
		require.Len(t, parsed.CustomRules, 1)
		assert.Equal(t, "ai_test_rule", parsed.CustomRules[0].RuleCode)
	}
	assert.True(t, found)
}

func TestSaveErrorInsightCustomAIRule_SyncsRelayErrorGovernanceConfig(t *testing.T) {
	setupLogScreeningTestDB(t)
	model.InitOptionMap()

	cfg := system_setting.GetRelayErrorGovernanceSetting()
	originalEnabled := cfg.Enabled
	originalRules := cfg.Rules
	originalCustomRules := append([]system_setting.RelayErrorGovernanceCustomRuleConfig(nil), cfg.CustomRules...)
	t.Cleanup(func() {
		cfg.Enabled = originalEnabled
		cfg.Rules = originalRules
		cfg.CustomRules = originalCustomRules
	})

	cfg.Enabled = true
	cfg.Rules = map[string]system_setting.RelayErrorGovernanceRuleConfig{}
	cfg.CustomRules = nil

	body := `{"rule":{"rule_code":"ai_saved_rule","category":"upstream","match_type":"contains","match_pattern":"upstream overloaded","safe_error_code":"upstream_overloaded","safe_error_type":"upstream_error","safe_error_message":"Upstream service is busy. Please try again later."}}`
	ctx, recorder := newLogScreeningAdminContext(t, http.MethodPost, "/api/error_insight/ai/rules", body)
	SaveErrorInsightCustomAIRule(ctx)

	resp := decodeLogScreeningResponse(t, recorder)
	require.True(t, resp.Success, resp.Message)
	require.Len(t, cfg.CustomRules, 1)
	assert.Equal(t, "ai_saved_rule", cfg.CustomRules[0].RuleCode)

	recorder = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/option/", nil)
	GetOptions(ctx)

	resp = decodeLogScreeningResponse(t, recorder)
	require.True(t, resp.Success, resp.Message)
	var options []model.Option
	require.NoError(t, json.Unmarshal(resp.Data, &options))
	found := false
	for _, option := range options {
		if option.Key != "relay_error_governance" {
			continue
		}
		found = true
		var parsed system_setting.RelayErrorGovernanceSetting
		require.NoError(t, json.Unmarshal([]byte(option.Value), &parsed))
		require.Len(t, parsed.CustomRules, 1)
		assert.Equal(t, "ai_saved_rule", parsed.CustomRules[0].RuleCode)
	}
	assert.True(t, found)
}

func TestGetErrorInsightAIResult_ReturnsPersistedDraft(t *testing.T) {
	setupLogScreeningTestDB(t)
	require.NoError(t, model.UpsertErrorInsightAIResult(
		context.Background(),
		"sig_ai_draft",
		1,
		`[{"rule_code":"ai_draft_rule","category":"upstream","match_type":"contains","match_pattern":"timeout","safe_error_code":"upstream_timeout","safe_error_type":"upstream_error","safe_error_message":"Upstream timeout.","confidence":0.91,"reason":"matched timeout"}]`,
		`{"rules":[{"rule_code":"ai_draft_rule"}]}`,
	))

	ctx, recorder := newLogScreeningAdminContext(t, http.MethodGet, "/api/error_insight/ai/results/sig_ai_draft", "")
	ctx.Params = gin.Params{{Key: "signature", Value: "sig_ai_draft"}}
	GetErrorInsightAIResult(ctx)

	resp := decodeLogScreeningResponse(t, recorder)
	require.True(t, resp.Success, resp.Message)
	var result ErrorInsightAIResultResponse
	require.NoError(t, json.Unmarshal(resp.Data, &result))
	require.Len(t, result.Rules, 1)
	assert.Equal(t, "ai_draft_rule", result.Rules[0].RuleCode)
	assert.JSONEq(t, `{"rules":[{"rule_code":"ai_draft_rule"}]}`, string(result.Raw))
}

func TestSaveErrorGovernanceAISetting_SyncsConfig(t *testing.T) {
	setupLogScreeningTestDB(t)
	model.InitOptionMap()

	cfg := system_setting.GetErrorGovernanceAISetting()
	originalEnabled := cfg.Enabled
	originalChannelID := cfg.ChannelID
	originalModel := cfg.Model
	originalRedactSensitive := cfg.RedactSensitive
	originalPromptTemplate := cfg.PromptTemplate
	originalJSONOutputParams := append([]byte(nil), cfg.JSONOutputParams...)
	t.Cleanup(func() {
		cfg.Enabled = originalEnabled
		cfg.ChannelID = originalChannelID
		cfg.Model = originalModel
		cfg.RedactSensitive = originalRedactSensitive
		cfg.PromptTemplate = originalPromptTemplate
		cfg.JSONOutputParams = originalJSONOutputParams
	})

	body := `{"enabled":true,"channel_id":12,"model":"gpt-test","redact_sensitive":true,"prompt_template":"organize {{governance_config}} {{conflicts}}","json_output_params":{"response_format":{"type":"json_object"}}}`
	ctx, recorder := newLogScreeningAdminContext(t, http.MethodPut, "/api/error_insight/governance-ai/settings", body)
	SaveErrorGovernanceAISetting(ctx)

	resp := decodeLogScreeningResponse(t, recorder)
	require.True(t, resp.Success, resp.Message)
	assert.True(t, cfg.Enabled)
	assert.Equal(t, 12, cfg.ChannelID)
	assert.Equal(t, "gpt-test", cfg.Model)
	assert.Contains(t, cfg.PromptTemplate, "governance_config")
}

func TestParseErrorGovernanceAIOrganization_NormalizesRules(t *testing.T) {
	result, err := parseErrorGovernanceAIOrganization(`{"summary":"merged duplicates","rules":[{"enabled":true,"rule_code":"ai_rule","category":"upstream","match_type":"contains","match_pattern":"all keys cooling down","safe_error_code":"upstream_cooling","safe_error_type":"upstream_error","safe_error_message":"Upstream is busy."}]}`)
	require.NoError(t, err)
	assert.Equal(t, "merged duplicates", result.Summary)
	require.Len(t, result.Rules, 1)
	assert.Equal(t, "ai_rule", result.Rules[0].RuleCode)
	assert.Equal(t, http.StatusServiceUnavailable, result.Rules[0].StatusCode)
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

// TestUpdateOption_DiscordGateConfigsValidateRoleMatch verifies all three
// scoped Discord gate option keys share the same typed validation contract.
func TestUpdateOption_DiscordGateConfigsValidateRoleMatch(t *testing.T) {
	setupLogScreeningTestDB(t)
	model.InitOptionMap()

	for _, key := range []string{"discord.register_gate", "discord.login_gate", "discord.patrol_gate"} {
		ctx, recorder := newOptionUpdateContext(t, key, `{"groups":[{"rules":[{"guild_id":"g1","role_ids":["r1"],"role_match":"foo"}]}]}`)
		UpdateOption(ctx)
		resp := decodeLogScreeningResponse(t, recorder)
		assert.False(t, resp.Success, "illegal role_match must be rejected for %s", key)
		assert.Contains(t, resp.Message, "role_match must be")

		var count int64
		require.NoError(t, model.DB.Model(&model.Option{}).Where("key = ?", key).Count(&count).Error)
		assert.Equal(t, int64(0), count, "rejected role_match must not be persisted for %s", key)
	}

	// Empty role_match -> accepted (normalized to "any").
	ctx, recorder := newOptionUpdateContext(t, "discord.register_gate", `{"groups":[{"rules":[{"guild_id":"g1","role_ids":["r1"],"role_match":""}]}]}`)
	UpdateOption(ctx)
	resp := decodeLogScreeningResponse(t, recorder)
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
	ctx, recorder = newOptionUpdateContext(t, operation_setting.ShortMsgExtraBillingOptionKey, `{"mode":"shadow","rules":[{"id":"  r1  ","group":"  default  ","trigger":"input_tokens_below","threshold":100,"fee_quota":500,"response_modes":["  claude ","claude"," gemini "]}]}`)
	UpdateOption(ctx)
	resp = decodeLogScreeningResponse(t, recorder)
	require.True(t, resp.Success, "valid config should persist: %s", resp.Message)

	qs := operation_setting.GetQuotaSetting()
	require.Equal(t, operation_setting.ShortMsgExtraBillingModeShadow, qs.ShortMsgExtraBilling.Mode)
	require.Len(t, qs.ShortMsgExtraBilling.Rules, 1)
	assert.Equal(t, "r1", qs.ShortMsgExtraBilling.Rules[0].ID, "id should be trimmed in-memory")
	assert.Equal(t, "default", qs.ShortMsgExtraBilling.Rules[0].Group, "group should be trimmed in-memory")
	assert.Equal(t, []string{"claude", "gemini"}, qs.ShortMsgExtraBilling.Rules[0].ResponseModes, "response_modes should be trimmed+deduped in-memory")

	// The persisted row holds the normalized JSON string, not the raw input.
	var opt model.Option
	require.NoError(t, model.DB.First(&opt, model.Option{Key: operation_setting.ShortMsgExtraBillingOptionKey}).Error)
	assert.JSONEq(t, `{"mode":"shadow","rules":[{"id":"r1","group":"default","trigger":"input_tokens_below","threshold":100,"fee_quota":500,"waive_when_completion_tokens_zero":false,"response_modes":["claude","gemini"]}]}`, opt.Value)
}
