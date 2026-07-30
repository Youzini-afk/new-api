package external_app_setting

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

const ConfigName = "external_game"

const (
	OptionPrefix                    = ConfigName + "."
	OptionEnabled                   = OptionPrefix + "enabled"
	OptionAppId                     = OptionPrefix + "app_id"
	OptionAppSecret                 = OptionPrefix + "app_secret"
	OptionRedirectUri               = OptionPrefix + "redirect_uri"
	OptionCodeTTLSeconds            = OptionPrefix + "code_ttl_seconds"
	OptionSignatureToleranceSeconds = OptionPrefix + "signature_tolerance_seconds"
)

const (
	envEnabled                   = "EXTERNAL_GAME_ENABLED"
	envAppId                     = "EXTERNAL_GAME_APP_ID"
	envAppSecret                 = "EXTERNAL_GAME_APP_SECRET"
	envRedirectUri               = "EXTERNAL_GAME_REDIRECT_URI"
	envCodeTTLSeconds            = "EXTERNAL_GAME_CODE_TTL_SECONDS"
	envSignatureToleranceSeconds = "EXTERNAL_GAME_SIGNATURE_TOLERANCE_SECONDS"
)

// Settings describes the single trusted game application connected to this
// New API deployment. Database options make it manageable from the root
// settings API, while environment variables can keep the shared secret out of
// the database in production.
type Settings struct {
	Enabled                   bool   `json:"enabled"`
	AppId                     string `json:"app_id"`
	AppSecret                 string `json:"app_secret"`
	RedirectUri               string `json:"redirect_uri"`
	CodeTTLSeconds            int    `json:"code_ttl_seconds"`
	SignatureToleranceSeconds int    `json:"signature_tolerance_seconds"`
}

// EnvironmentOverrides reports which database-backed fields are currently
// shadowed by deployment environment variables. It is safe to expose through
// the root settings API because it contains no environment values or secrets.
type EnvironmentOverrides struct {
	Enabled                   bool `json:"enabled"`
	AppId                     bool `json:"app_id"`
	AppSecret                 bool `json:"app_secret"`
	RedirectUri               bool `json:"redirect_uri"`
	CodeTTLSeconds            bool `json:"code_ttl_seconds"`
	SignatureToleranceSeconds bool `json:"signature_tolerance_seconds"`
}

func (overrides EnvironmentOverrides) Any() bool {
	return overrides.Enabled ||
		overrides.AppId ||
		overrides.AppSecret ||
		overrides.RedirectUri ||
		overrides.CodeTTLSeconds ||
		overrides.SignatureToleranceSeconds
}

var defaultSettings = Settings{
	AppId:                     "wtfib",
	CodeTTLSeconds:            120,
	SignatureToleranceSeconds: 300,
}

func init() {
	config.GlobalConfig.Register(ConfigName, &defaultSettings)
}

func IsOptionKey(key string) bool {
	switch key {
	case OptionEnabled,
		OptionAppId,
		OptionAppSecret,
		OptionRedirectUri,
		OptionCodeTTLSeconds,
		OptionSignatureToleranceSeconds:
		return true
	default:
		return false
	}
}

// GetStoredSettings returns the normalized database-backed snapshot without
// applying environment overrides. The secret is intentionally kept inside the
// backend and must never be serialized by a settings response.
func GetStoredSettings() Settings {
	var settings Settings
	if ok, err := config.GlobalConfig.Snapshot(ConfigName, &settings); !ok || err != nil {
		settings = defaultSettings
	}
	normalize(&settings)
	return settings
}

// GetEnvironmentOverrides mirrors the existing non-empty environment-variable
// override semantics used by common.GetEnvOrDefault*. Invalid non-empty values
// still count as deployment-managed so the UI directs operators to fix or
// remove the deployment value instead of editing an unrelated DB fallback.
func GetEnvironmentOverrides() EnvironmentOverrides {
	return EnvironmentOverrides{
		Enabled:                   os.Getenv(envEnabled) != "",
		AppId:                     os.Getenv(envAppId) != "",
		AppSecret:                 os.Getenv(envAppSecret) != "",
		RedirectUri:               os.Getenv(envRedirectUri) != "",
		CodeTTLSeconds:            os.Getenv(envCodeTTLSeconds) != "",
		SignatureToleranceSeconds: os.Getenv(envSignatureToleranceSeconds) != "",
	}
}

