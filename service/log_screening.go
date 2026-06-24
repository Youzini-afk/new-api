package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

const (
	// LogScreeningKindChatCompletions is the default screening scope identifier
	// accepted by RunLogScreening.
	LogScreeningKindChatCompletions = "chat_completions"

	// logScreeningUnifiedRequestPath is the synthetic request_path used when a
	// screening record covers multiple paths (an aggregate match). The list
	// filter treats this value as "any path".
	logScreeningUnifiedRequestPath = "all"

	// logScreeningWindowColumn mirrors the physical column backing the Window
	// field (see model.LogScreeningRecord). Referenced here so the service-
	// layer raw WHERE clause stays dialect-safe without quoting.
	logScreeningWindowColumn = "window_label"

	// logScreeningCandidateCap is the maximum number of candidate users
	// processed per rule. Users beyond this cap are dropped (truncation) to
	// bound memory + DB work on a 24h window.
	logScreeningCandidateCap = 1000

	// logScreeningDetailCap is the maximum total detail rows fetched per rule
	// (across all candidate batches). Bounds the in-memory detail map.
	logScreeningDetailCap = 50000

	// logScreeningDetailBatchSize is the batch size for `user_id IN ?` detail
	// queries. Kept bounded so the placeholder list + IN-clause stays sane
	// across SQLite/MySQL/PostgreSQL.
	logScreeningDetailBatchSize = 200
)

// LogScreeningRunSummary describes the result of a RunLogScreening invocation.
type LogScreeningRunSummary struct {
	Kind            string `json:"kind"`
	Status          string `json:"status"`
	Enabled         bool   `json:"enabled"`
	RulesTotal      int    `json:"rules_total"`
	RulesChecked    int    `json:"rules_checked"`
	RecordsCreated  int64  `json:"records_created"`
	RecordsUpdated  int64  `json:"records_updated"`
	Expired         int64  `json:"expired"`
	StartedAt       int64  `json:"started_at"`
	FinishedAt      int64  `json:"finished_at"`
	ElapsedMs       int64  `json:"elapsed_ms"`
	WindowStart     int64  `json:"window_start"`
	WindowEnd       int64  `json:"window_end"`
	Manual          bool   `json:"manual"`
	OperatorUserId  int    `json:"operator_user_id"`
	OperatorName    string `json:"operator_name"`
	// DoS caps: when the candidate/detail volume exceeds the caps, the run is
	// truncated and Capped=true. CandidatesSeen/DetailsSeen report the number of
	// rows fetched from LOG_DB (bounded by the caps), NOT the raw count of
	// matching logs/users. Use Capped=true to detect truncation.
	Capped          bool  `json:"capped"`
	CandidateLimit  int   `json:"candidate_limit"`
	DetailLimit     int   `json:"detail_limit"`
	CandidatesSeen  int   `json:"candidates_seen"`
	DetailsSeen     int   `json:"details_seen"`
}

