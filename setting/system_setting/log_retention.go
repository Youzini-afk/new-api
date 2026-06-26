package system_setting

import "github.com/QuantumNous/new-api/setting/config"

// LogRetentionSetting controls scheduled database-backed log cleanup.
// Retention days of 0 disable automatic deletion for that category.
type LogRetentionSetting struct {
	UsageLogRetentionDays int `json:"usage_log_retention_days"`
	ErrorLogRetentionDays int `json:"error_log_retention_days"`
	CleanupIntervalHours  int `json:"cleanup_interval_hours"`
}

const defaultLogRetentionCleanupIntervalHours = 24

var defaultLogRetentionSetting = LogRetentionSetting{
	UsageLogRetentionDays: 0,
	ErrorLogRetentionDays: 0,
	CleanupIntervalHours:  defaultLogRetentionCleanupIntervalHours,
}

func init() {
	config.GlobalConfig.Register("log_retention", &defaultLogRetentionSetting)
}

// GetLogRetentionSetting returns the current log retention setting with safe
// normalization applied in place.
func GetLogRetentionSetting() *LogRetentionSetting {
	NormalizeLogRetentionSetting(&defaultLogRetentionSetting)
	return &defaultLogRetentionSetting
}

func NormalizeLogRetentionSetting(setting *LogRetentionSetting) {
	if setting == nil {
		return
	}
	if setting.UsageLogRetentionDays < 0 {
		setting.UsageLogRetentionDays = 0
	}
	if setting.ErrorLogRetentionDays < 0 {
		setting.ErrorLogRetentionDays = 0
	}
	if setting.CleanupIntervalHours <= 0 {
		setting.CleanupIntervalHours = defaultLogRetentionCleanupIntervalHours
	}
}
