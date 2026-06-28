package model

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOptionMap_ContainsSensitiveUAKeys_NotAutoBanSync verifies that
// InitOptionMap registers the Phase 5 sensitive UA/prompt option keys AND that
// the deprecated ban_sync keys are NOT registered.
func TestOptionMap_ContainsSensitiveUAKeys_NotAutoBanSync(t *testing.T) {
	InitOptionMap()

	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()

	expectedSensitiveKeys := []string{
		"CheckSensitiveOnUAEnabled",
		"CheckSensitiveOnEmptyUAEnabled",
		"CheckSensitiveOnEmptyUAAutoBanEnabled",
		"SensitiveUABlockedRegexes",
		"SensitivePromptRegexRules",
		"SensitiveUARegexRules",
		"SensitiveUAGroupRegexRules",
		"SensitivePromptBlockedMessage",
		"SensitiveUABlockedMessage",
		"SensitiveEmptyUABlockedMessage",
		"SensitiveEmptyUABlockedHTTPStatusCode",
		"SensitiveEmptyUABlockedErrorCode",
	}
	for _, k := range expectedSensitiveKeys {
		_, ok := common.OptionMap[k]
		assert.True(t, ok, "expected option key %q in OptionMap", k)
	}

	// ban_sync keys must NOT be registered.
	for _, banned := range []string{"CheckSensitiveAutoBanSyncEnabled", "AutoBanSync"} {
		_, ok := common.OptionMap[banned]
		assert.False(t, ok, "banned ban_sync key %q must NOT be in OptionMap", banned)
	}

	// The registered config modules are exported under their dotted prefix.
	_, logScreeningOK := common.OptionMap["log_screening.enabled"]
	assert.True(t, logScreeningOK, "expected log_screening.enabled from ExportAllConfigs")
	_, relayParamOK := common.OptionMap["relay_param_record.max_value_bytes"]
	assert.True(t, relayParamOK, "expected relay_param_record.max_value_bytes from ExportAllConfigs")
}