// RunLogScreening executes a real log-screening pass: it aggregates per-user
// stats from LOG_DB.logs over each enabled rule's window, computes match
// metrics (RPM/RPH/TPM/param-hits/UA-hits/prompt-delta), upserts
// LogScreeningRecord rows, appends a de-duplicated user remark for new/manual
// matches, and finally cleans up expired records.
//
// It does NOT perform any banning and does NOT call ban_sync. No AutoBanSync.
// kind selects the screening scope; only "chat_completions" is supported in
// this version.
func RunLogScreening(ctx context.Context, operatorUserId int, operatorName string, kind string, manual bool) (*LogScreeningRunSummary, error) {
	start := time.Now()
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = LogScreeningKindChatCompletions
	}
	setting := system_setting.GetLogScreeningSetting()
	summary := &LogScreeningRunSummary{
		Kind:           kind,
		Enabled:        setting.Enabled,
		RulesTotal:     len(setting.Rules),
		StartedAt:      start.Unix(),
		Manual:         manual,
		OperatorUserId: operatorUserId,
		OperatorName:   strings.TrimSpace(operatorName),
	}
	if !setting.Enabled {
		summary.Status = "disabled"
		summary.FinishedAt = time.Now().Unix()
		summary.ElapsedMs = time.Since(start).Milliseconds()
		return summary, nil
	}
	if kind != LogScreeningKindChatCompletions {
		summary.Status = "error"
		summary.FinishedAt = time.Now().Unix()
		summary.ElapsedMs = time.Since(start).Milliseconds()
		return summary, fmt.Errorf("unsupported screening kind: %s (only %s is supported)", kind, LogScreeningKindChatCompletions)
	}
	// GetLogScreeningSetting normalizes ExpireDays to >=7, so this is a
	// defensive guard.
	if setting.ExpireDays <= 0 {
		summary.Status = "error"
		summary.FinishedAt = time.Now().Unix()
		summary.ElapsedMs = time.Since(start).Milliseconds()
		return summary, errors.New("expire_days must be > 0")
	}

	paths := logScreeningRequestPaths()
	windowEnd := common.GetTimestamp()
	summary.WindowEnd = windowEnd
	allowedFields := logScreeningAllowedParamFields()

	for _, rule := range setting.Rules {
		if !rule.Enabled {
			continue
		}
		// Skip rules with no actionable conditions.
		if rule.RequestCount <= 0 && rule.RPM <= 0 && rule.TPM <= 0 && rule.RPH <= 0 &&
			rule.PromptDeltaCount <= 0 && len(rule.ParamRules) == 0 && len(rule.UABlacklist) == 0 && len(rule.UADirect) == 0 {
			continue
		}
		if rule.Window == "" {
			rule.Window = system_setting.LogScreeningWindow1h
		}
		summary.RulesChecked++
		summary.CandidateLimit = logScreeningCandidateCap
		summary.DetailLimit = logScreeningDetailCap
		windowDuration := logScreeningWindowDuration(rule.Window)
		windowMinutes := int(windowDuration.Minutes())
		windowHours := int(windowDuration.Hours())
		if windowMinutes <= 0 {
			windowMinutes = 1
		}
		if windowHours <= 0 {
			windowHours = 1
		}
		windowStart := windowEnd - int64(windowDuration.Seconds())
		summary.WindowStart = windowStart

		// Build DB-side primary thresholds for HAVING pre-filter. Only
		// request_count and total_tokens can be pre-filtered at the DB level;
		// RPM/RPH require window-derived division done in Go.
		primaryThresholds := map[string]int{}
		if rule.RequestCount > 0 {
			primaryThresholds["request_count"] = rule.RequestCount
		}
		if rule.TPM > 0 {
			// TPM is total_tokens per minute; the threshold in terms of
			// total_tokens is TPM * windowMinutes.
			primaryThresholds["total_tokens"] = rule.TPM * windowMinutes
		}
		// For UA-direct / secondary-only rules with no primary thresholds, the
		// HAVING is empty and we rely on the DB-side Limit to bound the scan.
		// Use candidateCap+1 so the service can detect truncation.
		listLimit := logScreeningCandidateCap + 1
		if len(primaryThresholds) == 0 {
			// No HAVING filter → every user is a candidate; the DB-side Limit
			// bounds the scan to candidateCap+1.
		}

		rows, err := model.LogScreeningListTargets(ctx, windowStart, windowEnd, paths, listLimit, primaryThresholds)
		if err != nil {
			summary.Status = "error"
			summary.FinishedAt = time.Now().Unix()
			summary.ElapsedMs = time.Since(start).Milliseconds()
			return summary, err
		}
		// CandidatesSeen is the count of rows fetched from LOG_DB (capped at
		// candidateCap+1 by the DB-side Limit). When it equals candidateCap+1,
		// the DB truncated the result and Capped=true.
		summary.CandidatesSeen = len(rows)
		if len(rows) >= logScreeningCandidateCap+1 {
			summary.Capped = true
		}

		// Phase 1 pre-filter: keep only rows passing a primary rough threshold
		// OR rows that need a detail scan (UA-direct / secondary-only rules).
		// This bounds the candidate set before the detail query.
		hasUADirect := len(rule.UADirect) > 0
		hasSecondary := len(rule.ParamRules) > 0 || rule.PromptDeltaCount > 0 || len(rule.UABlacklist) > 0
		candidateRows := make([]model.LogScreeningAggRow, 0, len(rows))
		for _, row := range rows {
			if row.UserId <= 0 {
				continue
			}
			// If primary thresholds are configured and the row passes any, it's
			// a candidate. If no primary thresholds AND the rule is UA-direct /
			// secondary-only, all rows are candidates (subject to the cap).
			roughMatch := false
			if rule.RequestCount > 0 && row.RequestCount >= rule.RequestCount {
				roughMatch = true
			}
			if rule.RPM > 0 && int(float64(row.RequestCount)/float64(windowMinutes)) >= rule.RPM {
				roughMatch = true
			}
			if rule.TPM > 0 && int(float64(row.TotalTokens)/float64(windowMinutes)) >= rule.TPM {
				roughMatch = true
			}
			if rule.RPH > 0 && int(float64(row.RequestCount)/float64(windowHours)) >= rule.RPH {
				roughMatch = true
			}
			if !roughMatch && !hasUADirect && !hasSecondary {
				continue
			}
			candidateRows = append(candidateRows, row)
			if len(candidateRows) >= logScreeningCandidateCap {
				break
			}
		}

		userIds := make([]int, 0, len(candidateRows))
		for _, row := range candidateRows {
			if row.UserId > 0 {
				userIds = append(userIds, row.UserId)
			}
		}
		userMeta, err := model.LogScreeningFillUserMeta(ctx, userIds)
		if err != nil {
			summary.Status = "error"
			summary.FinishedAt = time.Now().Unix()
			summary.ElapsedMs = time.Since(start).Milliseconds()
			return summary, err
		}

		// Phase 2 detail scan: batch `user_id IN ?` to keep the placeholder
		// list bounded; cap total detail rows at logScreeningDetailCap.
		detailMap := make(map[int][]model.LogScreeningLogDetail)
		detailsSeen := 0
		detailCapped := false
		remainingDetailBudget := logScreeningDetailCap
		for i := 0; i < len(userIds); i += logScreeningDetailBatchSize {
			end := i + logScreeningDetailBatchSize
			if end > len(userIds) {
				end = len(userIds)
			}
			batch := userIds[i:end]
			if remainingDetailBudget <= 0 {
				detailCapped = true
				break
			}
			batchMap, err := model.LogScreeningListLogDetails(ctx, windowStart, windowEnd, paths, batch, remainingDetailBudget)
			if err != nil {
				summary.Status = "error"
				summary.FinishedAt = time.Now().Unix()
				summary.ElapsedMs = time.Since(start).Milliseconds()
				return summary, err
			}
			batchRows := 0
			for uid, details := range batchMap {
				detailMap[uid] = details
				batchRows += len(details)
			}
			detailsSeen += batchRows
			remainingDetailBudget -= batchRows
			if batchRows < remainingDetailBudget+batchRows && len(batchMap) < len(batch) {
				// The detail query returned fewer rows than the budget allowed
				// but covered all users in the batch → not necessarily capped.
			}
		}
		summary.DetailsSeen = detailsSeen
		// If the detail query hit the cap, mark capped.
		if detailsSeen >= logScreeningDetailCap {
			detailCapped = true
		}
		if detailCapped {
			summary.Capped = true
		}

		matches := computeLogScreeningMatches(rule, candidateRows, detailMap, windowMinutes, windowHours, allowedFields)
		for _, match := range matches {
			meta := userMeta[match.UserId]
			matchedAt := match.LastSeen
			if matchedAt == 0 {
				matchedAt = windowEnd
			}
			expiresAt := matchedAt + int64(setting.ExpireDays)*86400
			record := &model.LogScreeningRecord{
				UserId:              match.UserId,
				Username:            meta.Username,
				RiskLevel:           logScreeningRiskLevelHigh,
				ObservedUntil:       expiresAt,
				RequireManualReview: true,
				Ip:                  match.IP,
				TokenName:           match.TokenName,
				RuleName:            rule.Name,
				Window:              rule.Window,
				RequestCount:        match.RequestCount,
				RPM:                 match.RPM,
				RPH:                 match.RPH,
				TPM:                 match.TPM,
				ParamHits:           strings.Join(match.ParamHits, ","),
				UAHits:              strings.Join(match.UAHits, ","),
				PromptDeltaCount:    match.PromptDeltaCount,
				PromptDeltaMax:      match.PromptDeltaMax,
				RequestPath:         logScreeningUnifiedRequestPath,
				WindowStart:         windowStart,
				WindowEnd:           windowEnd,
				MatchedAt:           matchedAt,
				ExpiresAt:           expiresAt,
				OperatorUserId:      operatorUserId,
				OperatorName:        strings.TrimSpace(operatorName),
				ManualTriggered:     manual,
			}
			created, err := model.UpsertLogScreeningRecord(ctx, record)
			if err != nil {
				summary.Status = "error"
				summary.FinishedAt = time.Now().Unix()
				summary.ElapsedMs = time.Since(start).Milliseconds()
				return summary, err
			}
			if created {
				summary.RecordsCreated++
			} else {
				summary.RecordsUpdated++
			}
			if rule.Name != "" && (created || manual) {
				if err := appendLogScreeningUserRemark(ctx, match.UserId, rule.Name, matchedAt); err != nil {
					common.SysLog("log screening append user remark failed: " + err.Error())
				}
			}
		}
	}

	if setting.ExpireDays > 0 {
		expired, err := model.DeleteExpiredLogScreeningRecords(ctx, windowEnd, 1000)
		if err != nil {
			summary.Status = "error"
			summary.FinishedAt = time.Now().Unix()
			summary.ElapsedMs = time.Since(start).Milliseconds()
			return summary, err
		}
		summary.Expired = expired
	}

	summary.Status = "completed"
	summary.FinishedAt = time.Now().Unix()
	summary.ElapsedMs = time.Since(start).Milliseconds()
	return summary, nil
}

