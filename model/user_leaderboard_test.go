package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// leaderboardOrderClause — pure function tests
// ---------------------------------------------------------------------------

func TestLeaderboardOrderClause_Calls(t *testing.T) {
	clause := leaderboardOrderClause(LeaderboardMetricCalls)
	assert.Equal(t, "COUNT(*) DESC, user_id ASC", clause)
}

func TestLeaderboardOrderClause_Quota(t *testing.T) {
	clause := leaderboardOrderClause(LeaderboardMetricQuota)
	assert.Equal(t, "SUM(quota) DESC, user_id ASC", clause)
}

func TestLeaderboardOrderClause_RPH(t *testing.T) {
	clause := leaderboardOrderClause(LeaderboardMetricRPH)
	assert.Contains(t, clause, "COUNT(*) * 1.0 / CASE")
	assert.Contains(t, clause, "3600")
	assert.Contains(t, clause, "DESC, user_id ASC")
}

func TestLeaderboardOrderClause_Default(t *testing.T) {
	clause := leaderboardOrderClause(LeaderboardMetric("unknown"))
	assert.Equal(t, "COUNT(*) DESC, user_id ASC", clause)
}

// ---------------------------------------------------------------------------
// GetUserLeaderboard — integration tests on SQLite
// ---------------------------------------------------------------------------

// leaderboardTestSeed 插入标准排行榜测试数据并返回 base timestamp 和用户。
// 数据布局（所有日志 type=consume, request_path="/v1/chat"）：
//
//	u1(alice): 3 calls, quota=60, span=[base, base+7200] (2h)
//	u2(bob):   2 calls, quota=100, span=[base, base+60] (1m)
//	u3(carol): 1 call,  quota=10,  span=[base, base] (0 span)
//	u0(系统):  1 call,  quota=999  (user_id=0, 应被排除)
//	u1(alice): 1 error log @ base+100 (type=error, 应被排除)
func leaderboardTestSeed(t *testing.T) (base int64, u1, u2, u3 *User) {
	t.Helper()
	truncateStatsTables(t)
	base = int64(3000000)
	u1 = mustCreateStatsUser(t, "alice", "Alice", "VIP")
	u2 = mustCreateStatsUser(t, "bob", "Bob", "")
	u3 = mustCreateStatsUser(t, "carol", "Carol", "new user")

	mustInsertStatsLog(t, statsTestLog(u1.Id, LogTypeConsume, "1.1.1.1", "curl/8.0", "/v1/chat", base, 10))
	mustInsertStatsLog(t, statsTestLog(u1.Id, LogTypeConsume, "1.1.1.1", "curl/8.0", "/v1/chat", base+3600, 20))
	mustInsertStatsLog(t, statsTestLog(u1.Id, LogTypeConsume, "1.1.1.1", "curl/8.0", "/v1/chat", base+7200, 30))

	mustInsertStatsLog(t, statsTestLog(u2.Id, LogTypeConsume, "2.2.2.2", "python/3.11", "/v1/chat", base, 50))
	mustInsertStatsLog(t, statsTestLog(u2.Id, LogTypeConsume, "2.2.2.2", "python/3.11", "/v1/chat", base+60, 50))

	mustInsertStatsLog(t, statsTestLog(u3.Id, LogTypeConsume, "3.3.3.3", "go/1.22", "/v1/chat", base, 10))

	// user_id=0, should be excluded
	mustInsertStatsLog(t, statsTestLog(0, LogTypeConsume, "4.4.4.4", "java/17", "/v1/chat", base, 999))

	// Non-consume type, should be excluded from leaderboard
	mustInsertStatsLog(t, statsTestLog(u1.Id, LogTypeError, "1.1.1.1", "curl/8.0", "/v1/chat", base+100, 999))

	return
}

func TestGetUserLeaderboard_Calls(t *testing.T) {
	base, u1, u2, u3 := leaderboardTestSeed(t)
	ctx := context.Background()
	start, end := statsWideWindow(base)

	items, err := GetUserLeaderboard(ctx, LeaderboardMetricCalls, start, end, 100)
	require.NoError(t, err)
	require.Len(t, items, 3)

	// By call count desc: u1(3), u2(2), u3(1)
	assert.Equal(t, u1.Id, items[0].UserId)
	assert.Equal(t, int64(3), items[0].CallCount)
	assert.Equal(t, int64(60), items[0].QuotaSum)
	assert.Equal(t, base, items[0].FirstCall)
	assert.Equal(t, base+7200, items[0].LastCall)

	assert.Equal(t, u2.Id, items[1].UserId)
	assert.Equal(t, int64(2), items[1].CallCount)
	assert.Equal(t, int64(100), items[1].QuotaSum)

	assert.Equal(t, u3.Id, items[2].UserId)
	assert.Equal(t, int64(1), items[2].CallCount)
	assert.Equal(t, int64(10), items[2].QuotaSum)

	// Two-phase completion
	assert.Equal(t, "alice", items[0].Username)
	assert.Equal(t, "Alice", items[0].DisplayName)
	assert.Equal(t, "VIP", items[0].Remark)
	assert.Equal(t, "carol", items[2].Username)
	assert.Equal(t, "new user", items[2].Remark)
}

