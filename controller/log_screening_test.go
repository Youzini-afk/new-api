package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// logScreeningAPIResponse is the common envelope returned by the handlers.
type logScreeningAPIResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// setupLogScreeningTestDB wires an isolated in-memory SQLite DB for the
// controller tests and migrates the Phase 5 tables (+ users) the handlers need.
func setupLogScreeningTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)

	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainType := common.MainDatabaseType()
	originalLogType := common.LogDatabaseType()
	originalRedisEnabled := common.RedisEnabled

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	model.InitColumnNames()

	dsn := strings.ReplaceAll(t.Name(), "/", "_")
	db, err := gorm.Open(sqlite.Open("file:"+dsn+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db

	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.Option{},
		&model.Log{},
		&model.LogScreeningRecord{},
		&model.PromptBlockLog{},
		&model.UABlockLog{},
		&model.SuspiciousIPMark{},
	))

	// Ensure TranslateMessage is wired (auth middleware depends on it).
	if err := i18n.Init(); err != nil {
		// Non-fatal: messages fall back to keys; auth still aborts on rejection.
		t.Logf("i18n.Init returned %v (auth rejection still aborts)", err)
	}

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainType, originalLogType)
		common.RedisEnabled = originalRedisEnabled
		model.InitColumnNames()
	})
	return db
}