// TestUpdateOptionMap_SensitiveKeysRoundtrip verifies updateOptionMap applies
// persisted values for the new sensitive keys and enforces the empty-message
// fallbacks for the blocked-message fields.
func TestUpdateOptionMap_SensitiveKeysRoundtrip(t *testing.T) {
	// Initialize OptionMap so updateOptionMap can write into it.
	InitOptionMap()
	// Snapshot and restore the globals touched by updateOptionMap.
	origOnUA := setting.CheckSensitiveOnUAEnabled
	origEmptyUA := setting.CheckSensitiveOnEmptyUAEnabled
	origEmptyAutoBan := setting.CheckSensitiveOnEmptyUAAutoBanEnabled
	origPromptMsg := setting.SensitivePromptBlockedMessage
	origUAMsg := setting.SensitiveUABlockedMessage
	origEmptyMsg := setting.SensitiveEmptyUABlockedMessage
	origStatus := setting.SensitiveEmptyUABlockedHTTPStatusCode
	origCode := setting.SensitiveEmptyUABlockedErrorCode
	origPromptRules := setting.SensitivePromptRegexRules
	origUARules := setting.SensitiveUARegexRules
	origUAGroupRules := setting.SensitiveUAGroupRegexRules
	origUARegexes := setting.UABlockedRegexes
	t.Cleanup(func() {
		setting.CheckSensitiveOnUAEnabled = origOnUA
		setting.CheckSensitiveOnEmptyUAEnabled = origEmptyUA
		setting.CheckSensitiveOnEmptyUAAutoBanEnabled = origEmptyAutoBan
		setting.SensitivePromptBlockedMessage = origPromptMsg
		setting.SensitiveUABlockedMessage = origUAMsg
		setting.SensitiveEmptyUABlockedMessage = origEmptyMsg
		setting.SensitiveEmptyUABlockedHTTPStatusCode = origStatus
		setting.SensitiveEmptyUABlockedErrorCode = origCode
		setting.SensitivePromptRegexRules = origPromptRules
		setting.SensitiveUARegexRules = origUARules
		setting.SensitiveUAGroupRegexRules = origUAGroupRules
		setting.UABlockedRegexes = origUARegexes
	})

	require.NoError(t, UpdateOption("CheckSensitiveOnUAEnabled", "true"))
	assert.True(t, setting.CheckSensitiveOnUAEnabled)

	require.NoError(t, UpdateOption("CheckSensitiveOnEmptyUAEnabled", "true"))
	assert.True(t, setting.CheckSensitiveOnEmptyUAEnabled)

	require.NoError(t, UpdateOption("CheckSensitiveOnEmptyUAAutoBanEnabled", "true"))
	assert.True(t, setting.CheckSensitiveOnEmptyUAAutoBanEnabled)

	// Empty prompt/UA blocked message falls back to the default copy.
	require.NoError(t, UpdateOption("SensitivePromptBlockedMessage", "   "))
	assert.Equal(t, "请求包含违规内容，已被系统拦截", setting.SensitivePromptBlockedMessage)

	require.NoError(t, UpdateOption("SensitiveUABlockedMessage", "   "))
	assert.Equal(t, "当前请求来源已被系统策略拦截", setting.SensitiveUABlockedMessage)

	// Empty-UA message trims but does NOT fall back (stays empty).
	require.NoError(t, UpdateOption("SensitiveEmptyUABlockedMessage", "  hi  "))
	assert.Equal(t, "hi", setting.SensitiveEmptyUABlockedMessage)

	// Out-of-range HTTP status is now rejected by the validator (previously
	// clamped at apply time). Verify rejection.
	err := UpdateOption("SensitiveEmptyUABlockedHTTPStatusCode", "999")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "100-599")
	// Valid status still succeeds.
	require.NoError(t, UpdateOption("SensitiveEmptyUABlockedHTTPStatusCode", "418"))
	assert.Equal(t, 418, setting.SensitiveEmptyUABlockedHTTPStatusCode)

	// Empty error code is now rejected by the validator (previously fell back).
	err = UpdateOption("SensitiveEmptyUABlockedErrorCode", "   ")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不能为空")
	require.NoError(t, UpdateOption("SensitiveEmptyUABlockedErrorCode", "custom_err"))
	assert.Equal(t, "custom_err", setting.SensitiveEmptyUABlockedErrorCode)

	// Regex rules roundtrip through the option layer.
	rulesJSON := `[{"pattern":"foo","rule_name":"r1","message":"blocked","http_status_code":403,"error_code":"ua_blocked","auto_ban":true}]`
	require.NoError(t, UpdateOption("SensitivePromptRegexRules", rulesJSON))
	require.Len(t, setting.SensitivePromptRegexRules, 1)
	assert.Equal(t, "foo", setting.SensitivePromptRegexRules[0].Pattern)
	assert.True(t, setting.SensitivePromptRegexRules[0].AutoBan)
	require.NoError(t, UpdateOption("SensitiveUARegexRules", rulesJSON))
	require.Len(t, setting.SensitiveUARegexRules, 1)
	assert.Equal(t, "foo", setting.SensitiveUARegexRules[0].Pattern)
	require.NoError(t, UpdateOption("SensitiveUAGroupRegexRules", `{"vip":[{"pattern":"vipbot/.*","message":"vip blocked"}]}`))
	require.Len(t, setting.SensitiveUAGroupRegexRules["vip"], 1)
	assert.Equal(t, "vipbot/.*", setting.SensitiveUAGroupRegexRules["vip"][0].Pattern)

	// UA regex lines roundtrip.
	require.NoError(t, UpdateOption("SensitiveUABlockedRegexes", "curl/.*\npython/.*"))
	assert.Equal(t, []string{"curl/.*", "python/.*"}, setting.UABlockedRegexes)

	// The persisted Option row reflects the latest value (roundtrip via DB).
	var opt Option
	require.NoError(t, DB.WithContext(context.Background()).First(&opt, Option{Key: "SensitiveEmptyUABlockedErrorCode"}).Error)
	assert.Equal(t, "custom_err", opt.Value)
}

// TestUpdateOption_RejectsBanSyncLegacyKeys verifies that UpdateOption rejects
// (does not write to DB, does not store in OptionMap) any deprecated ban_sync
// legacy key. This prevents stale frontends/scripts from re-persisting them.
func TestUpdateOption_RejectsBanSyncLegacyKeys(t *testing.T) {
	for _, key := range []string{
		"CheckSensitiveAutoBanSyncEnabled",
		"AutoBanSync",
		"ban_sync.enabled",
		"ban_sync.project_id",
	} {
		t.Run(key, func(t *testing.T) {
			// Ensure the key is absent before the call.
			common.OptionMapRWMutex.Lock()
			delete(common.OptionMap, key)
			common.OptionMapRWMutex.Unlock()
			require.NoError(t, DB.Where("key = ?", key).Delete(&Option{}).Error)

			require.NoError(t, UpdateOption(key, "true"),
				"UpdateOption must return nil (silent reject), not an error")

			// OptionMap must NOT contain the banned key.
			common.OptionMapRWMutex.RLock()
			_, present := common.OptionMap[key]
			common.OptionMapRWMutex.RUnlock()
			assert.False(t, present, "banned key %q must not be stored in OptionMap", key)

			// The options table must NOT contain a row for the banned key.
			var count int64
			require.NoError(t, DB.Model(&Option{}).Where("key = ?", key).Count(&count).Error)
			assert.Equal(t, int64(0), count, "banned key %q must not be persisted to the options table", key)
		})
	}
}

