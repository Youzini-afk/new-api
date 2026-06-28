package controller

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

// RegisterScheduledSystemTasks wires the periodic channel test, upstream model
// update, and async task polling (Midjourney / Suno / video) jobs into the
// system task framework so a DB lease dedups execution across multiple master
// instances and each run is recorded as one task row. Call this before
// service.StartSystemTaskRunner.
func RegisterScheduledSystemTasks() {
	service.RegisterSystemTaskHandler(channelTestHandler{})
	service.RegisterSystemTaskHandler(modelUpdateHandler{})
	service.RegisterSystemTaskHandler(midjourneyPollHandler{})
	service.RegisterSystemTaskHandler(asyncTaskPollHandler{})
	service.RegisterSystemTaskHandler(discordGatePatrolHandler{})
}

// channelTestHandler runs the scheduled "test all channels" job. Enablement and
// cadence still come from the monitor settings; only the execution path moved
// into the system task runner.
type channelTestHandler struct{}

func (channelTestHandler) Type() string { return model.SystemTaskTypeChannelTest }

func (channelTestHandler) Enabled() bool {
	return operation_setting.GetMonitorSetting().AutoTestChannelEnabled
}

func (channelTestHandler) Interval() time.Duration {
	minutes := operation_setting.GetMonitorSetting().AutoTestChannelMinutes
	if minutes <= 0 {
		minutes = 10
	}
	return time.Duration(minutes * float64(time.Minute))
}

func (channelTestHandler) NewPayload() any { return nil }

// channelTestTaskPayload controls one channel_test run. A nil/empty payload is a
// scheduled run, which uses the configured monitor ChannelTestMode and does not
// notify. A manual "test all channels" trigger sets Mode=scheduled_all and
// Notify=true to reproduce the legacy manual behavior (test every channel and
// notify root on completion).
type channelTestTaskPayload struct {
	Mode   string `json:"mode,omitempty"`
	Notify bool   `json:"notify,omitempty"`
}

func (channelTestHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	payload := channelTestTaskPayload{}
	if err := task.DecodePayload(&payload); err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	summary, err := runChannelTestTask(ctx, payload.Mode, payload.Notify, service.NewSystemTaskProgressReporter(task, runnerID))
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}

// modelUpdateHandler runs the scheduled upstream model update detection job.
type modelUpdateHandler struct{}

func (modelUpdateHandler) Type() string { return model.SystemTaskTypeModelUpdate }

func (modelUpdateHandler) Enabled() bool {
	return common.GetEnvOrDefaultBool("CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_ENABLED", true)
}

func (modelUpdateHandler) Interval() time.Duration {
	intervalMinutes := common.GetEnvOrDefault(
		"CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_INTERVAL_MINUTES",
		channelUpstreamModelUpdateTaskDefaultIntervalMinutes,
	)
	if intervalMinutes < 1 {
		intervalMinutes = channelUpstreamModelUpdateTaskDefaultIntervalMinutes
	}
	return time.Duration(intervalMinutes) * time.Minute
}

func (modelUpdateHandler) NewPayload() any { return nil }

// modelUpdateTaskPayload controls one model_update run. A scheduled run
// (Manual=false) respects the per-channel minimum check interval and may
// auto-apply detected models when a channel has auto-sync enabled. A manual
// "detect all" trigger sets Manual=true to reproduce the legacy detect-all
// semantics: force a re-check regardless of the interval and never auto-apply,
// so the admin reviews and applies changes explicitly.
type modelUpdateTaskPayload struct {
	Manual bool `json:"manual,omitempty"`
}

func (modelUpdateHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	payload := modelUpdateTaskPayload{}
	if err := task.DecodePayload(&payload); err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	summary := runChannelUpstreamModelUpdateTaskOnce(ctx, payload.Manual, !payload.Manual, service.NewSystemTaskProgressReporter(task, runnerID))
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}

// midjourneyPollHandler runs one Midjourney polling pass per scheduled run.
// Enabled() folds the "are there unfinished tasks?" check into enablement so the
// scheduler creates no row when the system is idle; only when at least one
// Midjourney task is in progress does a row get scheduled.
type midjourneyPollHandler struct{}

func (midjourneyPollHandler) Type() string { return model.SystemTaskTypeMidjourneyPoll }

func (midjourneyPollHandler) Enabled() bool {
	return constant.UpdateTask && model.HasUnfinishedMidjourneyTasks()
}

func (midjourneyPollHandler) Interval() time.Duration { return 15 * time.Second }

func (midjourneyPollHandler) NewPayload() any { return nil }

func (midjourneyPollHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	summary := runMidjourneyTaskUpdateOnce(ctx, service.NewSystemTaskProgressReporter(task, runnerID))
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}

// asyncTaskPollHandler runs one async-task (Suno/video) polling pass per
// scheduled run. Like midjourneyPollHandler, Enabled() folds in the unfinished
// task existence check so an idle system schedules no rows.
type asyncTaskPollHandler struct{}

