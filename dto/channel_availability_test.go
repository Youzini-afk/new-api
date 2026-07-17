package dto

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func compileAvailabilityScheduleForTest(t *testing.T, schedule *ChannelAvailabilitySchedule) *CompiledChannelAvailabilitySchedule {
	t.Helper()
	compiled, err := CompileChannelAvailabilitySchedule(schedule)
	require.NoError(t, err)
	return compiled
}

func TestChannelAvailabilityScheduleDisabledIsAlwaysOpen(t *testing.T) {
	compiled := compileAvailabilityScheduleForTest(t, nil)
	assert.False(t, compiled.Enabled())
	assert.True(t, compiled.IsOpenAt(time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)))

	compiled = compileAvailabilityScheduleForTest(t, &ChannelAvailabilitySchedule{
		Enabled:  false,
		Timezone: "invalid-but-ignored-while-disabled",
	})
	assert.True(t, compiled.IsOpenAt(time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)))
}

func TestChannelAvailabilityScheduleSameDayWindow(t *testing.T) {
	compiled := compileAvailabilityScheduleForTest(t, &ChannelAvailabilitySchedule{
		Enabled:  true,
		Timezone: "Asia/Shanghai",
		Windows: []ChannelAvailabilityWindow{{
			Weekdays: []int{1},
			Start:    "00:00",
			End:      "08:00",
		}},
	})
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)

	assert.True(t, compiled.IsOpenAt(time.Date(2026, time.July, 20, 0, 0, 0, 0, location)))
	assert.True(t, compiled.IsOpenAt(time.Date(2026, time.July, 20, 7, 59, 59, 0, location)))
	assert.False(t, compiled.IsOpenAt(time.Date(2026, time.July, 20, 8, 0, 0, 0, location)))
	assert.False(t, compiled.IsOpenAt(time.Date(2026, time.July, 21, 1, 0, 0, 0, location)))
}

func TestChannelAvailabilityScheduleCrossMidnightBelongsToStartDay(t *testing.T) {
	compiled := compileAvailabilityScheduleForTest(t, &ChannelAvailabilitySchedule{
		Enabled:  true,
		Timezone: "Asia/Shanghai",
		Windows: []ChannelAvailabilityWindow{{
			Weekdays: []int{1},
			Start:    "23:00",
			End:      "07:00",
		}},
	})
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)

	assert.False(t, compiled.IsOpenAt(time.Date(2026, time.July, 20, 22, 59, 0, 0, location)))
	assert.True(t, compiled.IsOpenAt(time.Date(2026, time.July, 20, 23, 0, 0, 0, location)))
	assert.True(t, compiled.IsOpenAt(time.Date(2026, time.July, 21, 6, 59, 0, 0, location)))
	assert.False(t, compiled.IsOpenAt(time.Date(2026, time.July, 21, 7, 0, 0, 0, location)))
	assert.False(t, compiled.IsOpenAt(time.Date(2026, time.July, 22, 1, 0, 0, 0, location)))
}

func TestChannelAvailabilityScheduleUsesConfiguredTimezone(t *testing.T) {
	compiled := compileAvailabilityScheduleForTest(t, &ChannelAvailabilitySchedule{
		Enabled:  true,
		Timezone: "Asia/Shanghai",
		Windows: []ChannelAvailabilityWindow{{
			Weekdays: []int{1},
			Start:    "00:00",
			End:      "01:00",
		}},
	})

	// Sunday 16:30 UTC is Monday 00:30 in Shanghai.
	assert.True(t, compiled.IsOpenAt(time.Date(2026, time.July, 19, 16, 30, 0, 0, time.UTC)))
	assert.False(t, compiled.IsOpenAt(time.Date(2026, time.July, 19, 17, 0, 0, 0, time.UTC)))
}

func TestChannelAvailabilityScheduleNextTransitionSkipsOverlappedBoundary(t *testing.T) {
	compiled := compileAvailabilityScheduleForTest(t, &ChannelAvailabilitySchedule{
		Enabled:  true,
		Timezone: "UTC",
		Windows: []ChannelAvailabilityWindow{
			{Weekdays: []int{1}, Start: "09:00", End: "12:00"},
			{Weekdays: []int{1}, Start: "11:00", End: "14:00"},
		},
	})

	transition, action, ok := compiled.NextTransition(time.Date(2026, time.July, 20, 10, 0, 0, 0, time.UTC))
	require.True(t, ok)
	assert.Equal(t, time.Date(2026, time.July, 20, 14, 0, 0, 0, time.UTC), transition)
	assert.Equal(t, "close", action)

	transition, action, ok = compiled.NextTransition(time.Date(2026, time.July, 20, 8, 0, 0, 0, time.UTC))
	require.True(t, ok)
	assert.Equal(t, time.Date(2026, time.July, 20, 9, 0, 0, 0, time.UTC), transition)
	assert.Equal(t, "open", action)
}

func TestChannelAvailabilityScheduleValidation(t *testing.T) {
	tests := []struct {
		name     string
		schedule *ChannelAvailabilitySchedule
	}{
		{
			name: "invalid timezone",
			schedule: &ChannelAvailabilitySchedule{Enabled: true, Timezone: "Mars/Olympus", Windows: []ChannelAvailabilityWindow{
				{Weekdays: []int{1}, Start: "01:00", End: "02:00"},
			}},
		},
		{
			name: "invalid time",
			schedule: &ChannelAvailabilitySchedule{Enabled: true, Timezone: "UTC", Windows: []ChannelAvailabilityWindow{
				{Weekdays: []int{1}, Start: "1:00", End: "02:00"},
			}},
		},
		{
			name: "duplicate weekday",
			schedule: &ChannelAvailabilitySchedule{Enabled: true, Timezone: "UTC", Windows: []ChannelAvailabilityWindow{
				{Weekdays: []int{1, 1}, Start: "01:00", End: "02:00"},
			}},
		},
		{
			name: "equal boundaries",
			schedule: &ChannelAvailabilitySchedule{Enabled: true, Timezone: "UTC", Windows: []ChannelAvailabilityWindow{
				{Weekdays: []int{1}, Start: "01:00", End: "01:00"},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CompileChannelAvailabilitySchedule(tt.schedule)
			assert.Error(t, err)
		})
	}
}
