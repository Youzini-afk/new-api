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
	original := *cfg
	t.Cleanup(func() {
		_, _ = model.UpdateRelayErrorGovernanceSetting(original)
	})

	_, err := model.UpdateRelayErrorGovernanceSetting(system_setting.RelayErrorGovernanceSetting{
		Enabled: true,
		CustomRules: []system_setting.RelayErrorGovernanceCustomRuleConfig{
			{
				Enabled:          true,
				RuleCode:         "ai_test_rule",
				MatchType:        "contains",
				MatchPattern:     "upstream overloaded",
				SafeErrorCode:    "upstream_overloaded",
				SafeErrorType:    "upstream_error",
				SafeErrorMessage: "Upstream is overloaded.",
			},
		},
	})
	require.NoError(t, err)
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
	original := *cfg
	t.Cleanup(func() {
		_, _ = model.UpdateRelayErrorGovernanceSetting(original)
	})

	_, err := model.UpdateRelayErrorGovernanceSetting(system_setting.RelayErrorGovernanceSetting{
		Enabled: true,
		Rules:   map[string]system_setting.RelayErrorGovernanceRuleConfig{},
	})
	require.NoError(t, err)

	body := `{"rule":{"rule_code":"ai_saved_rule","category":"parameter_validation","match_type":"contains","match_pattern":"invalid request parameter","safe_error_code":"invalid_request","safe_error_type":"invalid_request_error","safe_error_message":"Request parameters are invalid.","status_code":400}}`
	ctx, recorder := newLogScreeningAdminContext(t, http.MethodPost, "/api/error_insight/ai/rules", body)
	SaveErrorInsightCustomAIRule(ctx)

	resp := decodeLogScreeningResponse(t, recorder)
	require.True(t, resp.Success, resp.Message)
	cfg = system_setting.GetRelayErrorGovernanceSetting()
	require.Len(t, cfg.CustomRules, 1)
	assert.Equal(t, "ai_saved_rule", cfg.CustomRules[0].RuleCode)
	assert.Equal(t, "invalid_request", cfg.CustomRules[0].SafeErrorCode)
	assert.Equal(t, http.StatusBadRequest, cfg.CustomRules[0].StatusCode)

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
		assert.Equal(t, "invalid_request", parsed.CustomRules[0].SafeErrorCode)
		assert.Equal(t, http.StatusBadRequest, parsed.CustomRules[0].StatusCode)
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
	assert.True(t, result.Rules[0].Enabled)
	assert.Equal(t, http.StatusServiceUnavailable, result.Rules[0].StatusCode)

	result, err = parseErrorGovernanceAIOrganization(`{"summary":"disabled","rules":[{"enabled":false,"rule_code":"disabled_rule","category":"parameter_validation","match_type":"contains","match_pattern":"disabled pattern","safe_error_message":"Disabled."}]}`)
	require.NoError(t, err)
	require.Len(t, result.Rules, 1)
	assert.False(t, result.Rules[0].Enabled)
}

func TestParseErrorInsightAISuggestionsPreservesAndInfersStatus(t *testing.T) {
	rules, _, err := parseErrorInsightAISuggestions(`{"rules":[{"rule_code":"invalid_parameter_rule","category":"parameter_validation","match_type":"contains","match_pattern":"invalid parameter","safe_error_code":"invalid_request","safe_error_type":"invalid_request_error","safe_error_message":"Invalid parameter.","status_code":418,"confidence":0.9,"reason":"specific"},{"rule_code":"upstream_timeout_rule","category":"upstream_timeout","match_type":"contains","match_pattern":"deadline exceeded","safe_error_code":"upstream_timeout","safe_error_type":"service_unavailable","safe_error_message":"Upstream timed out.","confidence":0.8,"reason":"timeout"}]}`)
	require.NoError(t, err)
	require.Len(t, rules, 2)
	assert.Equal(t, http.StatusTeapot, rules[0].StatusCode)
	assert.Equal(t, http.StatusGatewayTimeout, rules[1].StatusCode)
}