// LogScreeningRecordItem is the admin-facing list DTO for a screening record.
type LogScreeningRecordItem struct {
	Id                  int                    `json:"id"`
	UserId              int                    `json:"user_id"`
	Username            string                 `json:"username"`
	DiscordID           string                 `json:"discord_id"`
	DiscordUID          int64                  `json:"discord_uid"`
	RiskLevel           string                 `json:"risk_level"`
	ObservedUntil       int64                  `json:"observed_until"`
	RequireManualReview bool                   `json:"require_manual_review"`
	DisplayName         string                 `json:"display_name"`
	Remark              string                 `json:"remark"`
	TokenName           string                 `json:"token_name"`
	Ip                  string                 `json:"ip"`
	RuleName            string                 `json:"rule_name"`
	Window              string                 `json:"window"`
	WindowStart         int64                  `json:"window_start"`
	WindowEnd           int64                  `json:"window_end"`
	RequestCount        int                    `json:"request_count"`
	RPM                 int                    `json:"rpm"`
	RPH                 int                    `json:"rph"`
	TPM                 int                    `json:"tpm"`
	ParamHits           []string               `json:"param_hits"`
	UAHits              []string               `json:"ua_hits"`
	PromptDeltaCount    int                    `json:"prompt_delta_count"`
	PromptDeltaMax      int                    `json:"prompt_delta_max"`
	RequestPath         string                 `json:"request_path"`
	MatchedAt           int64                  `json:"matched_at"`
	ExpiresAt           int64                  `json:"expires_at"`
	CreatedAt           int64                  `json:"created_at"`
	UpdatedAt           int64                  `json:"updated_at"`
	OperatorUserId      int                    `json:"operator_user_id"`
	OperatorName        string                 `json:"operator_name"`
	ManualTriggered     bool                   `json:"manual_triggered"`
	SuspiciousIPs       []SuspiciousIPMarkItem `json:"suspicious_ips"`
}

