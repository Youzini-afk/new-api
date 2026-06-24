package model

import (
	"context"
	"math"
)

// LeaderboardMetric 定义用户排行榜的排序/指标类型。
// calls: 按调用量降序；quota: 按消费额度降序；rph: 按每小时请求数降序。
// coverage 是独立指标，由 GetUserCoverageLeaderboard 处理。
type LeaderboardMetric string

const (
	LeaderboardMetricCalls LeaderboardMetric = "calls"
	LeaderboardMetricQuota LeaderboardMetric = "quota"
	LeaderboardMetricRPH   LeaderboardMetric = "rph"
)

// LeaderboardItem 是 calls/quota/rph 排名的一行结果（含两阶段补全的用户展示信息）。
type LeaderboardItem struct {
	UserId      int     `json:"user_id"`
	Username    string  `json:"username"`
	DisplayName string  `json:"display_name"`
	Remark      string  `json:"remark"`
	CallCount   int64   `json:"call_count"`
	QuotaSum    int64   `json:"quota_sum"`
	RPH         float64 `json:"rph"`
	FirstCall   int64   `json:"first_call"`
	LastCall    int64   `json:"last_call"`
}

// CoverageItem 是时间槽覆盖率排名的一行结果。
type CoverageItem struct {
	UserId      int     `json:"user_id"`
	Username    string  `json:"username"`
	DisplayName string  `json:"display_name"`
	Remark      string  `json:"remark"`
	ActiveSlots int64   `json:"active_slots"`
	TotalSlots  int64   `json:"total_slots"`
	CoveragePct float64 `json:"coverage_pct"`
}

// leaderboardAggRow 是 calls/quota/rph 聚合的原始 SQL 扫描目标。
type leaderboardAggRow struct {
	UserId    int   `gorm:"column:user_id"`
	CallCount int64 `gorm:"column:call_count"`
	QuotaSum  int64 `gorm:"column:quota_sum"`
	FirstCall int64 `gorm:"column:first_call"`
	LastCall  int64 `gorm:"column:last_call"`
}

// coverageAggRow 是覆盖率聚合的原始 SQL 扫描目标。
type coverageAggRow struct {
	UserId      int   `gorm:"column:user_id"`
	ActiveSlots int64 `gorm:"column:active_slots"`
}

// leaderboardOrderClause 返回给定 metric 的 ORDER BY 子句。
// 使用原始聚合表达式而非别名，以兼容 PostgreSQL（部分版本不支持在 ORDER BY 中引用别名）。
// 所有分支追加 user_id ASC 作为二级排序，保证等值行顺序确定。
func leaderboardOrderClause(metric LeaderboardMetric) string {
	switch metric {
	case LeaderboardMetricQuota:
		return "SUM(quota) DESC, user_id ASC"
	case LeaderboardMetricRPH:
		// RPH = call_count / max(1h, (last_call - first_call) / 3600)
		// 使用原始表达式而非别名，兼容 PostgreSQL。
		return "COUNT(*) * 1.0 / CASE WHEN (MAX(created_at) - MIN(created_at)) < 3600 THEN 3600 ELSE (MAX(created_at) - MIN(created_at)) END DESC, user_id ASC"
	default: // calls
		return "COUNT(*) DESC, user_id ASC"
	}
}

