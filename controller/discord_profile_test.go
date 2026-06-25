package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type oauthTestSession map[interface{}]interface{}

func (s oauthTestSession) ID() string { return "" }
func (s oauthTestSession) Get(key interface{}) interface{} {
	return s[key]
}
func (s oauthTestSession) Set(key interface{}, val interface{}) {
	s[key] = val
}
func (s oauthTestSession) Delete(key interface{}) {
	delete(s, key)
}
func (s oauthTestSession) Clear() {
	for key := range s {
		delete(s, key)
	}
}
func (s oauthTestSession) AddFlash(value interface{}, vars ...string) {}
func (s oauthTestSession) Flashes(vars ...string) []interface{}       { return nil }
func (s oauthTestSession) Options(options sessions.Options)           {}
func (s oauthTestSession) Save() error                                { return nil }

type discordProfileControllerProvider struct {
	token     *oauth.OAuthToken
	oauthUser *oauth.OAuthUser
}

func (p discordProfileControllerProvider) GetName() string { return "Discord" }
func (p discordProfileControllerProvider) IsEnabled() bool { return true }
func (p discordProfileControllerProvider) ExchangeToken(context.Context, string, *gin.Context) (*oauth.OAuthToken, error) {
	return p.token, nil
}
func (p discordProfileControllerProvider) GetUserInfo(context.Context, *oauth.OAuthToken) (*oauth.OAuthUser, error) {
	return p.oauthUser, nil
}
func (p discordProfileControllerProvider) IsUserIDTaken(providerUserID string) bool {
	return model.IsDiscordIdAlreadyTaken(providerUserID)
}
func (p discordProfileControllerProvider) FillUserByProviderID(user *model.User, providerUserID string) error {
	user.DiscordId = providerUserID
	return user.FillUserByDiscordId()
}
func (p discordProfileControllerProvider) SetProviderUserID(user *model.User, providerUserID string) {
	user.DiscordId = providerUserID
}
func (p discordProfileControllerProvider) GetProviderPrefix() string { return "discord_" }
func (p discordProfileControllerProvider) PreUserMutation(ctx context.Context, preCtx oauth.PreUserMutationContext) error {
	if preCtx.Result == nil || preCtx.OAuthUser == nil {
		return nil
	}
	preCtx.Result.DiscordUsername = preCtx.OAuthUser.Username
	preCtx.Result.DiscordGlobalName = preCtx.OAuthUser.DisplayName
	if preCtx.OAuthUser.Extra != nil {
		if value, ok := preCtx.OAuthUser.Extra["discord_discriminator"].(string); ok {
			preCtx.Result.DiscordDiscriminator = value
		}
		if value, ok := preCtx.OAuthUser.Extra["discord_avatar_hash"].(string); ok {
			preCtx.Result.DiscordAvatarHash = value
		}
	}
	preCtx.Result.DiscordProfileSyncedAt = time.Now().Unix()
	preCtx.Result.HasDiscordProfileUpdate = preCtx.Result.DiscordUsername != ""
	return nil
}

func withDiscordProfileOAuthSettings(t *testing.T) {
	t.Helper()
	settings := system_setting.GetDiscordSettings()
	original := *settings
	settings.Enabled = true
	settings.RegisterGateEnabled = false
	settings.LoginGateEnabled = false
	t.Cleanup(func() { *settings = original })
}

func discordProfileOAuthUser(id, username, globalName, discriminator, avatarHash string) *oauth.OAuthUser {
	return &oauth.OAuthUser{
		ProviderUserID: id,
		Username:       username,
		DisplayName:    globalName,
		Extra: map[string]any{
			"discord_username":      username,
			"discord_global_name":   globalName,
			"discord_discriminator": discriminator,
			"discord_avatar_hash":   avatarHash,
		},
	}
}

func newOAuthHelperContext() *gin.Context {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/oauth/discord", nil)
	return ctx
}

func TestDiscordProfile_CreatePersistsMetadata(t *testing.T) {
	db := setupAvatarTestDB(t)
	withDiscordProfileOAuthSettings(t)

	user, err := findOrCreateOAuthUser(
		newOAuthHelperContext(),
		&oauth.DiscordProvider{},
		discordProfileOAuthUser("discord-create", "remotecreate", "Remote Create", "1234", "avatar-create"),
		&oauth.OAuthToken{AccessToken: "access-token"},
		oauthTestSession{},
	)
	require.NoError(t, err)
	require.NotZero(t, user.Id)

	var stored model.User
	require.NoError(t, db.Where("id = ?", user.Id).First(&stored).Error)
	assert.Equal(t, "discord-create", stored.DiscordId)
	assert.Equal(t, "remotecreate", stored.DiscordUsername)
	assert.Equal(t, "Remote Create", stored.DiscordGlobalName)
	assert.Equal(t, "1234", stored.DiscordDiscriminator)
	assert.Equal(t, "avatar-create", stored.DiscordAvatarHash)
	assert.NotZero(t, stored.DiscordProfileSyncedAt)
}

