package service

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedBanTestUser creates a user with the given role and returns it.
func seedBanTestUser(t *testing.T, username string, role int) *model.User {
	t.Helper()
	u := &model.User{
		Username: username,
		Password: "testpass123",
		Role:     role,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  username + "-aff",
	}
	require.NoError(t, model.DB.Create(u).Error)
	return u
}

// seedBanTestToken creates an enabled token for the user.
func seedBanTestToken(t *testing.T, userId int, name string) *model.Token {
	t.Helper()
	tok := &model.Token{
		UserId:      userId,
		Name:        name,
		Key:         name + "-key-" + strings.Repeat("x", 20),
		Status:      common.TokenStatusEnabled,
		Group:       "default",
		ExpiredTime: -1,
	}
	require.NoError(t, model.DB.Create(tok).Error)
	return tok
}

// truncateUserBanTables wipes the tables touched by the ban/auto-ban tests.
func truncateUserBanTables(t *testing.T) {
	t.Helper()
	require.NoError(t, model.DB.Exec("DELETE FROM suspicious_ip_marks").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM prompt_block_logs").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM ua_block_logs").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM tokens").Error)
	require.NoError(t, model.DB.Unscoped().Exec("DELETE FROM users").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM logs").Error)
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM suspicious_ip_marks")
		model.DB.Exec("DELETE FROM prompt_block_logs")
		model.DB.Exec("DELETE FROM ua_block_logs")
		model.DB.Exec("DELETE FROM tokens")
		model.DB.Unscoped().Exec("DELETE FROM users")
		model.DB.Exec("DELETE FROM logs")
	})
}

// TestBanUserAndDisableTokens_DisablesUserAndTokens verifies the local ban
// helper sets user.Status=disabled, disables all tokens, and appends the
// reason to the remark.
func TestBanUserAndDisableTokens_DisablesUserAndTokens(t *testing.T) {
	truncateUserBanTables(t)
	u := seedBanTestUser(t, "alice", common.RoleCommonUser)
	tok1 := seedBanTestToken(t, u.Id, "tok1")
	tok2 := seedBanTestToken(t, u.Id, "tok2")

	require.NoError(t, BanUserAndDisableTokens(u, "Prompt 拦截自动封禁：rule1"))

	var refreshed model.User
	require.NoError(t, model.DB.First(&refreshed, u.Id).Error)
	assert.Equal(t, common.UserStatusDisabled, refreshed.Status)
	assert.Contains(t, refreshed.Remark, "Prompt 拦截自动封禁：rule1")

	for _, tokId := range []int{tok1.Id, tok2.Id} {
		var tok model.Token
		require.NoError(t, model.DB.First(&tok, tokId).Error)
		assert.Equal(t, common.TokenStatusDisabled, tok.Status, "token %d must be disabled", tokId)
	}
}

// TestBanUserAndDisableTokens_ProtectsAdmin verifies admin/root users are
// rejected (no ban, no token disable).
func TestBanUserAndDisableTokens_ProtectsAdmin(t *testing.T) {
	truncateUserBanTables(t)
	for i, role := range []int{common.RoleAdminUser, common.RoleRootUser} {
		u := seedBanTestUser(t, "adminb"+strconv.Itoa(i), role)
		tok := seedBanTestToken(t, u.Id, "admintok"+strconv.Itoa(i))

		err := BanUserAndDisableTokens(u, "should not ban")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot ban admin/root user")

		var refreshed model.User
		require.NoError(t, model.DB.First(&refreshed, u.Id).Error)
		assert.Equal(t, common.UserStatusEnabled, refreshed.Status, "admin must stay enabled (iter %d)", i)

		var tk model.Token
		require.NoError(t, model.DB.First(&tk, tok.Id).Error)
		assert.Equal(t, common.TokenStatusEnabled, tk.Status, "admin token must stay enabled (iter %d)", i)
	}
}