func TestParseErrorGovernanceAIOrganizationRejectsInvalidStatusAndDuplicates(t *testing.T) {
	_, err := parseErrorGovernanceAIOrganization(`{"summary":"bad","rules":[{"enabled":true,"rule_code":"bad_status","category":"parameter_validation","match_type":"contains","match_pattern":"bad","safe_error_code":"bad_request","safe_error_type":"invalid_request_error","safe_error_message":"Bad request.","status_code":200}]}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400 and 599")

	_, err = parseErrorGovernanceAIOrganization(`{"summary":"duplicate","rules":[{"enabled":true,"rule_code":"same_rule","category":"parameter_validation","match_type":"contains","match_pattern":"first","safe_error_message":"First."},{"enabled":true,"rule_code":"same_rule","category":"parameter_validation","match_type":"contains","match_pattern":"second","safe_error_message":"Second."}]}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
}

func TestUpdateOptionRelayErrorGovernanceRejectsInvalidCustomRules(t *testing.T) {
	setupLogScreeningTestDB(t)
	model.InitOptionMap()

	tests := []struct {
		name    string
		config  string
		message string
	}{
		{
			name:    "duplicate rule code",
			config:  `{"enabled":true,"custom_rules":[{"enabled":true,"rule_code":"duplicate_rule","match_type":"contains","match_pattern":"first","safe_error_message":"First."},{"enabled":true,"rule_code":"duplicate_rule","match_type":"contains","match_pattern":"second","safe_error_message":"Second."}]}`,
			message: "duplicate",
		},
		{
			name:    "built-in rule conflict",
			config:  `{"enabled":true,"custom_rules":[{"enabled":true,"rule_code":"internal_error","match_type":"contains","match_pattern":"override","safe_error_message":"Override."}]}`,
			message: "built-in",
		},
		{
			name:    "invalid regex",
			config:  `{"enabled":true,"custom_rules":[{"enabled":true,"rule_code":"invalid_regex","match_type":"regex","match_pattern":"(","safe_error_message":"Invalid."}]}`,
			message: "valid Go regex",
		},
		{
			name:    "invalid status",
			config:  `{"enabled":true,"custom_rules":[{"enabled":true,"rule_code":"invalid_status","match_type":"contains","match_pattern":"bad","safe_error_message":"Invalid.","status_code":600}]}`,
			message: "400 and 599",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, recorder := newOptionUpdateContext(t, "relay_error_governance", test.config)
			UpdateOption(ctx)
			resp := decodeLogScreeningResponse(t, recorder)
			assert.False(t, resp.Success)
			assert.Contains(t, resp.Message, test.message)
		})
	}
}