func TestDiscordProfile_LoginRefreshesMetadataWithoutOverwritingLocalNames(t *testing.T) {
	db := setupAvatarTestDB(t)
	withDiscordProfileOAuthSettings(t)
	localUser := &model.User{
		Username:    "local-login",
		DisplayName: "Local Login",
		Password:    "password_hash",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		DiscordId:   "discord-login",
	}
	require.NoError(t, db.Create(localUser).Error)

	user, err := findOrCreateOAuthUser(
		newOAuthHelperContext(),
		&oauth.DiscordProvider{},
		discordProfileOAuthUser("discord-login", "remote-login", "Remote Login", "4321", "avatar-login"),
		&oauth.OAuthToken{AccessToken: "access-token"},
		oauthTestSession{},
	)
	require.NoError(t, err)
	require.Equal(t, localUser.Id, user.Id)

	var stored model.User
	require.NoError(t, db.Where("id = ?", localUser.Id).First(&stored).Error)
	assert.Equal(t, "local-login", stored.Username)
	assert.Equal(t, "Local Login", stored.DisplayName)
	assert.Equal(t, "remote-login", stored.DiscordUsername)
	assert.Equal(t, "Remote Login", stored.DiscordGlobalName)
	assert.Equal(t, "4321", stored.DiscordDiscriminator)
	assert.Equal(t, "avatar-login", stored.DiscordAvatarHash)
	assert.NotZero(t, stored.DiscordProfileSyncedAt)
}

func TestDiscordProfile_BindPersistsMetadataWithoutOverwritingLocalNames(t *testing.T) {
	db := setupAvatarTestDB(t)
	localUser := &model.User{
		Username:    "local-bind",
		DisplayName: "Local Bind",
		Password:    "password_hash",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	require.NoError(t, db.Create(localUser).Error)
	provider := discordProfileControllerProvider{
		token:     &oauth.OAuthToken{AccessToken: "access-token"},
		oauthUser: discordProfileOAuthUser("discord-bind", "remote-bind", "Remote Bind", "9999", "avatar-bind"),
	}

	r := gin.New()
	r.Use(sessions.Sessions("session", cookie.NewStore([]byte("discord-profile-test-secret"))))
	r.GET("/api/oauth/discord", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", localUser.Id)
		session.Set("username", localUser.Username)
		require.NoError(t, session.Save())
		handleOAuthBind(c, provider)
	})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/oauth/discord?code=ok", nil)
	r.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code, "body: %s", recorder.Body.String())

	var resp struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	require.True(t, resp.Success)

	var stored model.User
	require.NoError(t, db.Where("id = ?", localUser.Id).First(&stored).Error)
	assert.Equal(t, "local-bind", stored.Username)
	assert.Equal(t, "Local Bind", stored.DisplayName)
	assert.Equal(t, "discord-bind", stored.DiscordId)
	assert.Equal(t, "remote-bind", stored.DiscordUsername)
	assert.Equal(t, "Remote Bind", stored.DiscordGlobalName)
	assert.Equal(t, "9999", stored.DiscordDiscriminator)
	assert.Equal(t, "avatar-bind", stored.DiscordAvatarHash)
	assert.NotZero(t, stored.DiscordProfileSyncedAt)
}

func TestGetSelf_ReturnsSafeDiscordProfileFields(t *testing.T) {
	db := setupAvatarTestDB(t)
	user := seedAvatarUser(t, db, "discord-self")
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", user.Id).Updates(map[string]interface{}{
		"discord_id":                "discord-self-id",
		"discord_username":          "remote-self",
		"discord_global_name":       "Remote Self",
		"discord_discriminator":     "0007",
		"discord_avatar_hash":       "avatar-hash-should-not-return",
		"discord_profile_synced_at": int64(777),
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/user/self", nil)
	ctx.Set("id", user.Id)
	ctx.Set("role", user.Role)
	GetSelf(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "discord_avatar_hash")
	assert.NotContains(t, recorder.Body.String(), "avatar-hash-should-not-return")

	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			DiscordUsername        string `json:"discord_username"`
			DiscordGlobalName      string `json:"discord_global_name"`
			DiscordDiscriminator   string `json:"discord_discriminator"`
			DiscordProfileSyncedAt int64  `json:"discord_profile_synced_at"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	assert.Equal(t, "remote-self", resp.Data.DiscordUsername)
	assert.Equal(t, "Remote Self", resp.Data.DiscordGlobalName)
	assert.Equal(t, "0007", resp.Data.DiscordDiscriminator)
	assert.Equal(t, int64(777), resp.Data.DiscordProfileSyncedAt)
}