// GetUserLeaderboard 返回按指定 metric 排序的用户排行榜（calls/quota/rph）。
// 只统计 type=consume、user_id <> 0 的日志。
// 两阶段：先在 LOG_DB 按 user_id 聚合，再到 DB 批量查用户展示信息。
// limit 被 clamp 到 [100, 1000]。
// 时间窗口必须有效（start/end 必填、end > start、窗口 <= 31 天），否则返回 error。
// 主 DB 查询失败时返回 error。查询在 LOG_DB 上聚合，不涉及跨库 join。
func GetUserLeaderboard(ctx context.Context, metric LeaderboardMetric, startTimestamp, endTimestamp int64, limit int) ([]*LeaderboardItem, error) {
	if err := validateLogStatsWindow(startTimestamp, endTimestamp); err != nil {
		return nil, err
	}
	limit = clampRankLimit(limit)

	f := LogStatsFilter{
		Types:          []int{LogTypeConsume},
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
	}

	query := newLogStatsQuery(ctx, f).
		Where("user_id <> 0").
		Select("user_id, COUNT(*) as call_count, SUM(quota) as quota_sum, MIN(created_at) as first_call, MAX(created_at) as last_call").
		Group("user_id").
		Order(leaderboardOrderClause(metric)).
		Limit(limit)

	var rows []leaderboardAggRow
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []*LeaderboardItem{}, nil
	}

	userIds := make([]int, 0, len(rows))
	for _, r := range rows {
		if r.UserId != 0 {
			userIds = append(userIds, r.UserId)
		}
	}
	userMap, err := fetchLogUserDisplayMap(ctx, userIds)
	if err != nil {
		return nil, err
	}

	items := make([]*LeaderboardItem, 0, len(rows))
	for _, r := range rows {
		u := userMap[r.UserId]
		spanHours := float64(r.LastCall-r.FirstCall) / 3600.0
		if spanHours < 1 {
			spanHours = 1
		}
		rph := float64(r.CallCount) / spanHours
		rph = math.Round(rph*100) / 100

		items = append(items, &LeaderboardItem{
			UserId:      r.UserId,
			Username:    u.Username,
			DisplayName: u.DisplayName,
			Remark:      u.Remark,
			CallCount:   r.CallCount,
			QuotaSum:    r.QuotaSum,
			RPH:         rph,
			FirstCall:   r.FirstCall,
			LastCall:    r.LastCall,
		})
	}
	return items, nil
}

// GetUserCoverageLeaderboard 返回按时间槽覆盖率排序的用户排行榜。
// slotMinutes 为时间槽粒度（分钟），默认 5。
// 只统计 type=consume、user_id <> 0、created_at 在半开区间 [start, end) 内的日志。
// 两阶段：先在 LOG_DB 按 user_id 聚合 active_slots，再到 DB 批量查用户展示信息。
// limit 被 clamp 到 [100, 1000]。
// 时间窗口必须有效（start/end 必填、end > start、窗口 <= 31 天），否则返回 error。
// 主 DB 查询失败时返回 error。
//
// 桶编号使用相对起点 (created_at - startTimestamp) / slotSeconds，确保从 0 开始对齐窗口起点。
// 半开区间 [start, end) 保证 activeSlots <= totalSlots，无需 cap。
// 时间桶表达式由 logStatsCoverageBucketExpr 按方言生成（SQLite 整数除法、ClickHouse intDiv、默认 FLOOR）。
func GetUserCoverageLeaderboard(ctx context.Context, startTimestamp, endTimestamp int64, slotMinutes, limit int) ([]*CoverageItem, error) {
	if err := validateLogStatsWindow(startTimestamp, endTimestamp); err != nil {
		return nil, err
	}
	limit = clampRankLimit(limit)
	if slotMinutes <= 0 {
		slotMinutes = 5
	}
	slotSeconds := int64(slotMinutes) * 60

	rangeDuration := endTimestamp - startTimestamp
	totalSlots := int64(math.Ceil(float64(rangeDuration) / float64(slotSeconds)))
	if totalSlots <= 0 {
		totalSlots = 1
	}

	bucketExpr := logStatsCoverageBucketExpr(startTimestamp, slotSeconds)

	f := LogStatsFilter{
		Types:          []int{LogTypeConsume},
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
	}

	query := newLogStatsQuery(ctx, f).
		Where("user_id <> 0").
		Select("user_id, COUNT(DISTINCT " + bucketExpr + ") as active_slots").
		Group("user_id").
		Order("active_slots DESC, user_id ASC").
		Limit(limit)

	var rows []coverageAggRow
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []*CoverageItem{}, nil
	}

	userIds := make([]int, 0, len(rows))
	for _, r := range rows {
		if r.UserId != 0 {
			userIds = append(userIds, r.UserId)
		}
	}
	userMap, err := fetchLogUserDisplayMap(ctx, userIds)
	if err != nil {
		return nil, err
	}

	items := make([]*CoverageItem, 0, len(rows))
	for _, r := range rows {
		u := userMap[r.UserId]
		pct := float64(r.ActiveSlots) / float64(totalSlots) * 100
		pct = math.Round(pct*100) / 100
		items = append(items, &CoverageItem{
			UserId:      r.UserId,
			Username:    u.Username,
			DisplayName: u.DisplayName,
			Remark:      u.Remark,
			ActiveSlots: r.ActiveSlots,
			TotalSlots:  totalSlots,
			CoveragePct: pct,
		})
	}
	return items, nil
}
