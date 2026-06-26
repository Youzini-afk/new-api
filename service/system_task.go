package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	// systemTaskRunnerIdleInterval is the fallback poll interval used to pick up
	// tasks created on other nodes and mark expired leases failed.
	systemTaskRunnerIdleInterval = 15 * time.Second
	systemTaskLockTTL            = 60 * time.Second
	logCleanupBatchSize          = 100

	// systemTaskSchedulerInterval throttles how often the scheduler/stale-lock
	// pass runs, independent of how often the runner wakes to claim tasks.
	systemTaskSchedulerInterval = 15 * time.Second
	systemTaskStaleLockInterval = 30 * time.Second
)

// SystemTaskHandler executes a claimed task of a specific type. Run owns the
// task lifecycle from claim to terminal state: it MUST call
// model.FinishSystemTask (succeeded/failed) before returning and MUST honor
// ctx cancellation, which the runner triggers if the per-type lock is lost.
type SystemTaskHandler interface {
	Type() string
	Run(ctx context.Context, task *model.SystemTask, runnerID string)
}

// ScheduledSystemTaskHandler is a SystemTaskHandler that the scheduler also
// creates periodically when enabled and the configured interval has elapsed
// since the last run.
type ScheduledSystemTaskHandler interface {
	SystemTaskHandler
	Enabled() bool
	Interval() time.Duration
	NewPayload() any
}

var (
	systemTaskHandlersMu sync.RWMutex
	systemTaskHandlers   = map[string]SystemTaskHandler{}
)

// RegisterSystemTaskHandler registers a handler keyed by its Type(). It must be
// called before StartSystemTaskRunner (or any time, since the runner snapshots
// the registry every pass). Re-registering a type replaces the previous handler.
func RegisterSystemTaskHandler(h SystemTaskHandler) {
	if h == nil {
		return
	}
	systemTaskHandlersMu.Lock()
	defer systemTaskHandlersMu.Unlock()
	systemTaskHandlers[h.Type()] = h
}

func registeredSystemTaskHandlers() []SystemTaskHandler {
	systemTaskHandlersMu.RLock()
	defer systemTaskHandlersMu.RUnlock()
	handlers := make([]SystemTaskHandler, 0, len(systemTaskHandlers))
	for _, h := range systemTaskHandlers {
		handlers = append(handlers, h)
	}
	return handlers
}

// logCleanupHandler wraps the existing on-demand usage-log cleanup task.
type logCleanupHandler struct{}

func (logCleanupHandler) Type() string { return model.SystemTaskTypeLogCleanup }

func (logCleanupHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	runLogCleanupTask(ctx, task, runnerID)
}

// logRetentionCleanupHandler runs scheduled retention separately from manual
// usage-log cleanup so a manual request never returns an active scheduled task.
type logRetentionCleanupHandler struct{}

func (logRetentionCleanupHandler) Type() string { return model.SystemTaskTypeLogRetentionCleanup }

func (logRetentionCleanupHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	runLogCleanupTask(ctx, task, runnerID)
}

func (logRetentionCleanupHandler) Enabled() bool {
	if activeManualTask, err := model.GetActiveSystemTask(model.SystemTaskTypeLogCleanup); err != nil || activeManualTask != nil {
		return false
	}
	setting := system_setting.GetLogRetentionSetting()
	return setting.UsageLogRetentionDays > 0 || setting.ErrorLogRetentionDays > 0
}

func (logRetentionCleanupHandler) Interval() time.Duration {
	setting := system_setting.GetLogRetentionSetting()
	return time.Duration(setting.CleanupIntervalHours) * time.Hour
}

