package controller

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	logStatsDaySeconds int64 = 86400
)

// parseLogStatsTimeRange 解析日志统计查询的时间窗口查询参数。
//
// 支持的查询参数：
//   - start_timestamp: 半开区间起点（含），秒级 unix 时间戳
//   - end_timestamp: 半开区间终点（不含），秒级 unix 时间戳
//   - range_days: 回填用的窗口天数
//
// 解析规则：
//   - range_days 显式提供时必须为正整数；解析失败或 <=0 返回 error。
//     未提供时使用 defaultRangeDays。
//   - 若 start/end 同时提供，直接使用；range_days 仍按上述规则解析后回显。
//   - 若只提供 start，end = start + rangeDays*86400。
//   - 若只提供 end，start = end - rangeDays*86400。
//   - 若都未提供，end = now，start = end - rangeDays*86400。
//   - 时间戳参数解析失败按 0（未提供）处理，与现有 log 控制器保持一致；
//     最终窗口合法性（end > start、窗口上限等）由 model 层校验。
func parseLogStatsTimeRange(c *gin.Context, defaultRangeDays int) (startTs, endTs int64, rangeDays int, err error) {
	rangeDays = defaultRangeDays
	if rawRange := c.Query("range_days"); rawRange != "" {
		parsed, parseErr := strconv.Atoi(rawRange)
		if parseErr != nil {
			return 0, 0, 0, fmt.Errorf("range_days must be a positive integer (got %q)", rawRange)
		}
		if parsed <= 0 {
			return 0, 0, 0, fmt.Errorf("range_days must be a positive integer (got %d)", parsed)
		}
		rangeDays = parsed
	}
	if rangeDays <= 0 {
		return 0, 0, 0, fmt.Errorf("range_days must be a positive integer (got %d)", rangeDays)
	}

	startTs = parseTimestampQuery(c, "start_timestamp")
	endTs = parseTimestampQuery(c, "end_timestamp")

	daySeconds := int64(rangeDays) * logStatsDaySeconds
	switch {
	case startTs > 0 && endTs > 0:
		// 两者均提供，直接使用；range_days 仅作回显。
	case startTs > 0:
		endTs = startTs + daySeconds
	case endTs > 0:
		startTs = endTs - daySeconds
	default:
		endTs = time.Now().Unix()
		startTs = endTs - daySeconds
	}
	return startTs, endTs, rangeDays, nil
}

// parseTimestampQuery 解析秒级 unix 时间戳查询参数；空或解析失败返回 0。
func parseTimestampQuery(c *gin.Context, key string) int64 {
	raw := c.Query(key)
	if raw == "" {
		return 0
	}
	ts, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return ts
}

// parsePositiveIntQuery 解析正整数查询参数；缺失返回 defaultValue，解析失败或为负返回 error。
func parsePositiveIntQuery(c *gin.Context, key string, defaultValue int) (int, error) {
	raw := c.Query(key)
	if raw == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer (got %q)", key, raw)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer (got %d)", key, parsed)
	}
	return parsed, nil
}

// parseLimitQuery 解析 limit 查询参数；缺失或解析失败返回 defaultValue。
// 不校验 <=0：交由 model 层 clampRankLimit/clampPageSize 统一 clamp，
// 与现有 log 控制器的 lenient 风格保持一致。
func parseLimitQuery(c *gin.Context, defaultValue int) int {
	raw := c.Query("limit")
	if raw == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return defaultValue
	}
	return parsed
}

// conversationKindChatCompletions 等是 conversation 统计的 kind 别名。
const (
	conversationKindChatCompletions = "chat_completions"
	conversationKindResponses       = "responses"
	conversationKindMessages        = "messages"
)

// normalizeConversationKind 规范化 conversation kind 查询参数：空值默认 chat_completions。
func normalizeConversationKind(kind string) string {
	k := strings.TrimSpace(kind)
	if k == "" {
		return conversationKindChatCompletions
	}
	return k
}

// conversationRequestPathForKind 将 conversation kind 别名映射到实际 request_path。
// 空 kind 默认 chat_completions；非法 kind 返回 error。
func conversationRequestPathForKind(kind string) (string, error) {
	switch normalizeConversationKind(kind) {
	case conversationKindChatCompletions:
		return "/v1/chat/completions", nil
	case conversationKindResponses:
		return "/v1/responses", nil
	case conversationKindMessages:
		return "/v1/messages", nil
	default:
		return "", fmt.Errorf("unsupported kind %q (expected chat_completions, responses, or messages)", kind)
	}
}
