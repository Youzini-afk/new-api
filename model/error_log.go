package model

import (
	"context"
	"regexp"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// ErrorLog stores governance-classified relay error insights for admin
// debugging and aggregation. It is NOT a user-facing log — users never see
// this table. Admins use it to understand upstream error patterns, unmatched
// opaque errors, and per-signature aggregation.
//
// Stored in the main DB (not LOG_DB) to avoid ClickHouse schema complexity
// and because it is an admin insight table, not a high-volume log table.

var normalizedSignatureRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

const errorLogQueueSize = 5000

type ErrorLog struct {
	Id        int    `json:"id" gorm:"primaryKey"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;index"`
	RequestId string `json:"request_id" gorm:"column:request_id;type:varchar(64);index"`
	LogId     int    `json:"log_id" gorm:"index;default:0"`

	UserId      int    `json:"user_id" gorm:"index;default:0"`
	Username    string `json:"username" gorm:"type:varchar(64);index;default:''"`
	DisplayName string `json:"display_name" gorm:"type:varchar(64);index;default:''"`
	TokenId     int    `json:"token_id" gorm:"index;default:0"`
	TokenName   string `json:"token_name" gorm:"type:varchar(64);index;default:''"`
	ChannelId   int    `json:"channel_id" gorm:"index;default:0"`
	ModelName   string `json:"model_name" gorm:"type:varchar(255);index;default:''"`
	Group       string `json:"group" gorm:"column:group;type:varchar(64);index;default:''"`

	RequestPath string `json:"request_path" gorm:"type:varchar(255);index;default:''"`
	IsStream    bool   `json:"is_stream" gorm:"index;default:false"`

	ErrorSource string `json:"error_source" gorm:"type:varchar(32);index;default:''"`
	ErrorStage  string `json:"error_stage" gorm:"type:varchar(64);index;default:''"`

	ClientStatusCode   int `json:"client_status_code" gorm:"index;default:0"`
	UpstreamStatusCode int `json:"upstream_status_code" gorm:"index;default:0"`

	RuleCode        string `json:"rule_code" gorm:"type:varchar(64);index;default:''"`
	RuleMatched     bool   `json:"rule_matched" gorm:"index;default:false"`
	MatchSource     string `json:"match_source" gorm:"type:varchar(32);index;default:''"`
	UnmatchedReason string `json:"unmatched_reason" gorm:"type:varchar(64);index;default:''"`
	RuleVersion     int    `json:"rule_version" gorm:"index;default:0"`

	SafeErrorCode    string `json:"safe_error_code" gorm:"type:varchar(64);default:''"`
	SafeErrorType    string `json:"safe_error_type" gorm:"type:varchar(64);default:''"`
	SafeErrorMessage string `json:"safe_error_message" gorm:"type:varchar(512);default:''"`

	OriginalErrorCode       string `json:"original_error_code" gorm:"type:varchar(128);default:''"`
	OriginalErrorType       string `json:"original_error_type" gorm:"type:varchar(128);default:''"`
	OriginalErrorMessage    string `json:"original_error_message" gorm:"type:text"`
	OriginalErrorStatusCode int    `json:"original_error_status_code" gorm:"index;default:0"`

	NormalizedSignature string `json:"normalized_signature" gorm:"type:varchar(64);index;default:''"`
	NormalizedMessage   string `json:"normalized_message" gorm:"type:varchar(1000);default:''"`

	RequestTime    int `json:"request_time" gorm:"default:0"`
	FirstTokenTime int `json:"first_token_time" gorm:"default:0"`
	RetryCount     int `json:"retry_count" gorm:"default:0"`

	// Metadata is stored as TEXT (JSON string) for cross-DB compatibility
	// (SQLite/MySQL/PostgreSQL). Marshal with common.Marshal, unmarshal with
	// common.Unmarshal. Never use a database-specific JSON column type.
	Metadata string `json:"metadata" gorm:"type:text"`
}

type ErrorLogEvent = ErrorLog

var (
	errorLogQueue     chan ErrorLogEvent
	errorLogQueueOnce sync.Once
)

func ensureErrorLogQueue() chan ErrorLogEvent {
	errorLogQueueOnce.Do(func() {
		errorLogQueue = make(chan ErrorLogEvent, errorLogQueueSize)
		go runErrorLogWorker()
	})
	return errorLogQueue
}

// EnqueueErrorLog queues an error insight event for async DB write. Returns
// false if the queue is full (backpressure: drop the event rather than
// blocking the relay hot path).
func EnqueueErrorLog(event ErrorLogEvent) bool {
	if DB == nil {
		return false
	}
	if event.CreatedAt == 0 {
		event.CreatedAt = time.Now().Unix()
	}
	queue := ensureErrorLogQueue()
	select {
	case queue <- event:
		return true
	default:
		common.SysError("error insight queue full, dropped error log event")
		return false
	}
}

// runErrorLogWorker consumes events from the queue and writes them to the DB.
// It recovers from panics so a single bad event doesn't kill the worker
// goroutine permanently.
func runErrorLogWorker() {
	for event := range errorLogQueue {
		if DB == nil {
			continue
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					common.SysError("error insight worker panic recovered")
				}
			}()
			if err := DB.WithContext(context.Background()).Create(&event).Error; err != nil {
				common.SysError("failed to record error insight log: " + err.Error())
			}
		}()
	}
}