func (asyncTaskPollHandler) Type() string { return model.SystemTaskTypeAsyncTaskPoll }

func (asyncTaskPollHandler) Enabled() bool {
	return constant.UpdateTask && model.HasUnfinishedSyncTasks()
}

func (asyncTaskPollHandler) Interval() time.Duration { return 15 * time.Second }

func (asyncTaskPollHandler) NewPayload() any { return nil }

func (asyncTaskPollHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	summary := service.RunTaskPollingOnce(ctx, service.NewSystemTaskProgressReporter(task, runnerID))
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, summary, nil)
}

const (
	discordGatePatrolModeManualBatch = "manual_batch"
	discordGatePatrolModeScheduled   = "scheduled"
)

type discordGatePatrolPayload struct {
	Mode      string `json:"mode"`
	BatchSize int    `json:"batch_size,omitempty"`
}

type discordGatePatrolResult struct {
	Total          int            `json:"total"`
	Processed      int            `json:"processed"`
	Counts         map[string]int `json:"counts"`
	CircuitBreaker bool           `json:"circuit_breaker"`
}

type discordGatePatrolHandler struct{}

func (discordGatePatrolHandler) Type() string { return model.SystemTaskTypeDiscordGatePatrol }

func (discordGatePatrolHandler) Enabled() bool {
	settings := system_setting.GetDiscordSettings()
	return settings.Enabled && settings.LoginGatePatrolEnabled && (settings.RegisterGateEnabled || settings.LoginGateEnabled)
}

func (discordGatePatrolHandler) Interval() time.Duration {
	return time.Duration(system_setting.GetDiscordSettings().LoginGatePatrolIntervalMinutes) * time.Minute
}

func (discordGatePatrolHandler) NewPayload() any {
	settings := system_setting.GetDiscordSettings()
	count, err := model.CountDiscordGatePatrolEligibleUsers(context.Background())
	if err != nil {
		common.SysLog(fmt.Sprintf("discord gate patrol eligible count failed: %v", err))
	}
	return discordGatePatrolPayload{
		Mode:      discordGatePatrolModeScheduled,
		BatchSize: discordGatePatrolBatchSize(count, settings.LoginGatePatrolIntervalMinutes, settings.LoginGatePatrolTargetSweepHours, settings.LoginGatePatrolMaxBatchSize),
	}
}

func (discordGatePatrolHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	payload := discordGatePatrolPayload{}
	if err := task.DecodePayload(&payload); err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	result, err := runDiscordGatePatrolTask(ctx, task, runnerID, payload)
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, result, err)
		return
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, result, nil)
}

func discordGatePatrolBatchSize(eligibleCount int64, intervalMinutes, targetSweepHours, maxBatchSize int) int {
	if maxBatchSize <= 0 {
		maxBatchSize = 1000
	}
	if eligibleCount <= 0 {
		return 1
	}
	denominator := targetSweepHours * 60
	if denominator <= 0 {
		denominator = 24 * 60
	}
	batch := int(math.Ceil(float64(eligibleCount*int64(intervalMinutes)) / float64(denominator)))
	if batch < 1 {
		batch = 1
	}
	if batch > maxBatchSize {
		batch = maxBatchSize
	}
	return batch
}

func discordGatePatrolEffectiveBatchSize(mode string, requestedBatchSize int, eligibleCount int64, settings *system_setting.DiscordSettings) int {
	maxBatchSize := 1000
	intervalMinutes := 5
	targetSweepHours := 24
	if settings != nil {
		maxBatchSize = settings.LoginGatePatrolMaxBatchSize
		intervalMinutes = settings.LoginGatePatrolIntervalMinutes
		targetSweepHours = settings.LoginGatePatrolTargetSweepHours
	}
	if maxBatchSize <= 0 {
		maxBatchSize = 1000
	}
	if requestedBatchSize > 0 {
		if requestedBatchSize > maxBatchSize {
			return maxBatchSize
		}
		return requestedBatchSize
	}
	if mode == discordGatePatrolModeScheduled {
		return discordGatePatrolBatchSize(eligibleCount, intervalMinutes, targetSweepHours, maxBatchSize)
	}
	return maxBatchSize
}

