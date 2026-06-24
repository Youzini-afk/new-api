package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ipStatsTestSeed 插入一组标准测试数据并返回 base timestamp 和创建的用户。
// 数据布局（所有日志 request_path="/v1/chat", type=consume 除非另行标注）：
//
//	u1(alice): 1.1.1.1 @ base+0,  1.1.1.1 @ base+60,  1.1.1.1 @ base+300(ua="")
//	u2(bob):   1.1.1.1 @ base+120, 2.2.2.2 @ base+180
//	u0(系统):  3.3.3.3 @ base+240  (user_id=0, 应被排除)
//	u1(alice): ""      @ base+360  (空 ip, 应被 IP rank 排除)
func ipStatsTestSeed(t *testing.T) (base int64, u1, u2 *User) {
	t.Helper()
	truncateStatsTables(t)
	base = int64(1000000)
	u1 = mustCreateStatsUser(t, "alice", "Alice", "VIP")
	u2 = mustCreateStatsUser(t, "bob", "Bob", "")

	mustInsertStatsLog(t, statsTestLog(u1.Id, LogTypeConsume, "1.1.1.1", "curl/8.0", "/v1/chat", base, 10))
	mustInsertStatsLog(t, statsTestLog(u1.Id, LogTypeConsume, "1.1.1.1", "curl/8.0", "/v1/chat", base+60, 20))
	mustInsertStatsLog(t, statsTestLog(u1.Id, LogTypeConsume, "1.1.1.1", "", "/v1/chat", base+300, 5)) // empty UA
	mustInsertStatsLog(t, statsTestLog(u2.Id, LogTypeConsume, "1.1.1.1", "python/3.11", "/v1/chat", base+120, 5))
	mustInsertStatsLog(t, statsTestLog(u2.Id, LogTypeError, "2.2.2.2", "python/3.11", "/v1/chat", base+180, 15))
	mustInsertStatsLog(t, statsTestLog(0, LogTypeConsume, "3.3.3.3", "go/1.22", "/v1/chat", base+240, 1)) // user_id=0
	mustInsertStatsLog(t, statsTestLog(u1.Id, LogTypeConsume, "", "curl/8.0", "/v1/chat", base+360, 1))   // empty ip
	return
}

func TestListIPStatsRank_BasicAndFilters(t *testing.T) {
	base, _, _ := ipStatsTestSeed(t)
	ctx := context.Background()
	start, end := statsWideWindow(base)

	items, total, err := ListIPStatsRank(ctx, "/v1/chat", start, end, 100)
	require.NoError(t, err)
	// IP rank: user_id<>0 AND ip<>'' AND type IN (consume, error)
	//   1.1.1.1: u1@base, u1@base+60, u1@base+300, u2@base+120 => 4 条
	//   2.2.2.2: u2@base+180 (error) => 1 条
	//   3.3.3.3: user_id=0, 排除
	//   "":      空 ip, 排除
	assert.Equal(t, int64(2), total, "two distinct IPs: 1.1.1.1 and 2.2.2.2")
	require.Len(t, items, 2)
	assert.Equal(t, "1.1.1.1", items[0].IP)
	assert.Equal(t, int64(4), items[0].Count)
	assert.Equal(t, "2.2.2.2", items[1].IP)
	assert.Equal(t, int64(1), items[1].Count)
}

func TestListIPStatsRank_TimeRange(t *testing.T) {
	base, _, _ := ipStatsTestSeed(t)
	ctx := context.Background()

	// Half-open [base+120, base+240): u2@base+120, u2@base+180; u0@base+240 excluded by half-open AND user_id=0
	items, total, err := ListIPStatsRank(ctx, "/v1/chat", base+120, base+240, 100)
	require.NoError(t, err)
	// 在范围内:
	//   1.1.1.1: u2@base+120 => 1 条
	//   2.2.2.2: u2@base+180 => 1 条
	//   3.3.3.3: user_id=0, 排除
	assert.Equal(t, int64(2), total)
	require.Len(t, items, 2)
	assert.Equal(t, "1.1.1.1", items[0].IP)
	assert.Equal(t, int64(1), items[0].Count)
	assert.Equal(t, "2.2.2.2", items[1].IP)
	assert.Equal(t, int64(1), items[1].Count)
}

func TestListIPStatsRank_EmptyRequestPath(t *testing.T) {
	base, _, _ := ipStatsTestSeed(t)
	ctx := context.Background()
	start, end := statsWideWindow(base)

	// Empty requestPath with valid window → returns empty (not error)
	items, total, err := ListIPStatsRank(ctx, "", start, end, 100)
	require.NoError(t, err)
	assert.Empty(t, items)
	assert.Equal(t, int64(0), total)

	items, total, err = ListIPStatsRank(ctx, "   ", start, end, 100)
	require.NoError(t, err)
	assert.Empty(t, items)
	assert.Equal(t, int64(0), total)
}

