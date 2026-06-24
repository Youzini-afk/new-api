package controller

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

const (
	uaStatsDefaultRangeDays = 7
	uaStatsDefaultRankLimit = 100
)

// GetUserAgentRank 返回 User-Agent 调用量排名（只读）。
// GET /api/ua_stats/rank
//
// 查询参数：
//   - range_days: 默认 7
//   - start_timestamp / end_timestamp: 秒级 unix 时间戳，半开区间
//   - keyword: 可选 UA 包含关键词（trim 后 >= 2 字符）
//   - limit: 默认 100
func GetUserAgentRank(c *gin.Context) {
	startTs, endTs, rangeDays, err := parseLogStatsTimeRange(c, uaStatsDefaultRangeDays)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	keyword := c.Query("keyword")
	limit := parseLimitQuery(c, uaStatsDefaultRankLimit)
	items, totalUAs, err := model.ListUserAgentRank(c.Request.Context(), startTs, endTs, keyword, limit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"start_timestamp": startTs,
		"end_timestamp":   endTs,
		"range_days":      rangeDays,
		"keyword":         keyword,
		"limit":           limit,
		"total_uas":       totalUAs,
		"items":           items,
	})
}

// GetUserAgentUsers 返回指定 User-Agent 下的用户分布（只读，分页）。
// GET /api/ua_stats/users
//
// 查询参数：
//   - ua 或 user_agent: 必填（ua 优先）
//   - match: exact | contains，默认 contains；非法返回 error
//   - range_days / start_timestamp / end_timestamp: 同上
//   - p / page_size: 分页
func GetUserAgentUsers(c *gin.Context) {
	ua := strings.TrimSpace(c.Query("ua"))
	if ua == "" {
		ua = strings.TrimSpace(c.Query("user_agent"))
	}
	if ua == "" {
		common.ApiErrorMsg(c, "ua or user_agent is required")
		return
	}
	matchMode := model.UserAgentMatchContains
	if rawMatch := strings.TrimSpace(c.Query("match")); rawMatch != "" {
		switch model.UserAgentMatchMode(rawMatch) {
		case model.UserAgentMatchExact:
			matchMode = model.UserAgentMatchExact
		case model.UserAgentMatchContains:
			matchMode = model.UserAgentMatchContains
		default:
			common.ApiErrorMsg(c, "match must be 'exact' or 'contains'")
			return
		}
	}
	startTs, endTs, rangeDays, err := parseLogStatsTimeRange(c, uaStatsDefaultRangeDays)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo := common.GetPageQuery(c)
	items, totalUsers, err := model.ListUserAgentUsers(
		c.Request.Context(), ua, matchMode, startTs, endTs,
		pageInfo.GetStartIdx(), pageInfo.GetPageSize(),
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(totalUsers))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, gin.H{
		"ua":              ua,
		"match":           string(matchMode),
		"start_timestamp": startTs,
		"end_timestamp":   endTs,
		"range_days":      rangeDays,
		"page":            pageInfo.Page,
		"page_size":       pageInfo.PageSize,
		"total":           pageInfo.Total,
		"items":           items,
	})
}
