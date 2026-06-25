package oauth

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withDiscordLogBuffer swaps gin.DefaultErrorWriter for a buffer so tests can
// assert that sanitized diagnostics (and only sanitized diagnostics) reach the
// log. logger.LogError and logger.LogDebug both write to DefaultErrorWriter.
func withDiscordLogBuffer(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	common.LogWriterMu.Lock()
	oldWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &buf
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = oldWriter
		common.LogWriterMu.Unlock()
	})
	return &buf
}

// withDiscordDebugEnabled toggles common.DebugEnabled for the test and restores
// it on cleanup. Package globals must be t.Cleanup-restored per AGENTS.md.
func withDiscordDebugEnabled(t *testing.T, enabled bool) {
	t.Helper()
	oldDebug := common.DebugEnabled
	common.DebugEnabled = enabled
	t.Cleanup(func() { common.DebugEnabled = oldDebug })
}

// withDiscordOAuthServer wires discordAPIBaseURL/discordHTTPClient to a test
// server and seeds sane Discord OAuth credentials. It is a self-contained
// alternative to withDiscordMemberServer for tests that need to assert on the
// request the OAuth client sends.
func withDiscordOAuthServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	withDiscordSettings(t, func(settings *system_setting.DiscordSettings) {
		settings.ClientId = "test-client-id"
		settings.ClientSecret = "test-client-secret"
	})
	server := httptest.NewServer(handler)
	oldBaseURL := discordAPIBaseURL
	oldClient := discordHTTPClient
	discordAPIBaseURL = server.URL
	discordHTTPClient = server.Client()
	t.Cleanup(func() {
		server.Close()
		discordAPIBaseURL = oldBaseURL
		discordHTTPClient = oldClient
	})
}

// discordOAuthErrorAs asserts err is an *OAuthError and returns it.
func discordOAuthErrorAs(t *testing.T, err error) *OAuthError {
	t.Helper()
	var oauthErr *OAuthError
	require.ErrorAs(t, err, &oauthErr)
	return oauthErr
}

// TestDiscordExchangeToken_InvalidClientSanitized verifies that an
// invalid_client response whose body echoes secrets (worst case) does not
// leak client_secret, the OAuth code, or the response body into either the
// returned error or the log buffer.
func TestDiscordExchangeToken_InvalidClientSanitized(t *testing.T) {
	withDiscordDebugEnabled(t, true) // prove the old code-prefix debug log is gone
	logBuf := withDiscordLogBuffer(t)
	withDiscordOAuthServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/oauth2/token", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		// Discord would not normally echo secrets, but simulate the worst
		// case to prove sanitization.
		_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"client_secret=super-secret does not match code=leaked-auth-code"}`))
	})

	token, err := (&DiscordProvider{}).ExchangeToken(context.Background(), "leaked-auth-code", nil)
	require.Error(t, err)
	require.Nil(t, token)

	oauthErr := discordOAuthErrorAs(t, err)
	assert.Equal(t, i18n.MsgOAuthTokenFailed, oauthErr.MsgKey)
	assert.Equal(t, "Discord", oauthErr.Params["Provider"])
	assert.Contains(t, oauthErr.RawError, "category=invalid_client")
	assert.Contains(t, oauthErr.RawError, "status=401")
	assert.NotContains(t, oauthErr.RawError, "super-secret")
	assert.NotContains(t, oauthErr.RawError, "leaked-auth-code")
	assert.NotContains(t, oauthErr.RawError, "test-client-secret")

	logStr := logBuf.String()
	assert.Contains(t, logStr, "category=invalid_client")
	assert.Contains(t, logStr, "status=401")
	assert.NotContains(t, logStr, "super-secret")
	assert.NotContains(t, logStr, "leaked-auth-code")
	assert.NotContains(t, logStr, "test-client-secret")
}

// TestDiscordExchangeToken_RedirectURIMismatchSanitized verifies that an
// invalid_request error mentioning redirect_uri is classified as
// redirect_uri_mismatch without leaking the description text.
func TestDiscordExchangeToken_RedirectURIMismatchSanitized(t *testing.T) {
	withDiscordDebugEnabled(t, false)
	logBuf := withDiscordLogBuffer(t)
	withDiscordOAuthServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_request","error_description":"Invalid redirect_uri: mismatched value"}`))
	})

	_, err := (&DiscordProvider{}).ExchangeToken(context.Background(), "some-code", nil)
	require.Error(t, err)
	oauthErr := discordOAuthErrorAs(t, err)
	assert.Equal(t, i18n.MsgOAuthTokenFailed, oauthErr.MsgKey)
	assert.Contains(t, oauthErr.RawError, "category=redirect_uri_mismatch")
	assert.NotContains(t, oauthErr.RawError, "mismatched value")

	logStr := logBuf.String()
	assert.Contains(t, logStr, "category=redirect_uri_mismatch")
	assert.NotContains(t, logStr, "mismatched value")
}