func runDiscordGatePatrolTask(ctx context.Context, task *model.SystemTask, runnerID string, payload discordGatePatrolPayload) (discordGatePatrolResult, error) {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	settings := system_setting.GetDiscordSettings()
	if payload.Mode == "" {
		payload.Mode = discordGatePatrolModeManualBatch
	}
	if payload.Mode != discordGatePatrolModeManualBatch && payload.Mode != discordGatePatrolModeScheduled {
		return discordGatePatrolResult{}, fmt.Errorf("unsupported discord gate patrol mode: %s", payload.Mode)
	}
	count, err := model.CountDiscordGatePatrolEligibleUsers(runCtx)
	if err != nil {
		return discordGatePatrolResult{}, err
	}
	batchSize := discordGatePatrolEffectiveBatchSize(payload.Mode, payload.BatchSize, count, settings)
	users, err := model.FindDiscordGatePatrolEligibleUsers(runCtx, batchSize)
	if err != nil {
		return discordGatePatrolResult{}, err
	}
	result := discordGatePatrolResult{Total: len(users), Counts: map[string]int{}}
	if len(users) == 0 {
		return result, nil
	}
	reporter := service.NewSystemTaskProgressReporter(task, runnerID)
	jobs := make(chan *model.User)
	results := make(chan oauth.DiscordPatrolOutcome)
	workers := settings.LoginGatePatrolWorkerCount
	if workers > len(users) {
		workers = len(users)
	}
	if workers < 1 {
		workers = 1
	}
	rps := settings.LoginGatePatrolMaxRPS
	if rps < 1 {
		rps = 1
	}
	runCtx = oauth.ContextWithDiscordPatrolLimiter(runCtx, oauth.NewDiscordPatrolLimiter(rps))
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for user := range jobs {
				if runCtx.Err() != nil {
					results <- oauth.DiscordPatrolOutcome{UserID: user.Id, Result: oauth.DiscordPatrolOutcomeTransient, Reason: "context_cancelled"}
					continue
				}
				results <- runDiscordPatrolWithRetries(runCtx, user, settings.LoginGatePatrolMaxRetries)
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, user := range users {
			select {
			case <-runCtx.Done():
				return
			case jobs <- user:
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()
	circuitErrors := 0
	rateLimitedErrors := 0
	circuitThreshold := max(10, len(users)/10)
	if circuitThreshold < 1 {
		circuitThreshold = 1
	}
	for outcome := range results {
		result.Processed++
		result.Counts[outcome.Result]++
		if outcome.Result == oauth.DiscordPatrolOutcomeTransient {
			circuitErrors++
			if outcome.Reason == "rate_limited" {
				rateLimitedErrors++
			}
		}
		reporter(result.Processed, result.Total)
		if (circuitErrors >= circuitThreshold || rateLimitedErrors >= 5) && result.Processed < result.Total {
			result.CircuitBreaker = true
			cancel()
		}
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	return result, nil
}

func runDiscordPatrolWithRetries(ctx context.Context, user *model.User, maxRetries int) oauth.DiscordPatrolOutcome {
	var outcome oauth.DiscordPatrolOutcome
	for attempt := 0; attempt <= maxRetries; attempt++ {
		var err error
		outcome, err = oauth.PatrolDiscordGate(ctx, user)
		if err != nil {
			outcome = oauth.DiscordPatrolOutcome{UserID: user.Id, Result: oauth.DiscordPatrolOutcomeTransient, Reason: "patrol_error"}
		}
		if outcome.Result != oauth.DiscordPatrolOutcomeTransient {
			clearDiscordPatrolRetry(user.Id)
			return outcome
		}
		if outcome.RetryAfter >= time.Minute || attempt == maxRetries {
			persistDiscordPatrolRetry(user.Id, attempt+1, outcome)
			return outcome
		}
		backoff := time.Duration(attempt+1) * time.Second
		if outcome.RetryAfter > backoff {
			backoff = outcome.RetryAfter
		}
		select {
		case <-ctx.Done():
			outcome.Reason = "context_cancelled"
			return outcome
		case <-time.After(backoff):
		}
	}
	return outcome
}

func persistDiscordPatrolRetry(userID int, retryCount int, outcome oauth.DiscordPatrolOutcome) {
	retryAt := common.GetTimestamp() + int64(time.Duration(retryCount+1)*time.Minute/time.Second)
	if outcome.RetryAfter > 0 {
		retryAt = common.GetTimestamp() + int64(outcome.RetryAfter/time.Second)
	}
	if err := model.DB.Model(&model.User{}).Where("id = ?", userID).Updates(map[string]interface{}{
		"discord_patrol_retry_at":    retryAt,
		"discord_patrol_retry_count": retryCount,
		"discord_patrol_last_error":  outcome.Reason,
	}).Error; err != nil {
		common.SysLog(fmt.Sprintf("discord patrol retry persist failed for user %d: %v", userID, err))
	}
}

func clearDiscordPatrolRetry(userID int) {
	if err := model.DB.Model(&model.User{}).Where("id = ?", userID).Updates(map[string]interface{}{
		"discord_patrol_retry_at":    0,
		"discord_patrol_retry_count": 0,
		"discord_patrol_last_error":  "",
	}).Error; err != nil {
		common.SysLog(fmt.Sprintf("discord patrol retry clear failed for user %d: %v", userID, err))
	}
}

func finishSystemTaskHandler(task *model.SystemTask, runnerID string, status model.SystemTaskStatus, result any, runErr error) {
	errorMessage := ""
	if runErr != nil {
		errorMessage = runErr.Error()
	}
	if err := model.FinishSystemTask(task.TaskID, runnerID, status, result, errorMessage); err != nil {
		common.SysLog(fmt.Sprintf("system task %s failed to persist result: %v", task.TaskID, err))
	}
}
