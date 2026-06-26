package system_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeLogRetentionSetting(t *testing.T) {
	setting := LogRetentionSetting{
		UsageLogRetentionDays: -3,
		ErrorLogRetentionDays: -1,
		CleanupIntervalHours:  0,
	}

	NormalizeLogRetentionSetting(&setting)

	assert.Equal(t, 0, setting.UsageLogRetentionDays)
	assert.Equal(t, 0, setting.ErrorLogRetentionDays)
	assert.Equal(t, 24, setting.CleanupIntervalHours)
}

func TestNormalizeLogScreeningSettingClampsExpireDays(t *testing.T) {
	original := defaultLogScreeningSetting
	t.Cleanup(func() {
		defaultLogScreeningSetting = original
	})
	defaultLogScreeningSetting = LogScreeningSetting{ExpireDays: 1}

	setting := GetLogScreeningSetting()

	assert.Equal(t, MinLogScreeningExpireDays, setting.ExpireDays)
}

func TestLogRetentionDefaultSetting(t *testing.T) {
	setting := GetLogRetentionSetting()

	assert.Equal(t, 0, setting.UsageLogRetentionDays)
	assert.Equal(t, 0, setting.ErrorLogRetentionDays)
	assert.Equal(t, 24, setting.CleanupIntervalHours)
}
