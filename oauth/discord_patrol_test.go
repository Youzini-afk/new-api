package oauth

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvaluateDiscordGateForPatrolBanGuildOnlyUsesGuildList(t *testing.T) {
	withDiscordMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/users/@me/guilds", r.URL.Path)
		_, _ = w.Write([]byte(`[{"id":"banned-guild"}]`))
	})
	cfg := system_setting.DiscordRegisterGateConfig{
		Groups:    []system_setting.DiscordGateGroup{{Rules: []system_setting.DiscordGateRule{{GuildID: "allowed-guild", RoleIDs: []string{"role"}}}}},
		BanGroups: []system_setting.DiscordGateGroup{{Rules: []system_setting.DiscordGateRule{{GuildID: "banned-guild"}}}},
	}
	system_setting.NormalizeDiscordRegisterGate(&cfg)

	outcome := evaluateDiscordGateForPatrol(context.Background(), "access-token", cfg)
	assert.Equal(t, DiscordPatrolOutcomeBanMatched, outcome.Result)
}

func TestEvaluateDiscordGateForPatrolBanGroupsContinueAfterTransient(t *testing.T) {
	withDiscordMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/@me/guilds":
			_, _ = w.Write([]byte(`[{"id":"transient-guild"},{"id":"banned-guild"}]`))
		case "/users/@me/guilds/transient-guild/member":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	cfg := system_setting.DiscordRegisterGateConfig{
		Groups: []system_setting.DiscordGateGroup{{Rules: []system_setting.DiscordGateRule{{GuildID: "allowed-guild"}}}},
		BanGroups: []system_setting.DiscordGateGroup{
			{Rules: []system_setting.DiscordGateRule{{GuildID: "transient-guild", RoleIDs: []string{"ban-role"}}}},
			{Rules: []system_setting.DiscordGateRule{{GuildID: "banned-guild"}}},
		},
	}
	system_setting.NormalizeDiscordRegisterGate(&cfg)

	outcome := evaluateDiscordGateForPatrol(context.Background(), "access-token", cfg)
	assert.Equal(t, DiscordPatrolOutcomeBanMatched, outcome.Result)
}

func TestEvaluateDiscordGateForPatrolAllowAbsentFailsWithoutBan(t *testing.T) {
	withDiscordMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/users/@me/guilds", r.URL.Path)
		_, _ = w.Write([]byte(`[]`))
	})
	cfg := system_setting.DiscordRegisterGateConfig{Groups: []system_setting.DiscordGateGroup{{Rules: []system_setting.DiscordGateRule{{GuildID: "allowed-guild", RoleIDs: []string{"role"}}}}}}
	system_setting.NormalizeDiscordRegisterGate(&cfg)

	outcome := evaluateDiscordGateForPatrol(context.Background(), "access-token", cfg)
	assert.Equal(t, DiscordPatrolOutcomeAllowFailed, outcome.Result)
}