func TestUpdateOptionsBulkRejectsInvalidRelayErrorGovernanceDottedKey(t *testing.T) {
	setupLogScreeningTestDB(t)
	model.InitOptionMap()
	err := model.UpdateOptionsBulk(map[string]string{
		"relay_error_governance.custom_rules": `[{"enabled":true,"rule_code":"bad_status","match_type":"contains","match_pattern":"bad","safe_error_message":"Bad.","status_code":200}]`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400 and 599")
}

func TestRelayErrorGovernanceDottedWritesCannotBypassValidation(t *testing.T) {
	setupLogScreeningTestDB(t)
	model.InitOptionMap()
	invalid := `[{"enabled":true,"rule_code":"bad_status","match_type":"contains","match_pattern":"bad","safe_error_message":"Bad.","status_code":200}]`

	err := model.UpdateOption(system_setting.RelayErrorGovernanceCustomRulesOptionKey, invalid)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400 and 599")

	ctx, recorder := newOptionUpdateContext(t, system_setting.RelayErrorGovernanceCustomRulesOptionKey, invalid)
	UpdateOption(ctx)
	resp := decodeLogScreeningResponse(t, recorder)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "400 and 599")
}

func TestUpdateOptionsBulkRejectsConflictingRelayErrorGovernanceSnapshot(t *testing.T) {
	setupLogScreeningTestDB(t)
	model.InitOptionMap()
	before := *system_setting.GetRelayErrorGovernanceSetting()

	err := model.UpdateOptionsBulk(map[string]string{
		system_setting.RelayErrorGovernanceOptionKey:        `{"enabled":true}`,
		system_setting.RelayErrorGovernanceEnabledOptionKey: "false",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflicts")
	after := system_setting.GetRelayErrorGovernanceSetting()
	assert.Equal(t, before.Enabled, after.Enabled)
}

func TestUpdateOptionRelayErrorGovernanceDottedKeyPersistsCompleteSnapshot(t *testing.T) {
	setupLogScreeningTestDB(t)
	model.InitOptionMap()
	original := *system_setting.GetRelayErrorGovernanceSetting()
	t.Cleanup(func() {
		_, _ = model.UpdateRelayErrorGovernanceSetting(original)
	})

	require.NoError(t, model.UpdateOption(system_setting.RelayErrorGovernanceEnabledOptionKey, "false"))
	assert.False(t, system_setting.GetRelayErrorGovernanceSetting().Enabled)

	var aggregate model.Option
	require.NoError(t, model.DB.Where("key = ?", system_setting.RelayErrorGovernanceOptionKey).First(&aggregate).Error)
	var persisted system_setting.RelayErrorGovernanceSetting
	require.NoError(t, json.Unmarshal([]byte(aggregate.Value), &persisted))
	assert.False(t, persisted.Enabled)
}

func TestInitOptionMapLoadsLegacyAggregateAsCoherentRuntimeSnapshot(t *testing.T) {
	setupLogScreeningTestDB(t)
	original := *system_setting.GetRelayErrorGovernanceSetting()
	t.Cleanup(func() {
		_, _ = model.UpdateRelayErrorGovernanceSetting(original)
	})

	legacy := `{"enabled":false,"custom_rules":[{"enabled":true,"rule_code":"legacy_bad_status","match_type":"contains","match_pattern":"legacy failure","safe_error_message":"Legacy failure.","status_code":200}]}`
	require.NoError(t, model.DB.Create(&model.Option{
		Key:   system_setting.RelayErrorGovernanceOptionKey,
		Value: legacy,
	}).Error)

	model.InitOptionMap()
	cfg := system_setting.GetRelayErrorGovernanceSetting()
	assert.False(t, cfg.Enabled)
	require.Len(t, cfg.CustomRules, 1)
	assert.Equal(t, 200, cfg.CustomRules[0].StatusCode)

	common.OptionMapRWMutex.RLock()
	dotted := common.OptionMap[system_setting.RelayErrorGovernanceCustomRulesOptionKey]
	aggregate := common.OptionMap[system_setting.RelayErrorGovernanceOptionKey]
	common.OptionMapRWMutex.RUnlock()
	assert.Contains(t, dotted, "legacy_bad_status")
	var persisted system_setting.RelayErrorGovernanceSetting
	require.NoError(t, json.Unmarshal([]byte(aggregate), &persisted))
	assert.False(t, persisted.Enabled)
	require.Len(t, persisted.CustomRules, 1)
}

func TestInitOptionMapRejectsMalformedLegacyDottedSnapshotWithoutPartialApply(t *testing.T) {
	setupLogScreeningTestDB(t)
	model.InitOptionMap()
	original := *system_setting.GetRelayErrorGovernanceSetting()
	t.Cleanup(func() {
		_, _ = model.UpdateRelayErrorGovernanceSetting(original)
	})
	baseline, err := model.UpdateRelayErrorGovernanceSetting(system_setting.RelayErrorGovernanceSetting{Enabled: true})
	require.NoError(t, err)

	require.NoError(t, model.DB.Model(&model.Option{}).
		Where("key = ?", system_setting.RelayErrorGovernanceCustomRulesOptionKey).
		Update("value", "{not-json").Error)
	model.InitOptionMap()

	cfg := system_setting.GetRelayErrorGovernanceSetting()
	assert.Equal(t, baseline.Enabled, cfg.Enabled)
	assert.Empty(t, cfg.CustomRules)
	common.OptionMapRWMutex.RLock()
	dotted := common.OptionMap[system_setting.RelayErrorGovernanceCustomRulesOptionKey]
	common.OptionMapRWMutex.RUnlock()
	assert.NotEqual(t, "{not-json", dotted)
}

func TestInitOptionMapUsesDottedGovernanceValuesOverStaleAggregate(t *testing.T) {
	setupLogScreeningTestDB(t)
	original := *system_setting.GetRelayErrorGovernanceSetting()
	t.Cleanup(func() {
		_, _ = model.UpdateRelayErrorGovernanceSetting(original)
	})
	require.NoError(t, model.DB.Create(&model.Option{
		Key:   system_setting.RelayErrorGovernanceOptionKey,
		Value: `{"enabled":true}`,
	}).Error)
	require.NoError(t, model.DB.Create(&model.Option{
		Key:   system_setting.RelayErrorGovernanceEnabledOptionKey,
		Value: "false",
	}).Error)

	model.InitOptionMap()
	assert.False(t, system_setting.GetRelayErrorGovernanceSetting().Enabled)
	common.OptionMapRWMutex.RLock()
	aggregate := common.OptionMap[system_setting.RelayErrorGovernanceOptionKey]
	common.OptionMapRWMutex.RUnlock()
	var persisted system_setting.RelayErrorGovernanceSetting
	require.NoError(t, json.Unmarshal([]byte(aggregate), &persisted))
	assert.False(t, persisted.Enabled)
}

func TestInitOptionMapUsesValidDottedGovernanceWhenAggregateIsMalformed(t *testing.T) {
	setupLogScreeningTestDB(t)
	original := *system_setting.GetRelayErrorGovernanceSetting()
	t.Cleanup(func() {
		_, _ = model.UpdateRelayErrorGovernanceSetting(original)
	})
	require.NoError(t, model.DB.Create(&model.Option{
		Key:   system_setting.RelayErrorGovernanceOptionKey,
		Value: "{not-json",
	}).Error)
	require.NoError(t, model.DB.Create(&model.Option{
		Key:   system_setting.RelayErrorGovernanceEnabledOptionKey,
		Value: "false",
	}).Error)

	model.InitOptionMap()
	assert.False(t, system_setting.GetRelayErrorGovernanceSetting().Enabled)
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
