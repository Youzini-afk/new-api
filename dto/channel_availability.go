package dto

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	_ "time/tzdata"
)

const MaxChannelAvailabilityWindows = 32

var channelAvailabilityTimePattern = regexp.MustCompile(`^(?:[01]\d|2[0-3]):[0-5]\d$`)

// ChannelAvailabilitySchedule controls when a channel may receive new requests.
// Weekdays use ISO numbering: Monday=1 through Sunday=7.
type ChannelAvailabilitySchedule struct {
	Enabled  bool                        `json:"enabled,omitempty"`
	Timezone string                      `json:"timezone,omitempty"`
	Windows  []ChannelAvailabilityWindow `json:"windows,omitempty"`
}

type ChannelAvailabilityWindow struct {
	Weekdays []int  `json:"weekdays"`
	Start    string `json:"start"`
	End      string `json:"end"`
}

// ChannelAvailabilityState is computed at response time for the admin channel
// list/editor. It is not persisted.
type ChannelAvailabilityState struct {
	Enabled              bool   `json:"enabled"`
	Open                 bool   `json:"open"`
	EffectiveAvailable   bool   `json:"effective_available"`
	Timezone             string `json:"timezone,omitempty"`
	NextTransitionAt     int64  `json:"next_transition_at,omitempty"`
	NextTransitionAction string `json:"next_transition_action,omitempty"`
	Error                string `json:"error,omitempty"`
}

type compiledChannelAvailabilityWindow struct {
	weekdays    [8]bool
	startMinute int
	endMinute   int
}

// CompiledChannelAvailabilitySchedule is safe for concurrent reads and is used
// by the in-memory channel selector to avoid parsing settings per request.
type CompiledChannelAvailabilitySchedule struct {
	enabled  bool
	timezone string
	location *time.Location
	windows  []compiledChannelAvailabilityWindow
}

func (schedule *ChannelAvailabilitySchedule) Validate() error {
	_, err := CompileChannelAvailabilitySchedule(schedule)
	return err
}

func CompileChannelAvailabilitySchedule(schedule *ChannelAvailabilitySchedule) (*CompiledChannelAvailabilitySchedule, error) {
	if schedule == nil || !schedule.Enabled {
		return &CompiledChannelAvailabilitySchedule{}, nil
	}

	timezone := strings.TrimSpace(schedule.Timezone)
	if timezone == "" {
		return nil, fmt.Errorf("availability_schedule.timezone is required")
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("availability_schedule.timezone is invalid: %s", timezone)
	}
	if len(schedule.Windows) == 0 {
		return nil, fmt.Errorf("availability_schedule.windows requires at least one window")
	}
	if len(schedule.Windows) > MaxChannelAvailabilityWindows {
		return nil, fmt.Errorf("availability_schedule.windows supports at most %d windows", MaxChannelAvailabilityWindows)
	}

	compiled := &CompiledChannelAvailabilitySchedule{
		enabled:  true,
		timezone: timezone,
		location: location,
		windows:  make([]compiledChannelAvailabilityWindow, 0, len(schedule.Windows)),
	}
	for index, window := range schedule.Windows {
		if len(window.Weekdays) == 0 {
			return nil, fmt.Errorf("availability_schedule.windows[%d].weekdays requires at least one day", index)
		}
		compiledWindow := compiledChannelAvailabilityWindow{}
		for _, weekday := range window.Weekdays {
			if weekday < 1 || weekday > 7 {
				return nil, fmt.Errorf("availability_schedule.windows[%d].weekdays contains invalid day: %d", index, weekday)
			}
			if compiledWindow.weekdays[weekday] {
				return nil, fmt.Errorf("availability_schedule.windows[%d].weekdays contains duplicate day: %d", index, weekday)
			}
			compiledWindow.weekdays[weekday] = true
		}

		startMinute, err := parseChannelAvailabilityMinute(window.Start)
		if err != nil {
			return nil, fmt.Errorf("availability_schedule.windows[%d].start: %w", index, err)
		}
		endMinute, err := parseChannelAvailabilityMinute(window.End)
		if err != nil {
			return nil, fmt.Errorf("availability_schedule.windows[%d].end: %w", index, err)
		}
		if startMinute == endMinute {
			return nil, fmt.Errorf("availability_schedule.windows[%d] start and end must differ", index)
		}
		compiledWindow.startMinute = startMinute
		compiledWindow.endMinute = endMinute
		compiled.windows = append(compiled.windows, compiledWindow)
	}
	return compiled, nil
}

