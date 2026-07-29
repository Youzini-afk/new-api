package middleware

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/external_app_setting"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const (
	ExternalAppContextKey = "external_app_id"
	externalBodyLimit     = 64 * 1024
)

// BrowserSessionAuth authenticates a top-level browser redirect using the
// existing New API session cookie. Unlike UserAuth it intentionally does not
// require the JavaScript-only New-Api-User header, which a browser navigation
// cannot attach.
func BrowserSessionAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		userID, ok := session.Get("id").(int)
		if !ok || userID <= 0 {
			externalAuthError(c, http.StatusUnauthorized, "please sign in to New API first")
			return
		}
		user, err := model.GetUserCache(userID)
		if err != nil {
			externalAuthError(c, http.StatusInternalServerError, "failed to load the current user")
			return
		}
		if user.Status != common.UserStatusEnabled || !validUserInfo(user.Username, user.Role) {
			externalAuthError(c, http.StatusForbidden, "the current user cannot authorize this application")
			return
		}
		if user.Role < common.RoleAdminUser && discordSessionReauthRequired(userID) {
			session.Clear()
			_ = session.Save()
			externalAuthError(c, http.StatusUnauthorized, "Discord verification is required; please sign in again")
			return
		}
		c.Set("id", user.Id)
		c.Set("username", user.Username)
		c.Set("role", user.Role)
		c.Set("group", user.Group)
		c.Set("user_group", user.Group)
		user.WriteContext(c)
		c.Next()
	}
}

// ExternalAppHMAC verifies server-to-server requests from the configured game
// application. The signature covers the app id, timestamp, method, path, and
// exact request-body digest. Replay of a mutation is harmless because the
// controller additionally requires a durable operation id.
func ExternalAppHMAC() gin.HandlerFunc {
	return func(c *gin.Context) {
		settings := external_app_setting.GetSettings()
		if err := settings.Validate(); err != nil {
			externalAuthError(c, http.StatusServiceUnavailable, "external game integration is unavailable")
			return
		}
		appID := strings.TrimSpace(c.GetHeader("X-External-App"))
		if appID == "" || !hmac.Equal([]byte(appID), []byte(settings.AppId)) {
			externalAuthError(c, http.StatusUnauthorized, "invalid external application")
			return
		}
		timestampText := strings.TrimSpace(c.GetHeader("X-External-Timestamp"))
		timestamp, err := strconv.ParseInt(timestampText, 10, 64)
		if err != nil || absoluteInt64(time.Now().Unix()-timestamp) > int64(settings.SignatureToleranceSeconds) {
			externalAuthError(c, http.StatusUnauthorized, "external request timestamp is invalid or expired")
			return
		}
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, externalBodyLimit+1))
		if err != nil {
			externalAuthError(c, http.StatusBadRequest, "failed to read external request")
			return
		}
		if len(body) > externalBodyLimit {
			externalAuthError(c, http.StatusRequestEntityTooLarge, "external request body is too large")
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))

		provided, err := hex.DecodeString(strings.TrimSpace(c.GetHeader("X-External-Signature")))
		if err != nil {
			externalAuthError(c, http.StatusUnauthorized, "invalid external request signature")
			return
		}
		path := c.Request.URL.EscapedPath()
		expectedHex := BuildExternalAppSignature(appID, settings.AppSecret, timestampText, c.Request.Method, path, body)
		expected, _ := hex.DecodeString(expectedHex)
		if !hmac.Equal(provided, expected) {
			externalAuthError(c, http.StatusUnauthorized, "invalid external request signature")
			return
		}
		c.Set(ExternalAppContextKey, appID)
		c.Next()
	}
}

func BuildExternalAppSignature(appID, secret, timestamp, method, path string, body []byte) string {
	digest := sha256.Sum256(body)
	canonical := fmt.Sprintf("%s\n%s\n%s\n%s\n%s", appID, timestamp, strings.ToUpper(method), path, hex.EncodeToString(digest[:]))
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}

func externalAuthError(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, gin.H{"success": false, "message": message})
}

func absoluteInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