func TestGetUserLeaderboard_Quota(t *testing.T) {
	base, u1, u2, u3 := leaderboardTestSeed(t)
	ctx := context.Background()
	start, end := statsWideWindow(base)

	items, err := GetUserLeaderboard(ctx, LeaderboardMetricQuota, start, end, 100)
	require.NoError(t, err)
	require.Len(t, items, 3)

	// By quota desc: u2(100), u1(60), u3(10)
	assert.Equal(t, u2.Id, items[0].UserId)
	assert.Equal(t, int64(100), items[0].QuotaSum)
	assert.Equal(t, u1.Id, items[1].UserId)
	assert.Equal(t, int64(60), items[1].QuotaSum)
	assert.Equal(t, u3.Id, items[2].UserId)
	assert.Equal(t, int64(10), items[2].QuotaSum)
}

func TestGetUserLeaderboard_RPH(t *testing.T) {
	base, u1, u2, u3 := leaderboardTestSeed(t)
	ctx := context.Background()
	start, end := statsWideWindow(base)

	items, err := GetUserLeaderboard(ctx, LeaderboardMetricRPH, start, end, 100)
	require.NoError(t, err)
	require.Len(t, items, 3)

	// RPH = call_count / max(1h, span_hours)
	// u1: 3 calls, span=7200s=2h → RPH = 3/2 = 1.5
	// u2: 2 calls, span=60s < 1h → RPH = 2/1 = 2
	// u3: 1 call,  span=0s < 1h → RPH = 1/1 = 1
	// Order by RPH desc: u2(2), u1(1.5), u3(1)
	assert.Equal(t, u2.Id, items[0].UserId)
	assert.InDelta(t, 2.0, items[0].RPH, 0.001)
	assert.Equal(t, u1.Id, items[1].UserId)
	assert.InDelta(t, 1.5, items[1].RPH, 0.001)
	assert.Equal(t, u3.Id, items[2].UserId)
	assert.InDelta(t, 1.0, items[2].RPH, 0.001)
}

func TestGetUserLeaderboard_FiltersUserIdZero(t *testing.T) {
	base, _, _, _ := leaderboardTestSeed(t)
	ctx := context.Background()
	start, end := statsWideWindow(base)

	items, err := GetUserLeaderboard(ctx, LeaderboardMetricCalls, start, end, 100)
	require.NoError(t, err)
	for _, item := range items {
		assert.NotEqual(t, 0, item.UserId, "user_id=0 must be excluded")
	}
	assert.Len(t, items, 3)
}

func TestGetUserLeaderboard_TimeRange(t *testing.T) {
	base, _, _, _ := leaderboardTestSeed(t)
	ctx := context.Background()

	// Half-open [base+3500, base+7300): u1@3600, u1@7200; u2/u3 excluded
	items, err := GetUserLeaderboard(ctx, LeaderboardMetricCalls, base+3500, base+7300, 100)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, int64(2), items[0].CallCount, "u1 has 2 calls in range")
}

func TestGetUserLeaderboard_LimitClamped(t *testing.T) {
	base, _, _, _ := leaderboardTestSeed(t)
	ctx := context.Background()
	start, end := statsWideWindow(base)

	items, err := GetUserLeaderboard(ctx, LeaderboardMetricCalls, start, end, 2)
	require.NoError(t, err)
	require.Len(t, items, 2)

	items, err = GetUserLeaderboard(ctx, LeaderboardMetricCalls, start, end, 0)
	require.NoError(t, err)
	assert.Len(t, items, 3)

	items, err = GetUserLeaderboard(ctx, LeaderboardMetricCalls, start, end, -1)
	require.NoError(t, err)
	assert.Len(t, items, 3)

	items, err = GetUserLeaderboard(ctx, LeaderboardMetricCalls, start, end, 99999)
	require.NoError(t, err)
	assert.Len(t, items, 3)
}

