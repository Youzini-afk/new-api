package model

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// withLogDBType 临时切换 common.LogDatabaseType，在 t.Cleanup 中恢复原值。
// 仅影响依赖 common.LogDatabaseType() 的纯函数（如 userAgentContainsClause、
// logStatsCoverageBucketExpr），不调用 initCol()，因此 logGroupCol 不变。
func withLogDBType(t *testing.T, dbType common.DatabaseType, fn func()) {
	t.Helper()
	original := common.LogDatabaseType()
	common.SetLogDatabaseType(dbType)
	t.Cleanup(func() {
		common.SetLogDatabaseType(original)
	})
	fn()
}

// truncateStatsTables 清空 logs 和 users 表，供统计测试使用。
// 收尾清理不使用 require（避免在 Cleanup 中终止测试）。
func truncateStatsTables(t *testing.T) {
	t.Helper()
	require.NoError(t, LOG_DB.Exec("DELETE FROM logs").Error)
	require.NoError(t, DB.Unscoped().Exec("DELETE FROM users").Error)
	t.Cleanup(func() {
		LOG_DB.Exec("DELETE FROM logs")
		DB.Unscoped().Exec("DELETE FROM users")
	})
}

// mustCreateStatsUser 创建一个测试用户并返回（含自动分配的 Id）。
// aff_code 必须唯一（uniqueIndex），使用 username 派生以保证不冲突。
func mustCreateStatsUser(t *testing.T, username, displayName, remark string) *User {
	t.Helper()
	u := &User{
		Username:    username,
		Password:    "testpass123",
		DisplayName: displayName,
		Remark:      remark,
		Group:       "default",
		AffCode:     username + "-aff",
	}
	require.NoError(t, DB.Create(u).Error)
	return u
}

// statsTestLog 构造一条测试日志（不写入 DB）。
func statsTestLog(userId, typ int, ip, ua, requestPath string, createdAt int64, quota int) *Log {
	return &Log{
		UserId:      userId,
		CreatedAt:   createdAt,
		Type:        typ,
		Ip:          ip,
		UserAgent:   ua,
		RequestPath: requestPath,
		Quota:       quota,
		ModelName:   "test-model",
		TokenName:   "test-token",
		Group:       "default",
		ChannelId:   1,
	}
}

// mustInsertStatsLog 写入一条测试日志。
func mustInsertStatsLog(t *testing.T, log *Log) {
	t.Helper()
	require.NoError(t, LOG_DB.Create(log).Error)
}

// statsWideWindow 返回一个覆盖所有 seed 数据的宽窗口 [base-1, base+100000]。
// 100001 秒 < 31 天 (2678400s)，通过窗口校验。base >= 1000000 保证 base-1 > 0。
func statsWideWindow(base int64) (int64, int64) {
	return base - 1, base + 100000
}

// ---------------------------------------------------------------------------
// clamp helpers — pure unit tests
// ---------------------------------------------------------------------------

func TestClampRankLimit(t *testing.T) {
	assert.Equal(t, logStatsDefaultRankLimit, clampRankLimit(0))
	assert.Equal(t, logStatsDefaultRankLimit, clampRankLimit(-1))
	assert.Equal(t, 50, clampRankLimit(50))
	assert.Equal(t, logStatsMaxRankLimit, clampRankLimit(logStatsMaxRankLimit))
	assert.Equal(t, logStatsMaxRankLimit, clampRankLimit(logStatsMaxRankLimit+1))
}

func TestClampPageSize(t *testing.T) {
	assert.Equal(t, logStatsDefaultPageSize, clampPageSize(0))
	assert.Equal(t, logStatsDefaultPageSize, clampPageSize(-1))
	assert.Equal(t, 20, clampPageSize(20))
	assert.Equal(t, logStatsMaxPageSize, clampPageSize(logStatsMaxPageSize))
	assert.Equal(t, logStatsMaxPageSize, clampPageSize(logStatsMaxPageSize+1))
}