// TestBanUserAndDisableTokens_RemarkDeDupesAndTruncates verifies the ban helper
// de-duplicates the reason line in the remark and caps it at 255 runes.
func TestBanUserAndDisableTokens_RemarkDeDupesAndTruncates(t *testing.T) {
	truncateUserBanTables(t)
	u := seedBanTestUser(t, "dedup", common.RoleCommonUser)

	// First ban with a long reason. The remark is capped at 255 runes, so the
	// stored reason is a prefix of the input.
	longReason := strings.Repeat("原因", 200) // 400 runes
	require.NoError(t, BanUserAndDisableTokens(u, longReason))
	var refreshed model.User
	require.NoError(t, model.DB.First(&refreshed, u.Id).Error)
	assert.LessOrEqual(t, len([]rune(refreshed.Remark)), 255, "remark capped at 255 runes")
	storedReason := refreshed.Remark

	// Ban again with the same reason — the stored (truncated) line must not be
	// duplicated. We compare against the stored value, not the original input,
	// since the original was truncated.
	require.NoError(t, BanUserAndDisableTokens(u, longReason))
	require.NoError(t, model.DB.First(&refreshed, u.Id).Error)
	assert.Equal(t, storedReason, refreshed.Remark, "identical reason line must not be duplicated")
}

func TestBanUserForDiscordBanPatrolAndDisableTokensRejectsChangedBinding(t *testing.T) {
	truncateUserBanTables(t)
	u := seedBanTestUser(t, "discordban", common.RoleCommonUser)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", u.Id).Updates(map[string]interface{}{
		"discord_id":            "current-discord",
		"discord_refresh_token": "encrypted-refresh",
		"discord_gate_passed":   true,
	}).Error)
	tok := seedBanTestToken(t, u.Id, "discordbantok")

	err := BanUserForDiscordBanPatrolAndDisableTokens(u, "old-discord", "Discord ban patrol: banned guild matched", "ban_group_matched", common.GetTimestamp())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "discord binding changed")

	var refreshed model.User
	require.NoError(t, model.DB.First(&refreshed, u.Id).Error)
	assert.Equal(t, common.UserStatusEnabled, refreshed.Status)
	assert.True(t, refreshed.DiscordGatePassed)
	var refreshedToken model.Token
	require.NoError(t, model.DB.First(&refreshedToken, tok.Id).Error)
	assert.Equal(t, common.TokenStatusEnabled, refreshedToken.Status)
}

// TestAppendUserRemarkLine_DeDupes verifies the helper de-duplicates identical
// lines and truncates to 255 runes.
func TestAppendUserRemarkLine_DeDupes(t *testing.T) {
	truncateUserBanTables(t)
	u := seedBanTestUser(t, "remark", common.RoleCommonUser)

	remark, err := AppendUserRemarkLine(u.Id, "line1")
	require.NoError(t, err)
	assert.Equal(t, "line1", remark)

	remark, err = AppendUserRemarkLine(u.Id, "line1") // duplicate
	require.NoError(t, err)
	assert.Equal(t, "line1", remark, "duplicate line must not be appended again")

	remark, err = AppendUserRemarkLine(u.Id, "line2")
	require.NoError(t, err)
	assert.Contains(t, remark, "line1")
	assert.Contains(t, remark, "line2")

	// Truncation: append a very long line.
	longLine := strings.Repeat("x", 500)
	_, err = AppendUserRemarkLine(u.Id, longLine)
	require.NoError(t, err)
	var refreshed model.User
	require.NoError(t, model.DB.First(&refreshed, u.Id).Error)
	assert.LessOrEqual(t, len([]rune(refreshed.Remark)), 255, "remark capped at 255 runes")
}

