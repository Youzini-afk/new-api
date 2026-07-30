package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/external_app_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type externalGameSettingsAPIResponse struct {
	Success bool                         `json:"success"`
	Message string                       `json:"message"`
	Data    externalGameSettingsResponse `json:"data"`
}

func setupExternalGameSettingsTest(t *testing.T) {
	t.Helper()
	setupLogScreeningTestDB(t)
	for _, name := range []string{
		"EXTERNAL_GAME_ENABLED",
		"EXTERNAL_GAME_APP_ID",
		"EXTERNAL_GAME_APP_SECRET",
		"EXTERNAL_GAME_REDIRECT_URI",
		"EXTERNAL_GAME_CODE_TTL_SECONDS",
		"EXTERNAL_GAME_SIGNATURE_TOLERANCE_SECONDS",
	} {
		t.Setenv(name, "")
	}

	original := external_app_setting.GetStoredSettings()
	t.Cleanup(func() {
		values := external_app_setting.OptionValues(original, true)
		configValues := make(map[string]string, len(values))
		for key, value := range values {
			configValues[strings.TrimPrefix(key, external_app_setting.OptionPrefix)] = value
		}
		_, _ = config.GlobalConfig.Update("external_game", configValues)
	})

	model.InitOptionMap()
	baseline := external_app_setting.Settings{
		Enabled:                   false,
		AppId:                     "wtfib",
		CodeTTLSeconds:            120,
		SignatureToleranceSeconds: 300,
	}
	require.NoError(t, model.UpdateOptionsBulk(external_app_setting.OptionValues(baseline, true)))
}

func newExternalGameSettingsContext(t *testing.T, method, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, "/api/option/external-game", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("id", 1)
	ctx.Set("role", common.RoleRootUser)
	ctx.Set("username", "root")
	return ctx, recorder
}

func decodeExternalGameSettingsResponse(t *testing.T, recorder *httptest.ResponseRecorder) externalGameSettingsAPIResponse {
	t.Helper()
	var response externalGameSettingsAPIResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func TestGetExternalGameSettingsNeverReturnsSecret(t *testing.T) {
	setupExternalGameSettingsTest(t)
	require.NoError(t, model.UpdateOption(external_app_setting.OptionAppSecret, "database-secret-at-least-16"))

	ctx, recorder := newExternalGameSettingsContext(t, http.MethodGet, "")
	GetExternalGameSettings(ctx)

	response := decodeExternalGameSettingsResponse(t, recorder)
	require.True(t, response.Success, response.Message)
	assert.True(t, response.Data.AppSecretConfigured)
	assert.Equal(t, "database", response.Data.AppSecretSource)
	assert.NotContains(t, recorder.Body.String(), "database-secret-at-least-16")
	var raw map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &raw))
	data, ok := raw["data"].(map[string]any)
	require.True(t, ok)
	_, exposed := data["app_secret"]
	assert.False(t, exposed)
}

func TestUpdateExternalGameSettingsEmptySecretPreservesCredential(t *testing.T) {
	setupExternalGameSettingsTest(t)
	require.NoError(t, model.UpdateOptionsBulk(map[string]string{
		external_app_setting.OptionEnabled:     "true",
		external_app_setting.OptionAppSecret:   "existing-secret-at-least-16",
		external_app_setting.OptionRedirectUri: "https://stocks.example.com/login",
	}))

	body := `{"enabled":true,"app_id":"wtfib-v2","app_secret":"   ","redirect_uri":"https://stocks.example.com/callback","code_ttl_seconds":180,"signature_tolerance_seconds":240}`
	ctx, recorder := newExternalGameSettingsContext(t, http.MethodPut, body)
	UpdateExternalGameSettings(ctx)

	response := decodeExternalGameSettingsResponse(t, recorder)
	require.True(t, response.Success, response.Message)
	assert.Equal(t, "wtfib-v2", response.Data.AppId)
	assert.Equal(t, "https://stocks.example.com/callback", response.Data.RedirectUri)
	assert.Equal(t, 180, response.Data.CodeTTLSeconds)
	assert.Equal(t, 240, response.Data.SignatureToleranceSeconds)
	var secretOption model.Option
	require.NoError(t, model.DB.First(&secretOption, "key = ?", external_app_setting.OptionAppSecret).Error)
	assert.Equal(t, "existing-secret-at-least-16", secretOption.Value)
}

