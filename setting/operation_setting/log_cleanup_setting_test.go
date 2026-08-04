package operation_setting

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateLogCleanupOption(t *testing.T) {
	require.NoError(t, ValidateLogCleanupOption(LogCleanupEnabledOptionKey, "true"))
	require.NoError(t, ValidateLogCleanupOption(LogCleanupRetentionDaysOptionKey, "90"))
	require.NoError(t, ValidateLogCleanupOption(LogCleanupIntervalHoursOptionKey, "12"))
	assert.Error(t, ValidateLogCleanupOption(LogCleanupRetentionDaysOptionKey, "0"))
	assert.Error(t, ValidateLogCleanupOption(LogCleanupIntervalHoursOptionKey, "169"))
	assert.Error(t, ValidateLogCleanupOption(LogCleanupEnabledOptionKey, "yes"))
}

func TestLogCleanupSettingTargetTimestamp(t *testing.T) {
	setting := LogCleanupSetting{RetentionDays: 7, IntervalHours: 12}
	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	assert.Equal(t, now.Add(-7*24*time.Hour).Unix(), setting.TargetTimestamp(now))
	assert.Equal(t, 12*time.Hour, setting.Interval())
}

func TestRebuildLogCleanupSettingRuntimePublishesCompleteSnapshot(t *testing.T) {
	original := logCleanupSetting
	t.Cleanup(func() {
		logCleanupSetting = original
		RebuildLogCleanupSettingRuntime()
	})

	require.NoError(t, config.UpdateConfigFromMap(&logCleanupSetting, map[string]string{
		"enabled":        "true",
		"retention_days": "90",
		"interval_hours": "12",
	}))
	RebuildLogCleanupSettingRuntime()

	assert.Equal(t, LogCleanupSetting{
		Enabled:       true,
		RetentionDays: 90,
		IntervalHours: 12,
	}, GetLogCleanupSetting())
}
