package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func withDiscordGateRecheckDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}))
	original := model.DB
	originalRedisEnabled := common.RedisEnabled
	model.DB = db
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB = original
		common.RedisEnabled = originalRedisEnabled
	})
}

func createDiscordGateUser(t *testing.T, user model.User) *model.User {
	t.Helper()
	if user.Username == "" {
		user.Username = "discord_user"
	}
	if user.Password == "" {
		user.Password = "password_hash"
	}
	require.NoError(t, model.DB.Create(&user).Error)
	return &user
}

func encryptedDiscordRefreshToken(t *testing.T, refreshToken string) string {
	t.Helper()
	encrypted, err := common.EncryptWithCryptoSecret(refreshToken)
	require.NoError(t, err)
	return encrypted
}

func withDiscordTokenAndMemberServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	withDiscordMemberServer(t, handler)
}

func TestRecheckDiscordGate_ExemptDoesNotCallDiscord(t *testing.T) {
	withDiscordGateRecheckDB(t)
	user := createDiscordGateUser(t, model.User{DiscordGateExempt: true, DiscordGatePassed: false})
	called := false
	withDiscordTokenAndMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusInternalServerError)
	})

	outcome, err := RecheckDiscordGate(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, discordGateResultExempt, outcome.Result)
	assert.False(t, outcome.GatePassed)
	assert.True(t, outcome.Exempt)
	assert.False(t, called, "exempt recheck must not call Discord")
}

func TestRecheckDiscordGate_MissingRefreshTokenReauthRequired(t *testing.T) {
	withDiscordGateRecheckDB(t)
	user := createDiscordGateUser(t, model.User{DiscordId: "discord-1", DiscordGatePassed: true})

	outcome, err := RecheckDiscordGate(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, discordGateResultReauthRequired, outcome.Result)
	assert.Equal(t, "missing_refresh_token", outcome.Reason)
	assert.False(t, outcome.GatePassed)
}

func TestRecheckDiscordGate_InvalidGrantClearsToken(t *testing.T) {
	withDiscordGateRecheckDB(t)
	user := createDiscordGateUser(t, model.User{DiscordId: "discord-1", DiscordRefreshToken: encryptedDiscordRefreshToken(t, "old-refresh"), DiscordGatePassed: true})
	withDiscordTokenAndMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/oauth2/token", r.URL.Path)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	})

	outcome, err := RecheckDiscordGate(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, discordGateResultReauthRequired, outcome.Result)
	assert.Equal(t, "invalid_grant", outcome.Reason)
	assert.False(t, outcome.GatePassed)

	stored, err := model.GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.Empty(t, stored.DiscordRefreshToken)
}

