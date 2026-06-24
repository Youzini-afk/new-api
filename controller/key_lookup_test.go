package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type keyLookupResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type keyLookupTokenSummary struct {
	Key string `json:"key"`
}

func setupKeyLookupTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	gin.SetMode(gin.TestMode)
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainType := common.MainDatabaseType()
	originalLogType := common.LogDatabaseType()
	originalRedisEnabled := common.RedisEnabled

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	// 刷新方言相关列名（commonKeyCol 等），GetTokenByKey 依赖其拼接 SQL。
	model.InitColumnNames()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db

	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}))

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

func seedKeyLookupUser(t *testing.T, db *gorm.DB, username string, role int) *model.User {
	t.Helper()
	u := &model.User{
		Username: username,
		Password: "testpass123",
		Role:     role,
		Group:    "default",
		AffCode:  username + "-aff",
	}
	require.NoError(t, db.Create(u).Error)
	return u
}

func seedKeyLookupToken(t *testing.T, db *gorm.DB, user *model.User, name, rawKey string) *model.Token {
	t.Helper()
	token := &model.Token{
		UserId:         user.Id,
		Name:           name,
		Key:            rawKey,
		Status:         common.TokenStatusEnabled,
		CreatedTime:    1,
		AccessedTime:   1,
		ExpiredTime:    -1,
		RemainQuota:    100,
		UnlimitedQuota: true,
		Group:          "default",
	}
	require.NoError(t, db.Create(token).Error)
	return token
}

func newKeyLookupContext(t *testing.T, target string, callerRole int) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, target, nil)
	ctx.Set("role", callerRole)
	return ctx, recorder
}

func decodeKeyLookupResponse(t *testing.T, recorder *httptest.ResponseRecorder) keyLookupResponse {
	t.Helper()
	var resp keyLookupResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	return resp
}

func TestLookupByKey_RequiresKey(t *testing.T) {
	setupKeyLookupTestDB(t)

	ctx, recorder := newKeyLookupContext(t, "/api/key_lookup", common.RoleAdminUser)
	LookupByKey(ctx)

	resp := decodeKeyLookupResponse(t, recorder)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "key is required")
}

func TestLookupByKey_EmptyAfterTrim(t *testing.T) {
	setupKeyLookupTestDB(t)

	ctx, recorder := newKeyLookupContext(t, "/api/key_lookup/?key=+++", common.RoleAdminUser)
	LookupByKey(ctx)

	resp := decodeKeyLookupResponse(t, recorder)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "key is required")
}

func TestLookupByKey_StripsSkPrefixAndMasksKey(t *testing.T) {
	db := setupKeyLookupTestDB(t)
	user := seedKeyLookupUser(t, db, "lookup-user", common.RoleCommonUser)
	rawKey := "abcd1234efgh5678ijkl"
	token := seedKeyLookupToken(t, db, user, "lookup-token", rawKey)

	ctx, recorder := newKeyLookupContext(t, "/api/key_lookup/?key=sk-"+rawKey, common.RoleAdminUser)
	LookupByKey(ctx)

	resp := decodeKeyLookupResponse(t, recorder)
	require.True(t, resp.Success, "expected success, got message: %s", resp.Message)

	body := recorder.Body.String()
	// 绝不能泄露明文 key
	assert.NotContains(t, body, rawKey)
	// 应返回 masked key
	assert.Contains(t, body, token.GetMaskedKey())

	// 解析 token 摘要：key 字段应为 masked 形式
	var payload struct {
		Token     keyLookupTokenSummary `json:"token"`
		KeyMasked string                `json:"key_masked"`
	}
	require.NoError(t, common.Unmarshal(resp.Data, &payload))
	assert.Equal(t, token.GetMaskedKey(), payload.Token.Key)
	assert.Equal(t, token.GetMaskedKey(), payload.KeyMasked)
}

func TestLookupByKey_TokenNotFound(t *testing.T) {
	setupKeyLookupTestDB(t)

	ctx, recorder := newKeyLookupContext(t, "/api/key_lookup/?key=nonexistent-key-12345", common.RoleAdminUser)
	LookupByKey(ctx)

	resp := decodeKeyLookupResponse(t, recorder)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "token not found")
}

func TestLookupByKey_DeniesLowerRoleAccessingHigherRoleUser(t *testing.T) {
	db := setupKeyLookupTestDB(t)
	// target user is root; caller is admin (lower than root)
	target := seedKeyLookupUser(t, db, "root-user", common.RoleRootUser)
	rawKey := "rootkey1234abcd5678efgh"
	seedKeyLookupToken(t, db, target, "root-token", rawKey)

	ctx, recorder := newKeyLookupContext(t, "/api/key_lookup/?key="+rawKey, common.RoleAdminUser)
	LookupByKey(ctx)

	resp := decodeKeyLookupResponse(t, recorder)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "permission")
	// 拒绝路径下也不应泄露明文 key
	assert.NotContains(t, recorder.Body.String(), rawKey)
}

func TestLookupByKey_DeniesSameRoleAccess(t *testing.T) {
	db := setupKeyLookupTestDB(t)
	// caller and target are both common users; canManageTargetRole(1,1) == false
	target := seedKeyLookupUser(t, db, "peer-user", common.RoleCommonUser)
	rawKey := "peerkey1234abcd5678ijkl"
	seedKeyLookupToken(t, db, target, "peer-token", rawKey)

	ctx, recorder := newKeyLookupContext(t, "/api/key_lookup/?key="+rawKey, common.RoleCommonUser)
	LookupByKey(ctx)

	resp := decodeKeyLookupResponse(t, recorder)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "permission")
	assert.NotContains(t, recorder.Body.String(), rawKey)
}

func TestLookupByKey_RootCanAccessAnyRole(t *testing.T) {
	db := setupKeyLookupTestDB(t)
	target := seedKeyLookupUser(t, db, "admin-user", common.RoleAdminUser)
	rawKey := "adminkey1234abcd5678xy"
	seedKeyLookupToken(t, db, target, "admin-token", rawKey)

	ctx, recorder := newKeyLookupContext(t, "/api/key_lookup/?key="+rawKey, common.RoleRootUser)
	LookupByKey(ctx)

	resp := decodeKeyLookupResponse(t, recorder)
	require.True(t, resp.Success, "root should access admin user's token, got: %s", resp.Message)
	assert.NotContains(t, recorder.Body.String(), rawKey)
}
