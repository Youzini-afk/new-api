package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// truncateServiceScreeningTables wipes the Phase 5 tables (plus users) for a
// clean service-level fixture. Reused across the log_screening service tests.
func truncateServiceScreeningTables(t *testing.T) {
	t.Helper()
	require.NoError(t, model.DB.Exec("DELETE FROM log_screening_records").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM prompt_block_logs").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM ua_block_logs").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM suspicious_ip_marks").Error)
	require.NoError(t, model.DB.Unscoped().Exec("DELETE FROM users").Error)
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM log_screening_records")
		model.DB.Exec("DELETE FROM prompt_block_logs")
		model.DB.Exec("DELETE FROM ua_block_logs")
		model.DB.Exec("DELETE FROM suspicious_ip_marks")
		model.DB.Unscoped().Exec("DELETE FROM users")
	})
}

// seedScreeningUser creates a user and returns it for service tests.
func seedScreeningUser(t *testing.T, username, displayName, remark string) *model.User {
	t.Helper()
	u := &model.User{
		Username:    username,
		Password:    "testpass123",
		DisplayName: displayName,
		Remark:      remark,
		Group:       "default",
		AffCode:     username + "-aff",
	}
	require.NoError(t, model.DB.Create(u).Error)
	return u
}

// seedScreeningRecord inserts a LogScreeningRecord and returns it.
func seedScreeningRecord(t *testing.T, userId int, ruleName, window, requestPath string, expiresAt int64) *model.LogScreeningRecord {
	t.Helper()
	r := &model.LogScreeningRecord{
		UserId:       userId,
		RuleName:     ruleName,
		Window:       window,
		RequestPath:  requestPath,
		RequestCount: 10,
		RPM:          5,
		ParamHits:    "top_p,temperature",
		UAHits:        "curl/8.0",
		ExpiresAt:    expiresAt,
	}
	created, err := model.UpsertLogScreeningRecord(context.Background(), r)
	require.NoError(t, err)
	require.True(t, created)
	// Refresh from DB so Id/timestamps are populated.
	require.NoError(t, model.DB.First(r, r.Id).Error)
	return r
}

// TestRunLogScreening_DisabledAndUnsupportedKind verifies disabled and invalid
// scope behavior for the real screening runner.
func TestRunLogScreening_DisabledAndUnsupportedKind(t *testing.T) {
	truncateServiceScreeningTables(t)
	ctx := context.Background()

	// Snapshot and restore the log_screening setting so the test is isolated.
	original := *system_setting.GetLogScreeningSetting()
	t.Cleanup(func() { *system_setting.GetLogScreeningSetting() = original })

	// Disabled setting: returns immediately with status "disabled".
	setting := system_setting.GetLogScreeningSetting()
	setting.Enabled = false
	setting.ExpireDays = 7
	summary, err := RunLogScreening(ctx, 1, "root", "chat_completions", true)
	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.Equal(t, "disabled", summary.Status)
	assert.False(t, summary.Enabled)
	assert.Equal(t, 0, summary.RulesChecked)
	assert.Equal(t, int64(0), summary.RecordsCreated)
	assert.NotZero(t, summary.StartedAt)
	assert.GreaterOrEqual(t, summary.FinishedAt, summary.StartedAt)
	assert.True(t, summary.Manual)

	// Unsupported kind returns an error with status "error".
	setting.Enabled = true
	summary, err = RunLogScreening(ctx, 1, "root", "embeddings", true)
	require.Error(t, err)
	require.NotNil(t, summary)
	assert.Equal(t, "error", summary.Status)
	assert.Contains(t, err.Error(), "unsupported screening kind")

	// The getter normalizes an invalid ExpireDays back to a safe default, so the
	// run never sees expire_days <= 0 through the public path.
	setting.ExpireDays = 0
	require.Equal(t, 7, system_setting.GetLogScreeningSetting().ExpireDays)
}

