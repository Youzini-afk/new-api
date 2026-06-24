package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"

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
