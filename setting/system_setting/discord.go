package system_setting

import (
	"fmt"

	"github.com/QuantumNous/new-api/setting/config"
)

// DiscordSettings holds Discord OAuth credentials and the Discord gate
// contract. Register/login gate toggles share the nested RegisterGate rule set;
// runtime OAuth checks and manual rechecks fail closed when the gate is enabled
// but the rule set or refresh-token state is invalid.
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

	LoginGatePatrolEnabled          bool `json:"login_gate_patrol_enabled"`
	LoginGatePatrolIntervalMinutes  int  `json:"login_gate_patrol_interval_minutes"`
	LoginGatePatrolTargetSweepHours int  `json:"login_gate_patrol_target_sweep_hours"`
	LoginGatePatrolMaxBatchSize     int  `json:"login_gate_patrol_max_batch_size"`
	LoginGatePatrolWorkerCount      int  `json:"login_gate_patrol_worker_count"`
	LoginGatePatrolMaxRPS           int  `json:"login_gate_patrol_max_rps"`
	LoginGatePatrolMaxRetries       int  `json:"login_gate_patrol_max_retries"`
}

// 默认配置
var defaultDiscordSettings = DiscordSettings{
	LoginGatePatrolIntervalMinutes:  2,
	LoginGatePatrolTargetSweepHours: 12,
	LoginGatePatrolMaxBatchSize:     50000,
	LoginGatePatrolWorkerCount:      16,
	LoginGatePatrolMaxRPS:           25,
	LoginGatePatrolMaxRetries:       3,
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("discord", &defaultDiscordSettings)
}

func GetDiscordSettings() *DiscordSettings {
	NormalizeDiscordPatrolSettings(&defaultDiscordSettings)
	return &defaultDiscordSettings
}

func NormalizeDiscordPatrolSettings(settings *DiscordSettings) {
	if settings == nil {
		return
	}
	settings.LoginGatePatrolIntervalMinutes = clampDiscordPatrolInt(settings.LoginGatePatrolIntervalMinutes, 1, 60, 2)
	settings.LoginGatePatrolTargetSweepHours = clampDiscordPatrolInt(settings.LoginGatePatrolTargetSweepHours, 1, 168, 12)
	settings.LoginGatePatrolMaxBatchSize = clampDiscordPatrolInt(settings.LoginGatePatrolMaxBatchSize, 50, 100000, 50000)
	settings.LoginGatePatrolWorkerCount = clampDiscordPatrolInt(settings.LoginGatePatrolWorkerCount, 1, 64, 16)
	settings.LoginGatePatrolMaxRPS = clampDiscordPatrolInt(settings.LoginGatePatrolMaxRPS, 1, 100, 25)
	if settings.LoginGatePatrolMaxRetries < 0 {
		settings.LoginGatePatrolMaxRetries = 0
	} else if settings.LoginGatePatrolMaxRetries > 5 {
		settings.LoginGatePatrolMaxRetries = 5
	}
}

func clampDiscordPatrolInt(value, min, max, def int) int {
	if value == 0 {
		return def
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func ValidateDiscordPatrolSetting(key string, value int) error {
	ranges := map[string][2]int{
		"discord.login_gate_patrol_interval_minutes":   {1, 60},
		"discord.login_gate_patrol_target_sweep_hours": {1, 168},
		"discord.login_gate_patrol_max_batch_size":     {50, 100000},
		"discord.login_gate_patrol_worker_count":       {1, 64},
		"discord.login_gate_patrol_max_rps":            {1, 100},
		"discord.login_gate_patrol_max_retries":        {0, 5},
	}
	bounds, ok := ranges[key]
	if !ok {
		return nil
	}
	if value < bounds[0] || value > bounds[1] {
		return fmt.Errorf("%s must be between %d and %d", key, bounds[0], bounds[1])
	}
	return nil
}