// RecordErrorLogForTest writes an event synchronously (for tests only).
func RecordErrorLogForTest(event ErrorLogEvent) error {
	if event.CreatedAt == 0 {
		event.CreatedAt = time.Now().Unix()
	}
	return DB.Create(&event).Error
}

// GetErrorLogQueueForTest returns the queue for test inspection.
func GetErrorLogQueueForTest() chan ErrorLogEvent {
	return ensureErrorLogQueue()
}

// ============================================================================
// Admin query APIs
// ============================================================================

type ErrorLogsListParams struct {
	Page                int    `form:"page"`
	PageSize            int    `form:"page_size"`
	StartTimestamp      int64  `form:"start_timestamp"`
	EndTimestamp        int64  `form:"end_timestamp"`
	RuleMatched         *bool  `form:"rule_matched"`
	RuleCode            string `form:"rule_code"`
	UnmatchedReason     string `form:"unmatched_reason"`
	ModelName           string `form:"model_name"`
	ChannelId           int    `form:"channel_id"`
	NormalizedSignature string `form:"normalized_signature"`
	ErrorSource         string `form:"error_source"`
	ErrorStage          string `form:"error_stage"`
	ClientStatusCode    int    `form:"client_status_code"`
	UpstreamStatusCode  int    `form:"upstream_status_code"`
	IsStream            *bool  `form:"is_stream"`
	Username            string `form:"username"`
	RequestId           string `form:"request_id"`
	RequestPath         string `form:"request_path"`
}

var allowedErrorLogsOrderFields = map[string]bool{
	"id":                   true,
	"created_at":           true,
	"user_id":              true,
	"channel_id":           true,
	"model_name":           true,
	"rule_code":            true,
	"rule_matched":         true,
	"error_source":         true,
	"error_stage":          true,
	"client_status_code":   true,
	"upstream_status_code": true,
	"normalized_signature": true,
}

func applyErrorLogFilters(tx *gorm.DB, params *ErrorLogsListParams) *gorm.DB {
	if params.StartTimestamp != 0 {
		tx = tx.Where("created_at >= ?", params.StartTimestamp)
	}
	if params.EndTimestamp != 0 {
		tx = tx.Where("created_at <= ?", params.EndTimestamp)
	}
	if params.RuleMatched != nil {
		tx = tx.Where("rule_matched = ?", *params.RuleMatched)
	}
	if params.RuleCode != "" {
		tx = tx.Where("rule_code = ?", params.RuleCode)
	}
	if params.UnmatchedReason != "" {
		tx = tx.Where("unmatched_reason = ?", params.UnmatchedReason)
	}
	if params.ModelName != "" {
		tx = tx.Where("model_name = ?", params.ModelName)
	}
	if params.ChannelId != 0 {
		tx = tx.Where("channel_id = ?", params.ChannelId)
	}
	if params.NormalizedSignature != "" {
		tx = tx.Where("normalized_signature = ?", params.NormalizedSignature)
	}
	if params.ErrorSource != "" {
		tx = tx.Where("error_source = ?", params.ErrorSource)
	}
	if params.ErrorStage != "" {
		tx = tx.Where("error_stage = ?", params.ErrorStage)
	}
	if params.ClientStatusCode != 0 {
		tx = tx.Where("client_status_code = ?", params.ClientStatusCode)
	}
	if params.UpstreamStatusCode != 0 {
		tx = tx.Where("upstream_status_code = ?", params.UpstreamStatusCode)
	}
	if params.IsStream != nil {
		tx = tx.Where("is_stream = ?", *params.IsStream)
	}
	if params.Username != "" {
		tx = tx.Where("username = ?", params.Username)
	}
	if params.RequestId != "" {
		tx = tx.Where("request_id = ?", params.RequestId)
	}
	if params.RequestPath != "" {
		tx = tx.Where("request_path = ?", params.RequestPath)
	}
	return tx
}

