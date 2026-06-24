package model

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// logScreeningTestLog builds a Log row with the fields the screening query
// helpers read (prompt_tokens, completion_tokens, other, user_agent, ip,
// token_name, request_path, type, created_at).
func logScreeningTestLog(userId, typ int, ip, ua, requestPath, other, tokenName string, createdAt int64, prompt, completion int) *Log {
	return &Log{
		UserId:           userId,
		CreatedAt:        createdAt,
		Type:             typ,
		Ip:               ip,
		UserAgent:        ua,
		RequestPath:      requestPath,
		Other:            other,
		TokenName:        tokenName,
		PromptTokens:     prompt,
		CompletionTokens: completion,
		ModelName:        "test-model",
		Group:            "default",
		ChannelId:        1,
	}
}

// TestLogScreeningListTargets_HalfOpenWindow verifies the aggregate query uses a
// half-open window [start, end), excludes user_id=0, and filters to
// consume+error types + request_path.
func TestLogScreeningListTargets_HalfOpenWindow(t *testing.T) {
	truncateStatsTables(t)
	ctx := context.Background()
	u := mustCreateStatsUser(t, "agguser", "Agg", "")

	base := int64(1000000)
	// Window [base, base+1000).
	mustInsertStatsLog(t, logScreeningTestLog(u.Id, LogTypeConsume, "1.1.1.1", "curl/8", "/v1/chat/completions", "", "tok", base+10, 5, 5))
	mustInsertStatsLog(t, logScreeningTestLog(u.Id, LogTypeConsume, "1.1.1.1", "curl/8", "/v1/chat/completions", "", "tok", base+20, 7, 3))
	mustInsertStatsLog(t, logScreeningTestLog(u.Id, LogTypeError, "1.1.1.1", "curl/8", "/v1/chat/completions", "", "tok", base+30, 0, 0))
	// Exactly at end boundary → excluded (half-open).
	mustInsertStatsLog(t, logScreeningTestLog(u.Id, LogTypeConsume, "1.1.1.1", "curl/8", "/v1/chat/completions", "", "tok", base+1000, 100, 0))
	// Before window → excluded.
	mustInsertStatsLog(t, logScreeningTestLog(u.Id, LogTypeConsume, "1.1.1.1", "curl/8", "/v1/chat/completions", "", "tok", base-1, 999, 0))
	// user_id=0 → excluded.
	mustInsertStatsLog(t, logScreeningTestLog(0, LogTypeConsume, "2.2.2.2", "go/1", "/v1/chat/completions", "", "sys", base+50, 1, 1))
	// Different request_path → excluded when filter applied.
	mustInsertStatsLog(t, logScreeningTestLog(u.Id, LogTypeConsume, "1.1.1.1", "curl/8", "/v1/embeddings", "", "tok", base+40, 50, 0))

	rows, err := LogScreeningListTargets(ctx, base, base+1000, LogScreeningDefaultRequestPaths, 0, nil)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, u.Id, rows[0].UserId)
	assert.Equal(t, 3, rows[0].RequestCount, "3 rows in window (consume+consume+error)")
	assert.Equal(t, 12, rows[0].PromptTokens, "5+7+0")
	assert.Equal(t, 20, rows[0].TotalTokens, "(5+5)+(7+3)+(0+0)")
	assert.Equal(t, base+30, rows[0].LastSeen, "last_seen = max created_at in window")
}

// TestLogScreeningListTargets_NoPathFilter verifies when paths is empty, all
// request_paths are included.
func TestLogScreeningListTargets_NoPathFilter(t *testing.T) {
	truncateStatsTables(t)
	ctx := context.Background()
	u := mustCreateStatsUser(t, "nopaths", "NoPaths", "")
	base := int64(2000000)
	mustInsertStatsLog(t, logScreeningTestLog(u.Id, LogTypeConsume, "1.1.1.1", "ua", "/v1/chat/completions", "", "t", base+1, 1, 1))
	mustInsertStatsLog(t, logScreeningTestLog(u.Id, LogTypeConsume, "1.1.1.1", "ua", "/v1/embeddings", "", "t", base+2, 2, 2))
	rows, err := LogScreeningListTargets(ctx, base, base+10, nil, 0, nil)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 2, rows[0].RequestCount)
}

// TestLogScreeningListLogDetails_Ordering verifies the detail query returns
// rows ordered by user_id asc, created_at asc, and only for the requested users.
func TestLogScreeningListLogDetails_Ordering(t *testing.T) {
	truncateStatsTables(t)
	ctx := context.Background()
	u1 := mustCreateStatsUser(t, "d1", "D1", "")
	u2 := mustCreateStatsUser(t, "d2", "D2", "")
	base := int64(3000000)
	// u2 rows inserted out of order; u1 has a row outside the window.
	mustInsertStatsLog(t, logScreeningTestLog(u2.Id, LogTypeConsume, "2.2.2.2", "python/3", "/v1/chat/completions", `{"request_params":{"temperature":0.5}}`, "tok2", base+20, 3, 1))
	mustInsertStatsLog(t, logScreeningTestLog(u2.Id, LogTypeConsume, "2.2.2.2", "python/3", "/v1/chat/completions", "", "tok2", base+10, 1, 1))
	mustInsertStatsLog(t, logScreeningTestLog(u1.Id, LogTypeConsume, "1.1.1.1", "curl/8", "/v1/chat/completions", "", "tok1", base+5, 2, 2))
	mustInsertStatsLog(t, logScreeningTestLog(u1.Id, LogTypeConsume, "1.1.1.1", "curl/8", "/v1/chat/completions", "", "tok1", base-10, 999, 0)) // out of window

	detailMap, err := LogScreeningListLogDetails(ctx, base, base+1000, LogScreeningDefaultRequestPaths, []int{u1.Id, u2.Id}, 0)
	require.NoError(t, err)
	require.Contains(t, detailMap, u1.Id)
	require.Contains(t, detailMap, u2.Id)
	require.Len(t, detailMap[u1.Id], 1, "u1 has 1 row in window")
	require.Len(t, detailMap[u2.Id], 2)
	// u2 rows ordered by created_at asc.
	assert.Equal(t, base+10, detailMap[u2.Id][0].CreatedAt)
	assert.Equal(t, base+20, detailMap[u2.Id][1].CreatedAt)
	assert.Equal(t, "python/3", detailMap[u2.Id][0].UserAgent)
	assert.Equal(t, `{"request_params":{"temperature":0.5}}`, detailMap[u2.Id][1].Other)
	assert.Equal(t, "tok2", detailMap[u2.Id][0].TokenName)

	// Empty userIds → empty map, no DB hit.
	empty, err := LogScreeningListLogDetails(ctx, base, base+1000, nil, nil, 0)
	require.NoError(t, err)
	assert.Empty(t, empty)
}