func (logRetentionCleanupHandler) NewPayload() any {
	setting := system_setting.GetLogRetentionSetting()
	now := common.GetTimestamp()
	payload := LogCleanupPayload{
		Mode:                          LogCleanupModeScheduledRetention,
		BatchSize:                     logCleanupBatchSize,
		UsageLogRetentionEnabled:      setting.UsageLogRetentionDays > 0,
		ErrorLogRetentionEnabled:      setting.ErrorLogRetentionDays > 0,
		ScreeningRecordCleanupEnabled: setting.UsageLogRetentionDays > 0 || setting.ErrorLogRetentionDays > 0,
		ScreeningCutoffTimestamp:      now,
	}
	if payload.UsageLogRetentionEnabled {
		payload.TargetTimestamp = now - int64(setting.UsageLogRetentionDays)*86400
		payload.UsageCutoffTimestamp = payload.TargetTimestamp
	}
	if payload.ErrorLogRetentionEnabled {
		payload.ErrorCutoffTimestamp = now - int64(setting.ErrorLogRetentionDays)*86400
	}
	return payload
}

func init() {
	RegisterSystemTaskHandler(logCleanupHandler{})
	RegisterSystemTaskHandler(logRetentionCleanupHandler{})
}

type LogCleanupPayload struct {
	Mode                          string `json:"mode"`
	TargetTimestamp               int64  `json:"target_timestamp"`
	BatchSize                     int    `json:"batch_size"`
	UsageLogRetentionEnabled      bool   `json:"usage_log_retention_enabled"`
	UsageCutoffTimestamp          int64  `json:"usage_cutoff_timestamp"`
	ErrorLogRetentionEnabled      bool   `json:"error_log_retention_enabled"`
	ErrorCutoffTimestamp          int64  `json:"error_cutoff_timestamp"`
	ScreeningRecordCleanupEnabled bool   `json:"screening_record_cleanup_enabled"`
	ScreeningCutoffTimestamp      int64  `json:"screening_cutoff_timestamp"`
}

type LogCleanupState struct {
	Total      int64                              `json:"total"`
	Processed  int64                              `json:"processed"`
	Progress   int                                `json:"progress"`
	Remaining  int64                              `json:"remaining"`
	Categories map[string]LogCleanupCategoryState `json:"categories,omitempty"`
}

type LogCleanupResult struct {
	DeletedCount int64                               `json:"deleted_count"`
	Categories   map[string]LogCleanupCategoryResult `json:"categories,omitempty"`
}

type LogCleanupCategoryState struct {
	Enabled         bool   `json:"enabled"`
	CutoffTimestamp int64  `json:"cutoff_timestamp,omitempty"`
	Total           int64  `json:"total"`
	Processed       int64  `json:"processed"`
	Remaining       int64  `json:"remaining"`
	Progress        int    `json:"progress"`
	DeletedCount    int64  `json:"deleted_count"`
	Status          string `json:"status"`
	Reason          string `json:"reason,omitempty"`
}

type LogCleanupCategoryResult struct {
	Enabled         bool   `json:"enabled"`
	CutoffTimestamp int64  `json:"cutoff_timestamp,omitempty"`
	DeletedCount    int64  `json:"deleted_count"`
	Status          string `json:"status"`
	Reason          string `json:"reason,omitempty"`
}

const (
	LogCleanupModeManualUsage        = "manual_usage"
	LogCleanupModeScheduledRetention = "scheduled_retention"

	logCleanupCategoryUsageLogs        = "usage_logs"
	logCleanupCategoryErrorLogs        = "error_logs"
	logCleanupCategoryScreeningRecords = "screening_records"

	logCleanupCategoryStatusPending   = "pending"
	logCleanupCategoryStatusRunning   = "running"
	logCleanupCategoryStatusSucceeded = "succeeded"
	logCleanupCategoryStatusSkipped   = "skipped"
)

var (
	systemTaskRunnerOnce sync.Once
	// systemTaskWakeup signals the runner to check for runnable tasks
	// immediately instead of waiting for the idle poll. Buffered so a signal
	// raised while the runner is busy is not lost and is handled on the next loop.
	systemTaskWakeup = make(chan struct{}, 1)
)

// notifySystemTaskRunner wakes the runner without blocking. If a wakeup is
// already pending it is a no-op, which is fine since one pass drains all work.
func notifySystemTaskRunner() {
	select {
	case systemTaskWakeup <- struct{}{}:
	default:
	}
}