func clampErrorLogPageSize(n int) (int, int) {
	if n <= 0 || n > 100 {
		n = 20
	}
	page := 1
	return page, n
}

func GetErrorLogList(params *ErrorLogsListParams) ([]*ErrorLog, int64, error) {
	var logs []*ErrorLog
	tx := applyErrorLogFilters(DB.Model(&ErrorLog{}), params)

	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	pageSize := params.PageSize
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	page := params.Page
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * pageSize

	order := "id desc"
	err := tx.Order(order).Limit(pageSize).Offset(offset).Find(&logs).Error
	return logs, total, err
}

type ErrorLogSummary struct {
	TotalCount         int64  `json:"total_count"`
	RuleMatchedCount   int64  `json:"rule_matched_count"`
	UnmatchedCount     int64  `json:"unmatched_count"`
	DistinctSignatures int64  `json:"distinct_signatures"`
	AffectedUsers      int64  `json:"affected_users"`
	AffectedChannels   int64  `json:"affected_channels"`
	TopRuleCode        string `json:"top_rule_code"`
	TopRuleCodeCount   int64  `json:"top_rule_code_count"`
}

func GetErrorLogSummary(params *ErrorLogsListParams) (*ErrorLogSummary, error) {
	base := applyErrorLogFilters(DB.Model(&ErrorLog{}), params)

	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, err
	}

	var matched int64
	if err := applyErrorLogFilters(DB.Model(&ErrorLog{}), params).Where("rule_matched = ?", true).Count(&matched).Error; err != nil {
		return nil, err
	}

	var distinctSigs int64
	if err := applyErrorLogFilters(DB.Model(&ErrorLog{}), params).Where("normalized_signature <> ''").Select("COUNT(DISTINCT normalized_signature)").Scan(&distinctSigs).Error; err != nil {
		return nil, err
	}
	var affectedUsers int64
	if err := applyErrorLogFilters(DB.Model(&ErrorLog{}), params).Where("user_id > 0").Select("COUNT(DISTINCT user_id)").Scan(&affectedUsers).Error; err != nil {
		return nil, err
	}
	var affectedChannels int64
	if err := applyErrorLogFilters(DB.Model(&ErrorLog{}), params).Where("channel_id > 0").Select("COUNT(DISTINCT channel_id)").Scan(&affectedChannels).Error; err != nil {
		return nil, err
	}

	type topRule struct {
		RuleCode string `gorm:"column:rule_code"`
		Count    int64  `gorm:"column:count"`
	}
	var top topRule
	if err := applyErrorLogFilters(DB.Model(&ErrorLog{}), params).
		Select("rule_code, COUNT(*) as count").
		Group("rule_code").
		Order("count DESC").
		Limit(1).
		Scan(&top).Error; err != nil {
		top = topRule{}
	}

	return &ErrorLogSummary{
		TotalCount:         total,
		RuleMatchedCount:   matched,
		UnmatchedCount:     total - matched,
		DistinctSignatures: distinctSigs,
		AffectedUsers:      affectedUsers,
		AffectedChannels:   affectedChannels,
		TopRuleCode:        top.RuleCode,
		TopRuleCodeCount:   top.Count,
	}, nil
}