// TestDiscordExchangeToken_InvalidGrantClassified verifies invalid_grant is
// classified correctly and stays sanitized.
func TestDiscordExchangeToken_InvalidGrantClassified(t *testing.T) {
	logBuf := withDiscordLogBuffer(t)
	withDiscordOAuthServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Invalid authorization code"}`))
	})

	_, err := (&DiscordProvider{}).ExchangeToken(context.Background(), "expired-code", nil)
	require.Error(t, err)
	oauthErr := discordOAuthErrorAs(t, err)
	assert.Equal(t, i18n.MsgOAuthTokenFailed, oauthErr.MsgKey)
	assert.Contains(t, oauthErr.RawError, "category=invalid_grant")
	assert.Contains(t, logBuf.String(), "category=invalid_grant")
}

// TestDiscordExchangeToken_BodyTooLargeDoesNotPanic verifies that an oversized
// success body is capped and reported as body_too_large without panicking.
func TestDiscordExchangeToken_BodyTooLargeDoesNotPanic(t *testing.T) {
	logBuf := withDiscordLogBuffer(t)
	oversized := strings.Repeat("x", discordResponseBodyLimit+256)
	withDiscordOAuthServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(oversized))
	})

	require.NotPanics(t, func() {
		_, err := (&DiscordProvider{}).ExchangeToken(context.Background(), "code", nil)
		require.Error(t, err)
		oauthErr := discordOAuthErrorAs(t, err)
		assert.Contains(t, oauthErr.RawError, "category=body_too_large")
	})
	assert.Contains(t, logBuf.String(), "category=body_too_large")
}

// TestDiscordExchangeToken_RateLimitedDiagnostic verifies the 429 path
// captures Retry-After and never sleeps or retries.
func TestDiscordExchangeToken_RateLimitedDiagnostic(t *testing.T) {
	logBuf := withDiscordLogBuffer(t)
	withDiscordOAuthServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate_limited","retry_after":5,"global":false}`))
	})

	_, err := (&DiscordProvider{}).ExchangeToken(context.Background(), "code", nil)
	require.Error(t, err)
	oauthErr := discordOAuthErrorAs(t, err)
	assert.Equal(t, i18n.MsgOAuthTokenFailed, oauthErr.MsgKey)
	assert.Contains(t, oauthErr.RawError, "category=rate_limited")
	assert.Contains(t, oauthErr.RawError, "retry_after=5s")

	logStr := logBuf.String()
	assert.Contains(t, logStr, "category=rate_limited")
	assert.Contains(t, logStr, "retry_after=5s")
}

// TestDiscordExchangeToken_TimeoutDiagnostic verifies a transport timeout is
// classified as timeout (not the generic network_error bucket) and that the
// diagnostic carries no secrets. The server-side delay is a fixture that
// simulates a slow upstream so the client's Timeout fires; the test logic
// itself does not sleep on timing.
func TestDiscordExchangeToken_TimeoutDiagnostic(t *testing.T) {
	logBuf := withDiscordLogBuffer(t)
	withDiscordSettings(t, func(settings *system_setting.DiscordSettings) {
		settings.ClientId = "test-client-id"
		settings.ClientSecret = "test-client-secret"
	})
	// A POST request body prevents the client timeout from canceling the
	// server's request context (the connection stays half-open while the
	// body is being read), so the handler must return on its own. Keep the
	// server-side delay short so t.Cleanup does not stall.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	oldBaseURL := discordAPIBaseURL
	oldClient := discordHTTPClient
	discordAPIBaseURL = server.URL
	discordHTTPClient = &http.Client{Timeout: 50 * time.Millisecond}
	t.Cleanup(func() {
		server.Close()
		discordAPIBaseURL = oldBaseURL
		discordHTTPClient = oldClient
	})

	_, err := (&DiscordProvider{}).ExchangeToken(context.Background(), "code", nil)
	require.Error(t, err)
	oauthErr := discordOAuthErrorAs(t, err)
	assert.Equal(t, i18n.MsgOAuthConnectFailed, oauthErr.MsgKey)
	assert.Contains(t, oauthErr.RawError, "category=timeout")
	assert.NotContains(t, oauthErr.RawError, "test-client-secret")

	logStr := logBuf.String()
	assert.Contains(t, logStr, "category=timeout")
	assert.NotContains(t, logStr, "test-client-secret")
}