func TestPatrolDiscordGateStaleRefreshTokenCASSkipsMutation(t *testing.T) {
	withDiscordGateRecheckDB(t)
	originalEncrypted := encryptedDiscordRefreshToken(t, "old-refresh")
	newerEncrypted := encryptedDiscordRefreshToken(t, "new-refresh")
	user := createDiscordGateUser(t, model.User{
		Role:                   common.RoleCommonUser,
		Status:                 common.UserStatusEnabled,
		DiscordId:              "discord-cas",
		DiscordRefreshToken:    originalEncrypted,
		DiscordGatePassed:      true,
		DiscordOAuthScopes:     "identify guilds guilds.members.read",
		DiscordGateScopeStatus: model.DiscordGateScopeStatusOK,
	})
	withDiscordMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", user.Id).Update("discord_refresh_token", newerEncrypted).Error)
			_, _ = w.Write([]byte(`{"access_token":"access-token","refresh_token":"rotated-refresh","scope":"identify guilds guilds.members.read"}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	})

	outcome, err := PatrolDiscordGate(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, DiscordPatrolOutcomeSkipped, outcome.Result)
	assert.Equal(t, "discord_oauth_state_changed", outcome.Reason)
	var stored model.User
	require.NoError(t, model.DB.First(&stored, user.Id).Error)
	assert.Equal(t, newerEncrypted, stored.DiscordRefreshToken)
}

func TestPatrolDiscordGateInvalidGrantDisablesTokensOnly(t *testing.T) {
	withDiscordGateRecheckDB(t)
	user := createDiscordGateUser(t, model.User{
		Role:                common.RoleCommonUser,
		Status:              common.UserStatusEnabled,
		DiscordId:           "discord-invalid-grant",
		DiscordRefreshToken: encryptedDiscordRefreshToken(t, "old-refresh"),
		DiscordGatePassed:   true,
	})
	token := model.Token{UserId: user.Id, Key: "patrol-invalid-grant-token", Status: common.TokenStatusEnabled, Name: "invalid grant"}
	require.NoError(t, model.DB.Create(&token).Error)
	withDiscordMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/oauth2/token", r.URL.Path)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	})

	outcome, err := PatrolDiscordGate(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, DiscordPatrolOutcomeReauthRequired, outcome.Result)
	assert.Equal(t, "invalid_grant", outcome.Reason)
	var stored model.User
	require.NoError(t, model.DB.First(&stored, user.Id).Error)
	assert.Equal(t, common.UserStatusEnabled, stored.Status)
	assert.False(t, stored.DiscordGatePassed)
	assert.Empty(t, stored.DiscordRefreshToken)
	var storedToken model.Token
	require.NoError(t, model.DB.First(&storedToken, token.Id).Error)
	assert.Equal(t, common.TokenStatusDisabled, storedToken.Status)
}

func TestPatrolDiscordGateInvalidGrantStaleRefreshTokenSkipsMutation(t *testing.T) {
	withDiscordGateRecheckDB(t)
	originalEncrypted := encryptedDiscordRefreshToken(t, "old-refresh")
	newerEncrypted := encryptedDiscordRefreshToken(t, "new-refresh")
	user := createDiscordGateUser(t, model.User{
		Role:                common.RoleCommonUser,
		Status:              common.UserStatusEnabled,
		DiscordId:           "discord-invalid-grant-cas",
		DiscordRefreshToken: originalEncrypted,
		DiscordGatePassed:   true,
	})
	token := model.Token{UserId: user.Id, Key: "patrol-invalid-grant-cas-token", Status: common.TokenStatusEnabled, Name: "invalid grant cas"}
	require.NoError(t, model.DB.Create(&token).Error)
	withDiscordMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/oauth2/token", r.URL.Path)
		require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", user.Id).Update("discord_refresh_token", newerEncrypted).Error)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	})

	outcome, err := PatrolDiscordGate(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, DiscordPatrolOutcomeSkipped, outcome.Result)
	assert.Equal(t, "discord_oauth_state_changed", outcome.Reason)
	var stored model.User
	require.NoError(t, model.DB.First(&stored, user.Id).Error)
	assert.Equal(t, common.UserStatusEnabled, stored.Status)
	assert.True(t, stored.DiscordGatePassed)
	assert.Equal(t, newerEncrypted, stored.DiscordRefreshToken)
	var storedToken model.Token
	require.NoError(t, model.DB.First(&storedToken, token.Id).Error)
	assert.Equal(t, common.TokenStatusEnabled, storedToken.Status)
}

func TestPatrolDiscordGateScopeGapReauthOnly(t *testing.T) {
	withDiscordGateRecheckDB(t)
	encrypted, err := common.EncryptWithCryptoSecret("refresh-token")
	require.NoError(t, err)
	user := createDiscordGateUser(t, model.User{
		Role:                common.RoleCommonUser,
		Status:              common.UserStatusEnabled,
		DiscordId:           "discord-1",
		DiscordRefreshToken: encrypted,
		DiscordGatePassed:   true,
		DiscordOAuthScopes:  "identify guilds.members.read",
	})
	token := model.Token{UserId: user.Id, Key: "patrol-scope-gap-token", Status: common.TokenStatusEnabled, Name: "scope gap"}
	require.NoError(t, model.DB.Create(&token).Error)
	withDiscordMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			_, _ = w.Write([]byte(`{"access_token":"access-token","refresh_token":"refresh-token","scope":""}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	})

	outcome, err := PatrolDiscordGate(context.Background(), user)
	require.NoError(t, err)
	assert.Equal(t, DiscordPatrolOutcomeReauthRequired, outcome.Result)
	var stored model.User
	require.NoError(t, model.DB.First(&stored, user.Id).Error)
	assert.False(t, stored.DiscordGatePassed)
	assert.Equal(t, common.UserStatusEnabled, stored.Status)
	var storedToken model.Token
	require.NoError(t, model.DB.First(&storedToken, token.Id).Error)
	assert.Equal(t, common.TokenStatusEnabled, storedToken.Status)
}