func TestListIPStatsRank_LimitClamped(t *testing.T) {
	base, _, _ := ipStatsTestSeed(t)
	ctx := context.Background()
	start, end := statsWideWindow(base)

	items1, _, err := ListIPStatsRank(ctx, "/v1/chat", start, end, 0)
	require.NoError(t, err)
	assert.Len(t, items1, 2)

	items2, _, err := ListIPStatsRank(ctx, "/v1/chat", start, end, -1)
	require.NoError(t, err)
	assert.Len(t, items2, 2)

	items3, _, err := ListIPStatsRank(ctx, "/v1/chat", start, end, 99999)
	require.NoError(t, err)
	assert.Len(t, items3, 2)

	items4, _, err := ListIPStatsRank(ctx, "/v1/chat", start, end, 1)
	require.NoError(t, err)
	require.Len(t, items4, 1)
	assert.Equal(t, "1.1.1.1", items4[0].IP)
}

func TestListIPStatsRank_InvalidWindow(t *testing.T) {
	ipStatsTestSeed(t)
	ctx := context.Background()

	// No time range
	_, _, err := ListIPStatsRank(ctx, "/v1/chat", 0, 0, 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start_timestamp must be positive")

	// end <= start
	_, _, err = ListIPStatsRank(ctx, "/v1/chat", 1000, 1000, 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be greater than")

	// Window too large
	_, _, err = ListIPStatsRank(ctx, "/v1/chat", 1, 1+int64(logStatsMaxWindowSeconds)+1, 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not exceed")
}

func TestListIPStatsUsers_TwoPhaseCompletion(t *testing.T) {
	base, u1, u2 := ipStatsTestSeed(t)
	ctx := context.Background()
	start, end := statsWideWindow(base)

	items, total, err := ListIPStatsUsers(ctx, "/v1/chat", "1.1.1.1", start, end, 0, 100)
	require.NoError(t, err)
	// 1.1.1.1 下有两个用户: u1 (3 条: base, base+60, base+300) 和 u2 (1 条: base+120)
	assert.Equal(t, int64(2), total)
	require.Len(t, items, 2)

	// 按 count desc: u1 (3) 在前, u2 (1) 在后
	assert.Equal(t, u1.Id, items[0].UserId)
	assert.Equal(t, int64(3), items[0].Count)
	assert.Equal(t, base+300, items[0].LastSeen)
	assert.Equal(t, "alice", items[0].Username)
	assert.Equal(t, "Alice", items[0].DisplayName)
	assert.Equal(t, "VIP", items[0].Remark)

	assert.Equal(t, u2.Id, items[1].UserId)
	assert.Equal(t, int64(1), items[1].Count)
	assert.Equal(t, base+120, items[1].LastSeen)
	assert.Equal(t, "bob", items[1].Username)
	assert.Equal(t, "Bob", items[1].DisplayName)
	assert.Empty(t, items[1].Remark)
}

func TestListIPStatsUsers_ExcludesUserIdZero(t *testing.T) {
	base, _, _ := ipStatsTestSeed(t)
	ctx := context.Background()
	start, end := statsWideWindow(base)

	items, total, err := ListIPStatsUsers(ctx, "/v1/chat", "3.3.3.3", start, end, 0, 100)
	require.NoError(t, err)
	assert.Empty(t, items)
	assert.Equal(t, int64(0), total, "user_id=0 must be excluded")
}

func TestListIPStatsUsers_PageSizeAndOffset(t *testing.T) {
	base, u1, u2 := ipStatsTestSeed(t)
	ctx := context.Background()
	start, end := statsWideWindow(base)

	// pageSize=1 只返回第一个用户
	items, total, err := ListIPStatsUsers(ctx, "/v1/chat", "1.1.1.1", start, end, 0, 1)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total, "total still counts both users")
	require.Len(t, items, 1)
	assert.Equal(t, u1.Id, items[0].UserId)

	// offset=1 跳过第一个用户
	items, _, err = ListIPStatsUsers(ctx, "/v1/chat", "1.1.1.1", start, end, 1, 100)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, u2.Id, items[0].UserId)
}

func TestListIPStatsUsers_EmptyRequestPathOrIP(t *testing.T) {
	base, _, _ := ipStatsTestSeed(t)
	ctx := context.Background()
	start, end := statsWideWindow(base)

	items, total, err := ListIPStatsUsers(ctx, "", "1.1.1.1", start, end, 0, 100)
	require.NoError(t, err)
	assert.Empty(t, items)
	assert.Equal(t, int64(0), total)

	items, total, err = ListIPStatsUsers(ctx, "/v1/chat", "", start, end, 0, 100)
	require.NoError(t, err)
	assert.Empty(t, items)
	assert.Equal(t, int64(0), total)
}

func TestListIPStatsUsers_InvalidWindow(t *testing.T) {
	ipStatsTestSeed(t)
	ctx := context.Background()

	_, _, err := ListIPStatsUsers(ctx, "/v1/chat", "1.1.1.1", 0, 0, 0, 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start_timestamp must be positive")
}

func TestListIPStatsUsers_OffsetExceeded(t *testing.T) {
	base, _, _ := ipStatsTestSeed(t)
	ctx := context.Background()
	start, end := statsWideWindow(base)

	_, _, err := ListIPStatsUsers(ctx, "/v1/chat", "1.1.1.1", start, end, logStatsMaxOffset+1, 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not exceed")
}