func TestRecheckDiscordGate_RefreshSuccessGatePassSavesNewToken(t *testing.T) {
	withDiscordGateRecheckDB(t)
	withDiscordSettings(t, func(settings *system_setting.DiscordSettings) {
		settings.RegisterGate = discordGateConfig("guild-1", "role-1")
	})
	user := createDiscordGateUser(t, model.User{DiscordId: "discord-1", DiscordRefreshToken: encryptedDiscordRefreshToken(t, "old-refresh")})
	withDiscordTokenAndMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			_, _ = w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","token_type":"Bearer"}`))
		case "/users/@me":
			assert.Equal(t, "Bearer new-access", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"id":"discord-1","username":"remote-user","global_name":"Remote User","discriminator":"1234","avatar":"avatar-hash"}`))
		case "/users/@me/guilds/guild-1/member":
			assert.Equal(t, "Bearer new-access", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(discordTestMemberJSON(time.Now().Add(-24*time.Hour), "role-1")))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	outcome, err := RecheckDiscordGate(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, discordGateResultPass, outcome.Result)
	assert.True(t, outcome.GatePassed)
	assert.Empty(t, outcome.Message)

	stored, err := model.GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.Equal(t, discordGateResultPass, stored.DiscordLastCheckResult)
	assert.True(t, stored.DiscordGatePassed)
	assert.Equal(t, "remote-user", stored.DiscordUsername)
	assert.Equal(t, "Remote User", stored.DiscordGlobalName)
	assert.Equal(t, "1234", stored.DiscordDiscriminator)
	assert.Equal(t, "avatar-hash", stored.DiscordAvatarHash)
	assert.NotZero(t, stored.DiscordProfileSyncedAt)
	decrypted, err := common.DecryptWithCryptoSecret(stored.DiscordRefreshToken)
	require.NoError(t, err)
	assert.Equal(t, "new-refresh", decrypted)
}

func TestRecheckDiscordGate_ProfileFetchFailureDoesNotChangeGateOutcome(t *testing.T) {
	withDiscordGateRecheckDB(t)
	withDiscordSettings(t, func(settings *system_setting.DiscordSettings) {
		settings.RegisterGate = discordGateConfig("guild-1", "role-1")
	})
	user := createDiscordGateUser(t, model.User{
		DiscordId:              "discord-1",
		DiscordRefreshToken:    encryptedDiscordRefreshToken(t, "refresh"),
		DiscordGatePassed:      true,
		DiscordUsername:        "old-user",
		DiscordGlobalName:      "Old User",
		DiscordDiscriminator:   "0001",
		DiscordAvatarHash:      "old-avatar",
		DiscordProfileSyncedAt: 100,
	})
	withDiscordTokenAndMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			_, _ = w.Write([]byte(`{"access_token":"access"}`))
		case "/users/@me":
			w.WriteHeader(http.StatusServiceUnavailable)
		case "/users/@me/guilds/guild-1/member":
			_, _ = w.Write([]byte(discordTestMemberJSON(time.Now().Add(-24*time.Hour), "role-1")))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	outcome, err := RecheckDiscordGate(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, discordGateResultPass, outcome.Result)
	assert.True(t, outcome.GatePassed)

	stored, err := model.GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.True(t, stored.DiscordGatePassed)
	assert.Equal(t, discordGateResultPass, stored.DiscordLastCheckResult)
	assert.Equal(t, "old-user", stored.DiscordUsername)
	assert.Equal(t, "Old User", stored.DiscordGlobalName)
	assert.Equal(t, "0001", stored.DiscordDiscriminator)
	assert.Equal(t, "old-avatar", stored.DiscordAvatarHash)
	assert.Equal(t, int64(100), stored.DiscordProfileSyncedAt)
}

func TestRecheckDiscordGate_DenyAndBanClearGatePassed(t *testing.T) {
	for name, tc := range map[string]struct {
		roles  []string
		result string
	}{
		"deny": {roles: []string{"other-role"}, result: discordGateResultDeny},
		"ban":  {roles: []string{"role-1", "ban-role"}, result: discordGateResultBan},
	} {
		t.Run(name, func(t *testing.T) {
			withDiscordGateRecheckDB(t)
			withDiscordSettings(t, func(settings *system_setting.DiscordSettings) {
				settings.RegisterGate = system_setting.DiscordRegisterGateConfig{
					Groups:    []system_setting.DiscordGateGroup{{Rules: []system_setting.DiscordGateRule{{GuildID: "guild-1", RoleIDs: []string{"role-1"}}}}},
					BanGroups: []system_setting.DiscordGateGroup{{Rules: []system_setting.DiscordGateRule{{GuildID: "guild-1", RoleIDs: []string{"ban-role"}}}}},
				}
				system_setting.NormalizeDiscordRegisterGate(&settings.RegisterGate)
			})
			user := createDiscordGateUser(t, model.User{DiscordId: "discord-1", DiscordRefreshToken: encryptedDiscordRefreshToken(t, "refresh"), DiscordGatePassed: true})
			withDiscordTokenAndMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/oauth2/token" {
					_, _ = w.Write([]byte(`{"access_token":"access"}`))
					return
				}
				_, _ = w.Write([]byte(discordTestMemberJSON(time.Now(), tc.roles...)))
			})

			outcome, err := RecheckDiscordGate(context.Background(), user)
			require.NoError(t, err)
			assert.Equal(t, tc.result, outcome.Result)
			assert.False(t, outcome.GatePassed)
		})
	}
}

func TestRecheckDiscordGate_UnknownDoesNotClearExistingPass(t *testing.T) {
	withDiscordGateRecheckDB(t)
	withDiscordSettings(t, func(settings *system_setting.DiscordSettings) {
		settings.RegisterGate = discordGateConfig("guild-1", "role-1")
	})
	user := createDiscordGateUser(t, model.User{DiscordId: "discord-1", DiscordRefreshToken: encryptedDiscordRefreshToken(t, "refresh"), DiscordGatePassed: true})
	withDiscordTokenAndMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			_, _ = w.Write([]byte(`{"access_token":"access"}`))
			return
		}
		w.WriteHeader(http.StatusTooManyRequests)
	})

	outcome, err := RecheckDiscordGate(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, discordGateResultUnknown, outcome.Result)
	assert.True(t, outcome.GatePassed)

	stored, err := model.GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.True(t, stored.DiscordGatePassed)
	assert.Equal(t, discordGateResultUnknown, stored.DiscordLastCheckResult)
}

func TestRecheckDiscordGate_RefreshTransientErrorDoesNotClearExistingPass(t *testing.T) {
	withDiscordGateRecheckDB(t)
	withDiscordSettings(t, func(settings *system_setting.DiscordSettings) {
		settings.RegisterGate = discordGateConfig("guild-1", "role-1")
	})
	user := createDiscordGateUser(t, model.User{DiscordId: "discord-1", DiscordRefreshToken: encryptedDiscordRefreshToken(t, "refresh"), DiscordGatePassed: true})
	withDiscordTokenAndMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"temporarily_unavailable"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	outcome, err := RecheckDiscordGate(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, discordGateResultUnknown, outcome.Result)
	assert.Equal(t, "refresh_failed", outcome.Reason)
	assert.True(t, outcome.GatePassed)

	stored, err := model.GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.True(t, stored.DiscordGatePassed)
	assert.Equal(t, discordGateResultUnknown, stored.DiscordLastCheckResult)
}

func TestRecheckDiscordGate_InvalidConfigFailsClosed(t *testing.T) {
	withDiscordGateRecheckDB(t)
	withDiscordSettings(t, func(settings *system_setting.DiscordSettings) {
		settings.RegisterGate = system_setting.DiscordRegisterGateConfig{}
	})
	user := createDiscordGateUser(t, model.User{DiscordId: "discord-1", DiscordRefreshToken: encryptedDiscordRefreshToken(t, "refresh"), DiscordGatePassed: true})
	withDiscordTokenAndMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			_, _ = w.Write([]byte(`{"access_token":"access"}`))
		case "/users/@me":
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	outcome, err := RecheckDiscordGate(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, discordGateResultError, outcome.Result)
	assert.Equal(t, "invalid_config", outcome.Reason)
	assert.False(t, outcome.GatePassed)

	stored, err := model.GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.False(t, stored.DiscordGatePassed)
	assert.Equal(t, discordGateResultError, stored.DiscordLastCheckResult)
}

func TestForceDiscordGateReauthDoesNotClearDiscordID(t *testing.T) {
	withDiscordGateRecheckDB(t)
	user := createDiscordGateUser(t, model.User{DiscordId: "discord-1", DiscordRefreshToken: encryptedDiscordRefreshToken(t, "refresh"), DiscordGatePassed: true})

	outcome, err := ForceDiscordGateReauth(user)
	require.NoError(t, err)
	assert.Equal(t, discordGateResultReauthRequired, outcome.Result)
	assert.False(t, outcome.GatePassed)

	stored, err := model.GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.Equal(t, "discord-1", stored.DiscordId)
	assert.Empty(t, stored.DiscordRefreshToken)
	assert.False(t, stored.DiscordGatePassed)
}

func TestDiscordGateTruncateRunes(t *testing.T) {
	assert.Equal(t, "ab", truncateRunes(" abc ", 2))
	assert.Equal(t, strings.Repeat("界", 3), truncateRunes(strings.Repeat("界", 5), 3))
}

// TestRecheckDiscordGate_RefreshInvalidClientPreservesToken proves that only an
// explicit invalid_grant clears the refresh token; an invalid_client response
// (e.g., a rotated client_secret) must leave the token and the existing gate
// pass intact so the user is not forced through reauth on a transient config
// drift.
func TestRecheckDiscordGate_RefreshInvalidClientPreservesToken(t *testing.T) {
	withDiscordGateRecheckDB(t)
	withDiscordSettings(t, func(settings *system_setting.DiscordSettings) {
		settings.RegisterGate = discordGateConfig("guild-1", "role-1")
	})
	logBuf := withDiscordLogBuffer(t)
	encryptedRefreshToken := encryptedDiscordRefreshToken(t, "old-refresh")
	user := createDiscordGateUser(t, model.User{
		DiscordId:           "discord-1",
		DiscordRefreshToken: encryptedRefreshToken,
		DiscordGatePassed:   true,
	})
	withDiscordTokenAndMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/oauth2/token", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"client_secret=super-secret mismatch"}`))
	})

	outcome, err := RecheckDiscordGate(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, discordGateResultUnknown, outcome.Result)
	assert.Equal(t, "refresh_failed", outcome.Reason)
	assert.True(t, outcome.GatePassed, "existing gate pass must survive invalid_client")

	stored, err := model.GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.True(t, stored.DiscordGatePassed)
	decrypted, err := common.DecryptWithCryptoSecret(stored.DiscordRefreshToken)
	require.NoError(t, err)
	assert.Equal(t, "old-refresh", decrypted, "refresh token must NOT be cleared on invalid_client")

	logStr := logBuf.String()
	assert.Contains(t, logStr, "category=invalid_client")
	assert.NotContains(t, logStr, "super-secret")
	assert.NotContains(t, logStr, "old-refresh")
	assert.NotContains(t, logStr, encryptedRefreshToken)
}