// SuspiciousIPMarkItem is the admin-facing projection of a SuspiciousIPMark.
type SuspiciousIPMarkItem struct {
	IP              string `json:"ip"`
	Source          string `json:"source"`
	Context         string `json:"context"`
	BanContext      string `json:"ban_context"`
	BanReason       string `json:"ban_reason"`
	TriggerCount    int    `json:"trigger_count"`
	LastTriggeredAt int64  `json:"last_triggered_at"`
}

// LogScreeningListFilter captures the query parameters accepted by the admin
// screening-records list endpoint.
type LogScreeningListFilter struct {
	UserId      int
	Username    string
	Ip          string
	RuleName    string
	Window      string
	ParamKey    string
	UA          string
	RequestPath string
	Expired     *bool
	StartTime   int64
	EndTime     int64
	WindowStart int64
	WindowEnd   int64
}

// ListLogScreeningRecords returns a paginated, filtered list of screening
// records enriched with display_name/remark and suspicious IP marks.
func ListLogScreeningRecords(ctx context.Context, filter LogScreeningListFilter, startIdx int, pageSize int) (items []LogScreeningRecordItem, total int64, err error) {
	if pageSize <= 0 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}
	if startIdx < 0 {
		startIdx = 0
	}

	query := model.DB.WithContext(ctx).Model(&model.LogScreeningRecord{})
	if filter.UserId > 0 {
		query = query.Where("user_id = ?", filter.UserId)
	}
	if strings.TrimSpace(filter.Username) != "" {
		query = query.Where("username = ?", strings.TrimSpace(filter.Username))
	}
	if strings.TrimSpace(filter.Ip) != "" {
		query = query.Where("ip = ?", strings.TrimSpace(filter.Ip))
	}
	if strings.TrimSpace(filter.RuleName) != "" {
		query = query.Where("rule_name = ?", strings.TrimSpace(filter.RuleName))
	}
	if strings.TrimSpace(filter.Window) != "" {
		// Physical column is the non-reserved window_label — no quoting needed.
		query = query.Where(logScreeningWindowColumn+" = ?", strings.TrimSpace(filter.Window))
	}
	if strings.TrimSpace(filter.RequestPath) != "" && strings.TrimSpace(filter.RequestPath) != logScreeningUnifiedRequestPath {
		query = query.Where("request_path = ?", strings.TrimSpace(filter.RequestPath))
	}
	if strings.TrimSpace(filter.ParamKey) != "" {
		query = query.Where("param_hits LIKE ?", "%"+strings.TrimSpace(filter.ParamKey)+"%")
	}
	if strings.TrimSpace(filter.UA) != "" {
		query = query.Where("ua_hits LIKE ?", "%"+strings.TrimSpace(filter.UA)+"%")
	}
	if filter.StartTime > 0 {
		query = query.Where("matched_at >= ?", filter.StartTime)
	}
	if filter.EndTime > 0 {
		query = query.Where("matched_at <= ?", filter.EndTime)
	}
	if filter.WindowStart > 0 {
		query = query.Where("window_start >= ?", filter.WindowStart)
	}
	if filter.WindowEnd > 0 {
		query = query.Where("window_end <= ?", filter.WindowEnd)
	}
	if filter.Expired != nil {
		now := common.GetTimestamp()
		if *filter.Expired {
			query = query.Where("expires_at > 0 AND expires_at < ?", now)
		} else {
			query = query.Where("(expires_at = 0 OR expires_at >= ?)", now)
		}
	}

	if err = query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var records []model.LogScreeningRecord
	if err = query.Order("matched_at desc, id desc").
		Limit(pageSize).
		Offset(startIdx).
		Find(&records).Error; err != nil {
		return nil, 0, err
	}
	if len(records) == 0 {
		return []LogScreeningRecordItem{}, total, nil
	}

	userIds := make([]int, 0, len(records))
	for _, record := range records {
		if record.UserId > 0 {
			userIds = append(userIds, record.UserId)
		}
	}

	userMap := make(map[int]struct {
		DisplayName string
		Remark      string
	}, len(userIds))
	if len(userIds) > 0 {
		var users []struct {
			Id          int    `gorm:"column:id"`
			DisplayName string `gorm:"column:display_name"`
			Remark      string `gorm:"column:remark"`
		}
		if err = model.DB.WithContext(ctx).Table("users").
			Select("id, display_name, remark").
			Where("id IN ?", userIds).
			Find(&users).Error; err != nil {
			return nil, 0, err
		}
		for _, u := range users {
			userMap[u.Id] = struct {
				DisplayName string
				Remark      string
			}{
				DisplayName: u.DisplayName,
				Remark:      u.Remark,
			}
		}
	}

	markMap := make(map[int][]SuspiciousIPMarkItem, len(userIds))
	if len(userIds) > 0 {
		marksByUser, markErr := model.ListSuspiciousIPMarksByUserIDs(ctx, userIds)
		if markErr == nil {
			for uid, marks := range marksByUser {
				items := make([]SuspiciousIPMarkItem, 0, len(marks))
				for i := range marks {
					mark := marks[i]
					items = append(items, SuspiciousIPMarkItem{
						IP:              strings.TrimSpace(mark.Ip),
						Source:          strings.TrimSpace(mark.Source),
						Context:         strings.TrimSpace(mark.Context),
						BanContext:      strings.TrimSpace(mark.BanContext),
						BanReason:       strings.TrimSpace(mark.BanReason),
						TriggerCount:    mark.TriggerCount,
						LastTriggeredAt: mark.LastTriggeredAt,
					})
				}
				markMap[uid] = items
			}
		}
	}

	items = make([]LogScreeningRecordItem, 0, len(records))
	for _, record := range records {
		meta := userMap[record.UserId]
		items = append(items, LogScreeningRecordItem{
			Id:                  record.Id,
			UserId:              record.UserId,
			Username:            record.Username,
			DiscordID:           record.DiscordID,
			DiscordUID:          record.DiscordUID,
			RiskLevel:           record.RiskLevel,
			ObservedUntil:       record.ObservedUntil,
			RequireManualReview: record.RequireManualReview,
			DisplayName:         meta.DisplayName,
			Remark:              meta.Remark,
			TokenName:           record.TokenName,
			Ip:                  record.Ip,
			RuleName:            record.RuleName,
			Window:              record.Window,
			WindowStart:         record.WindowStart,
			WindowEnd:           record.WindowEnd,
			RequestCount:        record.RequestCount,
			RPM:                 record.RPM,
			RPH:                 record.RPH,
			TPM:                 record.TPM,
			ParamHits:           splitLogScreeningHits(record.ParamHits),
			UAHits:              splitLogScreeningHits(record.UAHits),
			PromptDeltaCount:    record.PromptDeltaCount,
			PromptDeltaMax:      record.PromptDeltaMax,
			RequestPath:         record.RequestPath,
			MatchedAt:           record.MatchedAt,
			ExpiresAt:           record.ExpiresAt,
			CreatedAt:           record.CreatedAt,
			UpdatedAt:           record.UpdatedAt,
			OperatorUserId:      record.OperatorUserId,
			OperatorName:        record.OperatorName,
			ManualTriggered:     record.ManualTriggered,
			SuspiciousIPs:       markMap[record.UserId],
		})
	}
	return items, total, nil
}

