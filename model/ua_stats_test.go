package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// uaStatsTestSeed 插入一组标准 UA 测试数据并返回 base timestamp 和创建的用户。
// 数据布局（所有日志 type=consume 除非另行标注）：
//
//	u1(alice): "curl/8.0"      @ base+0
//	u1(alice): "curl/8.0"      @ base+60
//	u1(alice): ""              @ base+300  (空 UA, 应被 UA rank 排除)
//	u2(bob):   "python/3.11"   @ base+120
//	u2(bob):   "python/3.11"   @ base+180  (type=error)
//	u0(系统):  "go/1.22"       @ base+240  (user_id=0, 应被排除)
func uaStatsTestSeed(t *testing.T) (base int64, u1, u2 *User) {
	t.Helper()
	truncateStatsTables(t)
	base = int64(2000000)
	u1 = mustCreateStatsUser(t, "alice", "Alice", "VIP")
	u2 = mustCreateStatsUser(t, "bob", "Bob", "")

	mustInsertStatsLog(t, statsTestLog(u1.Id, LogTypeConsume, "1.1.1.1", "curl/8.0", "/v1/chat", base, 10))
	mustInsertStatsLog(t, statsTestLog(u1.Id, LogTypeConsume, "1.1.1.1", "curl/8.0", "/v1/chat", base+60, 20))
	mustInsertStatsLog(t, statsTestLog(u1.Id, LogTypeConsume, "1.1.1.1", "", "/v1/chat", base+300, 5)) // empty UA
	mustInsertStatsLog(t, statsTestLog(u2.Id, LogTypeConsume, "2.2.2.2", "python/3.11", "/v1/chat", base+120, 5))
	mustInsertStatsLog(t, statsTestLog(u2.Id, LogTypeError, "2.2.2.2", "python/3.11", "/v1/chat", base+180, 15))
	mustInsertStatsLog(t, statsTestLog(0, LogTypeConsume, "3.3.3.3", "go/1.22", "/v1/chat", base+240, 1)) // user_id=0
	return
}

func TestListUserAgentRank_BasicAndFilters(t *testing.T) {
	base, _, _ := uaStatsTestSeed(t)
	ctx := context.Background()
	start, end := statsWideWindow(base)

	items, total, err := ListUserAgentRank(ctx, start, end, "", 100)
	require.NoError(t, err)
	// UA rank: user_id<>0 AND user_agent<>'' AND type IN (consume, error)
	//   "curl/8.0":    u1@base, u1@base+60 => 2
	//   "python/3.11": u2@base+120, u2@base+180(error) => 2
	//   "":            空 UA, 排除
	//   "go/1.22":     user_id=0, 排除
	assert.Equal(t, int64(2), total)
	require.Len(t, items, 2)
	// count 相同 (2), 按 user_agent asc: "curl/8.0" < "python/3.11"
	assert.Equal(t, "curl/8.0", items[0].UserAgent)
	assert.Equal(t, int64(2), items[0].Count)
	assert.Equal(t, "python/3.11", items[1].UserAgent)
	assert.Equal(t, int64(2), items[1].Count)
}

func TestListUserAgentRank_TimeRange(t *testing.T) {
	base, _, _ := uaStatsTestSeed(t)
	ctx := context.Background()

	// Half-open [base+120, base+240): python/3.11 @ base+120, base+180; curl/8.0 @ base+300 excluded
	items, total, err := ListUserAgentRank(ctx, base+120, base+240, "", 100)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	assert.Equal(t, "python/3.11", items[0].UserAgent)
	assert.Equal(t, int64(2), items[0].Count)
}

func TestListUserAgentRank_KeywordContains(t *testing.T) {
	base, _, _ := uaStatsTestSeed(t)
	ctx := context.Background()
	start, end := statsWideWindow(base)

	// contains "python"
	items, total, err := ListUserAgentRank(ctx, start, end, "python", 100)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	assert.Equal(t, "python/3.11", items[0].UserAgent)

	// contains "PYTHON" — SQLite LIKE is case-insensitive for ASCII by default,
	// so this matches "python/3.11" (case-insensitive behavior is dialect-dependent).
	items, total, err = ListUserAgentRank(ctx, start, end, "PYTHON", 100)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, items, 1)

	// contains "curl"
	items, total, err = ListUserAgentRank(ctx, start, end, "curl", 100)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	assert.Equal(t, "curl/8.0", items[0].UserAgent)

	// no match
	items, total, err = ListUserAgentRank(ctx, start, end, "nonexistent", 100)
	require.NoError(t, err)
	assert.Equal(t, int64(0), total)
	assert.Empty(t, items)
}

