package model

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// LogStatsFilter 集中表达日志统计查询的过滤条件。
// 所有字段均为可选（零值表示不过滤）；调用方负责设置业务必需的固定谓词
// （如 IP/UA 统计的 user_id <> 0、ip <> ” 等）。
//
// 时间范围使用半开区间 [StartTimestamp, EndTimestamp)：
// created_at >= StartTimestamp AND created_at < EndTimestamp。
// 半开区间避免边界时间戳的重复计数，并使 coverage bucket 数学自洽。
//
// 该结构由 Phase 2B 引入，作为 IP/UA 统计和用户排行榜的统一过滤入口，
// 后续 Phase 3 只读运营 API 直接复用。
type LogStatsFilter struct {
	StartTimestamp    int64
	EndTimestamp      int64
	Types             []int    // 为空时不按 type 过滤；IP/UA 统计默认 [consume, error]
	RequestPath       string   // 精确匹配单个 request_path
	RequestPaths      []string // 精确匹配多个 request_path
	UserId            int      // 为 0 时不过滤
	UserIds           []int
	IP                string
	UserAgent         string // 精确匹配
	UserAgentContains string // 包含匹配（方言化；%/_/! 作为字面字符）
	ChannelId         int
	Group             string
	ModelName         string
	TokenName         string
	RequestId         string
	UpstreamRequestId string
}

const (
	logStatsDefaultRankLimit = 100
	logStatsMaxRankLimit     = 1000
	logStatsDefaultPageSize  = 10
	logStatsMaxPageSize      = 100
	// logStatsMaxOffset 限制深分页的 offset 上限，避免 OFFSET 扫描开销过大。
	logStatsMaxOffset = 10000
	// logStatsMaxWindowSeconds 限制单次统计查询的时间窗口上限为 31 天，
	// 防止无界全表扫描。
	logStatsMaxWindowSeconds = 31 * 24 * 3600
	// logStatsMinUAContainsLen 限制 UA contains 关键词的最小长度，
	// 避免单字符全表 LIKE 扫描。exact match 不受此限制。
	logStatsMinUAContainsLen = 2
)

// clampRankLimit 将 rank 查询的 limit 规范化到 [default, max] 区间，
// 统一处理所有日志统计排名查询的上限保护。
func clampRankLimit(limit int) int {
	if limit <= 0 {
		return logStatsDefaultRankLimit
	}
	if limit > logStatsMaxRankLimit {
		return logStatsMaxRankLimit
	}
	return limit
}

// clampPageSize 将分页 pageSize 规范化到 [default, max] 区间，
// 统一处理所有日志统计分页查询的上限保护。
func clampPageSize(pageSize int) int {
	if pageSize <= 0 {
		return logStatsDefaultPageSize
	}
	if pageSize > logStatsMaxPageSize {
		return logStatsMaxPageSize
	}
	return pageSize
}

// defaultConsumeErrorTypes 返回 [consume, error] 类型，作为 IP/UA 统计的默认 type 过滤。
func defaultConsumeErrorTypes() []int {
	return []int{LogTypeConsume, LogTypeError}
}

// validateLogStatsWindow 校验日志统计查询的时间窗口。
// 要求 start/end 必填（正整数）、end > start、且窗口不超过 logStatsMaxWindowSeconds。
// 违反返回 error。这是 model 层 DoS 保护的核心校验，所有导出的日志统计函数必须调用。
func validateLogStatsWindow(startTimestamp, endTimestamp int64) error {
	if startTimestamp <= 0 {
		return fmt.Errorf("log stats: start_timestamp must be positive (got %d)", startTimestamp)
	}
	if endTimestamp <= 0 {
		return fmt.Errorf("log stats: end_timestamp must be positive (got %d)", endTimestamp)
	}
	if endTimestamp <= startTimestamp {
		return fmt.Errorf("log stats: end_timestamp (%d) must be greater than start_timestamp (%d)", endTimestamp, startTimestamp)
	}
	if endTimestamp-startTimestamp > logStatsMaxWindowSeconds {
		return fmt.Errorf("log stats: time window must not exceed %d seconds (got %d)", logStatsMaxWindowSeconds, endTimestamp-startTimestamp)
	}
	return nil
}

