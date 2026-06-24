package service

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// truncateRunScreeningTables wipes both LOG_DB.logs and the main-DB screening
// tables + users so each RunLogScreening test starts clean.
func truncateRunScreeningTables(t *testing.T) {
	t.Helper()
	require.NoError(t, model.DB.Exec("DELETE FROM log_screening_records").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM suspicious_ip_marks").Error)
	require.NoError(t, model.DB.Unscoped().Exec("DELETE FROM users").Error)
	require.NoError(t, model.LOG_DB.Exec("DELETE FROM logs").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM logs").Error)
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM log_screening_records")
		model.DB.Exec("DELETE FROM suspicious_ip_marks")
		model.DB.Unscoped().Exec("DELETE FROM users")
		model.LOG_DB.Exec("DELETE FROM logs")
		model.DB.Exec("DELETE FROM logs")
	})
}

// seedRunUser creates a user for RunLogScreening tests.
func seedRunUser(t *testing.T, username string) *model.User {
	t.Helper()
	u := &model.User{
		Username:    username,
		Password:    "testpass123",
		Group:       "default",
		AffCode:     username + "-aff",
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, model.DB.Create(u).Error)
	return u
}

// seedRunLog inserts a log row into LOG_DB with the given fields.
func seedRunLog(t *testing.T, userId, typ int, ip, ua, requestPath, other, tokenName string, createdAt int64, prompt, completion int) {
	t.Helper()
	log := &model.Log{
		UserId:           userId,
		CreatedAt:        createdAt,
		Type:             typ,
		Ip:               ip,
		UserAgent:        ua,
		RequestPath:      requestPath,
		Other:            other,
		TokenName:        tokenName,
		PromptTokens:     prompt,
		CompletionTokens:  completion,
		ModelName:        "test-model",
		Group:            "default",
		ChannelId:        1,
	}
	require.NoError(t, model.LOG_DB.Create(log).Error)
}

// TestRunLogScreening_RequestCountHit verifies a rule keyed on request_count
// creates a LogScreeningRecord with the right metrics + user remark.
func TestRunLogScreening_RequestCountHit(t *testing.T) {
	truncateRunScreeningTables(t)
	ctx := context.Background()
	u := seedRunUser(t, "rcuser")

	original := *system_setting.GetLogScreeningSetting()
	t.Cleanup(func() { *system_setting.GetLogScreeningSetting() = original })
	setting := system_setting.GetLogScreeningSetting()
	setting.Enabled = true
	setting.ExpireDays = 7
	setting.Rules = []system_setting.LogScreeningRule{
		{Name: "high_freq_1h", Enabled: true, Window: system_setting.LogScreeningWindow1h, RequestCount: 3},
	}

	now := common.GetTimestamp()
	// 4 consume logs in the last hour → request_count=4 >= 3 → hit.
	seedRunLog(t, u.Id, model.LogTypeConsume, "1.1.1.1", "curl/8", "/v1/chat/completions", "", "tok", now-60, 5, 5)
	seedRunLog(t, u.Id, model.LogTypeConsume, "1.1.1.1", "curl/8", "/v1/chat/completions", "", "tok", now-30, 7, 3)
	seedRunLog(t, u.Id, model.LogTypeConsume, "1.1.1.1", "curl/8", "/v1/chat/completions", "", "tok", now-10, 0, 0)
	seedRunLog(t, u.Id, model.LogTypeConsume, "1.1.1.1", "curl/8", "/v1/chat/completions", "", "tok", now-5, 1, 1)

	summary, err := RunLogScreening(ctx, 1, "root", "", true)
	require.NoError(t, err)
	assert.Equal(t, "completed", summary.Status)
	assert.True(t, summary.Enabled)
	assert.Equal(t, 1, summary.RulesChecked)
	assert.Equal(t, int64(1), summary.RecordsCreated, "one record created")
	assert.Equal(t, int64(0), summary.RecordsUpdated)
	assert.NotZero(t, summary.WindowStart)
	assert.NotZero(t, summary.WindowEnd)

	// Record written.
	var rec model.LogScreeningRecord
	require.NoError(t, model.DB.First(&rec, "user_id = ?", u.Id).Error)
	assert.Equal(t, "high_freq_1h", rec.RuleName)
	assert.Equal(t, "1h", rec.Window)
	assert.Equal(t, 4, rec.RequestCount)
	assert.Equal(t, "all", rec.RequestPath)
	assert.Equal(t, "high", rec.RiskLevel)
	assert.True(t, rec.RequireManualReview)
	assert.Equal(t, "1.1.1.1", rec.Ip, "most frequent IP")
	assert.Equal(t, "tok", rec.TokenName, "most frequent token")
	assert.True(t, rec.ManualTriggered)
	assert.Greater(t, rec.ExpiresAt, rec.MatchedAt)

	// User remark appended.
	var user model.User
	require.NoError(t, model.DB.First(&user, u.Id).Error)
	assert.Contains(t, user.Remark, "[日志筛查]high_freq_1h")
}