// TestMarkSuspiciousIP_UpsertsCount verifies the suspicious-IP upsert
// increments the trigger count on repeat hits.
func TestMarkSuspiciousIP_UpsertsCount(t *testing.T) {
	truncateUserBanTables(t)
	u := seedBanTestUser(t, "ipuser", common.RoleCommonUser)

	mark, created, err := MarkSuspiciousIP(context.Background(), MarkSuspiciousIPInput{
		UserID: u.Id, Username: u.Username, IP: "1.2.3.4",
		Source: "prompt_auto_ban", Context: "ctx", BanReason: "r",
	})
	require.NoError(t, err)
	require.NotNil(t, mark)
	assert.True(t, created, "first call should create")

	_, created2, err := MarkSuspiciousIP(context.Background(), MarkSuspiciousIPInput{
		UserID: u.Id, Username: u.Username, IP: "1.2.3.4",
		Source: "prompt_auto_ban",
	})
	require.NoError(t, err)
	assert.False(t, created2, "second call should update, not create")

	var got model.SuspiciousIPMark
	require.NoError(t, model.DB.First(&got, "user_id = ?", u.Id).Error)
	assert.Equal(t, 2, got.TriggerCount, "trigger count must increment")

	// Validation errors.
	_, _, err = MarkSuspiciousIP(context.Background(), MarkSuspiciousIPInput{UserID: 0, IP: "1.1.1.1"})
	require.Error(t, err)
	_, _, err = MarkSuspiciousIP(context.Background(), MarkSuspiciousIPInput{UserID: u.Id, IP: "  "})
	require.Error(t, err)
}

// TestAutoBanUserForPromptBlock_BansAndMarksIP verifies the prompt auto-ban
// path disables the user + tokens and marks the client IP suspicious.
func TestAutoBanUserForPromptBlock_BansAndMarksIP(t *testing.T) {
	truncateUserBanTables(t)
	u := seedBanTestUser(t, "promptban", common.RoleCommonUser)
	seedBanTestToken(t, u.Id, "tok")

	banned, reason, err := AutoBanUserForPromptBlock(context.Background(), u.Id, "rule_x", "badpattern", "5.6.7.8")
	require.NoError(t, err)
	assert.True(t, banned)
	assert.Contains(t, reason, "Prompt 拦截自动封禁")
	assert.Contains(t, reason, "rule_x")

	var refreshed model.User
	require.NoError(t, model.DB.First(&refreshed, u.Id).Error)
	assert.Equal(t, common.UserStatusDisabled, refreshed.Status)
	assert.Contains(t, refreshed.Remark, "[Prompt命中]rule_x(badpattern)")

	var marks []model.SuspiciousIPMark
	require.NoError(t, model.DB.Find(&marks, "user_id = ?", u.Id).Error)
	require.Len(t, marks, 1)
	assert.Equal(t, "5.6.7.8", marks[0].Ip)
	assert.Equal(t, "prompt_auto_ban", marks[0].Source)
}

// TestAutoBanUserForPromptBlock_AlreadyDisabled verifies an already-disabled
// user returns banned=false with a reason noting the existing state, and
// still disables tokens.
func TestAutoBanUserForPromptBlock_AlreadyDisabled(t *testing.T) {
	truncateUserBanTables(t)
	u := seedBanTestUser(t, "alreadydisabled", common.RoleCommonUser)
	u.Status = common.UserStatusDisabled
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", u.Id).Update("status", u.Status).Error)
	tok := seedBanTestToken(t, u.Id, "tok")

	banned, reason, err := AutoBanUserForPromptBlock(context.Background(), u.Id, "rule_y", "pat", "9.9.9.9")
	require.NoError(t, err)
	assert.False(t, banned, "already-disabled user: banned=false")
	assert.Contains(t, reason, "已处于封禁状态")

	var refreshed model.Token
	require.NoError(t, model.DB.First(&refreshed, tok.Id).Error)
	assert.Equal(t, common.TokenStatusDisabled, refreshed.Status, "tokens disabled for convergence")
}

// TestAutoBanUserForPromptBlock_ProtectsAdmin verifies admin/root are not
// banned or marked.
func TestAutoBanUserForPromptBlock_ProtectsAdmin(t *testing.T) {
	truncateUserBanTables(t)
	for i, role := range []int{common.RoleAdminUser, common.RoleRootUser} {
		u := seedBanTestUser(t, "adminp"+strconv.Itoa(i), role)
		banned, reason, err := AutoBanUserForPromptBlock(context.Background(), u.Id, "rule", "pat", "1.1.1.1")
		require.NoError(t, err)
		assert.False(t, banned)
		assert.Empty(t, reason)
		var refreshed model.User
		require.NoError(t, model.DB.First(&refreshed, u.Id).Error)
		assert.Equal(t, common.UserStatusEnabled, refreshed.Status)
		var count int64
		model.DB.Model(&model.SuspiciousIPMark{}).Where("user_id = ?", u.Id).Count(&count)
		assert.Equal(t, int64(0), count, "admin must not get a suspicious IP mark")
	}
}