func StartSystemTaskRunner() {
	systemTaskRunnerOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}

		runnerID := fmt.Sprintf("%s-%s", common.NodeName, common.GetRandomString(8))
		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("system task runner started: runner=%s idle_interval=%s", runnerID, systemTaskRunnerIdleInterval))

			ticker := time.NewTicker(systemTaskRunnerIdleInterval)
			defer ticker.Stop()

			var lastScheduler time.Time
			var lastStaleLockCleanup time.Time
			runPass := func() {
				// The scheduler/stale-lock pass is throttled independently of the
				// claim pass: wakeups (e.g. a manual log cleanup) should claim
				// immediately without re-running the scheduler every time.
				now := time.Now()
				if now.Sub(lastStaleLockCleanup) >= systemTaskStaleLockInterval {
					lastStaleLockCleanup = now
					if err := model.ExpireStaleSystemTaskLocks(common.GetTimestamp()); err != nil {
						logger.LogWarn(context.Background(), fmt.Sprintf("system task stale lock cleanup failed: %v", err))
					}
				}
				if now.Sub(lastScheduler) >= systemTaskSchedulerInterval {
					lastScheduler = now
					runSystemTaskScheduler()
				}
				runSystemTaskClaimPass(runnerID)
			}

			runPass()
			for {
				select {
				case <-ticker.C:
				case <-systemTaskWakeup:
				}
				runPass()
			}
		})
	})
}

func StartLogCleanupTask(targetTimestamp int64) (*model.SystemTask, error) {
	if targetTimestamp <= 0 {
		return nil, errors.New("target timestamp is required")
	}
	if activeRetentionTask, err := model.GetActiveSystemTask(model.SystemTaskTypeLogRetentionCleanup); err != nil {
		return nil, err
	} else if activeRetentionTask != nil {
		return nil, errors.New("scheduled log retention cleanup is already running")
	}

	activeTask, err := model.GetActiveSystemTask(model.SystemTaskTypeLogCleanup)
	if err != nil {
		return nil, err
	}
	if activeTask != nil {
		return activeTask, nil
	}

	payload := LogCleanupPayload{
		Mode:                     LogCleanupModeManualUsage,
		TargetTimestamp:          targetTimestamp,
		BatchSize:                logCleanupBatchSize,
		UsageLogRetentionEnabled: true,
		UsageCutoffTimestamp:     targetTimestamp,
	}
	state := LogCleanupState{}
	task, err := model.CreateSystemTask(model.SystemTaskTypeLogCleanup, payload, state)
	if err != nil {
		activeTask, activeErr := model.GetActiveSystemTask(model.SystemTaskTypeLogCleanup)
		if activeErr == nil && activeTask != nil {
			return activeTask, nil
		}
		return nil, err
	}
	notifySystemTaskRunner()
	return task, nil
}

// EnqueueSystemTask creates an on-demand task of the given type. The returned
// bool is true only when a new pending row was created; false means an active
// task of the same type already exists and was returned.
func EnqueueSystemTask(taskType string, payload any) (*model.SystemTask, bool, error) {
	activeTask, err := model.GetActiveSystemTask(taskType)
	if err != nil {
		return nil, false, err
	}
	if activeTask != nil {
		return activeTask, false, nil
	}

	task, err := model.CreateSystemTask(taskType, payload, nil)
	if err != nil {
		activeTask, activeErr := model.GetActiveSystemTask(taskType)
		if activeErr == nil && activeTask != nil {
			return activeTask, false, nil
		}
		return nil, false, err
	}
	notifySystemTaskRunner()
	return task, true, nil
}

// runSystemTaskClaimPass tries to claim one pending task per registered type
// and dispatches each claimed task in its own goroutine so a long-running
// handler (e.g. channel test) never blocks another type (e.g. log cleanup).
func runSystemTaskClaimPass(runnerID string) {
	handlers := registeredSystemTaskHandlers()
	taskTypes := make([]string, 0, len(handlers))
	for _, handler := range handlers {
		taskTypes = append(taskTypes, handler.Type())
	}
	pendingTasks, err := model.FindEarliestPendingSystemTasks(taskTypes)
	if err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("system task runner query failed: %v", err))
		return
	}
	for _, handler := range handlers {
		task := pendingTasks[handler.Type()]
		if task == nil {
			continue
		}
		claimedTask, claimed, err := model.ClaimSystemTask(task.ID, handler.Type(), runnerID, systemTaskLockUntil())
		if err != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("system task claim failed: %v", err))
			continue
		}
		if !claimed {
			continue
		}
		dispatchHandler := handler
		dispatchTask := claimedTask
		gopool.Go(func() {
			runWithLeaseHeartbeat(dispatchTask, runnerID, func(ctx context.Context) {
				dispatchHandler.Run(ctx, dispatchTask, runnerID)
			})
		})
	}
}

