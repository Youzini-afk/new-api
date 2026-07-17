package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetChannelFallsBackAfterScheduledHighPriorityChannelIsFiltered(t *testing.T) {
	originalDB := DB
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	t.Cleanup(func() {
		DB = originalDB
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
	})

	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, testDB.AutoMigrate(&Channel{}, &Ability{}))
	DB = testDB
	common.MemoryCacheEnabled = false

	now := time.Now().UTC()
	currentWeekday := isoWeekdayForSelectionTest(now.Weekday())
	closedWeekday := (currentWeekday+2)%7 + 1
	closedSettings, err := common.Marshal(dto.ChannelOtherSettings{
		AvailabilitySchedule: &dto.ChannelAvailabilitySchedule{
			Enabled:  true,
			Timezone: "UTC",
			Windows: []dto.ChannelAvailabilityWindow{{
				Weekdays: []int{closedWeekday},
				Start:    "00:00",
				End:      "01:00",
			}},
		},
	})
	require.NoError(t, err)

	highPriority := int64(100)
	lowPriority := int64(10)
	high := Channel{
		Name:          "scheduled-closed-high",
		Status:        common.ChannelStatusEnabled,
		Group:         "default",
		Models:        "test-model",
		Priority:      &highPriority,
		OtherSettings: string(closedSettings),
	}
	low := Channel{
		Name:          "always-open-low",
		Status:        common.ChannelStatusEnabled,
		Group:         "default",
		Models:        "test-model",
		Priority:      &lowPriority,
		OtherSettings: "{}",
	}
	require.NoError(t, DB.Create(&high).Error)
	require.NoError(t, DB.Create(&low).Error)
	require.NoError(t, DB.Create(&[]Ability{
		{Group: "default", Model: "test-model", ChannelId: high.Id, Enabled: true, Priority: &highPriority},
		{Group: "default", Model: "test-model", ChannelId: low.Id, Enabled: true, Priority: &lowPriority},
	}).Error)

	selected, err := GetChannel("default", "test-model", 0, "/v1/chat/completions")
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, low.Id, selected.Id)
}

func isoWeekdayForSelectionTest(weekday time.Weekday) int {
	if weekday == time.Sunday {
		return 7
	}
	return int(weekday)
}