func parseChannelAvailabilityMinute(value string) (int, error) {
	value = strings.TrimSpace(value)
	if !channelAvailabilityTimePattern.MatchString(value) {
		return 0, fmt.Errorf("must use HH:mm in 24-hour format")
	}
	return int(value[0]-'0')*600 + int(value[1]-'0')*60 + int(value[3]-'0')*10 + int(value[4]-'0'), nil
}

func isoWeekday(weekday time.Weekday) int {
	if weekday == time.Sunday {
		return 7
	}
	return int(weekday)
}

func (schedule *CompiledChannelAvailabilitySchedule) Enabled() bool {
	return schedule != nil && schedule.enabled
}

func (schedule *CompiledChannelAvailabilitySchedule) Timezone() string {
	if schedule == nil {
		return ""
	}
	return schedule.timezone
}

// IsOpenAt returns true for a disabled schedule. For an enabled schedule, the
// start is inclusive and the end is exclusive. Cross-midnight windows belong
// to their starting weekday (for example Monday 23:00-07:00 includes early
// Tuesday morning).
func (schedule *CompiledChannelAvailabilitySchedule) IsOpenAt(now time.Time) bool {
	if schedule == nil {
		return false
	}
	if !schedule.enabled {
		return true
	}
	localNow := now.In(schedule.location)
	weekday := isoWeekday(localNow.Weekday())
	previousWeekday := weekday - 1
	if previousWeekday == 0 {
		previousWeekday = 7
	}
	minute := localNow.Hour()*60 + localNow.Minute()

	for _, window := range schedule.windows {
		if window.startMinute < window.endMinute {
			if window.weekdays[weekday] && minute >= window.startMinute && minute < window.endMinute {
				return true
			}
			continue
		}
		if (window.weekdays[weekday] && minute >= window.startMinute) ||
			(window.weekdays[previousWeekday] && minute < window.endMinute) {
			return true
		}
	}
	return false
}

func (schedule *CompiledChannelAvailabilitySchedule) StateAt(now time.Time, channelEnabled bool) ChannelAvailabilityState {
	state := ChannelAvailabilityState{
		Enabled:            schedule != nil && schedule.enabled,
		Open:               schedule != nil && schedule.IsOpenAt(now),
		EffectiveAvailable: channelEnabled && schedule != nil && schedule.IsOpenAt(now),
		Timezone:           schedule.Timezone(),
	}
	if schedule == nil || !schedule.enabled {
		state.Open = true
		state.EffectiveAvailable = channelEnabled
		return state
	}
	if transitionAt, action, ok := schedule.NextTransition(now); ok {
		state.NextTransitionAt = transitionAt.Unix()
		state.NextTransitionAction = action
	}
	return state
}

// NextTransition returns the next boundary that changes the union of all
// windows. Looking two weeks ahead covers every weekly schedule and also skips
// boundaries hidden by overlapping windows.
func (schedule *CompiledChannelAvailabilitySchedule) NextTransition(now time.Time) (time.Time, string, bool) {
	if schedule == nil || !schedule.enabled {
		return time.Time{}, "", false
	}
	localNow := now.In(schedule.location)
	localMidnight := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, schedule.location)
	candidates := make([]time.Time, 0, len(schedule.windows)*28)

	for dayOffset := -1; dayOffset <= 14; dayOffset++ {
		day := localMidnight.AddDate(0, 0, dayOffset)
		weekday := isoWeekday(day.Weekday())
		for _, window := range schedule.windows {
			if !window.weekdays[weekday] {
				continue
			}
			start := channelAvailabilityTimeOnDay(day, window.startMinute, schedule.location)
			endDay := day
			if window.startMinute > window.endMinute {
				endDay = day.AddDate(0, 0, 1)
			}
			end := channelAvailabilityTimeOnDay(endDay, window.endMinute, schedule.location)
			if start.After(now) {
				candidates = append(candidates, start)
			}
			if end.After(now) {
				candidates = append(candidates, end)
			}
		}
	}

	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Before(candidates[j]) })
	for _, candidate := range candidates {
		before := schedule.IsOpenAt(candidate.Add(-time.Nanosecond))
		after := schedule.IsOpenAt(candidate)
		if before == after {
			continue
		}
		if after {
			return candidate, "open", true
		}
		return candidate, "close", true
	}
	return time.Time{}, "", false
}

func channelAvailabilityTimeOnDay(day time.Time, minute int, location *time.Location) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), minute/60, minute%60, 0, 0, location)
}
