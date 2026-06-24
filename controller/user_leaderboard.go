package controller

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

const (
	userLeaderboardDefaultRangeDays   = 7
	userLeaderboardDefaultRankLimit   = 100
	userLeaderboardDefaultSlotMinutes = 5
)

// parseLeaderboardMetric 解析用户排行榜 metric 参数；空默认 calls，非法返回 error。
func parseLeaderboardMetric(metric string) (model.LeaderboardMetric, error) {
	m := strings.TrimSpace(metric)
	if m == "" {
		return model.LeaderboardMetricCalls, nil
	}
	switch model.LeaderboardMetric(m) {
	case model.LeaderboardMetricCalls, model.LeaderboardMetricQuota, model.LeaderboardMetricRPH:
		return model.LeaderboardMetric(m), nil
	default:
		return "", fmt.Errorf("invalid metric %q (expected calls, quota, or rph)", metric)
	}
}

// GetUserLeaderboard 返回用户排行榜（calls | quota | rph，只读）。
// GET /api/user_leaderboard/rank
//
// 查询参数：
//   - metric: calls | quota | rph，默认 calls
//   - range_days: 默认 7
//   - start_timestamp / end_timestamp: 秒级 unix 时间戳，半开区间
//   - limit: 默认 100
func GetUserLeaderboard(c *gin.Context) {
	metric, err := parseLeaderboardMetric(c.Query("metric"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	startTs, endTs, rangeDays, err := parseLogStatsTimeRange(c, userLeaderboardDefaultRangeDays)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	limit := parseLimitQuery(c, userLeaderboardDefaultRankLimit)
	items, err := model.GetUserLeaderboard(c.Request.Context(), metric, startTs, endTs, limit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"metric":          string(metric),
		"start_timestamp": startTs,
		"end_timestamp":   endTs,
		"range_days":      rangeDays,
		"limit":           limit,
		"items":           items,
	})
}

// GetUserCoverageLeaderboard 返回用户时间槽覆盖率排行榜（只读）。
// GET /api/user_leaderboard/coverage
//
// 查询参数：
//   - slot_minutes: 默认 5；<=0 或解析失败返回 error
//   - range_days: 默认 7
//   - start_timestamp / end_timestamp: 秒级 unix 时间戳，半开区间
//   - limit: 默认 100
func GetUserCoverageLeaderboard(c *gin.Context) {
	slotMinutes, err := parsePositiveIntQuery(c, "slot_minutes", userLeaderboardDefaultSlotMinutes)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	startTs, endTs, rangeDays, err := parseLogStatsTimeRange(c, userLeaderboardDefaultRangeDays)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	limit := parseLimitQuery(c, userLeaderboardDefaultRankLimit)
	items, err := model.GetUserCoverageLeaderboard(c.Request.Context(), startTs, endTs, slotMinutes, limit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"slot_minutes":    slotMinutes,
		"start_timestamp": startTs,
		"end_timestamp":   endTs,
		"range_days":      rangeDays,
		"limit":           limit,
		"items":           items,
	})
}
