package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupExternalAppControllerTest(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	originalDB := model.DB
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.ExternalAppAuthCode{}))
	model.DB = db
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		model.DB = originalDB
	})

	t.Setenv("EXTERNAL_GAME_ENABLED", "true")
	t.Setenv("EXTERNAL_GAME_APP_ID", "wtfib")
	t.Setenv("EXTERNAL_GAME_APP_SECRET", "test-secret-with-at-least-16-chars")
	t.Setenv("EXTERNAL_GAME_REDIRECT_URI", "https://stocks.example.com/login?source=main")
	t.Setenv("EXTERNAL_GAME_CODE_TTL_SECONDS", "120")
	t.Setenv("EXTERNAL_GAME_SIGNATURE_TOLERANCE_SECONDS", "300")
	return db
}

func TestExternalAppAuthorizeEchoesStateAndUsesConfiguredRedirect(t *testing.T) {
	db := setupExternalAppControllerTest(t)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet,
		"/api/external-app/authorize?app_id=wtfib&state=state-123456", nil)
	ctx.Set("id", 123)

	ExternalAppAuthorize(ctx)

	require.Equal(t, http.StatusFound, recorder.Code)
	location, err := url.Parse(recorder.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "https", location.Scheme)
	assert.Equal(t, "stocks.example.com", location.Host)
	assert.Equal(t, "/login", location.Path)
	assert.Equal(t, "main", location.Query().Get("source"))
	assert.Equal(t, "new-api", location.Query().Get("provider"))
	assert.Equal(t, "state-123456", location.Query().Get("state"))
	assert.NotEmpty(t, location.Query().Get("code"))

	var record model.ExternalAppAuthCode
	require.NoError(t, db.First(&record).Error)
	assert.Equal(t, "wtfib", record.AppId)
	assert.Equal(t, 123, record.UserId)
	assert.NotEqual(t, location.Query().Get("code"), record.CodeHash)
}

func TestExternalAppAuthorizeRejectsMissingState(t *testing.T) {
	db := setupExternalAppControllerTest(t)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet,
		"/api/external-app/authorize?app_id=wtfib", nil)
	ctx.Set("id", 123)

	ExternalAppAuthorize(ctx)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	var count int64
	require.NoError(t, db.Model(&model.ExternalAppAuthCode{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestExternalAppExchangeTokenResponseMatchesGameContract(t *testing.T) {
	db := setupExternalAppControllerTest(t)
	user := &model.User{
		Username:    "main-user",
		DisplayName: "Main User",
		Password:    "test-password",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AffCode:     "external-contract-user",
		Quota:       1234567,
	}
	require.NoError(t, db.Create(user).Error)
	code, _, err := model.CreateExternalAppAuthCode("wtfib", user.Id, 2*time.Minute)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/external-app/token",
		bytes.NewBufferString(`{"code":"`+code+`"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set(middleware.ExternalAppContextKey, "wtfib")

	ExternalAppExchangeToken(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool             `json:"success"`
		Data    externalIdentity `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Equal(t, user.Id, response.Data.UserId)
	assert.Equal(t, "main-user", response.Data.Username)
	assert.Equal(t, "Main User", response.Data.DisplayName)
	assert.Equal(t, 1234567, response.Data.Quota)
	assert.Equal(t, int(common.QuotaPerUnit), response.Data.QuotaPerUnit)
}