// runSystemTaskScheduler creates a new task row for each enabled scheduled
// handler whose interval has elapsed since its last run and that has no active
// row. The task active_key unique index deduplicates concurrent creation while
// the per-type lock guarantees only one runner executes the task.
func runSystemTaskScheduler() {
	now := common.GetTimestamp()
	handlers := registeredSystemTaskHandlers()
	scheduledHandlers := make([]ScheduledSystemTaskHandler, 0, len(handlers))
	taskTypes := make([]string, 0, len(handlers))
	for _, handler := range handlers {
		scheduled, ok := handler.(ScheduledSystemTaskHandler)
		if !ok || !scheduled.Enabled() {
			continue
		}
		scheduledHandlers = append(scheduledHandlers, scheduled)
		taskTypes = append(taskTypes, scheduled.Type())
	}
	latestTasks, err := model.GetLatestSystemTasks(taskTypes)
	if err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("system task scheduler query failed: %v", err))
		return
	}
	for _, scheduled := range scheduledHandlers {
		latest := latestTasks[scheduled.Type()]
		if latest != nil {
			if latest.Status == model.SystemTaskStatusPending || latest.Status == model.SystemTaskStatusRunning {
				continue // an active row already exists
			}
			if now-latest.UpdatedAt < int64(scheduled.Interval().Seconds()) {
				continue // not due yet
			}
		}
		if _, err := model.CreateSystemTask(scheduled.Type(), scheduled.NewPayload(), nil); err != nil {
			activeTask, activeErr := model.GetActiveSystemTask(scheduled.Type())
			if activeErr == nil && activeTask != nil {
				continue
			}
			if activeErr != nil {
				logger.LogWarn(context.Background(), fmt.Sprintf("system task scheduler active lookup failed: type=%s err=%v", scheduled.Type(), activeErr))
			}
			logger.LogWarn(context.Background(), fmt.Sprintf("system task scheduler create failed: type=%s err=%v", scheduled.Type(), err))
			continue
		}
	}
}

// runWithLeaseHeartbeat renews the per-type lock on a background ticker while
// fn runs. The TTL is a crash-detection window, not a task time limit: an
// arbitrarily long handler stays alive as long as the heartbeat succeeds.
func runWithLeaseHeartbeat(task *model.SystemTask, runnerID string, fn func(ctx context.Context)) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	interval := systemTaskLockTTL / 3
	if interval <= 0 {
		interval = systemTaskLockTTL
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if err := model.RenewSystemTaskLock(task.TaskID, runnerID, systemTaskLockUntil()); err != nil {
					cancel()
					return
				}
			}
		}
	}()

	fn(ctx)
	close(done)
}

func runLogCleanupTask(ctx context.Context, task *model.SystemTask, runnerID string) {
	payload := LogCleanupPayload{}
	if err := task.DecodePayload(&payload); err != nil {
		failSystemTask(task, runnerID, err)
		return
	}
	normalizeLogCleanupPayload(&payload)
	if payload.BatchSize <= 0 {
		payload.BatchSize = logCleanupBatchSize
	}

	state := LogCleanupState{}
	if err := task.DecodeState(&state); err != nil {
		failSystemTask(task, runnerID, err)
		return
	}

	if err := runLogCleanupPayload(ctx, task, runnerID, payload, &state); err != nil {
		failSystemTask(task, runnerID, err)
		return
	}

	syncLogCleanupAggregateState(&state)
	if err := model.UpdateSystemTaskState(task.TaskID, runnerID, state); err != nil {
		logSystemTaskLockError(ctx, task, err)
		return
	}

	result := logCleanupResultFromState(state)
	if err := model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded, result, ""); err != nil {
		logSystemTaskLockError(ctx, task, err)
	}
}

