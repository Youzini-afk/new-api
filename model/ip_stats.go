package model

import (
	"context"
	"strings"

	"gorm.io/gorm"
)

// IPStatsRankItem 是 IP 排名的一行结果。
type IPStatsRankItem struct {
	IP    string `json:"ip" gorm:"column:ip"`
	Count int64  `json:"count" gorm:"column:count"`
}

// IPStatsUserItem 是某 IP 下用户分布的一行结果（含两阶段补全的用户展示信息）。
type IPStatsUserItem struct {
	UserId      int    `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Remark      string `json:"remark"`
	Count       int64  `json:"count"`
	LastSeen    int64  `json:"last_seen"`
}

// ipStatsFilter 构建 IP 统计的 LogStatsFilter，默认 type=consume/error。
func ipStatsFilter(requestPath string, startTimestamp, endTimestamp int64) LogStatsFilter {
	return LogStatsFilter{
		Types:          defaultConsumeErrorTypes(),
		RequestPath:    strings.TrimSpace(requestPath),
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
	}
}

// ListIPStatsRank 返回指定 request_path 下的 IP 调用量排名。
// 只统计 type IN (consume, error)、user_id <> 0、ip <> ” 的日志。
// limit 被 clamp 到 [100, 1000]；空 requestPath 返回空结果。
// 时间窗口必须有效（start/end 必填、end > start、窗口 <= 31 天），否则返回 error。
// 查询在 LOG_DB 上聚合，不涉及跨库 join。
func ListIPStatsRank(ctx context.Context, requestPath string, startTimestamp, endTimestamp int64, limit int) (items []*IPStatsRankItem, totalIps int64, err error) {
	if err = validateLogStatsWindow(startTimestamp, endTimestamp); err != nil {
		return nil, 0, err
	}
	f := ipStatsFilter(requestPath, startTimestamp, endTimestamp)
	if f.RequestPath == "" {
		return []*IPStatsRankItem{}, 0, nil
	}
	limit = clampRankLimit(limit)

	// 固定谓词：排除 user_id=0 和空 ip
	baseQuery := func() *gorm.DB {
		return newLogStatsQuery(ctx, f).Where("user_id <> 0").Where("ip <> ''")
	}

	if err = baseQuery().Distinct("ip").Count(&totalIps).Error; err != nil {
		return nil, 0, err
	}

	err = baseQuery().
		Select("ip, count(*) as count").
		Group("ip").
		Order("count desc, ip asc").
		Limit(limit).
		Scan(&items).Error
	return items, totalIps, err
}

// ListIPStatsUsers 返回指定 IP 在指定 request_path 下的用户分布。
// 两阶段：先在 LOG_DB 按 user_id 聚合，再到 DB 批量查用户展示信息。
// pageSize 被 clamp 到 [10, 100]，startIdx 必须在 [0, 10000] 范围内（超出返回 error）。
// 时间窗口必须有效（start/end 必填、end > start、窗口 <= 31 天），否则返回 error。
// 空 requestPath 或空 ip 返回空结果。主 DB 查询失败时返回 error。
func ListIPStatsUsers(ctx context.Context, requestPath, ip string, startTimestamp, endTimestamp int64, startIdx, pageSize int) (items []*IPStatsUserItem, totalUsers int64, err error) {
	if err = validateLogStatsWindow(startTimestamp, endTimestamp); err != nil {
		return nil, 0, err
	}
	if err = validateLogStatsOffset(startIdx); err != nil {
		return nil, 0, err
	}
	f := ipStatsFilter(requestPath, startTimestamp, endTimestamp)
	f.IP = strings.TrimSpace(ip)
	if f.RequestPath == "" || f.IP == "" {
		return []*IPStatsUserItem{}, 0, nil
	}
	pageSize = clampPageSize(pageSize)

	// f.IP 已设置，applyLogStatsFilter 会应用 ip = ? 谓词（隐含 ip <> ''）。
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
		return []*IPStatsUserItem{}, totalUsers, nil
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

	items = make([]*IPStatsUserItem, 0, len(rows))
	for _, row := range rows {
		u := userMap[row.UserId]
		items = append(items, &IPStatsUserItem{
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