func TestGetUserLeaderboard_EmptyResult(t *testing.T) {
	truncateStatsTables(t)
	ctx := context.Background()

	items, err := GetUserLeaderboard(ctx, LeaderboardMetricCalls, 1000, 2000, 100)
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestGetUserLeaderboard_InvalidWindow(t *testing.T) {
	leaderboardTestSeed(t)
	ctx := context.Background()

	_, err := GetUserLeaderboard(ctx, LeaderboardMetricCalls, 0, 0, 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start_timestamp must be positive")

	_, err = GetUserLeaderboard(ctx, LeaderboardMetricCalls, 1000, 500, 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be greater than")
}

// ---------------------------------------------------------------------------
// GetUserCoverageLeaderboard — integration tests on SQLite (half-open interval)
// ---------------------------------------------------------------------------

func TestGetUserCoverageLeaderboard_Basic(t *testing.T) {
	base, u1, _, _ := leaderboardTestSeed(t)
	ctx := context.Background()

	// slotMinutes=5 → slotSeconds=300
	// Half-open [base, base+7200): totalSlots = ceil(7200/300) = 24
	// u1: logs at base, base+3600, base+7200 → but base+7200 is excluded (half-open)!
	//   → buckets: (base-base)/300=0, (base+3600-base)/300=12 → 2 active slots
	// u2: logs at base, base+60 → same bucket 0 → 1 active slot
	// u3: log at base → bucket 0 → 1 active slot
	items, err := GetUserCoverageLeaderboard(ctx, base, base+7200, 5, 100)
	require.NoError(t, err)
	require.Len(t, items, 3)

	// u1 has most coverage (2 slots)
	assert.Equal(t, u1.Id, items[0].UserId)
	assert.Equal(t, int64(2), items[0].ActiveSlots)
	assert.Equal(t, int64(24), items[0].TotalSlots)
	assert.InDelta(t, 8.33, items[0].CoveragePct, 0.01) // 2/24*100 ≈ 8.33

	// u2 and u3 each have 1 slot → 1/24*100 ≈ 4.17
	assert.Equal(t, int64(1), items[1].ActiveSlots)
	assert.InDelta(t, 4.17, items[1].CoveragePct, 0.01)

	// Two-phase completion
	assert.Equal(t, "alice", items[0].Username)
	assert.Equal(t, "Alice", items[0].DisplayName)
	assert.Equal(t, "VIP", items[0].Remark)
}

func TestGetUserCoverageLeaderboard_DefaultSlotMinutes(t *testing.T) {
	base, u1, _, _ := leaderboardTestSeed(t)
	ctx := context.Background()

	// slotMinutes=0 → default 5 minutes
	items, err := GetUserCoverageLeaderboard(ctx, base, base+7200, 0, 100)
	require.NoError(t, err)
	require.NotEmpty(t, items)
	assert.Equal(t, u1.Id, items[0].UserId)
	assert.Equal(t, int64(2), items[0].ActiveSlots)
	assert.Equal(t, int64(24), items[0].TotalSlots)
}

func TestGetUserCoverageLeaderboard_FiltersUserIdZero(t *testing.T) {
	base, _, _, _ := leaderboardTestSeed(t)
	ctx := context.Background()

	items, err := GetUserCoverageLeaderboard(ctx, base, base+7200, 5, 100)
	require.NoError(t, err)
	for _, item := range items {
		assert.NotEqual(t, 0, item.UserId, "user_id=0 must be excluded")
	}
}

func TestGetUserCoverageLeaderboard_LimitClamped(t *testing.T) {
	base, _, _, _ := leaderboardTestSeed(t)
	ctx := context.Background()

	items, err := GetUserCoverageLeaderboard(ctx, base, base+7200, 5, 2)
	require.NoError(t, err)
	assert.Len(t, items, 2)

	items, err = GetUserCoverageLeaderboard(ctx, base, base+7200, 5, 0)
	require.NoError(t, err)
	assert.Len(t, items, 3)

	items, err = GetUserCoverageLeaderboard(ctx, base, base+7200, 5, 99999)
	require.NoError(t, err)
	assert.Len(t, items, 3)
}

func TestGetUserCoverageLeaderboard_InvalidWindow(t *testing.T) {
	leaderboardTestSeed(t)
	ctx := context.Background()

	_, err := GetUserCoverageLeaderboard(ctx, 0, 0, 5, 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start_timestamp must be positive")

	_, err = GetUserCoverageLeaderboard(ctx, 1000, 1000, 5, 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be greater than")
}

// TestGetUserCoverageLeaderboard_NonAlignedWindow verifies coverage math when the
// time range is NOT an exact multiple of slotSeconds. This replaces the old
// CoveragePctCappedAt100 test which masked a bug (activeSlots > totalSlots)
// caused by absolute bucket expressions and closed intervals.
func TestGetUserCoverageLeaderboard_NonAlignedWindow(t *testing.T) {
	truncateStatsTables(t)
	ctx := context.Background()

	u := mustCreateStatsUser(t, "nonalign", "Non Align", "")
	base := int64(4000000)

	// Range [base, base+200), slotSeconds=60 → totalSlots = ceil(200/60) = 4
	// Logs at base (bucket 0), base+60 (bucket 1), base+120 (bucket 2), base+180 (bucket 3)
	// base+200 is excluded by half-open → 4 active slots, coverage = 4/4 = 100%
	mustInsertStatsLog(t, statsTestLog(u.Id, LogTypeConsume, "1.1.1.1", "curl/8.0", "/v1/chat", base, 1))
	mustInsertStatsLog(t, statsTestLog(u.Id, LogTypeConsume, "1.1.1.1", "curl/8.0", "/v1/chat", base+60, 1))
	mustInsertStatsLog(t, statsTestLog(u.Id, LogTypeConsume, "1.1.1.1", "curl/8.0", "/v1/chat", base+120, 1))
	mustInsertStatsLog(t, statsTestLog(u.Id, LogTypeConsume, "1.1.1.1", "curl/8.0", "/v1/chat", base+180, 1))

	items, err := GetUserCoverageLeaderboard(ctx, base, base+200, 1, 100)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, u.Id, items[0].UserId)
	assert.Equal(t, int64(4), items[0].ActiveSlots, "4 distinct buckets in [base, base+200)")
	assert.Equal(t, int64(4), items[0].TotalSlots, "ceil(200/60)=4")
	assert.InDelta(t, 100.0, items[0].CoveragePct, 0.01)

	// Now test partial coverage: only 2 of 4 slots active
	truncateStatsTables(t)
	u2 := mustCreateStatsUser(t, "partial", "Partial", "")
	mustInsertStatsLog(t, statsTestLog(u2.Id, LogTypeConsume, "1.1.1.1", "curl/8.0", "/v1/chat", base, 1))
	mustInsertStatsLog(t, statsTestLog(u2.Id, LogTypeConsume, "1.1.1.1", "curl/8.0", "/v1/chat", base+120, 1))

	items, err = GetUserCoverageLeaderboard(ctx, base, base+200, 1, 100)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, u2.Id, items[0].UserId)
	assert.Equal(t, int64(2), items[0].ActiveSlots, "2 distinct buckets: 0 and 2")
	assert.Equal(t, int64(4), items[0].TotalSlots)
	assert.InDelta(t, 50.0, items[0].CoveragePct, 0.01) // 2/4*100 = 50%
}

// TestGetUserCoverageLeaderboard_BoundaryExcluded verifies that a log exactly at
// endTimestamp is excluded by the half-open interval, preventing activeSlots
// from exceeding totalSlots.
func TestGetUserCoverageLeaderboard_BoundaryExcluded(t *testing.T) {
	truncateStatsTables(t)
	ctx := context.Background()

	u := mustCreateStatsUser(t, "boundary", "Boundary", "")
	base := int64(5000000)

	// Range [base, base+120), slotSeconds=60 → totalSlots = ceil(120/60) = 2
	// Log at base (bucket 0), base+60 (bucket 1), base+120 (bucket 2, but EXCLUDED by half-open)
	mustInsertStatsLog(t, statsTestLog(u.Id, LogTypeConsume, "1.1.1.1", "curl/8.0", "/v1/chat", base, 1))
	mustInsertStatsLog(t, statsTestLog(u.Id, LogTypeConsume, "1.1.1.1", "curl/8.0", "/v1/chat", base+60, 1))
	mustInsertStatsLog(t, statsTestLog(u.Id, LogTypeConsume, "1.1.1.1", "curl/8.0", "/v1/chat", base+120, 1))

	items, err := GetUserCoverageLeaderboard(ctx, base, base+120, 1, 100)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, int64(2), items[0].ActiveSlots, "log at base+120 excluded by half-open; only 2 buckets")
	assert.Equal(t, int64(2), items[0].TotalSlots)
	assert.InDelta(t, 100.0, items[0].CoveragePct, 0.01)
	assert.LessOrEqual(t, items[0].CoveragePct, 100.0, "coverage must never exceed 100% with correct half-open math")
}
