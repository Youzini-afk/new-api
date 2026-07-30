package controller

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/external_app_setting"
	"github.com/gin-gonic/gin"
)

type externalGameSettingsResponse struct {
	Enabled                   bool                                      `json:"enabled"`
	AppId                     string                                    `json:"app_id"`
	RedirectUri               string                                    `json:"redirect_uri"`
	CodeTTLSeconds            int                                       `json:"code_ttl_seconds"`
	SignatureToleranceSeconds int                                       `json:"signature_tolerance_seconds"`
	AppSecretConfigured       bool                                      `json:"app_secret_configured"`
	AppSecretSource           string                                    `json:"app_secret_source"`
	EnvironmentManaged        bool                                      `json:"environment_managed"`
	EnvironmentOverrides      external_app_setting.EnvironmentOverrides `json:"environment_overrides"`
}

type externalGameSettingsUpdateRequest struct {
	Enabled                   *bool   `json:"enabled"`
	AppId                     *string `json:"app_id"`
	AppSecret                 *string `json:"app_secret"`
	RedirectUri               *string `json:"redirect_uri"`
	CodeTTLSeconds            *int    `json:"code_ttl_seconds"`
	SignatureToleranceSeconds *int    `json:"signature_tolerance_seconds"`
}

// GetExternalGameSettings exposes the effective non-secret configuration to
// root operators. The shared secret is never serialized; only its configured
// state and storage source are returned.
func GetExternalGameSettings(c *gin.Context) {
	common.ApiSuccess(c, buildExternalGameSettingsResponse())
}

// UpdateExternalGameSettings validates a complete effective candidate before
// atomically persisting the database-managed fields. Omitted fields are
// preserved, an empty secret means "keep the current secret", and fields
// shadowed by environment variables are deliberately ignored.
func UpdateExternalGameSettings(c *gin.Context) {
	var request externalGameSettingsUpdateRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorMsg(c, "invalid external game settings")
		return
	}

	overrides := external_app_setting.GetEnvironmentOverrides()
	candidate := external_app_setting.GetStoredSettings()
	changedKeys := make(map[string]struct{}, 6)
	includeSecret := false

	if request.Enabled != nil && !overrides.Enabled {
		candidate.Enabled = *request.Enabled
		changedKeys[external_app_setting.OptionEnabled] = struct{}{}
	}
	if request.AppId != nil && !overrides.AppId {
		candidate.AppId = *request.AppId
		changedKeys[external_app_setting.OptionAppId] = struct{}{}
	}
	if request.AppSecret != nil && !overrides.AppSecret {
		secret := strings.TrimSpace(*request.AppSecret)
		if secret != "" {
			candidate.AppSecret = secret
			includeSecret = true
			changedKeys[external_app_setting.OptionAppSecret] = struct{}{}
		}
	}
	if request.RedirectUri != nil && !overrides.RedirectUri {
		candidate.RedirectUri = *request.RedirectUri
		changedKeys[external_app_setting.OptionRedirectUri] = struct{}{}
	}
	if request.CodeTTLSeconds != nil && !overrides.CodeTTLSeconds {
		candidate.CodeTTLSeconds = *request.CodeTTLSeconds
		changedKeys[external_app_setting.OptionCodeTTLSeconds] = struct{}{}
	}
	if request.SignatureToleranceSeconds != nil && !overrides.SignatureToleranceSeconds {
		candidate.SignatureToleranceSeconds = *request.SignatureToleranceSeconds
		changedKeys[external_app_setting.OptionSignatureToleranceSeconds] = struct{}{}
	}

	prepared, err := external_app_setting.PrepareForStorage(candidate)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	effective := external_app_setting.ApplyEnvironmentOverrides(prepared)
	if err := effective.ValidateConfiguration(); err != nil {
		common.ApiError(c, err)
		return
	}

	allValues := external_app_setting.OptionValues(prepared, includeSecret)
	values := make(map[string]string, len(changedKeys))
	for key := range changedKeys {
		if value, ok := allValues[key]; ok {
			values[key] = value
		}
	}
	if err := model.UpdateOptionsBulk(values); err != nil {
		common.ApiError(c, err)
		return
	}

	if len(values) > 0 {
		recordManageAudit(c, "option.update", map[string]interface{}{
			"key": "external_game",
		})
	}
	common.ApiSuccess(c, buildExternalGameSettingsResponse())
}

func buildExternalGameSettingsResponse() externalGameSettingsResponse {
	stored := external_app_setting.GetStoredSettings()
	effective := external_app_setting.ApplyEnvironmentOverrides(stored)
	overrides := external_app_setting.GetEnvironmentOverrides()
	secretSource := "unset"
	if overrides.AppSecret {
		secretSource = "environment"
	} else if stored.AppSecret != "" {
		secretSource = "database"
	}
	return externalGameSettingsResponse{
		Enabled:                   effective.Enabled,
		AppId:                     effective.AppId,
		RedirectUri:               effective.RedirectUri,
		CodeTTLSeconds:            effective.CodeTTLSeconds,
		SignatureToleranceSeconds: effective.SignatureToleranceSeconds,
		AppSecretConfigured:       effective.AppSecret != "",
		AppSecretSource:           secretSource,
		EnvironmentManaged:        overrides.Any(),
		EnvironmentOverrides:      overrides,
	}
}
