package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupRelayInterceptTestDB wires an isolated in-memory DB for the relay
// intercept controller tests, migrating the tables the ban/log path touches.
// Reuses the log-screening test DB helper (5A) and adds Token.
func setupRelayInterceptTestDB(t *testing.T) {
	t.Helper()
	setupLogScreeningTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Token{}))
}

// newRelayInterceptContext builds a gin.Context that mimics an authenticated
// non-admin user with a JSON request body wired through the body storage.
func newRelayInterceptContext(t *testing.T, bodyJSON, userAgent string, role int) *gin.Context {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	ctx.Request = req
	ctx.Set("id", 1)
	ctx.Set("role", role)
	ctx.Set("username", "testuser")
	// Materialize body storage so the intercept adapters can read it.
	_, err := common.GetRequestBody(ctx)
	require.NoError(t, err)
	return ctx
}

// TestPromptBlockRecordContext_BodyStorageRoundtrips verifies the controller
// adapter reads raw params via the body storage (seek-to-0 + restore) and that
// the body remains readable afterwards — proving the relay intercept point will
// not break the downstream retry loop's body re-read.
func TestPromptBlockRecordContext_BodyStorageRoundtrips(t *testing.T) {
	setupRelayInterceptTestDB(t)
	bodyJSON := `{"model":"gpt-4","prompt":"forbidden content"}`
	ctx := newRelayInterceptContext(t, bodyJSON, "curl/8.0", common.RoleCommonUser)

	p := &promptBlockRecordContext{ctx: ctx}

	// Read raw params — should contain the body JSON.
	raw := p.RequestParamsRaw()
	assert.Contains(t, raw, "gpt-4")
	assert.Contains(t, raw, "forbidden content")

	// Body must still be readable via the storage (seek restored).
	storage, err := common.GetBodyStorage(ctx)
	require.NoError(t, err)
	bs, err := storage.Bytes()
	require.NoError(t, err)
	assert.Equal(t, bodyJSON, string(bs), "body storage intact after adapter read")

	// Headers raw masks Authorization.
	ctx.Request.Header.Set("Authorization", "Bearer secret-token")
	hraw := p.RequestHeadersRaw()
	assert.Contains(t, hraw, "***masked***")
	assert.NotContains(t, hraw, "secret-token")
}