// TestRecheckDiscordGate_RefreshRateLimitedPreservesToken proves a 429 from the
// token endpoint (with Retry-After) does not clear the refresh token or the
// existing gate pass.
func TestRecheckDiscordGate_RefreshRateLimitedPreservesToken(t *testing.T) {
	withDiscordGateRecheckDB(t)
	withDiscordSettings(t, func(settings *system_setting.DiscordSettings) {
		settings.RegisterGate = discordGateConfig("guild-1", "role-1")
	})
	user := createDiscordGateUser(t, model.User{
		DiscordId:           "discord-1",
		DiscordRefreshToken: encryptedDiscordRefreshToken(t, "old-refresh"),
		DiscordGatePassed:   true,
	})
	withDiscordTokenAndMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/oauth2/token", r.URL.Path)
		w.Header().Set("Retry-After", "10")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate_limited","retry_after":10,"global":true}`))
	})

	outcome, err := RecheckDiscordGate(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, discordGateResultUnknown, outcome.Result)
	assert.Equal(t, "refresh_failed", outcome.Reason)
	assert.True(t, outcome.GatePassed, "existing gate pass must survive 429")

	stored, err := model.GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.True(t, stored.DiscordGatePassed)
	decrypted, err := common.DecryptWithCryptoSecret(stored.DiscordRefreshToken)
	require.NoError(t, err)
	assert.Equal(t, "old-refresh", decrypted, "refresh token must NOT be cleared on 429")
}

