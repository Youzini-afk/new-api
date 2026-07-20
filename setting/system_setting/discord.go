package system_setting

import (
	"fmt"

	"github.com/QuantumNous/new-api/setting/config"
)

// DiscordSettings holds Discord OAuth credentials and the Discord gate
// contract. LoginGate and PatrolGate are pointers so nil can retain backward
// compatibility with installations that only have discord.register_gate:
// nil inherits RegisterGate, while an explicitly saved empty object remains an
// independent fail-closed configuration.
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
	// LoginGate is stored as JSON under "discord.login_gate". Nil inherits the
	// legacy RegisterGate configuration.
	LoginGate *DiscordRegisterGateConfig `json:"login_gate"`

	// PatrolGate is stored as JSON under "discord.patrol_gate" and is used by
	// scheduled/manual patrol plus the banned-server patrol. Nil inherits the
	// legacy RegisterGate configuration.
	PatrolGate *DiscordRegisterGateConfig `json:"patrol_gate"`

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

func GetDiscordRegisterGateConfig() DiscordRegisterGateConfig {
	return cloneDiscordGateConfig(GetDiscordSettings().RegisterGate)
}

func GetDiscordLoginGateConfig() DiscordRegisterGateConfig {
	settings := GetDiscordSettings()
	if settings.LoginGate != nil {
		return cloneDiscordGateConfig(*settings.LoginGate)
	}
	return cloneDiscordGateConfig(settings.RegisterGate)
}

func GetDiscordPatrolGateConfig() DiscordRegisterGateConfig {
	settings := GetDiscordSettings()
	if settings.PatrolGate != nil {
		return cloneDiscordGateConfig(*settings.PatrolGate)
	}
	return cloneDiscordGateConfig(settings.RegisterGate)
}

func DiscordGateConfigHasRules(cfg DiscordRegisterGateConfig) bool {
	return len(cfg.Groups) > 0 || len(cfg.BanGroups) > 0
}

func cloneDiscordGateConfig(cfg DiscordRegisterGateConfig) DiscordRegisterGateConfig {
	cloned := cfg
	cloned.Groups = cloneDiscordGateGroups(cfg.Groups)
	cloned.BanGroups = cloneDiscordGateGroups(cfg.BanGroups)
	return cloned
}

func cloneDiscordGateGroups(groups []DiscordGateGroup) []DiscordGateGroup {
	if len(groups) == 0 {
		return nil
	}
	cloned := make([]DiscordGateGroup, len(groups))
	for groupIndex, group := range groups {
		cloned[groupIndex] = group
		cloned[groupIndex].Rules = make([]DiscordGateRule, len(group.Rules))
		for ruleIndex, rule := range group.Rules {
			cloned[groupIndex].Rules[ruleIndex] = rule
			cloned[groupIndex].Rules[ruleIndex].RoleIDs = append([]string(nil), rule.RoleIDs...)
		}
	}
	return cloned
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