// TestBuildPromptBlockedErrorAndRecord_RuleAutoBan verifies the record+auto-ban
// builder sets AutoBanConfigured=true for a rule hit with AutoBan, writes a
// PromptBlockLog, and runs the local ban.
func TestBuildPromptBlockedErrorAndRecord_RuleAutoBan(t *testing.T) {
	truncateUserBanTables(t)
	u := seedBanTestUser(t, "rulehit", common.RoleCommonUser)
	seedBanTestToken(t, u.Id, "tok")

	rctx := &fakePromptBlockRecordContext{userId: u.Id, username: u.Username, ip: "1.1.1.1", path: "/v1/chat/completions", headers: "{}", params: "{}"}
	hit := &SensitiveRuleHit{
		Pattern:        "forbidden",
		RuleName:       "r1",
		Message:        "blocked",
		ErrorCode:      types.ErrorCode("prompt_blocked"),
		HTTPStatusCode: 403,
		AutoBan:        true,
		MatchMode:      "rule",
	}
	status, code, err := BuildPromptBlockedErrorAndRecord(rctx, hit, "fallback", "rule", "forbidden")
	require.Error(t, err)
	assert.Equal(t, 403, status)
	assert.Equal(t, types.ErrorCode("prompt_blocked"), code)

	// User banned.
	var refreshed model.User
	require.NoError(t, model.DB.First(&refreshed, u.Id).Error)
	assert.Equal(t, common.UserStatusDisabled, refreshed.Status)

	// PromptBlockLog written.
	var log model.PromptBlockLog
	require.NoError(t, model.DB.First(&log, "user_id = ?", u.Id).Error)
	assert.Equal(t, "forbidden", log.RulePattern)
	assert.Equal(t, "rule", log.MatchMode)
	assert.True(t, log.AutoBanConfigured)
	assert.True(t, log.AutoBanned)
	assert.Contains(t, log.BanReason, "Prompt 拦截自动封禁")
}

// TestBuildPromptBlockedErrorAndRecord_BasicNoAutoBan verifies a basic
// sensitive-word hit (matchMode=basic) writes a PromptBlockLog but does NOT
// auto-ban (AutoBanConfigured=false, AutoBanned=false, user stays enabled).
func TestBuildPromptBlockedErrorAndRecord_BasicNoAutoBan(t *testing.T) {
	truncateUserBanTables(t)
	u := seedBanTestUser(t, "basichit", common.RoleCommonUser)
	seedBanTestToken(t, u.Id, "tok")

	rctx := &fakePromptBlockRecordContext{userId: u.Id, username: u.Username, ip: "1.1.1.1", path: "/v1/chat/completions", headers: "{}", params: "{}"}
	status, code, err := BuildPromptBlockedErrorAndRecord(rctx, nil, "fallback msg", "basic", "badword")
	require.Error(t, err)
	assert.Equal(t, setting.DefaultSensitiveStatusCode, status)
	assert.Equal(t, types.ErrorCode(setting.DefaultSensitiveErrorCode), code)
	assert.Equal(t, "fallback msg", err.Error())

	// User NOT banned.
	var refreshed model.User
	require.NoError(t, model.DB.First(&refreshed, u.Id).Error)
	assert.Equal(t, common.UserStatusEnabled, refreshed.Status)

	// PromptBlockLog written, no auto-ban.
	var log model.PromptBlockLog
	require.NoError(t, model.DB.First(&log, "user_id = ?", u.Id).Error)
	assert.Equal(t, "badword", log.RulePattern)
	assert.Equal(t, "basic", log.MatchMode)
	assert.False(t, log.AutoBanConfigured)
	assert.False(t, log.AutoBanned)
	assert.Empty(t, log.BanReason)
}