// TestUpdateOptionsBulk_SkipsBanSyncLegacyKeys verifies the bulk path also skips
// banned keys while still persisting the legitimate keys in the same call.
func TestUpdateOptionsBulk_SkipsBanSyncLegacyKeys(t *testing.T) {
	for _, key := range []string{"CheckSensitiveAutoBanSyncEnabled", "AutoBanSync", "ban_sync.enabled"} {
		require.NoError(t, DB.Where("key = ?", key).Delete(&Option{}).Error)
	}
	require.NoError(t, DB.Where("key = ?", "SensitiveEmptyUABlockedErrorCode").Delete(&Option{}).Error)

	values := map[string]string{
		"CheckSensitiveAutoBanSyncEnabled": "true",
		"AutoBanSync":                      "true",
		"ban_sync.enabled":                 "true",
		"SensitiveEmptyUABlockedErrorCode": "bulk_custom_err",
	}
	require.NoError(t, UpdateOptionsBulk(values))

	// Legitimate key persisted + applied.
	var opt Option
	require.NoError(t, DB.First(&opt, Option{Key: "SensitiveEmptyUABlockedErrorCode"}).Error)
	assert.Equal(t, "bulk_custom_err", opt.Value)

	// Banned keys NOT persisted.
	for _, key := range []string{"CheckSensitiveAutoBanSyncEnabled", "AutoBanSync", "ban_sync.enabled"} {
		var count int64
		require.NoError(t, DB.Model(&Option{}).Where("key = ?", key).Count(&count).Error)
		assert.Equal(t, int64(0), count, "banned key %q must not be persisted by bulk update", key)
	}
}

// TestLoadOptionsFromDatabase_SkipsBanSyncLegacyKeys seeds a banned key directly
// into the options table and verifies loadOptionsFromDatabase skips it (does not
// populate OptionMap).
func TestLoadOptionsFromDatabase_SkipsBanSyncLegacyKeys(t *testing.T) {
	// Seed banned + legitimate rows directly.
	require.NoError(t, DB.Save(&Option{Key: "CheckSensitiveAutoBanSyncEnabled", Value: "true"}).Error)
	require.NoError(t, DB.Save(&Option{Key: "AutoBanSync", Value: "true"}).Error)
	require.NoError(t, DB.Save(&Option{Key: "ban_sync.enabled", Value: "true"}).Error)
	require.NoError(t, DB.Save(&Option{Key: "SensitiveEmptyUABlockedErrorCode", Value: "loaded_err"}).Error)

	// Clear OptionMap entry for the legitimate key to prove the load repopulates it.
	common.OptionMapRWMutex.Lock()
	delete(common.OptionMap, "SensitiveEmptyUABlockedErrorCode")
	common.OptionMapRWMutex.Unlock()

	loadOptionsFromDatabase()

	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	// Banned keys absent from OptionMap.
	for _, key := range []string{"CheckSensitiveAutoBanSyncEnabled", "AutoBanSync", "ban_sync.enabled"} {
		_, present := common.OptionMap[key]
		assert.False(t, present, "banned key %q must not be loaded into OptionMap", key)
	}
	// Legitimate key loaded.
	assert.Equal(t, "loaded_err", common.OptionMap["SensitiveEmptyUABlockedErrorCode"])
}

// TestOptionMap_SensitiveKeys_NoAutoBanSyncInSource is a static guard: it
// scans the registered sensitive option keys and asserts none mention ban_sync.
func TestOptionMap_SensitiveKeys_NoAutoBanSyncInSource(t *testing.T) {
	InitOptionMap()
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	for k := range common.OptionMap {
		if !strings.HasPrefix(k, "Sensitive") && !strings.HasPrefix(k, "CheckSensitive") {
			continue
		}
		assert.NotContains(t, strings.ToLower(k), "bansync",
			"no sensitive option key may reference ban_sync; found %q", k)
	}
}