// TestRunLogScreening_UpsertSecondRunUpdated verifies a second run upserts
// (updates) the existing record rather than creating a duplicate.
func TestRunLogScreening_UpsertSecondRunUpdated(t *testing.T) {
	truncateRunScreeningTables(t)
	ctx := context.Background()
	u := seedRunUser(t, "upsuser")

	original := *system_setting.GetLogScreeningSetting()
	t.Cleanup(func() { *system_setting.GetLogScreeningSetting() = original })
	setting := system_setting.GetLogScreeningSetting()
	setting.Enabled = true
	setting.ExpireDays = 7
	setting.Rules = []system_setting.LogScreeningRule{
		{Name: "rc_rule", Enabled: true, Window: system_setting.LogScreeningWindow1h, RequestCount: 2},
	}

	now := common.GetTimestamp()
	seedRunLog(t, u.Id, model.LogTypeConsume, "1.1.1.1", "ua", "/v1/chat/completions", "", "t", now-60, 5, 5)
	seedRunLog(t, u.Id, model.LogTypeConsume, "1.1.1.1", "ua", "/v1/chat/completions", "", "t", now-30, 5, 5)

	summary1, err := RunLogScreening(ctx, 1, "root", "", true)
	require.NoError(t, err)
	assert.Equal(t, int64(1), summary1.RecordsCreated)

	// Second run: same key → update.
	summary2, err := RunLogScreening(ctx, 1, "root", "", true)
	require.NoError(t, err)
	assert.Equal(t, int64(0), summary2.RecordsCreated, "second run should not create")
	assert.Equal(t, int64(1), summary2.RecordsUpdated, "second run should update")

	// Exactly one record.
	var count int64
	require.NoError(t, model.DB.Model(&model.LogScreeningRecord{}).Where("user_id = ?", u.Id).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

// TestRunLogScreening_ParamRuleHit verifies a param rule (field/op/value) hits
// when the recorded request_params value satisfies the comparison.
func TestRunLogScreening_ParamRuleHit(t *testing.T) {
	truncateRunScreeningTables(t)
	ctx := context.Background()
	u := seedRunUser(t, "paramuser")

	original := *system_setting.GetLogScreeningSetting()
	t.Cleanup(func() { *system_setting.GetLogScreeningSetting() = original })
	setting := system_setting.GetLogScreeningSetting()
	setting.Enabled = true
	setting.ExpireDays = 7
	setting.Rules = []system_setting.LogScreeningRule{
		{
			Name: "param_rule", Enabled: true, Window: system_setting.LogScreeningWindow1h,
			RequestCount: 1, // primary threshold so the user is a candidate
			ParamRules: []system_setting.LogScreeningParamRule{
				{Field: "temperature", Op: ">=", Value: 0.8},
			},
			SecondaryMode: system_setting.LogScreeningSecondaryModeOr,
		},
	}

	now := common.GetTimestamp()
	// other JSON contains request_params.temperature = 0.9 → hit (>= 0.8).
	otherJSON := `{"request_params":{"temperature":0.9}}`
	seedRunLog(t, u.Id, model.LogTypeConsume, "1.1.1.1", "ua", "/v1/chat/completions", otherJSON, "t", now-10, 5, 5)

	summary, err := RunLogScreening(ctx, 1, "root", "", true)
	require.NoError(t, err)
	assert.Equal(t, "completed", summary.Status)
	assert.Equal(t, int64(1), summary.RecordsCreated)

	var rec model.LogScreeningRecord
	require.NoError(t, model.DB.First(&rec, "user_id = ?", u.Id).Error)
	assert.Contains(t, rec.ParamHits, "temperature")
}

// TestRunLogScreening_ParamRuleNoHit verifies a param rule that does NOT satisfy
// the comparison produces no record (secondary gate fails with mode=or).
func TestRunLogScreening_ParamRuleNoHit(t *testing.T) {
	truncateRunScreeningTables(t)
	ctx := context.Background()
	u := seedRunUser(t, "noparam")

	original := *system_setting.GetLogScreeningSetting()
	t.Cleanup(func() { *system_setting.GetLogScreeningSetting() = original })
	setting := system_setting.GetLogScreeningSetting()
	setting.Enabled = true
	setting.ExpireDays = 7
	setting.Rules = []system_setting.LogScreeningRule{
		{
			Name: "param_rule_miss", Enabled: true, Window: system_setting.LogScreeningWindow1h,
			RequestCount: 1,
			ParamRules: []system_setting.LogScreeningParamRule{
				{Field: "temperature", Op: ">=", Value: 0.95},
			},
			SecondaryMode: system_setting.LogScreeningSecondaryModeOr,
		},
	}

	now := common.GetTimestamp()
	// temperature = 0.5 < 0.95 → no hit.
	otherJSON := `{"request_params":{"temperature":0.5}}`
	seedRunLog(t, u.Id, model.LogTypeConsume, "1.1.1.1", "ua", "/v1/chat/completions", otherJSON, "t", now-10, 5, 5)

	summary, err := RunLogScreening(ctx, 1, "root", "", true)
	require.NoError(t, err)
	assert.Equal(t, "completed", summary.Status)
	assert.Equal(t, int64(0), summary.RecordsCreated, "param rule miss → no record")
}

// TestRunLogScreening_UABlacklistHit verifies a UA blacklist entry matches and
// creates a record.
func TestRunLogScreening_UABlacklistHit(t *testing.T) {
	truncateRunScreeningTables(t)
	ctx := context.Background()
	u := seedRunUser(t, "uabl")

	original := *system_setting.GetLogScreeningSetting()
	t.Cleanup(func() { *system_setting.GetLogScreeningSetting() = original })
	setting := system_setting.GetLogScreeningSetting()
	setting.Enabled = true
	setting.ExpireDays = 7
	setting.Rules = []system_setting.LogScreeningRule{
		{
			Name: "ua_bl", Enabled: true, Window: system_setting.LogScreeningWindow1h,
			RequestCount: 1, // primary candidate
			UABlacklist: []string{"curl"},
			SecondaryMode: system_setting.LogScreeningSecondaryModeOr,
		},
	}

	now := common.GetTimestamp()
	seedRunLog(t, u.Id, model.LogTypeConsume, "1.1.1.1", "Mozilla curl/8.0", "/v1/chat/completions", "", "t", now-10, 5, 5)

	summary, err := RunLogScreening(ctx, 1, "root", "", true)
	require.NoError(t, err)
	assert.Equal(t, int64(1), summary.RecordsCreated)

	var rec model.LogScreeningRecord
	require.NoError(t, model.DB.First(&rec, "user_id = ?", u.Id).Error)
	assert.Contains(t, rec.UAHits, "curl")
}

// TestRunLogScreening_UADirectHit verifies a UA direct entry matches even
// without primary thresholds or secondary conditions.
func TestRunLogScreening_UADirectHit(t *testing.T) {
	truncateRunScreeningTables(t)
	ctx := context.Background()
	u := seedRunUser(t, "uadirect")

	original := *system_setting.GetLogScreeningSetting()
	t.Cleanup(func() { *system_setting.GetLogScreeningSetting() = original })
	setting := system_setting.GetLogScreeningSetting()
	setting.Enabled = true
	setting.ExpireDays = 7
	setting.Rules = []system_setting.LogScreeningRule{
		{
			Name: "ua_direct", Enabled: true, Window: system_setting.LogScreeningWindow1h,
			// No primary thresholds; UA direct should match on its own.
			UADirect: []string{"python"},
		},
	}

	now := common.GetTimestamp()
	seedRunLog(t, u.Id, model.LogTypeConsume, "1.1.1.1", "python/3.11", "/v1/chat/completions", "", "t", now-10, 1, 1)

	summary, err := RunLogScreening(ctx, 1, "root", "", true)
	require.NoError(t, err)
	assert.Equal(t, int64(1), summary.RecordsCreated, "UA direct must match without primary thresholds")

	var rec model.LogScreeningRecord
	require.NoError(t, model.DB.First(&rec, "user_id = ?", u.Id).Error)
	assert.Contains(t, rec.UAHits, "python")
}

// TestRunLogScreening_PromptDeltaHit verifies a prompt-delta rule matches when
// consecutive prompt token deltas meet the threshold.
func TestRunLogScreening_PromptDeltaHit(t *testing.T) {
	truncateRunScreeningTables(t)
	ctx := context.Background()
	u := seedRunUser(t, "pdelta")

	original := *system_setting.GetLogScreeningSetting()
	t.Cleanup(func() { *system_setting.GetLogScreeningSetting() = original })
	setting := system_setting.GetLogScreeningSetting()
	setting.Enabled = true
	setting.ExpireDays = 7
	setting.Rules = []system_setting.LogScreeningRule{
		{
			Name: "pd_rule", Enabled: true, Window: system_setting.LogScreeningWindow1h,
			RequestCount: 1, // primary candidate
			PromptDelta:  100, PromptDeltaCount: 1,
			SecondaryMode: system_setting.LogScreeningSecondaryModeOr,
		},
	}

	now := common.GetTimestamp()
	// Two logs with a 150-token delta → 1 delta >= 100.
	seedRunLog(t, u.Id, model.LogTypeConsume, "1.1.1.1", "ua", "/v1/chat/completions", "", "t", now-20, 100, 0)
	seedRunLog(t, u.Id, model.LogTypeConsume, "1.1.1.1", "ua", "/v1/chat/completions", "", "t", now-10, 250, 0)

	summary, err := RunLogScreening(ctx, 1, "root", "", true)
	require.NoError(t, err)
	assert.Equal(t, int64(1), summary.RecordsCreated)

	var rec model.LogScreeningRecord
	require.NoError(t, model.DB.First(&rec, "user_id = ?", u.Id).Error)
	assert.Equal(t, 1, rec.PromptDeltaCount)
	assert.Equal(t, 150, rec.PromptDeltaMax)
}

// TestRunLogScreening_CleanupExpired verifies the run cleans up expired records
// at the end.
func TestRunLogScreening_CleanupExpired(t *testing.T) {
	truncateRunScreeningTables(t)
	ctx := context.Background()

	original := *system_setting.GetLogScreeningSetting()
	t.Cleanup(func() { *system_setting.GetLogScreeningSetting() = original })
	setting := system_setting.GetLogScreeningSetting()
	setting.Enabled = true
	setting.ExpireDays = 7
	setting.Rules = nil // no rules → no matches, but cleanup still runs

	// Seed an expired record directly.
	now := common.GetTimestamp()
	require.NoError(t, model.DB.Create(&model.LogScreeningRecord{
		UserId: 99, RuleName: "expired", Window: "1h", RequestPath: "all",
		ExpiresAt: now - 100, MatchedAt: now - 200,
	}).Error)
	// And a non-expired one.
	require.NoError(t, model.DB.Create(&model.LogScreeningRecord{
		UserId: 98, RuleName: "keep", Window: "1h", RequestPath: "all",
		ExpiresAt: now + 100000, MatchedAt: now - 10,
	}).Error)

	summary, err := RunLogScreening(ctx, 1, "root", "", true)
	require.NoError(t, err)
	assert.Equal(t, "completed", summary.Status)
	assert.Equal(t, int64(1), summary.Expired, "one expired record cleaned up")

	var remaining int64
	require.NoError(t, model.DB.Model(&model.LogScreeningRecord{}).Count(&remaining).Error)
	assert.Equal(t, int64(1), remaining, "only the non-expired record remains")
}

// TestRunLogScreening_RemarkDedup verifies repeated runs do not duplicate the
// remark line (de-dup via AppendUserRemarkLine).
func TestRunLogScreening_RemarkDedup(t *testing.T) {
	truncateRunScreeningTables(t)
	ctx := context.Background()
	u := seedRunUser(t, "dedupremark")

	original := *system_setting.GetLogScreeningSetting()
	t.Cleanup(func() { *system_setting.GetLogScreeningSetting() = original })
	setting := system_setting.GetLogScreeningSetting()
	setting.Enabled = true
	setting.ExpireDays = 7
	setting.Rules = []system_setting.LogScreeningRule{
		{Name: "dedup_rule", Enabled: true, Window: system_setting.LogScreeningWindow1h, RequestCount: 1},
	}

	now := common.GetTimestamp()
	seedRunLog(t, u.Id, model.LogTypeConsume, "1.1.1.1", "ua", "/v1/chat/completions", "", "t", now-10, 5, 5)

	// First run (manual=true) → appends remark.
	_, err := RunLogScreening(ctx, 1, "root", "", true)
	require.NoError(t, err)
	var user model.User
	require.NoError(t, model.DB.First(&user, u.Id).Error)
	firstRemark := user.Remark
	assert.Contains(t, firstRemark, "[日志筛查]dedup_rule")

	// Second run (manual=true) → same line, should be de-duped.
	_, err = RunLogScreening(ctx, 1, "root", "", true)
	require.NoError(t, err)
	require.NoError(t, model.DB.First(&user, u.Id).Error)
	assert.Equal(t, firstRemark, user.Remark, "identical remark line must not be duplicated")
}

// TestRunLogScreening_NoBanSyncRuntime is a runtime guard: the run must not
// create any ban_sync-related rows or option keys, and the summary must not
// reference AutoBanSync.
func TestRunLogScreening_NoBanSyncRuntime(t *testing.T) {
	truncateRunScreeningTables(t)
	ctx := context.Background()

	original := *system_setting.GetLogScreeningSetting()
	t.Cleanup(func() { *system_setting.GetLogScreeningSetting() = original })
	setting := system_setting.GetLogScreeningSetting()
	setting.Enabled = true
	setting.ExpireDays = 7
	setting.Rules = []system_setting.LogScreeningRule{
		{Name: "nobansync", Enabled: true, Window: system_setting.LogScreeningWindow1h, RequestCount: 1},
	}

	u := seedRunUser(t, "nobansync")
	now := common.GetTimestamp()
	seedRunLog(t, u.Id, model.LogTypeConsume, "1.1.1.1", "ua", "/v1/chat/completions", "", "t", now-10, 5, 5)

	summary, err := RunLogScreening(ctx, 1, "root", "", true)
	require.NoError(t, err)
	// Summary JSON must not mention ban_sync.
	summaryBytes, _ := common.Marshal(summary)
	assert.NotContains(t, strings.ToLower(string(summaryBytes)), "bansync",
		"summary must not reference ban_sync")

	// User must NOT be banned (no real banning in screening).
	var refreshed model.User
	require.NoError(t, model.DB.First(&refreshed, u.Id).Error)
	assert.Equal(t, common.UserStatusEnabled, refreshed.Status, "screening must not ban users")

	// No ban_sync option keys in OptionMap.
	common.OptionMapRWMutex.RLock()
	for k := range common.OptionMap {
		assert.NotContains(t, strings.ToLower(k), "bansync",
			"no option key may reference ban_sync; found %q", k)
	}
	common.OptionMapRWMutex.RUnlock()
}

// TestRunLogScreening_SecondaryModeAll verifies secondary_mode=all requires ALL
// configured secondary conditions to pass.
func TestRunLogScreening_SecondaryModeAll(t *testing.T) {
	truncateRunScreeningTables(t)
	ctx := context.Background()
	u := seedRunUser(t, "modeall")

	original := *system_setting.GetLogScreeningSetting()
	t.Cleanup(func() { *system_setting.GetLogScreeningSetting() = original })
	setting := system_setting.GetLogScreeningSetting()
	setting.Enabled = true
	setting.ExpireDays = 7
	setting.Rules = []system_setting.LogScreeningRule{
		{
			Name: "all_rule", Enabled: true, Window: system_setting.LogScreeningWindow1h,
			RequestCount: 1,
			ParamRules: []system_setting.LogScreeningParamRule{
				{Field: "temperature", Op: ">=", Value: 0.8},
			},
			UABlacklist:  []string{"curl"},
			SecondaryMode: system_setting.LogScreeningSecondaryModeAll,
		},
	}

	now := common.GetTimestamp()
	// param hits (temperature=0.9) but UA does NOT contain curl → all mode fails.
	otherJSON := `{"request_params":{"temperature":0.9}}`
	seedRunLog(t, u.Id, model.LogTypeConsume, "1.1.1.1", "python/3.11", "/v1/chat/completions", otherJSON, "t", now-10, 5, 5)

	summary, err := RunLogScreening(ctx, 1, "root", "", true)
	require.NoError(t, err)
	assert.Equal(t, int64(0), summary.RecordsCreated, "all mode: UA miss → no match")

	// Now add a curl UA log → both conditions satisfied → match.
	seedRunLog(t, u.Id, model.LogTypeConsume, "1.1.1.1", "curl/8", "/v1/chat/completions", otherJSON, "t", now-5, 5, 5)
	summary2, err := RunLogScreening(ctx, 1, "root", "", true)
	require.NoError(t, err)
	assert.Equal(t, int64(1), summary2.RecordsCreated, "all mode: both conditions met → match")
}

// TestRunLogScreening_CandidateCap verifies that when the number of candidate
// users exceeds the cap, the summary reports Capped=true and CandidateLimit,
// and does not process more than the cap.
func TestRunLogScreening_CandidateCap(t *testing.T) {
	truncateRunScreeningTables(t)
	ctx := context.Background()

	original := *system_setting.GetLogScreeningSetting()
	t.Cleanup(func() { *system_setting.GetLogScreeningSetting() = original })
	setting := system_setting.GetLogScreeningSetting()
	setting.Enabled = true
	setting.ExpireDays = 7
	// Rule with no primary thresholds + UA-direct → every user is a candidate.
	// This forces the candidate pre-filter to keep all users (subject to cap).
	setting.Rules = []system_setting.LogScreeningRule{
		{Name: "cap_rule", Enabled: true, Window: system_setting.LogScreeningWindow1h, UADirect: []string{"ua-x"}},
	}

	now := common.GetTimestamp()
	// Seed cap+5 users, each with one log → all candidates.
	for i := 0; i < logScreeningCandidateCap+5; i++ {
		username := "capuser" + strconv.Itoa(i)
		u := seedRunUser(t, username)
		seedRunLog(t, u.Id, model.LogTypeConsume, "1.1.1.1", "ua-x", "/v1/chat/completions", "", "t", now-10, 1, 1)
	}

	summary, err := RunLogScreening(ctx, 1, "root", "", true)
	require.NoError(t, err)
	assert.Equal(t, "completed", summary.Status)
	assert.True(t, summary.Capped, "candidates exceeded cap → Capped=true")
	assert.Equal(t, logScreeningCandidateCap, summary.CandidateLimit)
	// CandidatesSeen is bounded at candidateCap+1 by the DB-side Limit.
	assert.Equal(t, logScreeningCandidateCap+1, summary.CandidatesSeen, "CandidatesSeen must be capped at candidateCap+1 by DB-side Limit")
	// At most cap records created (capped candidates processed).
	assert.LessOrEqual(t, summary.RecordsCreated, int64(logScreeningCandidateCap))
}

// TestRunLogScreening_DetailCap verifies the detail query is bounded by the
// detail cap when the detail volume is huge.
func TestRunLogScreening_DetailCap(t *testing.T) {
	truncateRunScreeningTables(t)
	ctx := context.Background()
	u := seedRunUser(t, "detailcap")

	original := *system_setting.GetLogScreeningSetting()
	t.Cleanup(func() { *system_setting.GetLogScreeningSetting() = original })
	setting := system_setting.GetLogScreeningSetting()
	setting.Enabled = true
	setting.ExpireDays = 7
	// Rule with a primary threshold of 1 → the single user is a candidate;
	// the detail scan fetches all their logs, capped at logScreeningDetailCap.
	// Use a 24h window so all seeded logs fall within the window.
	setting.Rules = []system_setting.LogScreeningRule{
		{Name: "detail_rule", Enabled: true, Window: system_setting.LogScreeningWindow24h, RequestCount: 1},
	}

	now := common.GetTimestamp()
	// Seed detail-cap + 100 logs for the single user, all within the 24h window
	// (24h = 86400 seconds; we space them 1 second apart, so cap+100 < 86400).
	count := logScreeningDetailCap + 100
	for i := 0; i < count; i++ {
		seedRunLog(t, u.Id, model.LogTypeConsume, "1.1.1.1", "ua", "/v1/chat/completions", "", "t", now-int64(count-i), 1, 1)
	}

	summary, err := RunLogScreening(ctx, 1, "root", "", true)
	require.NoError(t, err)
	assert.Equal(t, "completed", summary.Status)
	assert.True(t, summary.Capped, "detail volume exceeded cap → Capped=true")
	assert.Equal(t, logScreeningDetailCap, summary.DetailLimit)
	assert.Greater(t, summary.DetailsSeen, 0)
	assert.LessOrEqual(t, summary.DetailsSeen, logScreeningDetailCap, "DetailsSeen must not exceed the cap")
}