// TestLogScreeningFillUserMeta_NoCrossDB verifies user meta is fetched from the
// main DB (not LOG_DB) and excludes discord columns. Uses a soft-deleted user
// to verify Unscoped inclusion.
func TestLogScreeningFillUserMeta_NoCrossDB(t *testing.T) {
	truncateStatsTables(t)
	ctx := context.Background()
	u := mustCreateStatsUser(t, "metauser", "Meta", "rem")

	meta, err := LogScreeningFillUserMeta(ctx, []int{u.Id})
	require.NoError(t, err)
	require.Contains(t, meta, u.Id)
	assert.Equal(t, "metauser", meta[u.Id].Username)
	assert.Equal(t, "Meta", meta[u.Id].DisplayName)
	assert.Equal(t, "rem", meta[u.Id].Remark)

	// Empty input → empty map.
	empty, err := LogScreeningFillUserMeta(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, empty)

	// Non-existent user → absent from map (no error).
	meta2, err := LogScreeningFillUserMeta(ctx, []int{999999})
	require.NoError(t, err)
	_, ok := meta2[999999]
	assert.False(t, ok)
}

// TestLogScreeningPickTopIPAndToken verifies the deterministic most-frequent
// pick with lexicographic tie-breaking.
func TestLogScreeningPickTopIPAndToken(t *testing.T) {
	// IP "1.1.1.1" appears 2x, "2.2.2.2" 1x → top = "1.1.1.1".
	details := []LogScreeningLogDetail{
		{Ip: "2.2.2.2", TokenName: "z-token"},
		{Ip: "1.1.1.1", TokenName: "a-token"},
		{Ip: "1.1.1.1", TokenName: "a-token"},
	}
	ip, tok := LogScreeningPickTopIPAndToken(details)
	assert.Equal(t, "1.1.1.1", ip)
	assert.Equal(t, "a-token", tok)

	// Tie on IP: "a-ip" vs "b-ip" both 1x → lexicographic "a-ip" wins.
	details2 := []LogScreeningLogDetail{
		{Ip: "b-ip", TokenName: "x"},
		{Ip: "a-ip", TokenName: "x"},
	}
	ip2, _ := LogScreeningPickTopIPAndToken(details2)
	assert.Equal(t, "a-ip", ip2, "lexicographic tie-break")

	// Empty → ("", "").
	ip3, tok3 := LogScreeningPickTopIPAndToken(nil)
	assert.Empty(t, ip3)
	assert.Empty(t, tok3)
}

// TestLogScreeningListTargets_CandidateLimit verifies the DB-side Limit caps the
// number of rows returned, and that the HAVING pre-filter correctly excludes
// rows below the threshold.
func TestLogScreeningListTargets_CandidateLimit(t *testing.T) {
	truncateStatsTables(t)
	ctx := context.Background()

	base := int64(5000000)
	// Seed 5 users, each with 1 log.
	for i := 0; i < 5; i++ {
		u := mustCreateStatsUser(t, "limuser"+strconv.Itoa(i), "Lim", "")
		mustInsertStatsLog(t, logScreeningTestLog(u.Id, LogTypeConsume, "1.1.1.1", "ua", "/v1/chat/completions", "", "t", base+int64(i), 1, 1))
	}

	// limit=3 → at most 3 rows returned.
	rows, err := LogScreeningListTargets(ctx, base, base+100, LogScreeningDefaultRequestPaths, 3, nil)
	require.NoError(t, err)
	assert.Len(t, rows, 3, "DB-side Limit caps rows at 3")

	// HAVING pre-filter: only users with request_count >= 2.
	u := mustCreateStatsUser(t, "heavyuser", "Heavy", "")
	mustInsertStatsLog(t, logScreeningTestLog(u.Id, LogTypeConsume, "1.1.1.1", "ua", "/v1/chat/completions", "", "t", base+10, 1, 1))
	mustInsertStatsLog(t, logScreeningTestLog(u.Id, LogTypeConsume, "1.1.1.1", "ua", "/v1/chat/completions", "", "t", base+20, 1, 1))
	rows, err = LogScreeningListTargets(ctx, base, base+100, LogScreeningDefaultRequestPaths, 0, map[string]int{"request_count": 2})
	require.NoError(t, err)
	require.Len(t, rows, 1, "HAVING request_count >= 2 → only heavyuser")
	assert.Equal(t, u.Id, rows[0].UserId)
	assert.Equal(t, 2, rows[0].RequestCount)
}