// TestIsBannedBanSyncOptionKey_CaseInsensitive verifies the ban_sync legacy
// key detector matches the canonical spellings and dotted ban_sync.* keys,
// while legitimate sensitive keys are NOT matched.
func TestIsBannedBanSyncOptionKey_CaseInsensitive(t *testing.T) {
	for _, key := range []string{
		"CheckSensitiveAutoBanSyncEnabled",
		"checksensitiveautobansyncenabled",
		"CHECKSENSITIVEAUTOBANSYNCENABLED",
		"AutoBanSync",
		"autobansync",
		"ban_sync.enabled",
		"ban_sync.project_id",
		"ban_sync.screening_enabled",
	} {
		assert.True(t, isBannedBanSyncOptionKey(key), "expected %q to be banned", key)
	}
	for _, key := range []string{
		"CheckSensitiveOnUAEnabled",
		"CheckSensitiveOnEmptyUAAutoBanEnabled",
		"SensitiveEmptyUABlockedErrorCode",
		"SensitivePromptRegexRules",
		"log_screening.enabled",
		"relay_param_record.max_value_bytes",
		"",
	} {
		assert.False(t, isBannedBanSyncOptionKey(key), "expected %q to be ALLOWED", key)
	}
}

// keep strconv import referenced when this file is compiled standalone.
var _ = strconv.Itoa