func splitLogScreeningHits(raw string) []string {
	if raw == "" {
		return []string{}
	}
	parts := strings.Split(raw, ",")
	var items []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		items = append(items, part)
	}
	return items
}

// AppendLogScreeningRemark persists an admin remark for a screening record by
// appending it to the target user's remark field (matching gy behavior). It
// does not perform any banning and does not call ban_sync.
func AppendLogScreeningRemark(ctx context.Context, recordId int, operatorUserId int, operatorName string, remark string) error {
	remark = strings.TrimSpace(remark)
	if recordId <= 0 {
		return errors.New("invalid record id")
	}
	if remark == "" {
		return errors.New("remark is empty")
	}
	var record model.LogScreeningRecord
	if err := model.DB.WithContext(ctx).First(&record, recordId).Error; err != nil {
		return err
	}
	if record.UserId == 0 {
		return errors.New("user not found")
	}
	var user model.User
	if err := model.DB.WithContext(ctx).First(&user, record.UserId).Error; err != nil {
		return err
	}

	operatorDisplay := strings.TrimSpace(operatorName)
	if operatorDisplay == "" && operatorUserId > 0 {
		if username, displayName, _, err := logScreeningFillUserIdentity(ctx, operatorUserId); err == nil {
			if displayName != "" {
				operatorDisplay = displayName
			} else if username != "" {
				operatorDisplay = username
			}
		}
	}

	appendix := remark
	if operatorDisplay != "" {
		appendix = fmt.Sprintf("%s(%s)", remark, operatorDisplay)
	}
	if strings.TrimSpace(user.Remark) == "" {
		user.Remark = appendix
	} else {
		user.Remark = user.Remark + "\n" + appendix
	}
	if err := model.DB.WithContext(ctx).Model(&model.User{}).Where("id = ?", user.Id).Update("remark", user.Remark).Error; err != nil {
		return err
	}
	return model.InvalidateUserCache(user.Id)
}

// logScreeningFillUserIdentity returns username / display_name / remark for a
// user id. Used to label operators when appending remarks.
func logScreeningFillUserIdentity(ctx context.Context, userId int) (username string, displayName string, remark string, err error) {
	if userId <= 0 {
		return "", "", "", nil
	}
	var user struct {
		Username    string `gorm:"column:username"`
		DisplayName string `gorm:"column:display_name"`
		Remark      string `gorm:"column:remark"`
	}
	if err = model.DB.WithContext(ctx).Table("users").
		Select("username, display_name, remark").
		Where("id = ?", userId).
		First(&user).Error; err != nil {
		return "", "", "", err
	}
	return user.Username, user.DisplayName, user.Remark, nil
}

// DeleteExpiredLogScreeningRecords removes expired screening records in
// bounded batches. Delegates to the model-layer implementation.
func DeleteExpiredLogScreeningRecords(ctx context.Context, now int64, limit int) (int64, error) {
	return model.DeleteExpiredLogScreeningRecords(ctx, now, limit)
}