// ApplyEnvironmentOverrides returns the effective runtime settings for a
// stored candidate. Empty environment variables intentionally leave the
// database option in effect so operators may choose either deployment style.
func ApplyEnvironmentOverrides(settings Settings) Settings {
	settings.Enabled = common.GetEnvOrDefaultBool(envEnabled, settings.Enabled)
	settings.AppId = common.GetEnvOrDefaultString(envAppId, settings.AppId)
	settings.AppSecret = common.GetEnvOrDefaultString(envAppSecret, settings.AppSecret)
	settings.RedirectUri = common.GetEnvOrDefaultString(envRedirectUri, settings.RedirectUri)
	settings.CodeTTLSeconds = common.GetEnvOrDefault(envCodeTTLSeconds, settings.CodeTTLSeconds)
	settings.SignatureToleranceSeconds = common.GetEnvOrDefault(envSignatureToleranceSeconds, settings.SignatureToleranceSeconds)
	normalize(&settings)
	return settings
}

// GetSettings returns an immutable effective snapshot with optional
// environment overrides.
func GetSettings() Settings {
	return ApplyEnvironmentOverrides(GetStoredSettings())
}

func normalize(settings *Settings) {
	settings.AppId = strings.TrimSpace(settings.AppId)
	settings.AppSecret = strings.TrimSpace(settings.AppSecret)
	settings.RedirectUri = strings.TrimSpace(settings.RedirectUri)
	if settings.CodeTTLSeconds < 30 {
		settings.CodeTTLSeconds = 30
	}
	if settings.CodeTTLSeconds > 600 {
		settings.CodeTTLSeconds = 600
	}
	if settings.SignatureToleranceSeconds < 30 {
		settings.SignatureToleranceSeconds = 30
	}
	if settings.SignatureToleranceSeconds > 600 {
		settings.SignatureToleranceSeconds = 600
	}
}

// PrepareForStorage trims textual values and rejects out-of-range timing
// values instead of silently clamping an administrator's input. Required
// connection fields are validated separately against the effective candidate,
// because some of them may be supplied by environment variables.
func PrepareForStorage(settings Settings) (Settings, error) {
	settings.AppId = strings.TrimSpace(settings.AppId)
	settings.AppSecret = strings.TrimSpace(settings.AppSecret)
	settings.RedirectUri = strings.TrimSpace(settings.RedirectUri)
	if settings.CodeTTLSeconds < 30 || settings.CodeTTLSeconds > 600 {
		return Settings{}, fmt.Errorf("external game authorization code TTL must be between 30 and 600 seconds")
	}
	if settings.SignatureToleranceSeconds < 30 || settings.SignatureToleranceSeconds > 600 {
		return Settings{}, fmt.Errorf("external game signature tolerance must be between 30 and 600 seconds")
	}
	return settings, nil
}

// OptionValues serializes a stored settings snapshot for atomic persistence.
// Callers decide whether to include the secret so an empty password field can
// preserve the existing credential.
func OptionValues(settings Settings, includeSecret bool) map[string]string {
	values := map[string]string{
		OptionEnabled:                   strconv.FormatBool(settings.Enabled),
		OptionAppId:                     settings.AppId,
		OptionRedirectUri:               settings.RedirectUri,
		OptionCodeTTLSeconds:            strconv.Itoa(settings.CodeTTLSeconds),
		OptionSignatureToleranceSeconds: strconv.Itoa(settings.SignatureToleranceSeconds),
	}
	if includeSecret {
		values[OptionAppSecret] = settings.AppSecret
	}
	return values
}

// ValidateConfiguration accepts a disabled integration as a valid saved draft,
// but requires every effective connection field once it is enabled.
func (settings Settings) ValidateConfiguration() error {
	if !settings.Enabled {
		return nil
	}
	return settings.validateEnabled()
}

func (settings Settings) Validate() error {
	if !settings.Enabled {
		return fmt.Errorf("external game integration is disabled")
	}
	return settings.validateEnabled()
}

func (settings Settings) validateEnabled() error {
	if settings.AppId == "" {
		return fmt.Errorf("external game app id is not configured")
	}
	if len(settings.AppSecret) < 16 {
		return fmt.Errorf("external game app secret must contain at least 16 characters")
	}
	redirect, err := url.Parse(settings.RedirectUri)
	if err != nil || redirect.Scheme == "" || redirect.Host == "" {
		return fmt.Errorf("external game redirect URI is invalid")
	}
	if redirect.Scheme != "https" && !(redirect.Scheme == "http" && isLoopbackHost(redirect.Hostname())) {
		return fmt.Errorf("external game redirect URI must use HTTPS")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
