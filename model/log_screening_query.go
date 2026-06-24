package model

import (
	"context"
	"strings"

	"gorm.io/gorm"
)

// LogScreeningAggRow is the per-user aggregate produced by
// LogScreeningListTargets: request_count, total_tokens, prompt_tokens, and
// last_seen over a half-open time window.
type LogScreeningAggRow struct {
	UserId       int   `gorm:"column:user_id"`
	RequestCount int   `gorm:"column:request_count"`
	TotalTokens  int   `gorm:"column:total_tokens"`
	PromptTokens int   `gorm:"column:prompt_tokens"`
	LastSeen     int64 `gorm:"column:last_seen"`
}

// LogScreeningLogDetail is the per-log detail row fetched in the second phase
// for candidate users: user_id, prompt_tokens, other (JSON), user_agent, ip,
// token_name, created_at. Only LOG_DB is queried.
type LogScreeningLogDetail struct {
	UserId       int    `gorm:"column:user_id"`
	PromptTokens int    `gorm:"column:prompt_tokens"`
	Other        string `gorm:"column:other"`
	UserAgent    string `gorm:"column:user_agent"`
	Ip           string `gorm:"column:ip"`
	TokenName    string `gorm:"column:token_name"`
	CreatedAt    int64  `gorm:"column:created_at"`
}

// LogScreeningUserMeta is the per-user metadata fetched from the MAIN DB
// (two-phase, no cross-DB join). DiscordID/DiscordUID are intentionally absent
// — the upstream users table has no such columns; LogScreeningRecord leaves
// them blank.
type LogScreeningUserMeta struct {
	Username    string
	DisplayName string
	Remark      string
}

// LogScreeningDefaultRequestPaths is the default request_path filter for
// chat-completions screening.
var LogScreeningDefaultRequestPaths = []string{
	"/v1/chat/completions",
	"/v1/responses",
	"/v1/messages",
}

// LogScreeningListTargets aggregates per-user request stats from LOG_DB.logs
// over the half-open window [startTimestamp, endTimestamp), filtered to
// LogTypeConsume + LogTypeError, user_id <> 0, and (optionally) request_path.
//
// Phase 1: per-user GROUP BY with count/sum/max. No cross-DB join — user
// metadata is fetched separately via LogScreeningFillUserMeta. Cross-DB safe
// (uses GORM methods only; no dialect-specific operators).
//
// candidateLimit caps the number of rows returned by LOG_DB (DB-side Limit).
// When 0, no limit is applied (legacy callers). When >0, the query returns at
// most candidateLimit rows; callers should treat the result as bounded and
// check len(rows) == candidateLimit to detect truncation.
//
// When primaryThresholds is non-empty, the query adds a HAVING clause to
// pre-filter at the DB level (only rows whose request_count / total_tokens
// meet at least one threshold). This avoids scanning all users and truncating
// in the service layer. primaryThresholds is a map of column-name → threshold;
// supported keys: "request_count", "total_tokens". The HAVING uses OR across
// the provided thresholds so any one threshold passing makes the row a
// candidate (matching the service-layer roughMatch logic).
func LogScreeningListTargets(ctx context.Context, startTimestamp, endTimestamp int64, paths []string, candidateLimit int, primaryThresholds map[string]int) ([]LogScreeningAggRow, error) {
	var rows []LogScreeningAggRow
	query := LOG_DB.WithContext(ctx).
		Table("logs").
		Where("type IN ?", []int{LogTypeConsume, LogTypeError}).
		Where("user_id <> 0")
	if len(paths) > 0 {
		query = query.Where("request_path IN ?", paths)
	}
	query = query.Where("created_at >= ? AND created_at < ?", startTimestamp, endTimestamp)
	// Apply DB-side HAVING pre-filter when primary thresholds are provided.
	// This bounds the result set before Limit kicks in.
	if len(primaryThresholds) > 0 {
		var havingParts []string
		var havingArgs []interface{}
		if rc, ok := primaryThresholds["request_count"]; ok && rc > 0 {
			havingParts = append(havingParts, "count(*) >= ?")
			havingArgs = append(havingArgs, rc)
		}
		if tt, ok := primaryThresholds["total_tokens"]; ok && tt > 0 {
			havingParts = append(havingParts, "(sum(prompt_tokens) + sum(completion_tokens)) >= ?")
			havingArgs = append(havingArgs, tt)
		}
		if len(havingParts) > 0 {
			havingClause := strings.Join(havingParts, " OR ")
			query = query.Having(havingClause, havingArgs...)
		}
	}
	if candidateLimit > 0 {
		query = query.Limit(candidateLimit)
	}
	if err := query.Select("user_id, count(*) as request_count, sum(prompt_tokens) + sum(completion_tokens) as total_tokens, sum(prompt_tokens) as prompt_tokens, max(created_at) as last_seen").
		Group("user_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// LogScreeningListLogDetails fetches per-log detail rows for the given user IDs
// over the half-open window, still filtered to consume+error types and the
// optional request_path set. Ordered by user_id asc, created_at asc for
// deterministic prompt-delta computation. LOG_DB only.
//
// detailLimit caps the total number of detail rows returned (0 = no cap). When
// capped, the result reflects at most detailLimit rows; callers should treat
// the result as a sample for UA-direct / secondary-only rules.
func LogScreeningListLogDetails(ctx context.Context, startTimestamp, endTimestamp int64, paths []string, userIds []int, detailLimit int) (map[int][]LogScreeningLogDetail, error) {
	result := make(map[int][]LogScreeningLogDetail)
	if len(userIds) == 0 {
		return result, nil
	}
	var rows []LogScreeningLogDetail
	query := LOG_DB.WithContext(ctx).
		Table("logs").
		Select("user_id, prompt_tokens, other, user_agent, ip, token_name, created_at").
		Where("type IN ?", []int{LogTypeConsume, LogTypeError}).
		Where("user_id IN ?", userIds)
	if len(paths) > 0 {
		query = query.Where("request_path IN ?", paths)
	}
	query = query.Where("created_at >= ? AND created_at < ?", startTimestamp, endTimestamp).
		Order("user_id asc, created_at asc")
	if detailLimit > 0 {
		query = query.Limit(detailLimit)
	}
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.UserId] = append(result[row.UserId], row)
	}
	return result, nil
}