func TestPatrolDiscordGateAllowFailureDisablesTokensOnly(t *testing.T) {
	withDiscordGateRecheckDB(t)
	user := createDiscordGateUser(t, model.User{
		Role:                   common.RoleCommonUser,
		Status:                 common.UserStatusEnabled,
		DiscordId:              "discord-allow-fail",
		DiscordRefreshToken:    encryptedDiscordRefreshToken(t, "refresh-token"),
		DiscordGatePassed:      true,
		DiscordOAuthScopes:     "identify guilds guilds.members.read",
		DiscordGateScopeStatus: model.DiscordGateScopeStatusOK,
	})
	token := model.Token{UserId: user.Id, Key: "patrol-allow-fail-token", Status: common.TokenStatusEnabled, Name: "allow fail"}
	require.NoError(t, model.DB.Create(&token).Error)
	settings := system_setting.GetDiscordSettings()
	originalGate := settings.RegisterGate
	settings.RegisterGate = system_setting.DiscordRegisterGateConfig{Groups: []system_setting.DiscordGateGroup{{Rules: []system_setting.DiscordGateRule{{GuildID: "allowed-guild", RoleIDs: []string{"required-role"}}}}}}
	t.Cleanup(func() { settings.RegisterGate = originalGate })
	withDiscordMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			_, _ = w.Write([]byte(`{"access_token":"access-token","refresh_token":"refresh-token","scope":"identify guilds guilds.members.read"}`))
		case "/users/@me/guilds":
			_, _ = w.Write([]byte(`[]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	outcome, err := PatrolDiscordGate(context.Background(), user)
	require.NoError(t, err)
	require.Equal(t, DiscordPatrolOutcomeAllowFailed, outcome.Result, outcome.Reason)
	var stored model.User
	require.NoError(t, model.DB.First(&stored, user.Id).Error)
	assert.Equal(t, common.UserStatusEnabled, stored.Status)
	assert.False(t, stored.DiscordGatePassed)
	var storedToken model.Token
	require.NoError(t, model.DB.First(&storedToken, token.Id).Error)
	assert.Equal(t, common.TokenStatusDisabled, storedToken.Status)
}

func TestEvaluateDiscordGateForPatrolRoleRequiresMemberEndpoint(t *testing.T) {
	joinedAt := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)
	withDiscordMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/@me/guilds":
			_, _ = w.Write([]byte(`[{"id":"allowed-guild"}]`))
		case "/users/@me/guilds/allowed-guild/member":
			_, _ = w.Write([]byte(`{"roles":["role"],"joined_at":"` + joinedAt + `"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	cfg := system_setting.DiscordRegisterGateConfig{Groups: []system_setting.DiscordGateGroup{{Rules: []system_setting.DiscordGateRule{{GuildID: "allowed-guild", RoleIDs: []string{"role"}, MinJoinHours: 1}}}}}
	system_setting.NormalizeDiscordRegisterGate(&cfg)

	outcome := evaluateDiscordGateForPatrol(context.Background(), "access-token", cfg)
	assert.Equal(t, DiscordPatrolOutcomePass, outcome.Result)
}
