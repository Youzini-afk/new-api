package system_setting

import "github.com/QuantumNous/new-api/setting/config"

// DiscordSettings holds Discord OAuth credentials and the Phase 6.1 Discord
// gate contract. The gate toggles are persisted here so the contract is
// stable across restarts, but no real gate evaluator is wired in this phase:
// when a gate is enabled the Discord provider fails closed (returns a clear
// error) rather than silently passing users through.
type DiscordSettings struct {
	Enabled      bool   `json:"enabled"`
	ClientId     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`

	// RegisterGateEnabled gates new-user registration via Discord OAuth.
	RegisterGateEnabled bool `json:"register_gate_enabled"`
	// RegisterGate is the typed gate configuration (groups/ban_groups/rules/
	// role_match/min_join_hours/...). Stored as JSON under
	// "discord.register_gate". Empty config = no rules configured.
	RegisterGate DiscordRegisterGateConfig `json:"register_gate"`

	// LoginGateEnabled gates login of existing Discord-bound users.
	LoginGateEnabled bool `json:"login_gate_enabled"`

	// LoginGateAuditEnabled toggles periodic re-check of existing users.
	// The audit runner itself is NOT implemented in this phase.
	LoginGateAuditEnabled bool `json:"login_gate_audit_enabled"`
	// LoginGateAuditIntervalMinutes is the minimum interval between audit
	// runs. 0 means "not configured". Must be >= 1 when > 0.
	LoginGateAuditIntervalMinutes int `json:"login_gate_audit_interval_minutes"`
	// LoginGateAuditBatchSize is the number of users checked per audit run.
	// 0 means "not configured". Must be >= 1 when > 0.
	LoginGateAuditBatchSize int `json:"login_gate_audit_batch_size"`
}

// 默认配置
var defaultDiscordSettings = DiscordSettings{}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("discord", &defaultDiscordSettings)
}

func GetDiscordSettings() *DiscordSettings {
	return &defaultDiscordSettings
}
