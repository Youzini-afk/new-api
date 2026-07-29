package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const externalAppTestSecret = "test-secret-with-more-than-16-characters"

func setupExternalAppMiddlewareEnv(t *testing.T) {
	t.Helper()
	t.Setenv("EXTERNAL_GAME_ENABLED", "true")
	t.Setenv("EXTERNAL_GAME_APP_ID", "wtfib")
	t.Setenv("EXTERNAL_GAME_APP_SECRET", externalAppTestSecret)
	t.Setenv("EXTERNAL_GAME_REDIRECT_URI", "https://game.example/login")
	t.Setenv("EXTERNAL_GAME_SIGNATURE_TOLERANCE_SECONDS", "300")
}

func signedExternalAppRequest(t *testing.T, timestamp string, body []byte) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/signed", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-External-App", "wtfib")
	request.Header.Set("X-External-Timestamp", timestamp)
	request.Header.Set("X-External-Signature", BuildExternalAppSignature("wtfib", externalAppTestSecret, timestamp, http.MethodPost, "/signed", body))
	return request
}

func TestExternalAppHMACAcceptsValidRequestAndRestoresBody(t *testing.T) {
	setupExternalAppMiddlewareEnv(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/signed", ExternalAppHMAC(), func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		assert.Equal(t, `{"operation_id":"deposit-1"}`, string(body))
		assert.Equal(t, "wtfib", c.GetString(ExternalAppContextKey))
		c.Status(http.StatusNoContent)
	})

	body := []byte(`{"operation_id":"deposit-1"}`)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, signedExternalAppRequest(t, timestamp, body))
	assert.Equal(t, http.StatusNoContent, recorder.Code)
}

func TestExternalAppHMACRejectsTamperedBody(t *testing.T) {
	setupExternalAppMiddlewareEnv(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/signed", ExternalAppHMAC(), func(c *gin.Context) { c.Status(http.StatusNoContent) })

	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	request := signedExternalAppRequest(t, timestamp, []byte(`{"amount":100}`))
	request.Body = io.NopCloser(bytes.NewReader([]byte(`{"amount":101}`)))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestExternalAppHMACRejectsExpiredTimestamp(t *testing.T) {
	setupExternalAppMiddlewareEnv(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/signed", ExternalAppHMAC(), func(c *gin.Context) { c.Status(http.StatusNoContent) })

	timestamp := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, signedExternalAppRequest(t, timestamp, []byte(`{}`)))
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestBuildExternalAppSignatureCrossLanguageVector(t *testing.T) {
	body := []byte(`{"operation_id":"deposit-1","user_id":7,"amount":500000}`)
	signature := BuildExternalAppSignature(
		"wtfib",
		externalAppTestSecret,
		"1700000000",
		http.MethodPost,
		"/api/external-app/quota/debit",
		body,
	)
	assert.Equal(t, "be4e517f2572cc02e24624695530ea8eb6a683975c683c26c7f5320e2aa2b744", signature)
}