// TestBuildUABlockedErrorAndRecord_RuleAutoBan verifies the UA record+auto-ban
// builder writes a UABlockLog and runs the local ban when hit.AutoBan is set.
func TestBuildUABlockedErrorAndRecord_RuleAutoBan(t *testing.T) {
	truncateUserBanTables(t)
	u := seedBanTestUser(t, "uahit", common.RoleCommonUser)
	seedBanTestToken(t, u.Id, "tok")

	rctx := &fakeUABlockRecordContext{userId: u.Id, username: u.Username, ip: "2.2.2.2", path: "/v1/chat/completions", ua: "curl/8.0", headers: "{}", params: "{}"}
	hit := &SensitiveRuleHit{
		Pattern:        "curl/.*",
		RuleName:       "ua-curl",
		Message:        "curl blocked",
		ErrorCode:      types.ErrorCode("ua_blocked"),
		HTTPStatusCode: 403,
		AutoBan:        true,
		MatchMode:      "rule",
	}
	status, code, err := BuildUABlockedErrorAndRecord(rctx, hit)
	require.Error(t, err)
	assert.Equal(t, 403, status)
	assert.Equal(t, types.ErrorCode("ua_blocked"), code)

	// User banned.
	var refreshed model.User
	require.NoError(t, model.DB.First(&refreshed, u.Id).Error)
	assert.Equal(t, common.UserStatusDisabled, refreshed.Status)

	// UABlockLog written.
	var log model.UABlockLog
	require.NoError(t, model.DB.First(&log, "user_id = ?", u.Id).Error)
	assert.Equal(t, "curl/.*", log.RulePattern)
	assert.Equal(t, "curl/8.0", log.UserAgent)
	assert.False(t, log.IsEmptyUA)
	assert.True(t, log.AutoBanConfigured)
	assert.True(t, log.AutoBanned)

	// Suspicious IP marked.
	var marks []model.SuspiciousIPMark
	require.NoError(t, model.DB.Find(&marks, "user_id = ?", u.Id).Error)
	require.Len(t, marks, 1)
	assert.Equal(t, "ua_auto_ban", marks[0].Source)
}

// TestBuildUABlockedErrorAndRecord_EmptyUAAutoBan verifies the empty-UA path
// honors CheckSensitiveOnEmptyUAAutoBanEnabled.
func TestBuildUABlockedErrorAndRecord_EmptyUAAutoBan(t *testing.T) {
	truncateUserBanTables(t)
	u := seedBanTestUser(t, "emptyua", common.RoleCommonUser)
	seedBanTestToken(t, u.Id, "tok")

	origEmptyAutoBan := setting.CheckSensitiveOnEmptyUAAutoBanEnabled
	t.Cleanup(func() { setting.CheckSensitiveOnEmptyUAAutoBanEnabled = origEmptyAutoBan })

	rctx := &fakeUABlockRecordContext{userId: u.Id, username: u.Username, ip: "3.3.3.3", path: "/v1/chat", ua: "", headers: "{}", params: "{}"}
	hit := &SensitiveRuleHit{
		Pattern:        "<empty_ua>",
		Message:        "empty UA",
		HTTPStatusCode: 400,
		MatchMode:      "empty_ua",
	}

	// Flag off → no auto-ban.
	setting.CheckSensitiveOnEmptyUAAutoBanEnabled = false
	_, _, _ = BuildUABlockedErrorAndRecord(rctx, hit)
	var refreshed model.User
	require.NoError(t, model.DB.First(&refreshed, u.Id).Error)
	assert.Equal(t, common.UserStatusEnabled, refreshed.Status)
	var log model.UABlockLog
	require.NoError(t, model.DB.First(&log, "user_id = ?", u.Id).Error)
	assert.False(t, log.AutoBanConfigured)
	assert.True(t, log.IsEmptyUA)

	// Flag on → auto-ban.
	require.NoError(t, model.DB.Exec("DELETE FROM ua_block_logs").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM suspicious_ip_marks").Error)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", u.Id).Update("status", common.UserStatusEnabled).Error)
	setting.CheckSensitiveOnEmptyUAAutoBanEnabled = true
	_, _, _ = BuildUABlockedErrorAndRecord(rctx, hit)
	require.NoError(t, model.DB.First(&refreshed, u.Id).Error)
	assert.Equal(t, common.UserStatusDisabled, refreshed.Status)
	require.NoError(t, model.DB.First(&log, "user_id = ?", u.Id).Error)
	assert.True(t, log.AutoBanConfigured)
	assert.True(t, log.AutoBanned)
	var marks []model.SuspiciousIPMark
	require.NoError(t, model.DB.Find(&marks, "user_id = ?", u.Id).Error)
	require.Len(t, marks, 1)
	assert.Equal(t, "ua_auto_ban", marks[0].Source)
}