func normalizeLogCleanupPayload(payload *LogCleanupPayload) {
	if payload.Mode == "" {
		payload.Mode = LogCleanupModeManualUsage
	}
	if payload.BatchSize <= 0 {
		payload.BatchSize = logCleanupBatchSize
	}
	if payload.UsageCutoffTimestamp <= 0 {
		payload.UsageCutoffTimestamp = payload.TargetTimestamp
	}
	if payload.Mode == LogCleanupModeManualUsage {
		payload.UsageLogRetentionEnabled = true
		payload.ErrorLogRetentionEnabled = false
		payload.ScreeningRecordCleanupEnabled = false
	}
}

func runLogCleanupPayload(ctx context.Context, task *model.SystemTask, runnerID string, payload LogCleanupPayload, state *LogCleanupState) error {
	switch payload.Mode {
	case LogCleanupModeManualUsage:
		if payload.TargetTimestamp <= 0 {
			return errors.New("target timestamp is required")
		}
		return runLogCleanupCategory(ctx, task, runnerID, state, logCleanupCategoryUsageLogs, payload.TargetTimestamp, payload.BatchSize, model.CountOldLog, model.DeleteOldLogBatch)
	case LogCleanupModeScheduledRetention:
		if payload.UsageLogRetentionEnabled {
			cutoff := payload.UsageCutoffTimestamp
			if cutoff <= 0 {
				cutoff = payload.TargetTimestamp
			}
			if cutoff <= 0 {
				return errors.New("usage log cutoff timestamp is required")
			}
			if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
				if err := markLogCleanupCategorySkipped(task, runnerID, state, logCleanupCategoryUsageLogs, cutoff, "managed_by_clickhouse_ttl"); err != nil {
					return err
				}
			} else if err := runLogCleanupCategory(ctx, task, runnerID, state, logCleanupCategoryUsageLogs, cutoff, payload.BatchSize, model.CountOldLog, model.DeleteOldLogBatch); err != nil {
				return err
			}
		}
		if payload.ErrorLogRetentionEnabled {
			if payload.ErrorCutoffTimestamp <= 0 {
				return errors.New("error log cutoff timestamp is required")
			}
			if err := runLogCleanupCategory(ctx, task, runnerID, state, logCleanupCategoryErrorLogs, payload.ErrorCutoffTimestamp, payload.BatchSize, model.CountOldErrorLogs, model.DeleteOldErrorLogsBatch); err != nil {
				return err
			}
		}
		if payload.ScreeningRecordCleanupEnabled {
			cutoff := payload.ScreeningCutoffTimestamp
			if cutoff <= 0 {
				cutoff = common.GetTimestamp()
			}
			if err := runLogCleanupCategory(ctx, task, runnerID, state, logCleanupCategoryScreeningRecords, cutoff, payload.BatchSize, countExpiredLogScreeningRecords, model.DeleteExpiredLogScreeningRecords); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported log cleanup mode: %s", payload.Mode)
	}
}