// ---------------------------------------------------------------------------
// validateLogStatsWindow — pure unit tests
// ---------------------------------------------------------------------------

func TestValidateLogStatsWindow_Valid(t *testing.T) {
	assert.NoError(t, validateLogStatsWindow(1000, 2000))
	assert.NoError(t, validateLogStatsWindow(1, 2))
	// Max allowed window: exactly 31 days
	assert.NoError(t, validateLogStatsWindow(1, 1+int64(logStatsMaxWindowSeconds)))
}

func TestValidateLogStatsWindow_StartNotPositive(t *testing.T) {
	err := validateLogStatsWindow(0, 1000)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start_timestamp must be positive")

	err = validateLogStatsWindow(-1, 1000)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start_timestamp must be positive")
}

func TestValidateLogStatsWindow_EndNotPositive(t *testing.T) {
	err := validateLogStatsWindow(1000, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "end_timestamp must be positive")
}

func TestValidateLogStatsWindow_EndNotGreaterThanStart(t *testing.T) {
	err := validateLogStatsWindow(1000, 1000)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be greater than")

	err = validateLogStatsWindow(2000, 1000)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be greater than")
}

func TestValidateLogStatsWindow_WindowTooLarge(t *testing.T) {
	// 31 days + 1 second
	err := validateLogStatsWindow(1, 1+int64(logStatsMaxWindowSeconds)+1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not exceed")
}

// ---------------------------------------------------------------------------
// validateLogStatsOffset — pure unit tests
// ---------------------------------------------------------------------------

func TestValidateLogStatsOffset_Valid(t *testing.T) {
	assert.NoError(t, validateLogStatsOffset(0))
	assert.NoError(t, validateLogStatsOffset(1))
	assert.NoError(t, validateLogStatsOffset(logStatsMaxOffset))
}

func TestValidateLogStatsOffset_Negative(t *testing.T) {
	err := validateLogStatsOffset(-1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-negative")
}

func TestValidateLogStatsOffset_ExceedsMax(t *testing.T) {
	err := validateLogStatsOffset(logStatsMaxOffset + 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not exceed")
}

// ---------------------------------------------------------------------------
// validateUAContainsKeyword — pure unit tests
// ---------------------------------------------------------------------------

func TestValidateUAContainsKeyword_Empty(t *testing.T) {
	k, err := validateUAContainsKeyword("")
	require.NoError(t, err)
	assert.Empty(t, k)

	k, err = validateUAContainsKeyword("   ")
	require.NoError(t, err)
	assert.Empty(t, k, "whitespace-only should be treated as empty")
}

func TestValidateUAContainsKeyword_TooShort(t *testing.T) {
	_, err := validateUAContainsKeyword("a")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least 2 characters")
}

func TestValidateUAContainsKeyword_Valid(t *testing.T) {
	k, err := validateUAContainsKeyword("ab")
	require.NoError(t, err)
	assert.Equal(t, "ab", k)

	k, err = validateUAContainsKeyword("  python  ")
	require.NoError(t, err)
	assert.Equal(t, "python", k, "should be trimmed")
}

// ---------------------------------------------------------------------------
// escapeLikeLiteral — pure unit tests
// ---------------------------------------------------------------------------

func TestEscapeLikeLiteral_NoWildcards(t *testing.T) {
	assert.Equal(t, "Mozilla", escapeLikeLiteral("Mozilla"))
	assert.Equal(t, "curl/8.0", escapeLikeLiteral("curl/8.0"))
}

func TestEscapeLikeLiteral_PercentEscaped(t *testing.T) {
	assert.Equal(t, `!%`, escapeLikeLiteral("%"))
	assert.Equal(t, `a!%b`, escapeLikeLiteral("a%b"))
}

func TestEscapeLikeLiteral_UnderscoreEscaped(t *testing.T) {
	assert.Equal(t, `!_`, escapeLikeLiteral("_"))
	assert.Equal(t, `a!_b`, escapeLikeLiteral("a_b"))
}

func TestEscapeLikeLiteral_EscapeCharEscaped(t *testing.T) {
	assert.Equal(t, `!!`, escapeLikeLiteral("!"))
	assert.Equal(t, `a!!b`, escapeLikeLiteral("a!b"))
}

func TestEscapeLikeLiteral_AllWildcards(t *testing.T) {
	assert.Equal(t, `!%!_!!`, escapeLikeLiteral(`%_!`))
	assert.Equal(t, `a\b`, escapeLikeLiteral(`a\b`), "backslash is ordinary when ! is the escape char")
}

// ---------------------------------------------------------------------------
// userAgentContainsClause — dialect branch tests (pure, no DB)
// ---------------------------------------------------------------------------

func TestUserAgentContainsClause_EmptyKeyword(t *testing.T) {
	sql, args := userAgentContainsClause("user_agent", "")
	assert.Empty(t, sql)
	assert.Nil(t, args)

	sql, args = userAgentContainsClause("user_agent", "   ")
	assert.Empty(t, sql)
	assert.Nil(t, args)
}

func TestUserAgentContainsClause_SQLite(t *testing.T) {
	withLogDBType(t, common.DatabaseTypeSQLite, func() {
		sql, args := userAgentContainsClause("user_agent", "Mozilla")
		assert.Equal(t, "user_agent LIKE ? ESCAPE '!'", sql)
		assert.Equal(t, []interface{}{"%Mozilla%"}, args)
	})
}

func TestUserAgentContainsClause_PostgreSQL(t *testing.T) {
	withLogDBType(t, common.DatabaseTypePostgreSQL, func() {
		sql, args := userAgentContainsClause("user_agent", "curl")
		assert.Equal(t, "user_agent ILIKE ? ESCAPE '!'", sql)
		assert.Equal(t, []interface{}{"%curl%"}, args)
	})
}

func TestUserAgentContainsClause_ClickHouse(t *testing.T) {
	withLogDBType(t, common.DatabaseTypeClickHouse, func() {
		sql, args := userAgentContainsClause("user_agent", "python")
		assert.Equal(t, "lower(user_agent) LIKE lower(?) ESCAPE '!'", sql)
		assert.Equal(t, []interface{}{"%python%"}, args)
	})
}

func TestUserAgentContainsClause_CustomColumn(t *testing.T) {
	withLogDBType(t, common.DatabaseTypePostgreSQL, func() {
		sql, args := userAgentContainsClause("logs.user_agent", "foo")
		assert.Equal(t, "logs.user_agent ILIKE ? ESCAPE '!'", sql)
		assert.Equal(t, []interface{}{"%foo%"}, args)
	})
}

func TestUserAgentContainsClause_WildcardsEscaped(t *testing.T) {
	withLogDBType(t, common.DatabaseTypeSQLite, func() {
		// % in keyword must be escaped to !% (literal percent, not wildcard)
		sql, args := userAgentContainsClause("user_agent", "50%off")
		assert.Equal(t, "user_agent LIKE ? ESCAPE '!'", sql)
		assert.Equal(t, []interface{}{`%50!%off%`}, args)

		// _ in keyword must be escaped to !_ (literal underscore)
		sql, args = userAgentContainsClause("user_agent", "my_ua")
		assert.Equal(t, []interface{}{`%my!_ua%`}, args)

		// ! in keyword must be escaped to !!; backslash is ordinary.
		sql, args = userAgentContainsClause("user_agent", `a!b\c`)
		assert.Equal(t, []interface{}{`%a!!b\c%`}, args)
	})
}

// ---------------------------------------------------------------------------
// logStatsCoverageBucketExpr — dialect branch tests (pure, no DB)
// ---------------------------------------------------------------------------

func TestLogStatsCoverageBucketExpr_SQLite(t *testing.T) {
	withLogDBType(t, common.DatabaseTypeSQLite, func() {
		assert.Equal(t, "(created_at - 1000000) / 300", logStatsCoverageBucketExpr(1000000, 300))
		assert.Equal(t, "(created_at - 0) / 60", logStatsCoverageBucketExpr(0, 60))
	})
}

func TestLogStatsCoverageBucketExpr_ClickHouse(t *testing.T) {
	withLogDBType(t, common.DatabaseTypeClickHouse, func() {
		assert.Equal(t, "intDiv(created_at - 1000000, 300)", logStatsCoverageBucketExpr(1000000, 300))
		assert.Equal(t, "intDiv(created_at - 0, 600)", logStatsCoverageBucketExpr(0, 600))
	})
}

func TestLogStatsCoverageBucketExpr_Default(t *testing.T) {
	withLogDBType(t, common.DatabaseTypeMySQL, func() {
		assert.Equal(t, "FLOOR((created_at - 1000000) / 300)", logStatsCoverageBucketExpr(1000000, 300))
	})
	withLogDBType(t, common.DatabaseTypePostgreSQL, func() {
		assert.Equal(t, "FLOOR((created_at - 5000) / 300)", logStatsCoverageBucketExpr(5000, 300))
	})
}

func TestLogStatsCoverageBucketExpr_NonPositiveFallback(t *testing.T) {
	// 防御性回退：非正 slotSeconds 回退到 60，负 startTimestamp 回退到 0。
	withLogDBType(t, common.DatabaseTypeSQLite, func() {
		assert.Equal(t, "(created_at - 0) / 60", logStatsCoverageBucketExpr(0, 0))
		assert.Equal(t, "(created_at - 0) / 60", logStatsCoverageBucketExpr(-1, -1))
	})
}

// ---------------------------------------------------------------------------
// fetchLogUserDisplayMap — two-phase user lookup (integration)
// ---------------------------------------------------------------------------

func TestFetchLogUserDisplayMap_EmptyInput(t *testing.T) {
	m, err := fetchLogUserDisplayMap(context.Background(), nil)
	require.NoError(t, err)
	assert.Empty(t, m)

	m2, err := fetchLogUserDisplayMap(context.Background(), []int{})
	require.NoError(t, err)
	assert.Empty(t, m2)
}

func TestFetchLogUserDisplayMap_Basic(t *testing.T) {
	truncateStatsTables(t)
	u1 := mustCreateStatsUser(t, "alice", "Alice", "VIP customer")
	u2 := mustCreateStatsUser(t, "bob", "Bob", "")

	m, err := fetchLogUserDisplayMap(context.Background(), []int{u1.Id, u2.Id, 99999})
	require.NoError(t, err)
	assert.Len(t, m, 2, "should find 2 existing users, ignore non-existent")
	assert.Equal(t, "alice", m[u1.Id].Username)
	assert.Equal(t, "Alice", m[u1.Id].DisplayName)
	assert.Equal(t, "VIP customer", m[u1.Id].Remark)
	assert.Equal(t, "bob", m[u2.Id].Username)
	assert.Equal(t, "Bob", m[u2.Id].DisplayName)
	assert.Empty(t, m[u2.Id].Remark)
}

func TestFetchLogUserDisplayMap_IncludesSoftDeletedUser(t *testing.T) {
	truncateStatsTables(t)
	u := mustCreateStatsUser(t, "ghost", "Ghost User", "deleted")
	require.NoError(t, DB.Delete(u).Error)

	m, err := fetchLogUserDisplayMap(context.Background(), []int{u.Id})
	require.NoError(t, err)
	assert.Len(t, m, 1, "soft-deleted user must still be found via Unscoped")
	assert.Equal(t, "ghost", m[u.Id].Username)
	assert.Equal(t, "Ghost User", m[u.Id].DisplayName)
}

// TestFetchLogUserDisplayMap_DBError verifies that DB query failures propagate as errors.
// Swaps DB to an empty in-memory database (no users table) to force a query error.
func TestFetchLogUserDisplayMap_DBError(t *testing.T) {
	truncateStatsTables(t)
	u := mustCreateStatsUser(t, "erruser", "Err User", "")

	origDB := DB
	emptyDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB = emptyDB
	t.Cleanup(func() {
		DB = origDB
	})

	_, err = fetchLogUserDisplayMap(context.Background(), []int{u.Id})
	require.Error(t, err, "DB query failure must propagate as error")
}

// ---------------------------------------------------------------------------
// applyLogStatsFilter — integration test on SQLite (half-open interval)
// ---------------------------------------------------------------------------

func TestApplyLogStatsFilter_AllFields(t *testing.T) {
	truncateStatsTables(t)
	ctx := context.Background()

	u1 := mustCreateStatsUser(t, "alice", "Alice", "")
	u2 := mustCreateStatsUser(t, "bob", "Bob", "")

	base := int64(1000000)
	mustInsertStatsLog(t, statsTestLog(u1.Id, LogTypeConsume, "1.1.1.1", "curl/8.0", "/v1/chat", base, 10))
	mustInsertStatsLog(t, statsTestLog(u1.Id, LogTypeConsume, "1.1.1.1", "curl/8.0", "/v1/chat", base+60, 20))
	mustInsertStatsLog(t, statsTestLog(u2.Id, LogTypeConsume, "2.2.2.2", "python/3.11", "/v1/chat", base+120, 5))
	mustInsertStatsLog(t, statsTestLog(u2.Id, LogTypeError, "2.2.2.2", "python/3.11", "/v1/images", base+180, 15))
	mustInsertStatsLog(t, statsTestLog(u1.Id, LogTypeConsume, "1.1.1.1", "curl/8.0", "/v1/chat", base+240, 30))

	// Test: filter by type=consume, user_id=u1, model_name, half-open [base, base+61)
	// base+60 is included (base+60 < base+61), base is included.
	f := LogStatsFilter{
		Types:          []int{LogTypeConsume},
		UserId:         u1.Id,
		ModelName:      "test-model",
		StartTimestamp: base,
		EndTimestamp:   base + 61,
	}
	var logs []Log
	require.NoError(t, newLogStatsQuery(ctx, f).Find(&logs).Error)
	assert.Len(t, logs, 2, "u1 consume logs in [base, base+61)")

	// Test: half-open boundary — EndTimestamp=base+60 excludes log at base+60
	f = LogStatsFilter{
		Types:          []int{LogTypeConsume},
		UserId:         u1.Id,
		StartTimestamp: base,
		EndTimestamp:   base + 60,
	}
	logs = nil
	require.NoError(t, newLogStatsQuery(ctx, f).Find(&logs).Error)
	assert.Len(t, logs, 1, "half-open [base, base+60) excludes log at base+60")

	// Test: filter by request_path IN list (no time range → no time filter)
	f = LogStatsFilter{
		Types:        []int{LogTypeConsume, LogTypeError},
		RequestPaths: []string{"/v1/images"},
	}
	logs = nil
	require.NoError(t, newLogStatsQuery(ctx, f).Find(&logs).Error)
	assert.Len(t, logs, 1)
	assert.Equal(t, "/v1/images", logs[0].RequestPath)

	// Test: filter by IP
	f = LogStatsFilter{
		Types: []int{LogTypeConsume, LogTypeError},
		IP:    "2.2.2.2",
	}
	logs = nil
	require.NoError(t, newLogStatsQuery(ctx, f).Find(&logs).Error)
	assert.Len(t, logs, 2)

	// Test: filter by user_ids
	f = LogStatsFilter{
		Types:   []int{LogTypeConsume, LogTypeError},
		UserIds: []int{u1.Id, u2.Id},
	}
	logs = nil
	require.NoError(t, newLogStatsQuery(ctx, f).Find(&logs).Error)
	assert.Len(t, logs, 5, "all 5 logs belong to u1 or u2")
}