func TestListUserAgentRank_KeywordTooShort(t *testing.T) {
	base, _, _ := uaStatsTestSeed(t)
	ctx := context.Background()
	start, end := statsWideWindow(base)

	// Single character keyword → error
	_, _, err := ListUserAgentRank(ctx, start, end, "a", 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least 2 characters")
}

func TestListUserAgentRank_LimitClamped(t *testing.T) {
	base, _, _ := uaStatsTestSeed(t)
	ctx := context.Background()
	start, end := statsWideWindow(base)

	items1, _, err := ListUserAgentRank(ctx, start, end, "", 0)
	require.NoError(t, err)
	assert.Len(t, items1, 2)

	items2, _, err := ListUserAgentRank(ctx, start, end, "", 1)
	require.NoError(t, err)
	require.Len(t, items2, 1)
	assert.Equal(t, "curl/8.0", items2[0].UserAgent)

	items3, _, err := ListUserAgentRank(ctx, start, end, "", 99999)
	require.NoError(t, err)
	assert.Len(t, items3, 2)
}

func TestListUserAgentRank_InvalidWindow(t *testing.T) {
	uaStatsTestSeed(t)
	ctx := context.Background()

	_, _, err := ListUserAgentRank(ctx, 0, 0, "", 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start_timestamp must be positive")
}

func TestListUserAgentUsers_ExactMatch(t *testing.T) {
	base, u1, u2 := uaStatsTestSeed(t)
	ctx := context.Background()
	start, end := statsWideWindow(base)

	items, total, err := ListUserAgentUsers(ctx, "curl/8.0", UserAgentMatchExact, start, end, 0, 100)
	require.NoError(t, err)
	// "curl/8.0" 下只有 u1 (2 条: base, base+60)
	assert.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	assert.Equal(t, u1.Id, items[0].UserId)
	assert.Equal(t, int64(2), items[0].Count)
	assert.Equal(t, base+60, items[0].LastSeen)
	assert.Equal(t, "alice", items[0].Username)
	assert.Equal(t, "Alice", items[0].DisplayName)
	assert.Equal(t, "VIP", items[0].Remark)

	// exact match for "python/3.11" → u2
	items, total, err = ListUserAgentUsers(ctx, "python/3.11", UserAgentMatchExact, start, end, 0, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	assert.Equal(t, u2.Id, items[0].UserId)
	assert.Equal(t, int64(2), items[0].Count)
	assert.Equal(t, base+180, items[0].LastSeen)
	assert.Equal(t, "bob", items[0].Username)
	assert.Equal(t, "Bob", items[0].DisplayName)
}

func TestListUserAgentUsers_ContainsMatch(t *testing.T) {
	base, _, u2 := uaStatsTestSeed(t)
	ctx := context.Background()
	start, end := statsWideWindow(base)

	// contains "python" → matches "python/3.11" → u2 only
	items, total, err := ListUserAgentUsers(ctx, "python", UserAgentMatchContains, start, end, 0, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	assert.Equal(t, u2.Id, items[0].UserId)

	// contains "" (after trim) → treated as empty, returns empty
	items, total, err = ListUserAgentUsers(ctx, "  ", UserAgentMatchContains, start, end, 0, 100)
	require.NoError(t, err)
	assert.Empty(t, items)
	assert.Equal(t, int64(0), total)
}

func TestListUserAgentUsers_ContainsKeywordTooShort(t *testing.T) {
	base, _, _ := uaStatsTestSeed(t)
	ctx := context.Background()
	start, end := statsWideWindow(base)

	// Single character contains keyword → error
	_, _, err := ListUserAgentUsers(ctx, "a", UserAgentMatchContains, start, end, 0, 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least 2 characters")

	// Exact match with single character is OK (not subject to min length)
	items, _, err := ListUserAgentUsers(ctx, "a", UserAgentMatchExact, start, end, 0, 100)
	require.NoError(t, err)
	assert.Empty(t, items, "exact match with short keyword is allowed, just no results")
}

func TestListUserAgentUsers_PageSizeAndOffset(t *testing.T) {
	base, u1, u2 := uaStatsTestSeed(t)
	ctx := context.Background()
	start, end := statsWideWindow(base)
	mustInsertStatsLog(t, statsTestLog(u2.Id, LogTypeConsume, "9.9.9.9", "curl/8.0", "/v1/chat", base+400, 1))

	// "curl/8.0" 下现在有 u1 (2 条) 和 u2 (1 条)
	items, total, err := ListUserAgentUsers(ctx, "curl/8.0", UserAgentMatchExact, start, end, 0, 1)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	require.Len(t, items, 1)
	assert.Equal(t, u1.Id, items[0].UserId, "top user by count should be u1")

	// offset=1
	items, _, err = ListUserAgentUsers(ctx, "curl/8.0", UserAgentMatchExact, start, end, 1, 100)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, u2.Id, items[0].UserId)
}

func TestListUserAgentUsers_EmptyUA(t *testing.T) {
	base, _, _ := uaStatsTestSeed(t)
	ctx := context.Background()
	start, end := statsWideWindow(base)

	items, total, err := ListUserAgentUsers(ctx, "", UserAgentMatchExact, start, end, 0, 100)
	require.NoError(t, err)
	assert.Empty(t, items)
	assert.Equal(t, int64(0), total)

	items, total, err = ListUserAgentUsers(ctx, "   ", UserAgentMatchContains, start, end, 0, 100)
	require.NoError(t, err)
	assert.Empty(t, items)
	assert.Equal(t, int64(0), total)
}

func TestListUserAgentUsers_ExcludesUserIdZero(t *testing.T) {
	base, _, _ := uaStatsTestSeed(t)
	ctx := context.Background()
	start, end := statsWideWindow(base)

	items, total, err := ListUserAgentUsers(ctx, "go/1.22", UserAgentMatchExact, start, end, 0, 100)
	require.NoError(t, err)
	assert.Empty(t, items)
	assert.Equal(t, int64(0), total, "user_id=0 must be excluded")
}

func TestListUserAgentUsers_InvalidWindow(t *testing.T) {
	uaStatsTestSeed(t)
	ctx := context.Background()

	_, _, err := ListUserAgentUsers(ctx, "curl/8.0", UserAgentMatchExact, 0, 0, 0, 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start_timestamp must be positive")
}

func TestListUserAgentUsers_OffsetExceeded(t *testing.T) {
	base, _, _ := uaStatsTestSeed(t)
	ctx := context.Background()
	start, end := statsWideWindow(base)

	_, _, err := ListUserAgentUsers(ctx, "curl/8.0", UserAgentMatchExact, start, end, logStatsMaxOffset+1, 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not exceed")
}