// LogScreeningFillUserMeta fetches username/display_name/remark for the given
// user IDs from the MAIN DB (two-phase; no cross-DB join). Uses Unscoped to
// include soft-deleted users (logs may reference deleted users). DiscordID /
// DiscordUID are intentionally NOT selected — the upstream users table has no
// such columns; LogScreeningRecord leaves them blank.
func LogScreeningFillUserMeta(ctx context.Context, userIds []int) (map[int]LogScreeningUserMeta, error) {
	if len(userIds) == 0 {
		return map[int]LogScreeningUserMeta{}, nil
	}
	type row struct {
		Id          int    `gorm:"column:id"`
		Username    string `gorm:"column:username"`
		DisplayName string `gorm:"column:display_name"`
		Remark      string `gorm:"column:remark"`
	}
	var users []row
	if err := DB.Unscoped().WithContext(ctx).Table("users").
		Select("id, username, display_name, remark").
		Where("id IN ?", userIds).
		Find(&users).Error; err != nil {
		return nil, err
	}
	m := make(map[int]LogScreeningUserMeta, len(users))
	for _, u := range users {
		m[u.Id] = LogScreeningUserMeta{
			Username:    u.Username,
			DisplayName: u.DisplayName,
			Remark:      u.Remark,
		}
	}
	return m, nil
}

// LogScreeningPickTopIPAndToken selects, for a given user's detail rows, the IP
// and token_name with the highest frequency (ties broken by lexicographic
// order for determinism). Returns ("", "") when the detail list is empty.
// This is the deterministic "most frequent" pick used to populate
// LogScreeningRecord.Ip / TokenName without a cross-DB join.
func LogScreeningPickTopIPAndToken(details []LogScreeningLogDetail) (string, string) {
	if len(details) == 0 {
		return "", ""
	}
	ipCounts := make(map[string]int)
	tokCounts := make(map[string]int)
	for _, d := range details {
		if d.Ip != "" {
			ipCounts[d.Ip]++
		}
		if d.TokenName != "" {
			tokCounts[d.TokenName]++
		}
	}
	return pickTopKey(ipCounts), pickTopKey(tokCounts)
}

// pickTopKey returns the key with the highest count; ties broken by
// lexicographic ascending order so the result is deterministic across runs.
func pickTopKey(counts map[string]int) string {
	var best string
	bestCount := -1
	// Iterate deterministically by collecting + sorting keys.
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	// Sort for determinism (stable on key asc).
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	for _, k := range keys {
		c := counts[k]
		if c > bestCount {
			bestCount = c
			best = k
		}
	}
	return best
}

// keep gorm import referenced (used implicitly via LOG_DB/DB *gorm.DB).
var _ = gorm.ErrRecordNotFound