// validateLogStatsOffset 校验分页 offset：必须非负且不超过 logStatsMaxOffset。
// 返回 error 而非 clamp，避免深分页导致的 OFFSET 扫描性能退化。
func validateLogStatsOffset(offset int) error {
	if offset < 0 {
		return fmt.Errorf("log stats: offset must be non-negative (got %d)", offset)
	}
	if offset > logStatsMaxOffset {
		return fmt.Errorf("log stats: offset must not exceed %d (got %d)", logStatsMaxOffset, offset)
	}
	return nil
}

// validateUAContainsKeyword 校验 UA contains 关键词。
// 空关键词（trim 后）返回空字符串 + nil error，表示不过滤。
// 非空关键词长度必须 >= logStatsMinUAContainsLen，否则返回 error。
// exact match 不受此限制（调用方不调用此函数）。
func validateUAContainsKeyword(keyword string) (string, error) {
	k := strings.TrimSpace(keyword)
	if k == "" {
		return "", nil
	}
	if len(k) < logStatsMinUAContainsLen {
		return "", fmt.Errorf("log stats: UA contains keyword must be at least %d characters (got %d)", logStatsMinUAContainsLen, len(k))
	}
	return k, nil
}

// escapeLikeLiteral 转义 LIKE 模式中的通配符（%、_）和转义字符（!），
// 使其作为字面字符匹配。使用 ! 作为 ESCAPE 字符，避免 MySQL/ClickHouse
// 反斜杠字符串字面量差异。
// 调用方必须在 SQL 中附加 ESCAPE '!' 子句。
func escapeLikeLiteral(s string) string {
	s = strings.ReplaceAll(s, `!`, `!!`)
	s = strings.ReplaceAll(s, `%`, `!%`)
	s = strings.ReplaceAll(s, `_`, `!_`)
	return s
}

// newLogStatsQuery 在 LOG_DB 上创建一个带 context 的 logs 表查询，并应用统一过滤条件。
// 调用方可在返回的 tx 上继续追加 Select/Group/Order/Limit 等。
func newLogStatsQuery(ctx context.Context, f LogStatsFilter) *gorm.DB {
	tx := LOG_DB.WithContext(ctx).Table("logs")
	return applyLogStatsFilter(tx, f)
}

// applyLogStatsFilter 将 LogStatsFilter 中的非零字段应用到 tx 上。
// 不修改原始 filter；所有字段均为可选（零值 = 不过滤）。
// 时间范围使用半开区间 [start, end)：created_at >= start AND created_at < end。
// 方言相关的谓词（如 UA contains）在内部集中处理，调用方无需关心方言差异。
func applyLogStatsFilter(tx *gorm.DB, f LogStatsFilter) *gorm.DB {
	if len(f.Types) > 0 {
		tx = tx.Where("type IN ?", f.Types)
	}
	if f.StartTimestamp > 0 {
		tx = tx.Where("created_at >= ?", f.StartTimestamp)
	}
	if f.EndTimestamp > 0 {
		tx = tx.Where("created_at < ?", f.EndTimestamp) // 半开区间 [start, end)
	}
	if f.RequestPath != "" {
		tx = tx.Where("request_path = ?", f.RequestPath)
	}
	if len(f.RequestPaths) > 0 {
		tx = tx.Where("request_path IN ?", f.RequestPaths)
	}
	if f.UserId != 0 {
		tx = tx.Where("user_id = ?", f.UserId)
	}
	if len(f.UserIds) > 0 {
		tx = tx.Where("user_id IN ?", f.UserIds)
	}
	if f.IP != "" {
		tx = tx.Where("ip = ?", f.IP)
	}
	if f.UserAgent != "" {
		tx = tx.Where("user_agent = ?", f.UserAgent)
	}
	if f.UserAgentContains != "" {
		if sql, args := userAgentContainsClause("user_agent", f.UserAgentContains); sql != "" {
			tx = tx.Where(sql, args...)
		}
	}
	if f.ChannelId != 0 {
		tx = tx.Where("channel_id = ?", f.ChannelId)
	}
	if f.Group != "" {
		tx = tx.Where(logGroupCol+" = ?", f.Group)
	}
	if f.ModelName != "" {
		tx = tx.Where("model_name = ?", f.ModelName)
	}
	if f.TokenName != "" {
		tx = tx.Where("token_name = ?", f.TokenName)
	}
	if f.RequestId != "" {
		tx = tx.Where("request_id = ?", f.RequestId)
	}
	if f.UpstreamRequestId != "" {
		tx = tx.Where("upstream_request_id = ?", f.UpstreamRequestId)
	}
	return tx
}