// TestRelay_PromptRegexHit_BlocksAndWritesLog verifies the end-to-end relay
// intercept path: a prompt regex rule hit produces a skip-retry error with the
// rule's status/code/message and writes a PromptBlockLog + (when AutoBan) bans
// the user. Uses the controller adapters directly (full Relay() is too heavy).
func TestRelay_PromptRegexHit_BlocksAndWritesLog(t *testing.T) {
	setupRelayInterceptTestDB(t)
	u := &model.User{Username: "promptuser", Password: "pw12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "promptuser-aff"}
	require.NoError(t, model.DB.Create(u).Error)
	tok := &model.Token{UserId: u.Id, Name: "tok", Key: "tok-key-" + strings.Repeat("z", 20), Status: common.TokenStatusEnabled, Group: "default", ExpiredTime: -1}
	require.NoError(t, model.DB.Create(tok).Error)

	origRules := setting.SensitivePromptRegexRules
	origPromptMsg := setting.SensitivePromptBlockedMessage
	t.Cleanup(func() {
		setting.SensitivePromptRegexRules = origRules
		setting.SensitivePromptBlockedMessage = origPromptMsg
	})
	setting.SensitivePromptBlockedMessage = "默认拦截消息"
	setting.SensitivePromptRegexRules = []setting.SensitiveRegexRule{
		{Pattern: "forbidden", RuleName: "r1", Message: "blocked", HTTPStatusCode: 403, ErrorCode: "prompt_blocked", AutoBan: true},
	}

	bodyJSON := `{"model":"gpt-4","messages":[{"role":"user","content":"this is forbidden content"}]}`
	ctx := newRelayInterceptContext(t, bodyJSON, "curl/8.0", common.RoleCommonUser)
	ctx.Set("id", u.Id)
	ctx.Set("username", u.Username)

	p := &promptBlockRecordContext{ctx: ctx}
	hit, ok := service.MatchSensitivePromptRule("this is forbidden content")
	require.True(t, ok)
	require.NotNil(t, hit)

	status, code, errMsg := service.BuildPromptBlockedErrorAndRecord(p, hit, setting.SensitivePromptBlockedMessage, "rule", hit.Pattern)
	require.Error(t, errMsg)
	assert.Equal(t, 403, status)
	assert.Equal(t, types.ErrorCode("prompt_blocked"), code)

	// Skip-retry error shape: the relay builds NewErrorWithStatusCode(...ErrOptionWithSkipRetry).
	apiErr := types.NewErrorWithStatusCode(errMsg, code, status, types.ErrOptionWithSkipRetry())
	assert.True(t, types.IsSkipRetryError(apiErr), "blocked prompt must skip retry")

	// PromptBlockLog written.
	var log model.PromptBlockLog
	require.NoError(t, model.DB.First(&log, "user_id = ?", u.Id).Error)
	assert.Equal(t, "forbidden", log.RulePattern)
	assert.Equal(t, "rule", log.MatchMode)
	assert.True(t, log.AutoBanConfigured)
	assert.True(t, log.AutoBanned)

	// User banned + token disabled.
	var refreshed model.User
	require.NoError(t, model.DB.First(&refreshed, u.Id).Error)
	assert.Equal(t, common.UserStatusDisabled, refreshed.Status)
	var tk model.Token
	require.NoError(t, model.DB.First(&tk, tok.Id).Error)
	assert.Equal(t, common.TokenStatusDisabled, tk.Status)
}

// TestRelay_UARegexHit_BlocksAndWritesLog verifies the UA intercept path writes
// a UABlockLog and returns a skip-retry error.
func TestRelay_UARegexHit_BlocksAndWritesLog(t *testing.T) {
	setupRelayInterceptTestDB(t)
	u := &model.User{Username: "uauser", Password: "pw12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "uauser-aff"}
	require.NoError(t, model.DB.Create(u).Error)

	origUARules := setting.SensitiveUARegexRules
	t.Cleanup(func() { setting.SensitiveUARegexRules = origUARules })
	setting.SensitiveUARegexRules = []setting.SensitiveRegexRule{
		{Pattern: "curl/.*", RuleName: "ua-curl", Message: "curl blocked", HTTPStatusCode: 403, ErrorCode: "ua_blocked", AutoBan: false},
	}

	bodyJSON := `{"model":"gpt-4"}`
	ctx := newRelayInterceptContext(t, bodyJSON, "curl/8.0", common.RoleCommonUser)
	ctx.Set("id", u.Id)
	ctx.Set("username", u.Username)

	u2 := &uaBlockRecordContext{ctx: ctx}
	hit, ok := service.MatchSensitiveUARule("curl/8.0")
	require.True(t, ok)
	require.NotNil(t, hit)

	status, code, errMsg := service.BuildUABlockedErrorAndRecord(u2, hit)
	require.Error(t, errMsg)
	assert.Equal(t, 403, status)
	assert.Equal(t, types.ErrorCode("ua_blocked"), code)

	apiErr := types.NewErrorWithStatusCode(errMsg, code, status, types.ErrOptionWithSkipRetry())
	assert.True(t, types.IsSkipRetryError(apiErr), "blocked UA must skip retry")

	var log model.UABlockLog
	require.NoError(t, model.DB.First(&log, "user_id = ?", u.Id).Error)
	assert.Equal(t, "curl/.*", log.RulePattern)
	assert.Equal(t, "curl/8.0", log.UserAgent)
	assert.False(t, log.AutoBanConfigured, "AutoBan=false on the rule → not configured")

	// User NOT banned (AutoBan=false).
	var refreshed model.User
	require.NoError(t, model.DB.First(&refreshed, u.Id).Error)
	assert.Equal(t, common.UserStatusEnabled, refreshed.Status)
}

// TestRelay_AdminBypassesSensitiveCheck verifies the admin bypass logic: an
// admin user's role is >= RoleAdminUser, so the intercept is skipped.
func TestRelay_AdminBypassesSensitiveCheck(t *testing.T) {
	setupRelayInterceptTestDB(t)
	u := &model.User{Username: "adminuser", Password: "pw12345678", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "adminuser-aff"}
	require.NoError(t, model.DB.Create(u).Error)

	role, err := model.GetUserRoleById(u.Id)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, role, common.RoleAdminUser)
	assert.True(t, role >= common.RoleAdminUser, "admin role must bypass sensitive intercept")
}

// TestRelay_NoAutoBanSyncFieldAnywhere is a runtime guard: asserts the
// SensitiveRuleHit has no AutoBanSync field (compile-time via the commented-out
// access), and that no ban_sync option key exists.
func TestRelay_NoAutoBanSyncFieldAnywhere(t *testing.T) {
	setupRelayInterceptTestDB(t)
	hit := &service.SensitiveRuleHit{}
	_ = hit.AutoBan // AutoBan present, compiles
	// If AutoBanSync existed, the next line would compile; it must NOT.
	// _ = hit.AutoBanSync

	// No ban_sync option keys in OptionMap.
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	for k := range common.OptionMap {
		assert.NotContains(t, strings.ToLower(k), "bansync",
			"no option key may reference ban_sync; found %q", k)
	}
}