// TestRecheckDiscordGate_RefreshTimeoutPreservesToken proves token endpoint
// timeouts are treated as transient unknowns and do not clear the refresh token
// or an existing gate pass.
func TestRecheckDiscordGate_RefreshTimeoutPreservesToken(t *testing.T) {
	withDiscordGateRecheckDB(t)
	withDiscordSettings(t, func(settings *system_setting.DiscordSettings) {
		settings.RegisterGate = discordGateConfig("guild-1", "role-1")
	})
	user := createDiscordGateUser(t, model.User{
		DiscordId:           "discord-1",
		DiscordRefreshToken: encryptedDiscordRefreshToken(t, "old-refresh"),
		DiscordGatePassed:   true,
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/oauth2/token", r.URL.Path)
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{"access_token":"late-access"}`))
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

	outcome, err := RecheckDiscordGate(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, discordGateResultUnknown, outcome.Result)
	assert.Equal(t, "refresh_failed", outcome.Reason)
	assert.True(t, outcome.GatePassed, "existing gate pass must survive timeout")

	stored, err := model.GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.True(t, stored.DiscordGatePassed)
	decrypted, err := common.DecryptWithCryptoSecret(stored.DiscordRefreshToken)
	require.NoError(t, err)
	assert.Equal(t, "old-refresh", decrypted, "refresh token must NOT be cleared on timeout")
}

// TestRecheckDiscordGate_RefreshBodyTooLargePreservesToken proves an oversized
// response body from the token endpoint is capped without panic and does not
// clear the refresh token.
func TestRecheckDiscordGate_RefreshBodyTooLargePreservesToken(t *testing.T) {
	withDiscordGateRecheckDB(t)
	withDiscordSettings(t, func(settings *system_setting.DiscordSettings) {
		settings.RegisterGate = discordGateConfig("guild-1", "role-1")
	})
	user := createDiscordGateUser(t, model.User{
		DiscordId:           "discord-1",
		DiscordRefreshToken: encryptedDiscordRefreshToken(t, "old-refresh"),
		DiscordGatePassed:   true,
	})
	oversized := strings.Repeat("x", discordResponseBodyLimit+256)
	require.NotPanics(t, func() {
		withDiscordTokenAndMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/oauth2/token", r.URL.Path)
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(oversized))
		})

		outcome, err := RecheckDiscordGate(context.Background(), user)
		require.NoError(t, err)
		assert.Equal(t, discordGateResultUnknown, outcome.Result)
		assert.Equal(t, "refresh_failed", outcome.Reason)
		assert.True(t, outcome.GatePassed)

		stored, err := model.GetUserById(user.Id, true)
		require.NoError(t, err)
		assert.True(t, stored.DiscordGatePassed)
		decrypted, decryptErr := common.DecryptWithCryptoSecret(stored.DiscordRefreshToken)
		require.NoError(t, decryptErr)
		assert.Equal(t, "old-refresh", decrypted, "refresh token must NOT be cleared on body_too_large")
	})
}

// TestRecheckDiscordGate_GuildMember401StaysUnknown proves a 401 from the guild
// member endpoint is treated as unknown (transient/auth failure), never as
// "not a member".
func TestRecheckDiscordGate_GuildMember401StaysUnknown(t *testing.T) {
	withDiscordGateRecheckDB(t)
	withDiscordSettings(t, func(settings *system_setting.DiscordSettings) {
		settings.RegisterGate = discordGateConfig("guild-1", "role-1")
	})
	user := createDiscordGateUser(t, model.User{
		DiscordId:           "discord-1",
		DiscordRefreshToken: encryptedDiscordRefreshToken(t, "refresh"),
		DiscordGatePassed:   true,
	})
	withDiscordTokenAndMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			_, _ = w.Write([]byte(`{"access_token":"access"}`))
		case "/users/@me/guilds/guild-1/member":
			w.WriteHeader(http.StatusUnauthorized)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	outcome, err := RecheckDiscordGate(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, discordGateResultUnknown, outcome.Result)
	assert.True(t, outcome.GatePassed, "401 must not clear existing gate pass")

	stored, err := model.GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.True(t, stored.DiscordGatePassed)
	assert.Equal(t, discordGateResultUnknown, stored.DiscordLastCheckResult)
}

// TestRecheckDiscordGate_GuildMember403StaysUnknown proves a 403 from the guild
// member endpoint is treated as unknown, never as "not a member".
func TestRecheckDiscordGate_GuildMember403StaysUnknown(t *testing.T) {
	withDiscordGateRecheckDB(t)
	withDiscordSettings(t, func(settings *system_setting.DiscordSettings) {
		settings.RegisterGate = discordGateConfig("guild-1", "role-1")
	})
	user := createDiscordGateUser(t, model.User{
		DiscordId:           "discord-1",
		DiscordRefreshToken: encryptedDiscordRefreshToken(t, "refresh"),
		DiscordGatePassed:   true,
	})
	withDiscordTokenAndMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			_, _ = w.Write([]byte(`{"access_token":"access"}`))
		case "/users/@me/guilds/guild-1/member":
			w.WriteHeader(http.StatusForbidden)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	outcome, err := RecheckDiscordGate(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, discordGateResultUnknown, outcome.Result)
	assert.True(t, outcome.GatePassed)
}

// TestRecheckDiscordGate_GuildMemberBodyTooLargeStaysUnknown proves an oversized
// guild member body is capped without panic and still yields unknown (never
// "not a member").
func TestRecheckDiscordGate_GuildMemberBodyTooLargeStaysUnknown(t *testing.T) {
	withDiscordGateRecheckDB(t)
	withDiscordSettings(t, func(settings *system_setting.DiscordSettings) {
		settings.RegisterGate = discordGateConfig("guild-1", "role-1")
	})
	user := createDiscordGateUser(t, model.User{
		DiscordId:           "discord-1",
		DiscordRefreshToken: encryptedDiscordRefreshToken(t, "refresh"),
		DiscordGatePassed:   true,
	})
	oversized := strings.Repeat("x", discordResponseBodyLimit+256)
	withDiscordTokenAndMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			_, _ = w.Write([]byte(`{"access_token":"access"}`))
		case "/users/@me/guilds/guild-1/member":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(oversized))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	require.NotPanics(t, func() {
		outcome, err := RecheckDiscordGate(context.Background(), user)
		require.NoError(t, err)
		assert.Equal(t, discordGateResultUnknown, outcome.Result)
		assert.True(t, outcome.GatePassed, "body_too_large must not clear existing gate pass")
	})
}