// TestDiscordGetUserInfo_UnauthorizedSanitized verifies that a 401 from
// /users/@me produces a generic user-facing error and that the access token
// (and any echoed body content) never reaches the error or the log.
func TestDiscordGetUserInfo_UnauthorizedSanitized(t *testing.T) {
	logBuf := withDiscordLogBuffer(t)
	withDiscordOAuthServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/users/@me", r.URL.Path)
		require.Equal(t, "Bearer leaked-access-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		// Worst case: body echoes the token.
		_, _ = w.Write([]byte(`{"code":0,"message":"401: Unauthorized: token=leaked-access-token"}`))
	})

	user, err := (&DiscordProvider{}).GetUserInfo(context.Background(), &OAuthToken{AccessToken: "leaked-access-token"})
	require.Error(t, err)
	require.Nil(t, user)
	oauthErr := discordOAuthErrorAs(t, err)
	assert.Equal(t, i18n.MsgOAuthGetUserErr, oauthErr.MsgKey)
	assert.Contains(t, oauthErr.RawError, "category=unauthorized")
	assert.NotContains(t, oauthErr.RawError, "leaked-access-token")

	logStr := logBuf.String()
	assert.Contains(t, logStr, "category=unauthorized")
	assert.NotContains(t, logStr, "leaked-access-token")
}

// TestDiscordGetUserInfo_RateLimitedSanitized verifies the 429 path on
// /users/@me captures Retry-After and stays sanitized.
func TestDiscordGetUserInfo_RateLimitedSanitized(t *testing.T) {
	logBuf := withDiscordLogBuffer(t)
	withDiscordOAuthServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"retry_after":3,"global":true,"message":"rate limited"}`))
	})

	_, err := (&DiscordProvider{}).GetUserInfo(context.Background(), &OAuthToken{AccessToken: "leaked-access-token"})
	require.Error(t, err)
	oauthErr := discordOAuthErrorAs(t, err)
	assert.Equal(t, i18n.MsgOAuthGetUserErr, oauthErr.MsgKey)
	assert.Contains(t, oauthErr.RawError, "category=rate_limited")
	assert.Contains(t, oauthErr.RawError, "retry_after=3s")
	assert.NotContains(t, oauthErr.RawError, "leaked-access-token")

	logStr := logBuf.String()
	assert.Contains(t, logStr, "category=rate_limited")
	assert.NotContains(t, logStr, "leaked-access-token")
}

// TestDiscordGetUserInfo_ForbiddenClassified verifies 403 is mapped to
// forbidden (treated as an unknown/auth-style failure, not "not a member").
func TestDiscordGetUserInfo_ForbiddenClassified(t *testing.T) {
	logBuf := withDiscordLogBuffer(t)
	withDiscordOAuthServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})

	_, err := (&DiscordProvider{}).GetUserInfo(context.Background(), &OAuthToken{AccessToken: "tok"})
	require.Error(t, err)
	oauthErr := discordOAuthErrorAs(t, err)
	assert.Contains(t, oauthErr.RawError, "category=forbidden")
	assert.Contains(t, logBuf.String(), "category=forbidden")
}

// TestDiscordGetUserInfo_BodyTooLargeDoesNotPanic verifies an oversized
// /users/@me success body is capped without panic.
func TestDiscordGetUserInfo_BodyTooLargeDoesNotPanic(t *testing.T) {
	logBuf := withDiscordLogBuffer(t)
	oversized := strings.Repeat("x", discordResponseBodyLimit+256)
	withDiscordOAuthServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(oversized))
	})

	require.NotPanics(t, func() {
		_, err := (&DiscordProvider{}).GetUserInfo(context.Background(), &OAuthToken{AccessToken: "tok"})
		require.Error(t, err)
		oauthErr := discordOAuthErrorAs(t, err)
		assert.Contains(t, oauthErr.RawError, "category=body_too_large")
	})
	assert.Contains(t, logBuf.String(), "category=body_too_large")
}

// TestDiscordDiagnosticRawFormat verifies the diagnostic formatter produces
// the compact, secret-free format callers and tests rely on.
func TestDiscordDiagnosticRawFormat(t *testing.T) {
	assert.Equal(t, "category=invalid_client", discordDiagnostic{Category: discordCategoryInvalidClient}.Raw())
	assert.Equal(t, "category=rate_limited status=429 retry_after=5s",
		discordDiagnostic{Category: discordCategoryRateLimited, Status: 429, RetryAfter: 5 * time.Second}.Raw())
}

// TestParseDiscordRetryAfter covers the parser's branches without exercising
// real HTTP timing.
func TestParseDiscordRetryAfter(t *testing.T) {
	assert.Equal(t, time.Duration(0), parseDiscordRetryAfter(""))
	assert.Equal(t, time.Duration(0), parseDiscordRetryAfter("not-a-number"))
	assert.Equal(t, 5*time.Second, parseDiscordRetryAfter("5"))
	assert.Equal(t, discordRetryAfterClamp, parseDiscordRetryAfter("999999"))
	assert.Equal(t, time.Duration(0), parseDiscordRetryAfter("-3"))
}
