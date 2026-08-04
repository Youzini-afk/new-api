package operation_setting

import (
	"fmt"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/setting/config"
)

const (
	LogCleanupEnabledOptionKey       = "log_cleanup_setting.enabled"
	LogCleanupRetentionDaysOptionKey = "log_cleanup_setting.retention_days"
	LogCleanupIntervalHoursOptionKey = "log_cleanup_setting.interval_hours"

	MinLogCleanupRetentionDays = 1
	MaxLogCleanupRetentionDays = 3650
	MinLogCleanupIntervalHours = 1
	MaxLogCleanupIntervalHours = 168
)

type LogCleanupSetting struct {
	Enabled       bool `json:"enabled"`
	RetentionDays int  `json:"retention_days"`
	IntervalHours int  `json:"interval_hours"`
}

var logCleanupSetting = LogCleanupSetting{
	Enabled:       false,
	RetentionDays: 30,
	IntervalHours: 24,
}

var logCleanupSettingSnapshot atomic.Pointer[LogCleanupSetting]

func init() {
	config.GlobalConfig.Register("log_cleanup_setting", &logCleanupSetting)
	RebuildLogCleanupSettingRuntime()
}

func GetLogCleanupSetting() LogCleanupSetting {
	snapshot := logCleanupSettingSnapshot.Load()
	if snapshot == nil {
		return normalizedLogCleanupSetting(logCleanupSetting)
	}
	return *snapshot
}

func RebuildLogCleanupSettingRuntime() {
	setting := normalizedLogCleanupSetting(logCleanupSetting)
	logCleanupSettingSnapshot.Store(&setting)
}

func normalizedLogCleanupSetting(setting LogCleanupSetting) LogCleanupSetting {
	if setting.RetentionDays < MinLogCleanupRetentionDays || setting.RetentionDays > MaxLogCleanupRetentionDays {
		setting.RetentionDays = 30
	}
	if setting.IntervalHours < MinLogCleanupIntervalHours || setting.IntervalHours > MaxLogCleanupIntervalHours {
		setting.IntervalHours = 24
	}
	return setting
}

func (setting LogCleanupSetting) Interval() time.Duration {
	return time.Duration(setting.IntervalHours) * time.Hour
}

func (setting LogCleanupSetting) TargetTimestamp(now time.Time) int64 {
	return now.Add(-time.Duration(setting.RetentionDays) * 24 * time.Hour).Unix()
}

func ValidateLogCleanupOption(key, value string) error {
	switch key {
	case LogCleanupEnabledOptionKey:
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("automatic log cleanup enabled must be true or false")
		}
	case LogCleanupRetentionDaysOptionKey:
		return validateLogCleanupInteger(value, MinLogCleanupRetentionDays, MaxLogCleanupRetentionDays, "retention days")
	case LogCleanupIntervalHoursOptionKey:
		return validateLogCleanupInteger(value, MinLogCleanupIntervalHours, MaxLogCleanupIntervalHours, "interval hours")
	}
	return nil
}

func validateLogCleanupInteger(value string, minValue, maxValue int, label string) error {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < minValue || parsed > maxValue {
		return fmt.Errorf("automatic log cleanup %s must be an integer between %d and %d", label, minValue, maxValue)
	}
	return nil
}