// TestUpdateOption_RejectsInvalidSensitiveRegex verifies that model.UpdateOption
// (the model-layer path, bypassing the controller) rejects invalid sensitive
// regex/status/code values and does NOT write to DB or OptionMap.
func TestUpdateOption_RejectsInvalidSensitiveRegex(t *testing.T) {
	InitOptionMap()

	// Invalid UA blocked regex (unclosed group).
	require.NoError(t, DB.Where("key = ?", "SensitiveUABlockedRegexes").Delete(&Option{}).Error)
	err := UpdateOption("SensitiveUABlockedRegexes", "good\n(unclosed\n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "非法正则")
	// Not persisted.
	var count int64
	require.NoError(t, DB.Model(&Option{}).Where("key = ?", "SensitiveUABlockedRegexes").Count(&count).Error)
	assert.Equal(t, int64(0), count, "invalid value must not be persisted")

	// Invalid prompt regex rules JSON.
	err = UpdateOption("SensitivePromptRegexRules", "{not-json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JSON")

	// Prompt rule with auto_ban but no rule_name.
	err = UpdateOption("SensitivePromptRegexRules", `[{"pattern":"foo","auto_ban":true}]`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rule_name")

	// Out-of-range HTTP status code.
	err = UpdateOption("SensitiveEmptyUABlockedHTTPStatusCode", "999")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "100-599")

	// Empty error code.
	err = UpdateOption("SensitiveEmptyUABlockedErrorCode", "   ")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不能为空")

	// Valid values still succeed.
	err = UpdateOption("SensitiveUABlockedRegexes", "curl/.*")
	require.NoError(t, err)
	err = UpdateOption("SensitiveEmptyUABlockedHTTPStatusCode", "418")
	require.NoError(t, err)
}

// TestUpdateOption_ShortMsgExtraBillingValidateAndNormalize verifies the
// model-layer Phase 10C guard: model.UpdateOption (which bypasses the
// controller) must reject invalid `quota_setting.short_msg_extra_billing`
// values and must persist + apply the normalized form for valid values.
func TestUpdateOption_ShortMsgExtraBillingValidateAndNormalize(t *testing.T) {
	InitOptionMap()

	// Snapshot and restore the in-memory QuotaSetting so the test does not
	// leak global state to other tests in the package.
	orig := *operation_setting.GetQuotaSetting()
	t.Cleanup(func() {
		*operation_setting.GetQuotaSetting() = orig
	})

	// Invalid JSON -> rejected, nothing persisted.
	require.NoError(t, DB.Where("key = ?", operation_setting.ShortMsgExtraBillingOptionKey).Delete(&Option{}).Error)
	err := UpdateOption(operation_setting.ShortMsgExtraBillingOptionKey, `{not-json`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "无效 JSON")
	var count int64
	require.NoError(t, DB.Model(&Option{}).Where("key = ?", operation_setting.ShortMsgExtraBillingOptionKey).Count(&count).Error)
	assert.Equal(t, int64(0), count, "invalid config must not be persisted")

	// Unknown mode -> rejected.
	err = UpdateOption(operation_setting.ShortMsgExtraBillingOptionKey, `{"mode":"bogus"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mode 必须是")

	// Rule with non-positive fee_quota -> rejected.
	err = UpdateOption(operation_setting.ShortMsgExtraBillingOptionKey, `{"mode":"shadow","rules":[{"id":"r1","group":"m","trigger":"input_tokens_below","threshold":1,"fee_quota":0}]}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fee_quota 必须 > 0")

	// Valid config -> persisted + applied + normalized (whitespace trimmed,
	// duplicate response_modes dropped).
	raw := `{"mode":"shadow","rules":[{"id":"  r1  ","group":"  gpt-4o-mini  ","trigger":"input_tokens_below","threshold":100,"fee_quota":500,"response_modes":[" claude ","claude"," gemini "]}]}`
	require.NoError(t, UpdateOption(operation_setting.ShortMsgExtraBillingOptionKey, raw))

	qs := operation_setting.GetQuotaSetting()
	require.Equal(t, operation_setting.ShortMsgExtraBillingModeShadow, qs.ShortMsgExtraBilling.Mode)
	require.Len(t, qs.ShortMsgExtraBilling.Rules, 1)
	assert.Equal(t, "r1", qs.ShortMsgExtraBilling.Rules[0].ID)
	assert.Equal(t, "gpt-4o-mini", qs.ShortMsgExtraBilling.Rules[0].Group)
	assert.Equal(t, []string{"claude", "gemini"}, qs.ShortMsgExtraBilling.Rules[0].ResponseModes)

	// The DB row holds the normalized form, not the raw input.
	var opt Option
	require.NoError(t, DB.WithContext(context.Background()).First(&opt, Option{Key: operation_setting.ShortMsgExtraBillingOptionKey}).Error)
	assert.JSONEq(t, `{"mode":"shadow","rules":[{"id":"r1","group":"gpt-4o-mini","trigger":"input_tokens_below","threshold":100,"fee_quota":500,"waive_when_completion_tokens_zero":false,"response_modes":["claude","gemini"]}]}`, opt.Value)
}

// TestUpdateOptionsBulk_ShortMsgExtraBillingValidateAndNormalize verifies the
// bulk path also validates and substitutes the normalized value so internal
// callers cannot bypass the controller to persist an invalid config.
func TestUpdateOptionsBulk_ShortMsgExtraBillingValidateAndNormalize(t *testing.T) {
	InitOptionMap()

	orig := *operation_setting.GetQuotaSetting()
	t.Cleanup(func() {
		*operation_setting.GetQuotaSetting() = orig
	})

	// Bulk with one invalid value -> entire transaction rejected, nothing
	// persisted for any key in the batch.
	require.NoError(t, DB.Where("key = ?", operation_setting.ShortMsgExtraBillingOptionKey).Delete(&Option{}).Error)
	require.NoError(t, DB.Where("key = ?", "SensitiveEmptyUABlockedErrorCode").Delete(&Option{}).Error)
	err := UpdateOptionsBulk(map[string]string{
		operation_setting.ShortMsgExtraBillingOptionKey: `{"mode":"bogus"}`,
		"SensitiveEmptyUABlockedErrorCode":              "bulk_err",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mode 必须是")
	// Neither key persisted (transaction rolled back).
	for _, key := range []string{operation_setting.ShortMsgExtraBillingOptionKey, "SensitiveEmptyUABlockedErrorCode"} {
		var count int64
		require.NoError(t, DB.Model(&Option{}).Where("key = ?", key).Count(&count).Error)
		assert.Equal(t, int64(0), count, "key %q must not be persisted on failed bulk", key)
	}

	// Bulk with all valid values -> persisted + normalized.
	require.NoError(t, UpdateOptionsBulk(map[string]string{
		operation_setting.ShortMsgExtraBillingOptionKey: `{"mode":"enforce","rules":[{"id":"r1","group":"gpt-4o-mini","trigger":"input_tokens_below","threshold":50,"fee_quota":250}]}`,
		"SensitiveEmptyUABlockedErrorCode":              "bulk_err",
	}))
	qs := operation_setting.GetQuotaSetting()
	require.Equal(t, operation_setting.ShortMsgExtraBillingModeEnforce, qs.ShortMsgExtraBilling.Mode)
	require.Len(t, qs.ShortMsgExtraBilling.Rules, 1)
	assert.Equal(t, "r1", qs.ShortMsgExtraBilling.Rules[0].ID)

	var opt Option
	require.NoError(t, DB.First(&opt, Option{Key: operation_setting.ShortMsgExtraBillingOptionKey}).Error)
	// Normalized form is persisted (no extra whitespace, stable field order).
	assert.JSONEq(t, `{"mode":"enforce","rules":[{"id":"r1","group":"gpt-4o-mini","trigger":"input_tokens_below","threshold":50,"fee_quota":250,"waive_when_completion_tokens_zero":false}]}`, opt.Value)
}