func runLogCleanupCategory(
	ctx context.Context,
	task *model.SystemTask,
	runnerID string,
	state *LogCleanupState,
	category string,
	cutoff int64,
	batchSize int,
	countFunc func(context.Context, int64) (int64, error),
	deleteFunc func(context.Context, int64, int) (int64, error),
) error {
	ensureLogCleanupCategoryState(state, category, cutoff)

	for {
		remaining, err := countFunc(ctx, cutoff)
		if err != nil {
			return err
		}
		categoryState := state.Categories[category]
		syncLogCleanupCategoryStateFromRemaining(&categoryState, remaining)
		categoryState.Status = logCleanupCategoryStatusRunning
		state.Categories[category] = categoryState
		syncLogCleanupAggregateState(state)
		if err := model.UpdateSystemTaskState(task.TaskID, runnerID, state); err != nil {
			logSystemTaskLockError(ctx, task, err)
			return err
		}
		if categoryState.Remaining == 0 {
			break
		}

		progressed := false
		for categoryState.Remaining > 0 {
			rowsAffected, err := deleteFunc(ctx, cutoff, batchSize)
			if err != nil {
				return err
			}
			if rowsAffected == 0 {
				break
			}
			progressed = true

			categoryState.Processed += rowsAffected
			categoryState.DeletedCount += rowsAffected
			if categoryState.Total < categoryState.Processed {
				categoryState.Total = categoryState.Processed
			}
			if categoryState.Remaining > rowsAffected {
				categoryState.Remaining -= rowsAffected
			} else {
				categoryState.Remaining = 0
			}
			categoryState.Progress = logCleanupProgress(categoryState.Processed, categoryState.Total)
			state.Categories[category] = categoryState
			syncLogCleanupAggregateState(state)

			if err := model.UpdateSystemTaskState(task.TaskID, runnerID, state); err != nil {
				logSystemTaskLockError(ctx, task, err)
				return err
			}
		}

		if !progressed {
			return fmt.Errorf("no %s rows were deleted", category)
		}
	}

	categoryState := state.Categories[category]
	categoryState.Remaining = 0
	categoryState.Progress = 100
	categoryState.Status = logCleanupCategoryStatusSucceeded
	if categoryState.Total < categoryState.Processed {
		categoryState.Total = categoryState.Processed
	}
	state.Categories[category] = categoryState
	syncLogCleanupAggregateState(state)
	return nil
}

func ensureLogCleanupCategoryState(state *LogCleanupState, category string, cutoff int64) {
	if state.Categories == nil {
		state.Categories = map[string]LogCleanupCategoryState{}
	}
	categoryState := state.Categories[category]
	categoryState.Enabled = true
	categoryState.CutoffTimestamp = cutoff
	if categoryState.Status == "" {
		categoryState.Status = logCleanupCategoryStatusPending
	}
	state.Categories[category] = categoryState
}

func markLogCleanupCategorySkipped(task *model.SystemTask, runnerID string, state *LogCleanupState, category string, cutoff int64, reason string) error {
	ensureLogCleanupCategoryState(state, category, cutoff)
	categoryState := state.Categories[category]
	categoryState.Status = logCleanupCategoryStatusSkipped
	categoryState.Reason = reason
	categoryState.Progress = 100
	state.Categories[category] = categoryState
	syncLogCleanupAggregateState(state)
	return model.UpdateSystemTaskState(task.TaskID, runnerID, state)
}

func syncLogCleanupCategoryStateFromRemaining(state *LogCleanupCategoryState, remaining int64) {
	if state.Total <= 0 {
		state.Total = remaining
		state.Processed = 0
	} else {
		processedFromRemaining := state.Total - remaining
		if processedFromRemaining > state.Processed {
			state.Processed = processedFromRemaining
		}
	}
	if state.Processed < 0 {
		state.Processed = 0
	}
	state.Remaining = remaining
	state.Progress = logCleanupProgress(state.Processed, state.Total)
}

func syncLogCleanupAggregateState(state *LogCleanupState) {
	if len(state.Categories) == 0 {
		if state.Total < state.Processed {
			state.Total = state.Processed
		}
		state.Progress = logCleanupProgress(state.Processed, state.Total)
		return
	}

	var total int64
	var processed int64
	var remaining int64
	for _, categoryState := range state.Categories {
		total += categoryState.Total
		processed += categoryState.Processed
		remaining += categoryState.Remaining
	}
	state.Total = total
	state.Processed = processed
	state.Remaining = remaining
	state.Progress = logCleanupProgress(processed, total)
}

func logCleanupResultFromState(state LogCleanupState) LogCleanupResult {
	result := LogCleanupResult{
		DeletedCount: state.Processed,
	}
	if len(state.Categories) == 0 {
		return result
	}
	result.Categories = make(map[string]LogCleanupCategoryResult, len(state.Categories))
	for category, categoryState := range state.Categories {
		result.Categories[category] = LogCleanupCategoryResult{
			Enabled:         categoryState.Enabled,
			CutoffTimestamp: categoryState.CutoffTimestamp,
			DeletedCount:    categoryState.DeletedCount,
			Status:          categoryState.Status,
			Reason:          categoryState.Reason,
		}
	}
	return result
}

