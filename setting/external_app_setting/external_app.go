package external_app_setting

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

const configName = "external_game"

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

var defaultSettings = Settings{
	AppId:                     "wtfib",
	CodeTTLSeconds:            120,
	SignatureToleranceSeconds: 300,
}

func init() {
	config.GlobalConfig.Register(configName, &defaultSettings)
}

// GetSettings returns an immutable snapshot with optional environment
// overrides. Empty environment variables intentionally leave the database
// option in effect so operators may choose either deployment style.
func GetSettings() Settings {
	var settings Settings
	if ok, err := config.GlobalConfig.Snapshot(configName, &settings); !ok || err != nil {
		settings = defaultSettings
	}
	settings.Enabled = common.GetEnvOrDefaultBool("EXTERNAL_GAME_ENABLED", settings.Enabled)
	settings.AppId = common.GetEnvOrDefaultString("EXTERNAL_GAME_APP_ID", settings.AppId)
	settings.AppSecret = common.GetEnvOrDefaultString("EXTERNAL_GAME_APP_SECRET", settings.AppSecret)
	settings.RedirectUri = common.GetEnvOrDefaultString("EXTERNAL_GAME_REDIRECT_URI", settings.RedirectUri)
	settings.CodeTTLSeconds = common.GetEnvOrDefault("EXTERNAL_GAME_CODE_TTL_SECONDS", settings.CodeTTLSeconds)
	settings.SignatureToleranceSeconds = common.GetEnvOrDefault("EXTERNAL_GAME_SIGNATURE_TOLERANCE_SECONDS", settings.SignatureToleranceSeconds)
	normalize(&settings)
	return settings
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

func (settings Settings) Validate() error {
	if !settings.Enabled {
		return fmt.Errorf("external game integration is disabled")
	}
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