func TestUpdateExternalGameSettingsCanRotateSecret(t *testing.T) {
	setupExternalGameSettingsTest(t)
	body := `{"enabled":true,"app_id":"wtfib","app_secret":"rotated-secret-at-least-16","redirect_uri":"https://stocks.example.com/login","code_ttl_seconds":120,"signature_tolerance_seconds":300}`
	ctx, recorder := newExternalGameSettingsContext(t, http.MethodPut, body)
	UpdateExternalGameSettings(ctx)

	response := decodeExternalGameSettingsResponse(t, recorder)
	require.True(t, response.Success, response.Message)
	assert.True(t, response.Data.AppSecretConfigured)
	assert.Equal(t, "database", response.Data.AppSecretSource)
	var secretOption model.Option
	require.NoError(t, model.DB.First(&secretOption, "key = ?", external_app_setting.OptionAppSecret).Error)
	assert.Equal(t, "rotated-secret-at-least-16", secretOption.Value)
	assert.NotContains(t, recorder.Body.String(), "rotated-secret-at-least-16")
}

func TestUpdateExternalGameSettingsRejectsInvalidCandidateWithoutPartialWrite(t *testing.T) {
	setupExternalGameSettingsTest(t)
	body := `{"enabled":true,"app_id":"must-not-persist","redirect_uri":"http://public.example.com/callback","code_ttl_seconds":120,"signature_tolerance_seconds":300}`
	ctx, recorder := newExternalGameSettingsContext(t, http.MethodPut, body)
	UpdateExternalGameSettings(ctx)

	response := decodeExternalGameSettingsResponse(t, recorder)
	assert.False(t, response.Success)
	assert.Contains(t, response.Message, "secret")
	var appID model.Option
	require.NoError(t, model.DB.First(&appID, "key = ?", external_app_setting.OptionAppId).Error)
	assert.Equal(t, "wtfib", appID.Value)
	var enabled model.Option
	require.NoError(t, model.DB.First(&enabled, "key = ?", external_app_setting.OptionEnabled).Error)
	assert.Equal(t, "false", enabled.Value)
}

func TestExternalGameSettingsReportsAndProtectsEnvironmentOverrides(t *testing.T) {
	setupExternalGameSettingsTest(t)
	t.Setenv("EXTERNAL_GAME_ENABLED", "true")
	t.Setenv("EXTERNAL_GAME_APP_ID", "environment-app")
	t.Setenv("EXTERNAL_GAME_APP_SECRET", "environment-secret-at-least-16")
	t.Setenv("EXTERNAL_GAME_REDIRECT_URI", "https://environment.example.com/callback")

	body := `{"enabled":false,"app_id":"database-app","app_secret":"database-secret-at-least-16","redirect_uri":"https://database.example.com/callback","code_ttl_seconds":180,"signature_tolerance_seconds":240}`
	ctx, recorder := newExternalGameSettingsContext(t, http.MethodPut, body)
	UpdateExternalGameSettings(ctx)

	response := decodeExternalGameSettingsResponse(t, recorder)
	require.True(t, response.Success, response.Message)
	assert.True(t, response.Data.Enabled)
	assert.Equal(t, "environment-app", response.Data.AppId)
	assert.Equal(t, "https://environment.example.com/callback", response.Data.RedirectUri)
	assert.Equal(t, "environment", response.Data.AppSecretSource)
	assert.True(t, response.Data.EnvironmentManaged)
	assert.True(t, response.Data.EnvironmentOverrides.Enabled)
	assert.True(t, response.Data.EnvironmentOverrides.AppId)
	assert.True(t, response.Data.EnvironmentOverrides.AppSecret)
	assert.True(t, response.Data.EnvironmentOverrides.RedirectUri)
	assert.Equal(t, 180, response.Data.CodeTTLSeconds)
	assert.Equal(t, 240, response.Data.SignatureToleranceSeconds)

	var appID model.Option
	require.NoError(t, model.DB.First(&appID, "key = ?", external_app_setting.OptionAppId).Error)
	assert.Equal(t, "wtfib", appID.Value, "environment-managed app ID must not overwrite the database fallback")
	var secret model.Option
	require.NoError(t, model.DB.First(&secret, "key = ?", external_app_setting.OptionAppSecret).Error)
	assert.Empty(t, secret.Value, "environment-managed secret must never be copied into the database")
}

func TestGenericOptionEndpointRejectsExternalGameFields(t *testing.T) {
	setupExternalGameSettingsTest(t)
	ctx, recorder := newOptionUpdateContext(t, external_app_setting.OptionAppSecret, "must-not-be-stored")
	UpdateOption(ctx)

	response := decodeLogScreeningResponse(t, recorder)
	assert.False(t, response.Success)
	assert.Contains(t, response.Message, "dedicated settings API")
	var secret model.Option
	require.NoError(t, model.DB.First(&secret, "key = ?", external_app_setting.OptionAppSecret).Error)
	assert.Empty(t, secret.Value)

	ctx, recorder = newOptionUpdateContext(t, "external_game.unknown", "must-not-be-stored")
	UpdateOption(ctx)
	response = decodeLogScreeningResponse(t, recorder)
	assert.False(t, response.Success)
	var count int64
	require.NoError(t, model.DB.Model(&model.Option{}).Where("key = ?", "external_game.unknown").Count(&count).Error)
	assert.Zero(t, count)
}