// newLogScreeningAdminContext builds a gin context whose request targets the
// given path and whose auth context is an admin operator.
func newLogScreeningAdminContext(t *testing.T, method, target string, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	var bodyReader *strings.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	var req *http.Request
	if bodyReader != nil {
		req = httptest.NewRequest(method, target, bodyReader)
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	ctx.Request = req
	// AdminAuth would have set these on a successful admin session.
	ctx.Set("id", 1)
	ctx.Set("role", common.RoleAdminUser)
	ctx.Set("username", "root")
	return ctx, recorder
}

// decodeLogScreeningResponse parses the handler envelope.
func decodeLogScreeningResponse(t *testing.T, recorder *httptest.ResponseRecorder) logScreeningAPIResponse {
	t.Helper()
	var resp logScreeningAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	return resp
}

func TestParseLogScreeningBoolQuery_Tristate(t *testing.T) {
	assert.Nil(t, parseLogScreeningBoolQuery(""))
	assert.Nil(t, parseLogScreeningBoolQuery("   "))

	v := parseLogScreeningBoolQuery("1")
	require.NotNil(t, v)
	assert.True(t, *v)

	v = parseLogScreeningBoolQuery("true")
	require.NotNil(t, v)
	assert.True(t, *v)

	v = parseLogScreeningBoolQuery("TRUE")
	require.NotNil(t, v)
	assert.True(t, *v)

	v = parseLogScreeningBoolQuery("0")
	require.NotNil(t, v)
	assert.False(t, *v)

	v = parseLogScreeningBoolQuery("false")
	require.NotNil(t, v)
	assert.False(t, *v)
}

// TestRunLogScreening_Handler_RunSummary verifies the POST /run handler returns
// a run summary envelope for an admin context. With no logs, the run completes
// with 0 records created.
func TestRunLogScreening_Handler_RunSummary(t *testing.T) {
	setupLogScreeningTestDB(t)

	ctx, recorder := newLogScreeningAdminContext(t, http.MethodPost, "/api/log_screening/run", `{"kind":"chat_completions"}`)
	RunLogScreening(ctx)

	resp := decodeLogScreeningResponse(t, recorder)
	require.True(t, resp.Success, "handler should succeed: %s", resp.Message)

	var summary struct {
		Kind           string `json:"kind"`
		Status         string `json:"status"`
		Enabled        bool   `json:"enabled"`
		RulesTotal     int    `json:"rules_total"`
		RecordsCreated int64  `json:"records_created"`
		Manual         bool   `json:"manual"`
	}
	require.NoError(t, common.Unmarshal(resp.Data, &summary))
	assert.Equal(t, "chat_completions", summary.Kind)
	assert.True(t, summary.Enabled, "default setting is enabled")
	assert.NotEqual(t, "shadow", summary.Status, "status must not be 'shadow' — real run")
	assert.True(t, summary.Manual, "manual trigger via the admin endpoint")
}

// TestListLogScreeningRecords_Handler_Pagination verifies the list handler
// returns paginated records with the standard PageInfo envelope.
func TestListLogScreeningRecords_Handler_Pagination(t *testing.T) {
	db := setupLogScreeningTestDB(t)
	u := &model.User{Username: "alice", Password: "pw12345678", Group: "default", AffCode: "alice-aff"}
	require.NoError(t, db.Create(u).Error)
	for i := 0; i < 3; i++ {
		_, err := model.UpsertLogScreeningRecord(context.Background(), &model.LogScreeningRecord{
			UserId: u.Id, RuleName: "rule_a_" + strconv.Itoa(i), Window: "1h", RequestPath: "all",
		})
		require.NoError(t, err)
	}

	ctx, recorder := newLogScreeningAdminContext(t, http.MethodGet, "/api/log_screening/records?page_size=2", "")
	ListLogScreeningRecords(ctx)

	resp := decodeLogScreeningResponse(t, recorder)
	require.True(t, resp.Success, resp.Message)

	var page struct {
		Page     int `json:"page"`
		PageSize int `json:"page_size"`
		Total    int `json:"total"`
		Items    []struct {
			UserId   int    `json:"user_id"`
			RuleName string `json:"rule_name"`
		} `json:"items"`
	}
	require.NoError(t, common.Unmarshal(resp.Data, &page))
	assert.Equal(t, 2, page.PageSize)
	assert.Equal(t, 3, page.Total)
	assert.Len(t, page.Items, 2)
}

// TestCleanupLogScreeningRecords_Handler verifies the cleanup handler returns
// the deleted count and removes only expired rows.
func TestCleanupLogScreeningRecords_Handler(t *testing.T) {
	db := setupLogScreeningTestDB(t)
	now := int64(2000000)
	require.NoError(t, db.Create(&model.LogScreeningRecord{UserId: 1, RuleName: "exp", Window: "1h", RequestPath: "all", ExpiresAt: now - 1}).Error)
	require.NoError(t, db.Create(&model.LogScreeningRecord{UserId: 1, RuleName: "keep", Window: "1h", RequestPath: "all", ExpiresAt: now + 1000}).Error)

	ctx, recorder := newLogScreeningAdminContext(t, http.MethodPost, "/api/log_screening/cleanup?now="+strconv.FormatInt(now, 10)+"&limit=100", "")
	CleanupLogScreeningRecords(ctx)

	resp := decodeLogScreeningResponse(t, recorder)
	require.True(t, resp.Success, resp.Message)
	var data struct {
		Deleted int64 `json:"deleted"`
	}
	require.NoError(t, common.Unmarshal(resp.Data, &data))
	assert.Equal(t, int64(1), data.Deleted)

	var remaining int64
	require.NoError(t, db.Model(&model.LogScreeningRecord{}).Count(&remaining).Error)
	assert.Equal(t, int64(1), remaining)
}

// TestAppendRemarkHandlers_RejectInvalidId verifies the remark handlers reject
// a missing/invalid record id before touching the DB.
func TestAppendRemarkHandlers_RejectInvalidId(t *testing.T) {
	setupLogScreeningTestDB(t)

	for name, handler := range map[string]gin.HandlerFunc{
		"screening": AppendLogScreeningRemark,
		"ua":        AppendUABlockLogRemark,
		"prompt":    AppendPromptBlockLogRemark,
	} {
		t.Run(name, func(t *testing.T) {
			ctx, recorder := newLogScreeningAdminContext(t, http.MethodPost, "/api/log_screening/records/0/remark", `{"remark":"x"}`)
			handler(ctx)
			resp := decodeLogScreeningResponse(t, recorder)
			assert.False(t, resp.Success)
			assert.Contains(t, resp.Message, "invalid record id")
		})
	}
}

// TestLogScreeningRoutes_RegisteredUnderAdminAuth builds a gin engine with the
// Phase 5 routes wired behind middleware.AdminAuth() and asserts:
//  1. all expected routes are registered (route exists), and
//  2. an unauthenticated request is rejected (non-admin forbidden).
func TestLogScreeningRoutes_RegisteredUnderAdminAuth(t *testing.T) {
	setupLogScreeningTestDB(t)

	r := gin.New()
	store := cookie.NewStore([]byte("log-screening-test-secret"))
	r.Use(sessions.Sessions("logscreentest", store))

	logScreeningRoute := r.Group("/api/log_screening")
	logScreeningRoute.Use(middleware.AdminAuth())
	{
		logScreeningRoute.GET("/records", ListLogScreeningRecords)
		logScreeningRoute.GET("/ua_block_logs", ListUABlockLogs)
		logScreeningRoute.GET("/ua_block_logs/:id", GetUABlockLogDetail)
		logScreeningRoute.GET("/prompt_block_logs", ListPromptBlockLogs)
		logScreeningRoute.GET("/prompt_block_logs/:id", GetPromptBlockLogDetail)
		logScreeningRoute.POST("/run", RunLogScreening)
		logScreeningRoute.POST("/records/:id/remark", AppendLogScreeningRemark)
		logScreeningRoute.POST("/ua_block_logs/:id/remark", AppendUABlockLogRemark)
		logScreeningRoute.POST("/prompt_block_logs/:id/remark", AppendPromptBlockLogRemark)
		logScreeningRoute.POST("/cleanup", CleanupLogScreeningRecords)
	}

	// 1) All expected routes exist.
	expected := map[string]string{
		"GET /api/log_screening/records":                   "GET",
		"GET /api/log_screening/ua_block_logs":             "GET",
		"GET /api/log_screening/ua_block_logs/:id":          "GET",
		"GET /api/log_screening/prompt_block_logs":         "GET",
		"GET /api/log_screening/prompt_block_logs/:id":     "GET",
		"POST /api/log_screening/run":                      "POST",
		"POST /api/log_screening/records/:id/remark":       "POST",
		"POST /api/log_screening/ua_block_logs/:id/remark":  "POST",
		"POST /api/log_screening/prompt_block_logs/:id/remark": "POST",
		"POST /api/log_screening/cleanup":                  "POST",
	}
	registered := make(map[string]bool)
	for _, ri := range r.Routes() {
		registered[ri.Method+" "+ri.Path] = true
	}
	for key := range expected {
		assert.True(t, registered[key], "expected route %q to be registered", key)
	}

	// 2) Unauthenticated request is rejected (no session, no access token).
	req := httptest.NewRequest(http.MethodPost, "/api/log_screening/run", nil)
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, req)

	var resp logScreeningAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	assert.False(t, resp.Success, "unauthenticated request must be rejected by AdminAuth")
}