func countExpiredLogScreeningRecords(ctx context.Context, now int64) (int64, error) {
	if now <= 0 {
		now = common.GetTimestamp()
	}
	var total int64
	if err := model.DB.WithContext(ctx).
		Model(&model.LogScreeningRecord{}).
		Where("expires_at > 0 AND expires_at < ?", now).
		Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func syncLogCleanupStateFromRemaining(state *LogCleanupState, remaining int64) {
	if len(state.Categories) > 0 {
		categoryState := state.Categories[logCleanupCategoryUsageLogs]
		syncLogCleanupCategoryStateFromRemaining(&categoryState, remaining)
		state.Categories[logCleanupCategoryUsageLogs] = categoryState
		syncLogCleanupAggregateState(state)
		return
	}
	if state.Total <= 0 {
		state.Total = remaining
		state.Processed = 0
	} else {
		processedFromRemaining := state.Total - remaining
		if processedFromRemaining > state.Processed {
			state.Processed = processedFromRemaining
		}
	}
	if state.Processed < 0 {
		state.Processed = 0
	}
	state.Remaining = remaining
	state.Progress = logCleanupProgress(state.Processed, state.Total)
}

func logCleanupProgress(processed int64, total int64) int {
	if total <= 0 {
		return 100
	}
	if processed <= 0 {
		return 0
	}
	if processed >= total {
		return 100
	}
	return int(processed * 100 / total)
}

func systemTaskLockUntil() int64 {
	return common.GetTimestamp() + int64(systemTaskLockTTL.Seconds())
}

// SystemTaskProgress is the state shape used by handlers that report percentage
// progress (channel test, model update). The frontend reads the progress field
// (0-100) to render a per-task progress indicator.
type SystemTaskProgress struct {
	Total     int `json:"total"`
	Processed int `json:"processed"`
	Progress  int `json:"progress"`
}

// NewSystemTaskProgressReporter returns a throttled progress callback bound to a
// running task. Handlers call it with (processed, total) as they iterate work;
// it persists a {processed,total,progress} state at most once every ~2s, always
// emitting the first update and the final 100%.
// Lock-loss errors are ignored: the lease heartbeat cancels the handler ctx on
// loss, so progress writes are best-effort and never abort the run themselves.
// The returned func is single-goroutine only (call it from the handler loop).
func NewSystemTaskProgressReporter(task *model.SystemTask, runnerID string) func(processed, total int) {
	const minWriteInterval = 2 * time.Second
	var (
		lastWriteAt  time.Time
		lastProgress = -1
	)
	return func(processed, total int) {
		progress := 100
		if total > 0 {
			progress = processed * 100 / total
		}
		if progress < 0 {
			progress = 0
		} else if progress > 100 {
			progress = 100
		}

		if progress < 100 {
			if progress == lastProgress {
				return
			}
			if !lastWriteAt.IsZero() && time.Since(lastWriteAt) < minWriteInterval {
				return
			}
		}
		lastProgress = progress
		lastWriteAt = time.Now()

		state := SystemTaskProgress{Total: total, Processed: processed, Progress: progress}
		_ = model.UpdateSystemTaskState(task.TaskID, runnerID, state)
	}
}

func failSystemTask(task *model.SystemTask, runnerID string, err error) {
	logger.LogWarn(context.Background(), fmt.Sprintf("system task %s failed: %v", task.TaskID, err))
	if finishErr := model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, err.Error()); finishErr != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("system task %s failed to save failure state: %v", task.TaskID, finishErr))
	}
}

func logSystemTaskLockError(ctx context.Context, task *model.SystemTask, err error) {
	if errors.Is(err, model.ErrSystemTaskLockLost) {
		logger.LogWarn(ctx, fmt.Sprintf("system task %s lock lost", task.TaskID))
		return
	}
	logger.LogWarn(ctx, fmt.Sprintf("system task %s update failed: %v", task.TaskID, err))
}