// TestListLogScreeningRecords_Filters verifies list filtering (by user, rule,
// window, expired) and user/suspicious-IP enrichment.
func TestListLogScreeningRecords_Filters(t *testing.T) {
	truncateServiceScreeningTables(t)
	ctx := context.Background()
	u := seedScreeningUser(t, "alice", "Alice", "VIP")

	now := common.GetTimestamp()
	r1 := seedScreeningRecord(t, u.Id, "high_freq_1h", "1h", "all", now+3600)         // not expired
	r2 := seedScreeningRecord(t, u.Id, "high_freq_24h", "24h", "all", now-3600)        // expired
	_ = r2

	// Attach a suspicious IP mark for the user so enrichment is exercised.
	_, err := model.UpsertSuspiciousIPMark(ctx, &model.SuspiciousIPMark{
		UserId: u.Id,
		Username: u.Username,
		Ip:     "9.9.9.9",
		Source: "log_screening",
	})
	require.NoError(t, err)

	// List all (no filter): both records, ordered matched_at desc.
	items, total, err := ListLogScreeningRecords(ctx, LogScreeningListFilter{}, 0, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	require.Len(t, items, 2)
	// Enrichment: display_name/remark + suspicious IPs present.
	assert.Equal(t, "Alice", items[0].DisplayName)
	assert.Equal(t, "VIP", items[0].Remark)
	require.NotEmpty(t, items[0].SuspiciousIPs)
	assert.Equal(t, "9.9.9.9", items[0].SuspiciousIPs[0].IP)
	// ParamHits/UAHits split into slices.
	assert.Equal(t, []string{"top_p", "temperature"}, items[0].ParamHits)
	assert.Equal(t, []string{"curl/8.0"}, items[0].UAHits)

	// Filter by rule name.
	items, total, err = ListLogScreeningRecords(ctx, LogScreeningListFilter{RuleName: "high_freq_1h"}, 0, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	assert.Equal(t, r1.Id, items[0].Id)

	// Filter by window (dialect-safe window_label column).
	items, total, err = ListLogScreeningRecords(ctx, LogScreeningListFilter{Window: "24h"}, 0, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	assert.Equal(t, "high_freq_24h", items[0].RuleName)

	// Filter by expired=true -> only the expired record.
	expired := true
	items, total, err = ListLogScreeningRecords(ctx, LogScreeningListFilter{Expired: &expired}, 0, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	assert.Equal(t, "high_freq_24h", items[0].RuleName)

	// Filter by user_id.
	items, total, err = ListLogScreeningRecords(ctx, LogScreeningListFilter{UserId: u.Id}, 0, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)

	// Non-matching user_id returns empty.
	items, total, err = ListLogScreeningRecords(ctx, LogScreeningListFilter{UserId: 99999}, 0, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(0), total)
	assert.Empty(t, items)
}

// TestAppendLogScreeningRemark_PersistsToUserRemark verifies the remark helper
// appends to the target user's remark (least-invasive gy behavior) and returns
// sensible errors for bad input.
func TestAppendLogScreeningRemark_PersistsToUserRemark(t *testing.T) {
	truncateServiceScreeningTables(t)
	ctx := context.Background()
	u := seedScreeningUser(t, "bob", "Bob", "")
	r := seedScreeningRecord(t, u.Id, "rule_x", "1h", "all", 0)

	require.NoError(t, AppendLogScreeningRemark(ctx, r.Id, 0, "admin-alice", "first note"))
	var refreshed model.User
	require.NoError(t, model.DB.First(&refreshed, u.Id).Error)
	assert.Equal(t, "first note(admin-alice)", refreshed.Remark)

	// Second append extends the remark on a new line.
	require.NoError(t, AppendLogScreeningRemark(ctx, r.Id, 0, "admin-alice", "second note"))
	require.NoError(t, model.DB.First(&refreshed, u.Id).Error)
	assert.Contains(t, refreshed.Remark, "first note(admin-alice)")
	assert.Contains(t, refreshed.Remark, "second note(admin-alice)")

	// Error: empty remark.
	err := AppendLogScreeningRemark(ctx, r.Id, 0, "admin-alice", "   ")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "remark is empty")

	// Error: invalid record id.
	err = AppendLogScreeningRemark(ctx, 0, 0, "admin-alice", "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid record id")

	// Error: record not found.
	err = AppendLogScreeningRemark(ctx, 999999, 0, "admin-alice", "x")
	require.Error(t, err)
}

// TestDeleteExpiredLogScreeningRecords_ServiceDelegates verifies the service
// helper delegates to the model layer and removes only expired rows.
func TestDeleteExpiredLogScreeningRecords_ServiceDelegates(t *testing.T) {
	truncateServiceScreeningTables(t)
	ctx := context.Background()
	now := int64(5000000)
	seedScreeningRecord(t, 1, "expired1", "1h", "all", now-1)
	seedScreeningRecord(t, 2, "keep1", "1h", "all", now+1000)
	seedScreeningRecord(t, 3, "keep2", "1h", "all", 0) // 0 == never expires

	deleted, err := DeleteExpiredLogScreeningRecords(ctx, now, 1000)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	var remaining int64
	require.NoError(t, model.DB.WithContext(ctx).Model(&model.LogScreeningRecord{}).Count(&remaining).Error)
	assert.Equal(t, int64(2), remaining)
}

// TestPromptAndUABlockLog_ListDetail verifies list + detail for the prompt/UA
// block logs (including raw header/param fields on detail).
func TestPromptAndUABlockLog_ListDetail(t *testing.T) {
	truncateServiceScreeningTables(t)
	ctx := context.Background()
	u := seedScreeningUser(t, "carol", "Carol", "note")

	require.NoError(t, CreatePromptBlockLog(ctx, PromptBlockLogCreateInput{
		UserId: u.Id, Username: u.Username, IP: "1.1.1.1",
		RequestHeadersRaw: `{"h":"x"}`, RequestParamsRaw: `{"p":"y"}`,
		RulePattern: "bad-prompt", MatchMode: "rule", HTTPStatusCode: 400,
		ErrorCode: "sensitive_words_detected",
	}))
	require.NoError(t, CreateUABlockLog(ctx, UABlockLogCreateInput{
		UserId: u.Id, Username: u.Username, IP: "1.1.1.1", UserAgent: "curl/8.0",
		RequestHeadersRaw: `{"h":"x"}`, RequestParamsRaw: `{"p":"y"}`,
		RulePattern: "bad-ua", IsEmptyUA: false, HTTPStatusCode: 400,
	}))

	// List prompt block logs, filtered by match_mode.
	items, total, err := ListPromptBlockLogs(ctx, PromptBlockLogListFilter{MatchMode: "rule"}, 0, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	assert.Equal(t, "bad-prompt", items[0].RulePattern)
	assert.Equal(t, "Carol", items[0].DisplayName)
	assert.Equal(t, "note", items[0].Remark)

	// Detail returns raw headers/params.
	detail, err := GetPromptBlockLogDetail(ctx, items[0].Id)
	require.NoError(t, err)
	assert.Equal(t, `{"h":"x"}`, detail.RequestHeadersRaw)
	assert.Equal(t, `{"p":"y"}`, detail.RequestParamsRaw)

	// List UA block logs.
	uitems, utotal, err := ListUABlockLogs(ctx, UABlockLogListFilter{}, 0, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(1), utotal)
	require.Len(t, uitems, 1)
	assert.Equal(t, "curl/8.0", uitems[0].UserAgent)

	udetail, err := GetUABlockLogDetail(ctx, uitems[0].Id)
	require.NoError(t, err)
	assert.Equal(t, `{"p":"y"}`, udetail.RequestParamsRaw)

	// Detail error: invalid id.
	_, err = GetPromptBlockLogDetail(ctx, 0)
	require.Error(t, err)
	_, err = GetUABlockLogDetail(ctx, -1)
	require.Error(t, err)
}
