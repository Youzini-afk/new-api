package controller

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

const (
	ipStatsDefaultRangeDays = 1
	ipStatsDefaultRankLimit = 100
)

// GetIPStatsRank 返回指定 conversation kind 下的 IP 调用量排名（只读）。
// GET /api/ip_stats/conversation/rank
//
// 查询参数：
//   - kind: chat_completions | responses | messages；空默认 chat_completions
//   - range_days: 默认 1
//   - start_timestamp / end_timestamp: 秒级 unix 时间戳，半开区间
//   - limit: 默认 100
func GetIPStatsRank(c *gin.Context) {
	kind := c.Query("kind")
	requestPath, err := conversationRequestPathForKind(kind)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	startTs, endTs, rangeDays, err := parseLogStatsTimeRange(c, ipStatsDefaultRangeDays)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	limit := parseLimitQuery(c, ipStatsDefaultRankLimit)
	items, totalIps, err := model.ListIPStatsRank(c.Request.Context(), requestPath, startTs, endTs, limit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"kind":            normalizeConversationKind(kind),
		"request_path":    requestPath,
		"start_timestamp": startTs,
		"end_timestamp":   endTs,
		"range_days":      rangeDays,
		"limit":           limit,
		"total_ips":       totalIps,
		"items":           items,
	})
}

// GetIPStatsUsers 返回指定 IP 在指定 conversation kind 下的用户分布（只读，分页）。
// GET /api/ip_stats/conversation/users
//
// 查询参数：
//   - kind: 同上
//   - ip: 必填
//   - range_days / start_timestamp / end_timestamp: 同上
//   - p / page_size: 分页（common.GetPageQuery）
func GetIPStatsUsers(c *gin.Context) {
	kind := c.Query("kind")
	requestPath, err := conversationRequestPathForKind(kind)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	ip := strings.TrimSpace(c.Query("ip"))
	if ip == "" {
		common.ApiErrorMsg(c, "ip is required")
		return
	}
	startTs, endTs, rangeDays, err := parseLogStatsTimeRange(c, ipStatsDefaultRangeDays)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo := common.GetPageQuery(c)
	items, totalUsers, err := model.ListIPStatsUsers(
		c.Request.Context(), requestPath, ip, startTs, endTs,
		pageInfo.GetStartIdx(), pageInfo.GetPageSize(),
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(totalUsers))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, gin.H{
		"kind":            normalizeConversationKind(kind),
		"request_path":    requestPath,
		"ip":              ip,
		"start_timestamp": startTs,
		"end_timestamp":   endTs,
		"range_days":      rangeDays,
		"page":            pageInfo.Page,
		"page_size":       pageInfo.PageSize,
		"total":           pageInfo.Total,
		"items":           items,
	})
}
