package model

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newRecordLogContext 构造一个带 URL path 与 User-Agent 的 *gin.Context，
// 供 RecordConsumeLog / RecordErrorLog 测试使用。path 可包含 query string，
// 用于验证 request_path 不应包含 query。
func newRecordLogContext(t *testing.T, method, path, userAgent string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("User-Agent", userAgent)
	ctx.Request = req
	ctx.Set(common.RequestIdKey, "test-request-id")
	return ctx
}

// fetchLogById 从 LOG_DB 取出指定 id 的 Log 行（用于校验写入结果）。
func fetchLogById(t *testing.T, id int) *Log {
	t.Helper()
	var got Log
	require.NoError(t, LOG_DB.First(&got, id).Error)
	return &got
}

// latestLogId 取出当前 logs 表里最大的自增 id，方便测试拿到刚插入的行。
func latestLogId(t *testing.T) int {
	t.Helper()
	var got Log
	require.NoError(t, LOG_DB.Order("id DESC").First(&got).Error)
	return got.Id
}

// truncateLogsTable 清空 logs 表，并通过 t.Cleanup 注册收尾清理，
// 保证即使测试中途失败也不会污染全局 LOG_DB 影响后续测试。
func truncateLogsTable(t *testing.T) {
	t.Helper()
	require.NoError(t, LOG_DB.Exec("DELETE FROM logs").Error)
	t.Cleanup(func() {
		// 收尾清理不阻断测试，仅记录失败。
		if err := LOG_DB.Exec("DELETE FROM logs").Error; err != nil {
			t.Logf("failed to cleanup logs table: %v", err)
		}
	})
}