// userAgentContainsClause 返回大小写不敏感包含匹配的 WHERE SQL 片段与绑定参数。
// 关键词中的 %、_、! 被转义为字面字符（使用 escapeLikeLiteral），附加 ESCAPE '!' 子句。
//
// 方言处理：
//   - PostgreSQL：使用 ILIKE ? ESCAPE '!'（原生大小写不敏感）
//   - ClickHouse：使用 lower(col) LIKE lower(?) ESCAPE '!'（ClickHouse 无 ILIKE）
//   - 默认（SQLite/MySQL）：使用 LIKE ? ESCAPE '!'（大小写敏感性取决于 collation；
//     SQLite 默认大小写敏感，MySQL 取决于列 collation）
//
// 空关键词返回空 SQL（调用方应跳过）。
// 此函数是纯函数（仅依赖 common.LogDatabaseType()），便于直接测试方言分支。
func userAgentContainsClause(column, keyword string) (string, []interface{}) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return "", nil
	}
	pattern := "%" + escapeLikeLiteral(keyword) + "%"
	switch common.LogDatabaseType() {
	case common.DatabaseTypePostgreSQL:
		return column + " ILIKE ? ESCAPE '!'", []interface{}{pattern}
	case common.DatabaseTypeClickHouse:
		return "lower(" + column + ") LIKE lower(?) ESCAPE '!'", []interface{}{pattern}
	default:
		return column + " LIKE ? ESCAPE '!'", []interface{}{pattern}
	}
}

// logStatsCoverageBucketExpr 返回将 created_at 映射到相对起点的时间桶编号的 SQL 表达式，
// 用于 coverage 排行榜的 COUNT(DISTINCT <bucket>) 聚合。
//
// 使用相对起点 (created_at - startTimestamp) / slotSeconds 而非绝对 created_at / slotSeconds，
// 确保桶编号从 0 开始对齐到查询窗口起点，使 totalSlots 计算与 activeSlots 自洽。
//
// startTimestamp 与 slotSeconds 都被校验为非负/正整数后格式化为字面量直接拼接到 SQL 中，
// 而非使用 ? 绑定参数——因为 ClickHouse 在聚合表达式中对 ? 绑定的支持不稳定。
//
// 方言处理：
//   - SQLite：整数除法天然截断 (created_at - start) / N，不需要 FLOOR
//   - ClickHouse：使用 intDiv(created_at - start, N)，避免表达式内 ? 绑定
//   - 默认（MySQL/PostgreSQL）：使用 FLOOR((created_at - start) / N)
//
// 此函数是纯函数（仅依赖 common.LogDatabaseType()），便于直接测试方言分支。
// 对非正 slotSeconds 做防御性回退到 60 秒，对负 startTimestamp 回退到 0，避免除零或负值。
func logStatsCoverageBucketExpr(startTimestamp, slotSeconds int64) string {
	if startTimestamp < 0 {
		startTimestamp = 0
	}
	if slotSeconds <= 0 {
		slotSeconds = 60
	}
	start := strconv.FormatInt(startTimestamp, 10)
	bucket := strconv.FormatInt(slotSeconds, 10)
	switch common.LogDatabaseType() {
	case common.DatabaseTypeSQLite:
		return "(created_at - " + start + ") / " + bucket
	case common.DatabaseTypeClickHouse:
		return "intDiv(created_at - " + start + ", " + bucket + ")"
	default:
		return "FLOOR((created_at - " + start + ") / " + bucket + ")"
	}
}

// logUserDisplayInfo 是日志统计两阶段补全的用户展示信息。
type logUserDisplayInfo struct {
	Username    string
	DisplayName string
	Remark      string
}

// fetchLogUserDisplayMap 从主 DB（非 LOG_DB）批量查询用户展示信息。
// 这是两阶段查询的第二阶段：先在 LOG_DB 聚合得到 user_id 列表，
// 再到 DB 批量查 username/display_name/remark，避免跨库 join/subquery。
// 使用 Unscoped 以包含软删除用户（日志可能引用已删除用户）。
// 返回 error 以传播主 DB 查询失败，调用方必须检查。
func fetchLogUserDisplayMap(ctx context.Context, userIds []int) (map[int]logUserDisplayInfo, error) {
	if len(userIds) == 0 {
		return map[int]logUserDisplayInfo{}, nil
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
	m := make(map[int]logUserDisplayInfo, len(users))
	for _, u := range users {
		m[u.Id] = logUserDisplayInfo{
			Username:    u.Username,
			DisplayName: u.DisplayName,
			Remark:      u.Remark,
		}
	}
	return m, nil
}
