package model

import (
	"context"
	"strings"

	"gorm.io/gorm"
)

// UserAgentRankItem 是 User-Agent 排名的一行结果。
type UserAgentRankItem struct {
	UserAgent string `json:"user_agent" gorm:"column:user_agent"`
	Count     int64  `json:"count" gorm:"column:count"`
}

// UserAgentUserItem 是某 UA 下的用户分布（含两阶段补全的用户展示信息）。
type UserAgentUserItem struct {
	UserId      int    `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Remark      string `json:"remark"`
	Count       int64  `json:"count"`
	LastSeen    int64  `json:"last_seen"`
}

// UserAgentMatchMode 控制 UA 匹配方式。
type UserAgentMatchMode string

const (
	UserAgentMatchExact    UserAgentMatchMode = "exact"
	UserAgentMatchContains UserAgentMatchMode = "contains"
)

// uaStatsFilter 构建 UA 统计的 LogStatsFilter，默认 type=consume/error。
func uaStatsFilter(startTimestamp, endTimestamp int64) LogStatsFilter {
	return LogStatsFilter{
		Types:          defaultConsumeErrorTypes(),
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
	}
}

// ListUserAgentRank 返回 User-Agent 调用量排名。
// 只统计 type IN (consume, error)、user_id <> 0、user_agent <> ” 的日志。
// keyword 非空时按包含匹配过滤（大小写不敏感，方言化；%/_/\ 作为字面字符）。
// keyword 非空时 trim 后长度必须 >= 2，否则返回 error（避免单字符全表扫描）。
// limit 被 clamp 到 [100, 1000]。
// 时间窗口必须有效（start/end 必填、end > start、窗口 <= 31 天），否则返回 error。
// 查询在 LOG_DB 上聚合，不涉及跨库 join。
func ListUserAgentRank(ctx context.Context, startTimestamp, endTimestamp int64, keyword string, limit int) (items []*UserAgentRankItem, totalUAs int64, err error) {
	if err = validateLogStatsWindow(startTimestamp, endTimestamp); err != nil {
		return nil, 0, err
	}
	limit = clampRankLimit(limit)
	k, err := validateUAContainsKeyword(keyword)
	if err != nil {
		return nil, 0, err
	}
	f := uaStatsFilter(startTimestamp, endTimestamp)
	f.UserAgentContains = k

	// 固定谓词：排除 user_id=0 和空 user_agent
	baseQuery := func() *gorm.DB {
		return newLogStatsQuery(ctx, f).Where("user_id <> 0").Where("user_agent <> ''")
	}

	if err = baseQuery().Distinct("user_agent").Count(&totalUAs).Error; err != nil {
		return nil, 0, err
	}

	err = baseQuery().
		Select("user_agent, count(*) as count").
		Group("user_agent").
		Order("count desc, user_agent asc").
		Limit(limit).
		Scan(&items).Error
	return items, totalUAs, err
}

// ListUserAgentUsers 返回指定 UA 下的用户分布。
// matchMode 为 exact 或 contains；contains 按大小写不敏感包含匹配（方言化，%/_/\ 作为字面字符）。
// contains 模式下 UA 非空时 trim 后长度必须 >= 2，否则返回 error。exact match 不受此限制。
// 两阶段：先在 LOG_DB 按 user_id 聚合，再到 DB 批量查用户展示信息。
// pageSize 被 clamp 到 [10, 100]，startIdx 必须在 [0, 10000] 范围内（超出返回 error）。
// 时间窗口必须有效（start/end 必填、end > start、窗口 <= 31 天），否则返回 error。
// 空 ua 返回空结果。主 DB 查询失败时返回 error。
func ListUserAgentUsers(ctx context.Context, ua string, matchMode UserAgentMatchMode, startTimestamp, endTimestamp int64, startIdx, pageSize int) (items []*UserAgentUserItem, totalUsers int64, err error) {
	if err = validateLogStatsWindow(startTimestamp, endTimestamp); err != nil {
		return nil, 0, err
	}
	if err = validateLogStatsOffset(startIdx); err != nil {
		return nil, 0, err
	}
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return []*UserAgentUserItem{}, 0, nil
	}
	pageSize = clampPageSize(pageSize)

	f := uaStatsFilter(startTimestamp, endTimestamp)
	switch matchMode {
	case UserAgentMatchContains:
		k, err := validateUAContainsKeyword(ua)
		if err != nil {
			return nil, 0, err
		}
		f.UserAgentContains = k
	default:
		f.UserAgent = ua
	}

	// f.UserAgent 或 f.UserAgentContains 已设置，applyLogStatsFilter 会应用对应谓词
	// （exact: user_agent = ?，隐含 user_agent <> ''；contains 同理）。
	// 仅需额外排除 user_id=0。
	baseQuery := func() *gorm.DB {
		return newLogStatsQuery(ctx, f).Where("user_id <> 0")
	}

	if err = baseQuery().Distinct("user_id").Count(&totalUsers).Error; err != nil {
		return nil, 0, err
	}

	type aggRow struct {
		UserId   int   `gorm:"column:user_id"`
		Count    int64 `gorm:"column:count"`
		LastSeen int64 `gorm:"column:last_seen"`
	}
	var rows []aggRow
	if err = baseQuery().
		Select("user_id, count(*) as count, max(created_at) as last_seen").
		Group("user_id").
		Order("count desc, user_id asc").
		Limit(pageSize).
		Offset(startIdx).
		Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	if len(rows) == 0 {
		return []*UserAgentUserItem{}, totalUsers, nil
	}

	userIds := make([]int, 0, len(rows))
	for _, row := range rows {
		if row.UserId != 0 {
			userIds = append(userIds, row.UserId)
		}
	}
	userMap, err := fetchLogUserDisplayMap(ctx, userIds)
	if err != nil {
		return nil, 0, err
	}

	items = make([]*UserAgentUserItem, 0, len(rows))
	for _, row := range rows {
		u := userMap[row.UserId]
		items = append(items, &UserAgentUserItem{
			UserId:      row.UserId,
			Username:    u.Username,
			DisplayName: u.DisplayName,
			Remark:      u.Remark,
			Count:       row.Count,
			LastSeen:    row.LastSeen,
		})
	}
	return items, totalUsers, nil
}