func truncateLogUsersTable(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Exec("DELETE FROM users").Error)
	t.Cleanup(func() {
		if err := DB.Exec("DELETE FROM users").Error; err != nil {
			t.Logf("failed to cleanup users table: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// truncateRunesToByteBudget — rune-safe 截断单元测试
// ---------------------------------------------------------------------------

func TestTruncateRunesToByteBudget_ShortUnchanged(t *testing.T) {
	assert.Equal(t, "abc", truncateRunesToByteBudget("abc", 100))
	// 预算等于字符串长度时不变。
	assert.Equal(t, "abc", truncateRunesToByteBudget("abc", 3))
	// 空串不变。
	assert.Equal(t, "", truncateRunesToByteBudget("", 10))
}

func TestTruncateRunesToByteBudget_TruncatesAndStaysUTF8Valid(t *testing.T) {
	// ASCII 超长：截断到预算，长度恰好等于预算。
	long := strings.Repeat("a", 100)
	got := truncateRunesToByteBudget(long, 10)
	assert.Equal(t, 10, len(got))
	assert.Equal(t, "aaaaaaaaaa", got)
	assert.True(t, utf8.ValidString(got))
}

func TestTruncateRunesToByteBudget_MultibyteNotSplit(t *testing.T) {
	// 每个中文字符占 3 字节。预算 10 字节只能完整容纳 3 个中文字符（9 字节），
	// 第 4 个会让字节数变 12 > 10，必须在边界前停下，不能切在多字节字符中间。
	s := "中文测试字符串" // 7 个 rune = 21 字节
	got := truncateRunesToByteBudget(s, 10)
	assert.True(t, utf8.ValidString(got), "result must remain valid UTF-8")
	assert.LessOrEqual(t, len(got), 10, "result must fit byte budget")
	assert.Equal(t, "中文测", got, "should keep 3 complete runes (9 bytes), not split the 4th")
}

func TestTruncateRunesToByteBudget_BudgetSmallerThanOneRune(t *testing.T) {
	// 预算小于单个多字节 rune 的字节数：返回空串，不写入不完整的 rune。
	got := truncateRunesToByteBudget("中", 2) // "中" 占 3 字节
	assert.Equal(t, "", got)
	assert.True(t, utf8.ValidString(got))
}

func TestTruncateRunesToByteBudget_ZeroOrNegativeBudget(t *testing.T) {
	assert.Equal(t, "", truncateRunesToByteBudget("abc", 0))
	assert.Equal(t, "", truncateRunesToByteBudget("abc", -1))
}

// ---------------------------------------------------------------------------
// normalizeRequestPath — 纯单元测试
// ---------------------------------------------------------------------------

func TestNormalizeRequestPath_Empty(t *testing.T) {
	assert.Equal(t, "", normalizeRequestPath(""))
	assert.Equal(t, "", normalizeRequestPath("   "))
	assert.Equal(t, "", normalizeRequestPath("\t\n"))
}

func TestNormalizeRequestPath_Trim(t *testing.T) {
	assert.Equal(t, "/v1/chat", normalizeRequestPath("  /v1/chat  "))
	assert.Equal(t, "/v1/chat", normalizeRequestPath("\t/v1/chat\n"))
}

func TestNormalizeRequestPath_StripsQueryString(t *testing.T) {
	// Other["request_path"] 可能由调用方误带 query，必须去掉。
	assert.Equal(t, "/v1/chat/completions", normalizeRequestPath("/v1/chat/completions?foo=bar&baz=1"))
	// path 中段出现的 ? 也按 query 分隔符处理。
	assert.Equal(t, "/v1/path", normalizeRequestPath("/v1/path?x=1?y=2"))
	// 无 query 时不影响 path。
	assert.Equal(t, "/v1/chat", normalizeRequestPath("/v1/chat"))
}

func TestNormalizeRequestPath_TruncatesOverlong(t *testing.T) {
	// 构造超过 512 字节的 path，验证截断后长度合规且 UTF-8 仍合法。
	long := "/" + strings.Repeat("a", logRequestPathMaxLength+50)
	got := normalizeRequestPath(long)
	assert.LessOrEqual(t, len(got), logRequestPathMaxLength, "must fit varchar(512)")
	assert.True(t, utf8.ValidString(got))
	// 截断保留前缀（去掉 query 后从头部保留）。
	assert.True(t, strings.HasPrefix(got, "/"))
}

func TestNormalizeRequestPath_MultibyteTruncateStaysValid(t *testing.T) {
	// 多字节字符 path 超长：截断后必须仍是合法 UTF-8 且不切在字符中间。
	s := "/" + strings.Repeat("中", 300) // 1 + 300*3 = 901 字节
	got := normalizeRequestPath(s)
	assert.True(t, utf8.ValidString(got), "result must be valid UTF-8")
	assert.LessOrEqual(t, len(got), logRequestPathMaxLength)
}

func TestNormalizeRequestPath_InvalidUTF8Fixed(t *testing.T) {
	// 含非法 UTF-8 字节序列（0xff 0xfe）的 path：修复为空，返回合法 UTF-8。
	s := "/v1/\xff\xfechat"
	got := normalizeRequestPath(s)
	assert.True(t, utf8.ValidString(got), "invalid UTF-8 must be repaired")
	// 合法前缀保留，非法部分被替换为空（ToValidUTF8 用 "" 替换）。
	assert.Equal(t, "/v1/chat", got)
}

func TestNormalizeRequestPath_QueryStrippedBeforeInvalidUTF8(t *testing.T) {
	// 去 query 与修复非法 UTF-8 同时生效：先去 ? 后的部分，再修复剩余。
	s := "/v1/chat?bad=\xff\xfe"
	got := normalizeRequestPath(s)
	assert.Equal(t, "/v1/chat", got)
	assert.True(t, utf8.ValidString(got))
}

// ---------------------------------------------------------------------------
// normalizeUserAgent — 纯单元测试
// ---------------------------------------------------------------------------

func TestNormalizeUserAgent_Empty(t *testing.T) {
	assert.Equal(t, "", normalizeUserAgent(""))
	assert.Equal(t, "", normalizeUserAgent("   "))
}

func TestNormalizeUserAgent_Trim(t *testing.T) {
	assert.Equal(t, "Mozilla/5.0 Safari", normalizeUserAgent("  Mozilla/5.0 Safari  "))
	assert.Equal(t, "curl/8.0", normalizeUserAgent("\tcurl/8.0\n"))
}

func TestNormalizeUserAgent_InvalidUTF8Repaired(t *testing.T) {
	// 含非法 UTF-8 字节的 UA：ToValidUTF8 替换为空，结果必须是合法 UTF-8。
	s := "Mozilla/5.0 \xff\xfe Safari"
	got := normalizeUserAgent(s)
	assert.True(t, utf8.ValidString(got), "invalid UTF-8 must be repaired")
	// 非法部分被移除，合法部分保留（中间会少两个字节，导致双空格，trim 不会合并中间空格）。
	assert.Contains(t, got, "Mozilla/5.0")
	assert.Contains(t, got, "Safari")
}

func TestNormalizeUserAgent_TruncatesToMaxLength(t *testing.T) {
	long := strings.Repeat("a", logUserAgentMaxLength+50)
	got := normalizeUserAgent(long)
	assert.Equal(t, logUserAgentMaxLength, len(got))
	assert.True(t, utf8.ValidString(got))
	assert.Equal(t, strings.Repeat("a", logUserAgentMaxLength), got)
}

func TestNormalizeUserAgent_AtMaxLengthUnchanged(t *testing.T) {
	exact := strings.Repeat("b", logUserAgentMaxLength)
	assert.Equal(t, exact, normalizeUserAgent(exact))
}

func TestNormalizeUserAgent_MultibyteTruncateStaysValid(t *testing.T) {
	// 多字节 UA 超长：rune-safe 截断后仍合法 UTF-8，且不切在字符中间。
	s := strings.Repeat("中", 300) // 900 字节
	got := normalizeUserAgent(s)
	assert.True(t, utf8.ValidString(got))
	assert.LessOrEqual(t, len(got), logUserAgentMaxLength)
	// 应保留完整的 "中" 字符，数量 = floor(512/3) = 170 个。
	assert.Equal(t, 170*3, len(got))
	assert.Equal(t, strings.Repeat("中", 170), got)
}

// ---------------------------------------------------------------------------
// resolveLogRequestPath — 优先级 + Other 同步清洗（纯单元测试，无 DB）
// ---------------------------------------------------------------------------

func TestResolveLogRequestPath_OtherPriorityOverContext(t *testing.T) {
	other := map[string]interface{}{
		"request_path": "/v1/chat/completions",
	}
	ctx := newRecordLogContext(t, http.MethodPost, "/v1/other", "ua")
	// other 优先，即使 context 也有 path 也应使用 other 中的值。
	got := resolveLogRequestPath(ctx, other)
	assert.Equal(t, "/v1/chat/completions", got)
	// 同步：other 中的值被规范化（此处无变化）后写回。
	assert.Equal(t, "/v1/chat/completions", other["request_path"])
}

func TestResolveLogRequestPath_FallbackToContextPath(t *testing.T) {
	// other 中没有 request_path：回退到 c.Request.URL.Path。
	// 注意 httptest.NewRequest 会解析 query，URL.Path 不含 query。
	ctx := newRecordLogContext(t, http.MethodPost, "/v1/chat/completions?foo=bar&baz=1", "ua")
	got := resolveLogRequestPath(ctx, nil)
	assert.Equal(t, "/v1/chat/completions", got)
	// other 为空 map 也应回退，且不写入（nil context 已被规范化为空，但 other 非 nil 时会写回）。
	other := map[string]interface{}{}
	got2 := resolveLogRequestPath(ctx, other)
	assert.Equal(t, "/v1/chat/completions", got2)
	// other 中现在被同步写入了规范化后的 path。
	assert.Equal(t, "/v1/chat/completions", other["request_path"])
}

func TestResolveLogRequestPath_EmptyOtherValueFallsBack(t *testing.T) {
	// other 中 request_path 为空字符串：视为无值，回退到 context path。
	other := map[string]interface{}{
		"request_path": "",
	}
	ctx := newRecordLogContext(t, http.MethodPost, "/v1/chat/completions", "ua")
	got := resolveLogRequestPath(ctx, other)
	assert.Equal(t, "/v1/chat/completions", got)
	// 同步：空值被规范化为 context path 后写回 other。
	assert.Equal(t, "/v1/chat/completions", other["request_path"])
}

func TestResolveLogRequestPath_NonStringOtherValueFallsBack(t *testing.T) {
	// other 中 request_path 类型不对（例如 int）：回退到 context path。
	other := map[string]interface{}{
		"request_path": 42,
	}
	ctx := newRecordLogContext(t, http.MethodPost, "/v1/chat/completions", "ua")
	got := resolveLogRequestPath(ctx, other)
	assert.Equal(t, "/v1/chat/completions", got)
	// 同步：非字符串值被规范化后的 context path 替换。
	assert.Equal(t, "/v1/chat/completions", other["request_path"])
}

func TestResolveLogRequestPath_NilContext(t *testing.T) {
	// context 为 nil 且 other 也无值：返回空串，不 panic。
	assert.Equal(t, "", resolveLogRequestPath(nil, nil))
	assert.NotPanics(t, func() {
		_ = resolveLogRequestPath(nil, map[string]interface{}{"request_path": "/x"})
	})
	// nil context + other 有 path：仍能从 other 取值并规范化。
	assert.Equal(t, "/x", resolveLogRequestPath(nil, map[string]interface{}{"request_path": "/x"}))
}

func TestResolveLogRequestPath_StripsQueryAndSyncsOther(t *testing.T) {
	// other 中的 request_path 带 query：规范化去 query，并同步回 other（防止 Other 泄漏 query）。
	other := map[string]interface{}{
		"request_path": "/v1/chat/completions?secret=token&foo=bar",
		"model_ratio":  1.0,
	}
	ctx := newRecordLogContext(t, http.MethodPost, "/v1/other", "ua")
	got := resolveLogRequestPath(ctx, other)
	assert.Equal(t, "/v1/chat/completions", got, "query string must be stripped from request_path")
	// other 中的 request_path 被同步替换为去 query 后的值，不含原 query。
	assert.Equal(t, "/v1/chat/completions", other["request_path"])
	// other 中其他键不受影响。
	assert.Equal(t, 1.0, other["model_ratio"])
}

func TestResolveLogRequestPath_DeletesKeyWhenNormalizedEmpty(t *testing.T) {
	// other 中 request_path 仅含 query（去 query 后为空）：规范化为空，从 other 中删除该键。
	// 注意：raw 来自 other 的非空字符串 "?foo=bar"，故不会回退到 context path；
	// 但 normalizeRequestPath 去掉 '?' 后内容为空 → 返回 ""。
	other := map[string]interface{}{
		"request_path": "?foo=bar",
		"keep":         "me",
	}
	ctx := newRecordLogContext(t, http.MethodPost, "/v1/anything", "ua")
	got := resolveLogRequestPath(ctx, other)
	assert.Equal(t, "", got)
	// request_path 键被删除，避免 Other 中残留空 path 键。
	_, exists := other["request_path"]
	assert.False(t, exists, "request_path key must be removed when normalized to empty")
	assert.Equal(t, "me", other["keep"])
}

func TestResolveLogRequestPath_TruncatesOverlongInOther(t *testing.T) {
	// other 中 request_path 超长：规范化截断，并同步回 other。
	longPath := "/" + strings.Repeat("a", logRequestPathMaxLength+50)
	other := map[string]interface{}{
		"request_path": longPath,
	}
	got := resolveLogRequestPath(nil, other)
	assert.LessOrEqual(t, len(got), logRequestPathMaxLength)
	assert.True(t, utf8.ValidString(got))
	// other 中也被同步为截断后的值。
	synced, ok := other["request_path"].(string)
	require.True(t, ok)
	assert.Equal(t, got, synced)
	assert.LessOrEqual(t, len(synced), logRequestPathMaxLength)
}

// ---------------------------------------------------------------------------
// extractUserAgent — 从 gin.Context 提取并规范化
// ---------------------------------------------------------------------------

func TestExtractUserAgent_TrimAndPassthrough(t *testing.T) {
	ctx := newRecordLogContext(t, http.MethodGet, "/x", "  Mozilla/5.0 Safari  ")
	assert.Equal(t, "Mozilla/5.0 Safari", extractUserAgent(ctx))
}

func TestExtractUserAgent_TruncateToMaxLength(t *testing.T) {
	long := strings.Repeat("a", logUserAgentMaxLength+50)
	ctx := newRecordLogContext(t, http.MethodGet, "/x", long)
	got := extractUserAgent(ctx)
	assert.Equal(t, logUserAgentMaxLength, len(got))
	assert.True(t, utf8.ValidString(got))
	assert.Equal(t, strings.Repeat("a", logUserAgentMaxLength), got)
}

func TestExtractUserAgent_NilContext(t *testing.T) {
	assert.Equal(t, "", extractUserAgent(nil))
	assert.NotPanics(t, func() { _ = extractUserAgent(nil) })
}

func TestFormatUserLogsStripsSensitiveAuditFields(t *testing.T) {
	logs := []*Log{
		{
			Id:          99,
			ChannelName: "admin-channel",
			Ip:          "203.0.113.10",
			UserAgent:   "curl/8.0",
			Other: common.MapToJsonStr(map[string]interface{}{
				"request_params":          map[string]interface{}{"prompt": "secret prompt", "temperature": 0.5},
				"response_text":           "secret model output",
				"response_text_truncated": true,
				"admin_info":              map[string]interface{}{"use_channel": []interface{}{1}},
				"stream_status":           map[string]interface{}{"status": "ok"},
				"safe_user_visible_field": "keep-me",
			}),
		},
	}

	formatUserLogs(logs, 0, false)

	assert.Equal(t, 1, logs[0].Id)
	assert.Empty(t, logs[0].ChannelName)
	assert.Empty(t, logs[0].Ip)
	assert.Empty(t, logs[0].UserAgent)
	otherMap, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	assert.NotContains(t, otherMap, "request_params")
	assert.NotContains(t, otherMap, "response_text")
	assert.NotContains(t, otherMap, "response_text_truncated")
	assert.NotContains(t, otherMap, "admin_info")
	assert.NotContains(t, otherMap, "stream_status")
	assert.Equal(t, "keep-me", otherMap["safe_user_visible_field"])
}

func TestFormatUserLogsKeepsIPWhenUserAllowsIt(t *testing.T) {
	logs := []*Log{{Id: 99, Ip: "203.0.113.10"}}

	formatUserLogs(logs, 0, true)

	assert.Equal(t, 1, logs[0].Id)
	assert.Equal(t, "203.0.113.10", logs[0].Ip)
}

func TestGetAllLogsEnrichesUserProfile(t *testing.T) {
	truncateLogsTable(t)
	truncateLogUsersTable(t)

	avatarURL := "/api/user/avatar/100/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.png"
	require.NoError(t, DB.Create(&User{
		Id:                100,
		Username:          "avatar-user",
		Password:          "password123",
		AvatarURL:         avatarURL,
		AvatarSource:      AvatarSourceUploaded,
		DiscordUsername:   "remote_user",
		DiscordGlobalName: "Remote User",
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    100,
		CreatedAt: 123,
		Type:      LogTypeConsume,
		Username:  "avatar-user",
	}).Error)

	logs, total, err := GetAllLogs(LogTypeConsume, 0, 0, "", "", "", 0, 10, 0, "", "", "")
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, logs, 1)
	assert.Equal(t, avatarURL, logs[0].AvatarURL)
	assert.Equal(t, AvatarSourceUploaded, logs[0].AvatarSource)
	assert.Equal(t, "remote_user", logs[0].DiscordUsername)
	assert.Equal(t, "Remote User", logs[0].DiscordGlobalName)
}

// ---------------------------------------------------------------------------
// SQLite in-memory：AutoMigrate + RecordConsumeLog / RecordErrorLog 写入新列
// ---------------------------------------------------------------------------

// TestLogAutoMigrateHasRequestPathAndUserAgentColumns 验证 AutoMigrate 后
// logs 表确实包含 request_path 与 user_agent 两列（即 Phase 2A 的 DB 地基）。
func TestLogAutoMigrateHasRequestPathAndUserAgentColumns(t *testing.T) {
	var cols []struct {
		Name string `gorm:"column:name"`
	}
	require.NoError(t, LOG_DB.Raw("PRAGMA table_info(logs)").Scan(&cols).Error)
	names := make(map[string]struct{}, len(cols))
	for _, c := range cols {
		names[c.Name] = struct{}{}
	}
	assert.Contains(t, names, "request_path", "request_path column must be created by AutoMigrate")
	assert.Contains(t, names, "user_agent", "user_agent column must be created by AutoMigrate")
}

// TestRecordConsumeLogPersistsRequestPathAndUserAgent 验证 RecordConsumeLog 在
// 没有显式 other["request_path"] 时，从 c.Request.URL.Path 提取路径并写入顶层字段，
// 同时写入 trim 过的 user_agent。
func TestRecordConsumeLogPersistsRequestPathAndUserAgent(t *testing.T) {
	truncateLogsTable(t)
	ctx := newRecordLogContext(t, http.MethodPost, "/v1/chat/completions?keep=1", "  curl/8.0  ")
	params := RecordConsumeLogParams{
		ChannelId:        1,
		PromptTokens:     10,
		CompletionTokens: 5,
		ModelName:        "gpt-test",
		TokenName:        "tok",
		Quota:            7,
		Content:          "consume test",
		TokenId:          0,
		UseTimeSeconds:   1,
		IsStream:         false,
		Group:            "default",
		// 故意不设置 Other.request_path，验证回退到 c.Request.URL.Path。
		Other: map[string]interface{}{"model_ratio": 1.0},
	}
	RecordConsumeLog(ctx, 1, params)

	id := latestLogId(t)
	got := fetchLogById(t, id)
	assert.Equal(t, "/v1/chat/completions", got.RequestPath, "request_path should fall back to context path without query string")
	assert.Equal(t, "curl/8.0", got.UserAgent, "user_agent should be trimmed and persisted")
	assert.NotEmpty(t, got.Ip, "ip should be persisted for admin stats even when user IP display is off")
	assert.Equal(t, LogTypeConsume, got.Type)
}

// TestRecordConsumeLogPrefersOtherRequestPath 验证当 Other 中已包含 request_path 时，
// 顶层字段优先采用该值（例如 relay 转换链中保存的原始 path）。
func TestRecordConsumeLogPrefersOtherRequestPath(t *testing.T) {
	truncateLogsTable(t)
	ctx := newRecordLogContext(t, http.MethodPost, "/v1/responses", "ua-x")
	params := RecordConsumeLogParams{
		ChannelId: 2,
		ModelName: "gpt-test",
		Quota:     1,
		Content:   "consume with explicit path",
		Group:     "default",
		Other: map[string]interface{}{
			"request_path": "/v1/chat/completions",
		},
	}
	RecordConsumeLog(ctx, 1, params)

	got := fetchLogById(t, latestLogId(t))
	assert.Equal(t, "/v1/chat/completions", got.RequestPath)
	assert.Equal(t, "ua-x", got.UserAgent)
}

// TestRecordConsumeLogStripsQueryFromOtherRequestPath 验证当 Other.request_path 带 query 时，
// 顶层 RequestPath 去 query，且 Other JSON 中的 request_path 也被同步清洗（不泄漏 query）。
func TestRecordConsumeLogStripsQueryFromOtherRequestPath(t *testing.T) {
	truncateLogsTable(t)
	ctx := newRecordLogContext(t, http.MethodPost, "/v1/responses", "ua-sync")
	params := RecordConsumeLogParams{
		ChannelId: 2,
		ModelName: "gpt-test",
		Quota:     1,
		Content:   "consume with query in other path",
		Group:     "default",
		Other: map[string]interface{}{
			"request_path": "/v1/chat/completions?secret=token&foo=bar",
			"model_ratio":  1.0,
		},
	}
	RecordConsumeLog(ctx, 1, params)

	got := fetchLogById(t, latestLogId(t))
	// 顶层字段去 query。
	assert.Equal(t, "/v1/chat/completions", got.RequestPath, "query must be stripped from top-level RequestPath")
	// Other JSON 中的 request_path 也被同步清洗，不含 query。
	otherMap, err := common.StrToMap(got.Other)
	require.NoError(t, err)
	reqPath, ok := otherMap["request_path"].(string)
	require.True(t, ok, "request_path must exist in Other JSON (synced)")
	assert.Equal(t, "/v1/chat/completions", reqPath, "Other.request_path must have query stripped")
	assert.NotContains(t, got.Other, "secret", "Other JSON must not leak query params")
	// 其他键保留。
	assert.Equal(t, 1.0, otherMap["model_ratio"])
}

// TestRecordConsumeLogTruncatesOverlongRequestPath 验证超长 request_path 被截断到列长度内，
// 顶层字段与 Other 同步均为截断后的合法 UTF-8。
func TestRecordConsumeLogTruncatesOverlongRequestPath(t *testing.T) {
	truncateLogsTable(t)
	ctx := newRecordLogContext(t, http.MethodPost, "/v1/responses", "ua-long")
	longPath := "/v1/" + strings.Repeat("a", logRequestPathMaxLength)
	params := RecordConsumeLogParams{
		ChannelId: 2,
		ModelName: "gpt-test",
		Quota:     1,
		Content:   "consume with overlong path",
		Group:     "default",
		Other: map[string]interface{}{
			"request_path": longPath,
		},
	}
	RecordConsumeLog(ctx, 1, params)

	got := fetchLogById(t, latestLogId(t))
	assert.LessOrEqual(t, len(got.RequestPath), logRequestPathMaxLength, "top-level RequestPath must fit varchar(512)")
	assert.True(t, utf8.ValidString(got.RequestPath))
	// Other 中也被同步为截断后的值。
	otherMap, err := common.StrToMap(got.Other)
	require.NoError(t, err)
	synced, ok := otherMap["request_path"].(string)
	require.True(t, ok)
	assert.Equal(t, got.RequestPath, synced, "Other.request_path must be synced to truncated value")
}

// TestRecordConsumeLogInvalidUTF8UserAgent 验证含非法 UTF-8 的 user_agent 被修复，
// 截断后仍是合法 UTF-8 且长度合规。
func TestRecordConsumeLogInvalidUTF8UserAgent(t *testing.T) {
	truncateLogsTable(t)
	// 构造含非法 UTF-8 字节且超长的 UA。
	rawUA := "Mozilla/5.0 \xff\xfe" + strings.Repeat("中", 300)
	ctx := newRecordLogContext(t, http.MethodPost, "/v1/chat", rawUA)
	params := RecordConsumeLogParams{
		ChannelId: 1,
		ModelName: "gpt-test",
		Quota:     1,
		Content:   "consume invalid UA",
		Group:     "default",
		Other:     map[string]interface{}{"model_ratio": 1.0},
	}
	RecordConsumeLog(ctx, 1, params)

	got := fetchLogById(t, latestLogId(t))
	assert.True(t, utf8.ValidString(got.UserAgent), "user_agent must be valid UTF-8 after normalization")
	assert.LessOrEqual(t, len(got.UserAgent), logUserAgentMaxLength, "user_agent must fit byte budget")
	assert.NotEmpty(t, got.UserAgent, "valid prefix must be retained")
}

// TestRecordErrorLogPersistsRequestPathAndUserAgent 验证 RecordErrorLog 同样写入
// request_path（含 other map 优先级回退）与 user_agent。
func TestRecordErrorLogPersistsRequestPathAndUserAgent(t *testing.T) {
	truncateLogsTable(t)
	ctx := newRecordLogContext(t, http.MethodPost, "/v1/messages?q=1", "  python-requests/2.31 ")
	other := map[string]interface{}{
		"error_type":   "upstream_error",
		"error_code":   500,
		"status_code":  500,
		"channel_id":   3,
		"channel_name": "test-channel",
		// 不写 request_path，验证回退到 c.Request.URL.Path。
	}
	RecordErrorLog(ctx, 1, 3, "claude-test", "tok", "upstream 500", 0, 1, false, "default", other)

	got := fetchLogById(t, latestLogId(t))
	assert.Equal(t, "/v1/messages", got.RequestPath, "request_path should fall back to context path without query string")
	assert.Equal(t, "python-requests/2.31", got.UserAgent)
	assert.Equal(t, LogTypeError, got.Type)
}

// TestRecordErrorLogPrefersOtherRequestPath 验证 RecordErrorLog 优先采用 other 中的
// request_path（错误流程里 controller 已写入 c.Request.URL.Path 到 other["request_path"]）。
func TestRecordErrorLogPrefersOtherRequestPath(t *testing.T) {
	truncateLogsTable(t)
	ctx := newRecordLogContext(t, http.MethodPost, "/v1/responses", "ua-err")
	other := map[string]interface{}{
		"request_path": "/v1/chat/completions",
		"error_code":   "rate_limited",
	}
	RecordErrorLog(ctx, 1, 3, "gpt-test", "tok", "rate limited", 0, 1, false, "default", other)

	got := fetchLogById(t, latestLogId(t))
	assert.Equal(t, "/v1/chat/completions", got.RequestPath)
	assert.Equal(t, "ua-err", got.UserAgent)
}

// TestRecordErrorLogStripsQueryAndSyncsOther 验证 RecordErrorLog 对 Other.request_path
// 带 query 的情况：顶层去 query，Other JSON 同步清洗，不泄漏 query。
func TestRecordErrorLogStripsQueryAndSyncsOther(t *testing.T) {
	truncateLogsTable(t)
	ctx := newRecordLogContext(t, http.MethodPost, "/v1/responses", "ua-err-sync")
	other := map[string]interface{}{
		"request_path": "/v1/chat/completions?secret=abc",
		"error_code":   "rate_limited",
	}
	RecordErrorLog(ctx, 1, 3, "gpt-test", "tok", "rate limited", 0, 1, false, "default", other)

	got := fetchLogById(t, latestLogId(t))
	assert.Equal(t, "/v1/chat/completions", got.RequestPath)
	assert.NotContains(t, got.Other, "secret", "Other JSON must not leak query params")
	otherMap, err := common.StrToMap(got.Other)
	require.NoError(t, err)
	reqPath, ok := otherMap["request_path"].(string)
	require.True(t, ok)
	assert.Equal(t, "/v1/chat/completions", reqPath, "Other.request_path must be synced without query")
}