type ErrorLogSignature struct {
	NormalizedSignature string `json:"normalized_signature" gorm:"column:normalized_signature"`
	NormalizedMessage   string `json:"normalized_message" gorm:"column:normalized_message"`
	RuleCode            string `json:"rule_code" gorm:"column:rule_code"`
	UnmatchedReason     string `json:"unmatched_reason" gorm:"column:unmatched_reason"`
	ClientStatusCode    int    `json:"client_status_code" gorm:"column:client_status_code"`
	UpstreamStatusCode  int    `json:"upstream_status_code" gorm:"column:upstream_status_code"`
	ErrorSource         string `json:"error_source" gorm:"column:error_source"`
	ErrorStage          string `json:"error_stage" gorm:"column:error_stage"`
	Count               int64  `json:"count" gorm:"column:count"`
	AffectedUsers       int64  `json:"affected_users" gorm:"column:affected_users"`
	AffectedChannels    int64  `json:"affected_channels" gorm:"column:affected_channels"`
	FirstSeenAt         int64  `json:"first_seen_at" gorm:"column:first_seen_at"`
	LatestAt            int64  `json:"latest_at" gorm:"column:latest_at"`
	HasAIResult         bool   `json:"has_ai_result" gorm:"-"`
}

func GetErrorLogSignatures(params *ErrorLogsListParams) ([]*ErrorLogSignature, error) {
	var sigs []*ErrorLogSignature
	err := applyErrorLogFilters(DB.Model(&ErrorLog{}), params).
		Select("normalized_signature, MAX(normalized_message) as normalized_message, MAX(rule_code) as rule_code, MAX(unmatched_reason) as unmatched_reason, MAX(client_status_code) as client_status_code, MAX(upstream_status_code) as upstream_status_code, MAX(error_source) as error_source, MAX(error_stage) as error_stage, COUNT(*) as count, COUNT(DISTINCT CASE WHEN user_id > 0 THEN user_id END) as affected_users, COUNT(DISTINCT CASE WHEN channel_id > 0 THEN channel_id END) as affected_channels, MIN(created_at) as first_seen_at, MAX(created_at) as latest_at").
		Where("normalized_signature <> ''").
		Group("normalized_signature").
		Order("count DESC, affected_users DESC, latest_at DESC").
		Limit(200).
		Scan(&sigs).Error
	if err != nil || len(sigs) == 0 {
		return sigs, err
	}
	signatures := make([]string, 0, len(sigs))
	for _, sig := range sigs {
		if sig != nil && sig.NormalizedSignature != "" {
			signatures = append(signatures, sig.NormalizedSignature)
		}
	}
	existing, aiErr := ExistingErrorInsightAIResultSignatures(context.Background(), signatures)
	if aiErr != nil {
		return sigs, aiErr
	}
	for _, sig := range sigs {
		if sig != nil {
			sig.HasAIResult = existing[sig.NormalizedSignature]
		}
	}
	return sigs, err
}

// ValidateNormalizedSignature checks that a normalized_signature is non-empty,
// reasonable length (<=64), and contains only safe characters.
func ValidateNormalizedSignature(sig string) bool {
	if sig == "" || len(sig) > 64 {
		return false
	}
	return normalizedSignatureRe.MatchString(sig)
}

// DeleteErrorLogsBySignature deletes all error_logs rows matching the given
// normalized_signature and returns the number of rows deleted.
func DeleteErrorLogsBySignature(signature string) (int64, error) {
	result := DB.Where("normalized_signature = ?", signature).Delete(&ErrorLog{})
	return result.RowsAffected, result.Error
}

func CountOldErrorLogs(ctx context.Context, cutoff int64) (int64, error) {
	var total int64
	if err := DB.WithContext(ctx).Model(&ErrorLog{}).Where("created_at < ?", cutoff).Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func DeleteOldErrorLogsBatch(ctx context.Context, cutoff int64, limit int) (int64, error) {
	if limit <= 0 {
		limit = 100
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	var ids []int
	if err := DB.WithContext(ctx).
		Model(&ErrorLog{}).
		Where("created_at < ?", cutoff).
		Order("id asc").
		Limit(limit).
		Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}

	result := DB.WithContext(ctx).Where("id IN ?", ids).Delete(&ErrorLog{})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}