// TestBuildUABlockedErrorAndRecord_LineRegexNoAutoBan verifies a synthetic
// line-regex hit (AutoBan=false) writes a log but does not ban.
func TestBuildUABlockedErrorAndRecord_LineRegexNoAutoBan(t *testing.T) {
	truncateUserBanTables(t)
	u := seedBanTestUser(t, "lineregex", common.RoleCommonUser)
	seedBanTestToken(t, u.Id, "tok")

	rctx := &fakeUABlockRecordContext{userId: u.Id, username: u.Username, ip: "4.4.4.4", path: "/v1/chat", ua: "python/3.11", headers: "{}", params: "{}"}
	hit := &SensitiveRuleHit{
		Pattern:        "python/.*",
		Message:        setting.SensitiveUABlockedMessage,
		ErrorCode:      types.ErrorCodeSensitiveWordsDetected,
		HTTPStatusCode: 400,
		AutoBan:        false,
		MatchMode:      "blocked_regex",
	}
	_, _, _ = BuildUABlockedErrorAndRecord(rctx, hit)
	var refreshed model.User
	require.NoError(t, model.DB.First(&refreshed, u.Id).Error)
	assert.Equal(t, common.UserStatusEnabled, refreshed.Status, "line-regex block must not auto-ban")
	var log model.UABlockLog
	require.NoError(t, model.DB.First(&log, "user_id = ?", u.Id).Error)
	assert.False(t, log.AutoBanConfigured)
	assert.False(t, log.AutoBanned)
}

// fakePromptBlockRecordContext is a test fake implementing
// service.PromptBlockRecordContext.
type fakePromptBlockRecordContext struct {
	userId   int
	username string
	ip       string
	path     string
	headers  string
	params   string
}

func (f *fakePromptBlockRecordContext) RequestContext() context.Context { return context.Background() }
func (f *fakePromptBlockRecordContext) UserID() int                     { return f.userId }
func (f *fakePromptBlockRecordContext) Username() string                { return f.username }
func (f *fakePromptBlockRecordContext) ClientIP() string                { return f.ip }
func (f *fakePromptBlockRecordContext) RequestPath() string             { return f.path }
func (f *fakePromptBlockRecordContext) RequestHeadersRaw() string       { return f.headers }
func (f *fakePromptBlockRecordContext) RequestParamsRaw() string        { return f.params }

// fakeUABlockRecordContext is a test fake implementing
// service.UABlockRecordContext.
type fakeUABlockRecordContext struct {
	userId   int
	username string
	ip       string
	path     string
	ua       string
	headers  string
	params   string
}

func (f *fakeUABlockRecordContext) RequestContext() context.Context { return context.Background() }
func (f *fakeUABlockRecordContext) UserID() int                     { return f.userId }
func (f *fakeUABlockRecordContext) Username() string                { return f.username }
func (f *fakeUABlockRecordContext) ClientIP() string                { return f.ip }
func (f *fakeUABlockRecordContext) RequestPath() string             { return f.path }
func (f *fakeUABlockRecordContext) UserAgent() string               { return f.ua }
func (f *fakeUABlockRecordContext) RequestHeadersRaw() string       { return f.headers }
func (f *fakeUABlockRecordContext) RequestParamsRaw() string        { return f.params }
