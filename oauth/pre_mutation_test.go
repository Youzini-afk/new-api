package oauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withDiscordSettings(t *testing.T, mutate func(*system_setting.DiscordSettings)) {
	t.Helper()
	settings := system_setting.GetDiscordSettings()
	original := *settings
	t.Cleanup(func() { *settings = original })
	mutate(settings)
}

func withDiscordMemberServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
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

func discordGateConfig(guildID string, roleIDs ...string) system_setting.DiscordRegisterGateConfig {
	cfg := system_setting.DiscordRegisterGateConfig{
		Groups: []system_setting.DiscordGateGroup{{
			Rules: []system_setting.DiscordGateRule{{
				GuildID:   guildID,
				RoleIDs:   roleIDs,
				RoleMatch: "any",
			}},
		}},
	}
	system_setting.NormalizeDiscordRegisterGate(&cfg)
	return cfg
}

func discordTestGuildIDFromPath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part == "guilds" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func discordTestMemberJSON(joinedAt time.Time, roles ...string) string {
	quoted := make([]string, 0, len(roles))
	for _, role := range roles {
		quoted = append(quoted, `"`+role+`"`)
	}
	return `{"roles":[` + strings.Join(quoted, ",") + `],"joined_at":"` + joinedAt.UTC().Format(time.RFC3339) + `"}`
}

// noHookProvider is a minimal Provider implementation that does NOT implement
// PreUserMutationValidator. It proves non-hook providers are unaffected.
type noHookProvider struct{}

func (noHookProvider) GetName() string { return "NoHook" }
func (noHookProvider) IsEnabled() bool { return true }
func (noHookProvider) ExchangeToken(context.Context, string, *gin.Context) (*OAuthToken, error) {
	return nil, nil
}
func (noHookProvider) GetUserInfo(context.Context, *OAuthToken) (*OAuthUser, error) {
	return nil, nil
}
func (noHookProvider) IsUserIDTaken(string) bool                      { return false }
func (noHookProvider) FillUserByProviderID(*model.User, string) error { return nil }
func (noHookProvider) SetProviderUserID(*model.User, string)          {}
func (noHookProvider) GetProviderPrefix() string                      { return "nohook_" }

// alwaysBlockProvider implements PreUserMutationValidator and always returns
// an error — proves a hook error blocks the mutation.
type alwaysBlockProvider struct{ noHookProvider }

func (alwaysBlockProvider) PreUserMutation(ctx context.Context, preCtx PreUserMutationContext) error {
	return errors.New("blocked by hook: gate denied")
}

// TestRunPreUserMutation_NonHookProvider_NoOp verifies that a provider which
// does not implement PreUserMutationValidator is a no-op (nil error), so the
// hook skeleton never breaks GitHub/OIDC/LinuxDo/custom OAuth providers.
func TestRunPreUserMutation_NonHookProvider_NoOp(t *testing.T) {
	p := noHookProvider{}
	err := RunPreUserMutation(context.Background(), p, PreUserMutationContext{
		ProviderName: "NoHook",
		Flow:         OAuthFlowCreate,
		Token:        &OAuthToken{AccessToken: "tok"},
		OAuthUser:    &OAuthUser{ProviderUserID: "uid"},
	})
	require.NoError(t, err, "non-hook provider must be a no-op")
}

// TestRunPreUserMutation_HookError_BlocksMutation verifies that when the hook
// returns an error, RunPreUserMutation surfaces it so the controller blocks
// before the local user record is mutated.
func TestRunPreUserMutation_HookError_BlocksMutation(t *testing.T) {
	p := alwaysBlockProvider{}
	err := RunPreUserMutation(context.Background(), p, PreUserMutationContext{
		ProviderName: "Blocker",
		Flow:         OAuthFlowCreate,
		Token:        &OAuthToken{AccessToken: "tok"},
		OAuthUser:    &OAuthUser{ProviderUserID: "uid"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocked by hook")
}

// TestRunPreUserMutation_NilProviderSafe guards against a nil provider (e.g.
// unknown provider slug resolved to nil) so the helper never panics.
func TestRunPreUserMutation_NilProviderSafe(t *testing.T) {
	err := RunPreUserMutation(context.Background(), nil, PreUserMutationContext{
		Flow: OAuthFlowLogin,
	})
	require.NoError(t, err)
}

// TestDiscordPreUserMutation_GateDisabled_NoOp verifies the Discord provider
// is a no-op when neither gate is enabled (the default state), so normal
// OAuth login/create/bind is unaffected in this phase.
func TestDiscordPreUserMutation_GateDisabled_NoOp(t *testing.T) {
	withDiscordSettings(t, func(settings *system_setting.DiscordSettings) {
		settings.RegisterGateEnabled = false
		settings.LoginGateEnabled = false
	})

	p := &DiscordProvider{}
	err := p.PreUserMutation(context.Background(), PreUserMutationContext{
		Flow: OAuthFlowCreate,
	})
	require.NoError(t, err)
	err = p.PreUserMutation(context.Background(), PreUserMutationContext{
		Flow: OAuthFlowLogin,
	})
	require.NoError(t, err)
}

func TestDiscordPreUserMutation_RegisterGatePassSetsResult(t *testing.T) {
	withDiscordSettings(t, func(settings *system_setting.DiscordSettings) {
		settings.RegisterGateEnabled = true
		settings.LoginGateEnabled = false
		settings.RegisterGate = discordGateConfig("guild-1", "role-1")
	})
	joinedAt := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)
	withDiscordMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer access-token", r.Header.Get("Authorization"))
		assert.Equal(t, "/users/@me/guilds/guild-1/member", r.URL.Path)
		_, _ = w.Write([]byte(`{"roles":["role-1"],"joined_at":"` + joinedAt + `"}`))
	})

	result := &PreUserMutationResult{}
	err := (&DiscordProvider{}).PreUserMutation(context.Background(), PreUserMutationContext{
		Flow:   OAuthFlowCreate,
		Token:  &OAuthToken{AccessToken: "access-token", RefreshToken: "refresh-token"},
		Result: result,
	})
	require.NoError(t, err)
	assert.True(t, result.HasDiscordGateUpdate)
	assert.True(t, result.DiscordGatePassed)
	assert.True(t, result.HasDiscordRefreshTokenUpdate)
	decrypted, err := common.DecryptWithCryptoSecret(result.EncryptedDiscordRefreshToken)
	require.NoError(t, err)
	assert.Equal(t, "refresh-token", decrypted)
}

func TestDiscordPreUserMutation_EnabledEmptyConfigFailsClosed(t *testing.T) {
	withDiscordSettings(t, func(settings *system_setting.DiscordSettings) {
		settings.RegisterGateEnabled = true
		settings.LoginGateEnabled = false
		settings.RegisterGate = system_setting.DiscordRegisterGateConfig{}
	})

	err := (&DiscordProvider{}).PreUserMutation(context.Background(), PreUserMutationContext{
		Flow:  OAuthFlowCreate,
		Token: &OAuthToken{AccessToken: "access-token"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Discord gate is not configured correctly")
}

func TestDiscordPreUserMutation_LoginUnknownAllowsAlreadyPassedUser(t *testing.T) {
	withDiscordSettings(t, func(settings *system_setting.DiscordSettings) {
		settings.RegisterGateEnabled = false
		settings.LoginGateEnabled = true
		settings.RegisterGate = discordGateConfig("guild-1", "role-1")
	})
	withDiscordMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})

	result := &PreUserMutationResult{}
	err := (&DiscordProvider{}).PreUserMutation(context.Background(), PreUserMutationContext{
		Flow:        OAuthFlowLogin,
		Token:       &OAuthToken{AccessToken: "access-token", RefreshToken: "refresh-token"},
		CurrentUser: &model.User{Id: 1, DiscordGatePassed: true},
		Result:      result,
	})
	require.NoError(t, err)
	assert.False(t, result.HasDiscordGateUpdate, "transient unknown must not clear or rewrite passed state")
	assert.True(t, result.HasDiscordRefreshTokenUpdate)
}

func TestDiscordPreUserMutation_LoginUnknownBlocksUnpassedUser(t *testing.T) {
	withDiscordSettings(t, func(settings *system_setting.DiscordSettings) {
		settings.RegisterGateEnabled = false
		settings.LoginGateEnabled = true
		settings.RegisterGate = discordGateConfig("guild-1", "role-1")
	})
	withDiscordMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})

	err := (&DiscordProvider{}).PreUserMutation(context.Background(), PreUserMutationContext{
		Flow:        OAuthFlowLogin,
		Token:       &OAuthToken{AccessToken: "access-token"},
		CurrentUser: &model.User{Id: 1, DiscordGatePassed: false},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Discord verification is temporarily unavailable")
}

func TestEvaluateDiscordGate_BanGroupWinsBeforeAllow(t *testing.T) {
	withDiscordMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "guild-1", discordTestGuildIDFromPath(r.URL.Path))
		_, _ = w.Write([]byte(discordTestMemberJSON(time.Now(), "allow-role", "ban-role")))
	})
	cfg := system_setting.DiscordRegisterGateConfig{
		Groups:    []system_setting.DiscordGateGroup{{Rules: []system_setting.DiscordGateRule{{GuildID: "guild-1", RoleIDs: []string{"allow-role"}}}}},
		BanGroups: []system_setting.DiscordGateGroup{{Rules: []system_setting.DiscordGateRule{{GuildID: "guild-1", RoleIDs: []string{"ban-role"}}}}},
	}
	system_setting.NormalizeDiscordRegisterGate(&cfg)

	result := evaluateDiscordGate(context.Background(), "access-token", cfg)
	assert.Equal(t, discordGateDecisionBan, result.Decision)
}

func TestEvaluateDiscordGate_BanGroupPassBeatsEarlierUnknown(t *testing.T) {
	withDiscordMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch discordTestGuildIDFromPath(r.URL.Path) {
		case "unknown-guild":
			w.WriteHeader(http.StatusTooManyRequests)
		case "banned-guild":
			_, _ = w.Write([]byte(discordTestMemberJSON(time.Now(), "ban-role")))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	cfg := system_setting.DiscordRegisterGateConfig{
		Groups: []system_setting.DiscordGateGroup{{Rules: []system_setting.DiscordGateRule{{GuildID: "allow-guild", RoleIDs: []string{"allow-role"}}}}},
		BanGroups: []system_setting.DiscordGateGroup{
			{Rules: []system_setting.DiscordGateRule{{GuildID: "unknown-guild", RoleIDs: []string{"ban-role"}}}},
			{Rules: []system_setting.DiscordGateRule{{GuildID: "banned-guild", RoleIDs: []string{"ban-role"}}}},
		},
	}
	system_setting.NormalizeDiscordRegisterGate(&cfg)

	result := evaluateDiscordGate(context.Background(), "access-token", cfg)
	assert.Equal(t, discordGateDecisionBan, result.Decision)

	allowed := discordGateAllowsFlow(OAuthFlowLogin, &model.User{DiscordGatePassed: true}, result)
	assert.False(t, allowed, "ban must block even for an already-passed login")
}

func TestEvaluateDiscordGate_GroupRulesAllAndGroupsAny(t *testing.T) {
	withDiscordMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch discordTestGuildIDFromPath(r.URL.Path) {
		case "guild-1":
			_, _ = w.Write([]byte(discordTestMemberJSON(time.Now(), "r1")))
		case "guild-2":
			_, _ = w.Write([]byte(discordTestMemberJSON(time.Now(), "missing")))
		case "guild-3":
			_, _ = w.Write([]byte(discordTestMemberJSON(time.Now(), "r3")))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	cfg := system_setting.DiscordRegisterGateConfig{Groups: []system_setting.DiscordGateGroup{
		{Rules: []system_setting.DiscordGateRule{
			{GuildID: "guild-1", RoleIDs: []string{"r1"}},
			{GuildID: "guild-2", RoleIDs: []string{"r2"}},
		}},
		{Rules: []system_setting.DiscordGateRule{{GuildID: "guild-3", RoleIDs: []string{"r3"}}}},
	}}
	system_setting.NormalizeDiscordRegisterGate(&cfg)

	result := evaluateDiscordGate(context.Background(), "access-token", cfg)
	assert.Equal(t, discordGateDecisionPass, result.Decision)
}

func TestEvaluateDiscordGate_RoleAllAndMinJoinHours(t *testing.T) {
	joinedAt := time.Now().Add(-48 * time.Hour)
	withDiscordMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(discordTestMemberJSON(joinedAt, "r1", "r2")))
	})
	cfg := system_setting.DiscordRegisterGateConfig{Groups: []system_setting.DiscordGateGroup{{Rules: []system_setting.DiscordGateRule{{
		GuildID:      "guild-1",
		RoleIDs:      []string{"r1", "r2"},
		RoleMatch:    "all",
		MinJoinHours: 24,
	}}}}}
	system_setting.NormalizeDiscordRegisterGate(&cfg)

	result := evaluateDiscordGate(context.Background(), "access-token", cfg)
	assert.Equal(t, discordGateDecisionPass, result.Decision)

	cfg.Groups[0].Rules[0].MinJoinHours = 72
	result = evaluateDiscordGate(context.Background(), "access-token", cfg)
	assert.Equal(t, discordGateDecisionDeny, result.Decision)
}

func TestEvaluateDiscordGate_404FalseAnd429Unknown(t *testing.T) {
	withDiscordMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch discordTestGuildIDFromPath(r.URL.Path) {
		case "missing-guild":
			w.WriteHeader(http.StatusNotFound)
		case "rate-limited-guild":
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(discordTestMemberJSON(time.Now(), "r1")))
		}
	})
	missingCfg := discordGateConfig("missing-guild", "r1")
	result := evaluateDiscordGate(context.Background(), "access-token", missingCfg)
	assert.Equal(t, discordGateDecisionDeny, result.Decision)

	rateLimitedCfg := discordGateConfig("rate-limited-guild", "r1")
	result = evaluateDiscordGate(context.Background(), "access-token", rateLimitedCfg)
	assert.Equal(t, discordGateDecisionUnknown, result.Decision)
}

func TestEvaluateDiscordGate_GroupFailBeatsUnknownInsideSameGroup(t *testing.T) {
	withDiscordMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch discordTestGuildIDFromPath(r.URL.Path) {
		case "unknown-guild":
			w.WriteHeader(http.StatusTooManyRequests)
		case "missing-guild":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	cfg := system_setting.DiscordRegisterGateConfig{Groups: []system_setting.DiscordGateGroup{{Rules: []system_setting.DiscordGateRule{
		{GuildID: "unknown-guild", RoleIDs: []string{"r1"}},
		{GuildID: "missing-guild", RoleIDs: []string{"r2"}},
	}}}}
	system_setting.NormalizeDiscordRegisterGate(&cfg)

	result := evaluateDiscordGate(context.Background(), "access-token", cfg)
	assert.Equal(t, discordGateDecisionDeny, result.Decision)
}

// Compile-time assertion: DiscordProvider implements PreUserMutationValidator.
var _ PreUserMutationValidator = (*DiscordProvider)(nil)

// TestPreUserMutationContext_Fields documents the contract fields that the
// task requires (provider name, flow, token, oauthUser, current user).
func TestPreUserMutationContext_Fields(t *testing.T) {
	ctx := PreUserMutationContext{
		ProviderName: "Discord",
		Flow:         OAuthFlowBind,
		Token:        &OAuthToken{AccessToken: "a"},
		OAuthUser:    &OAuthUser{ProviderUserID: "u"},
		CurrentUser:  &model.User{Id: 7},
		Result:       &PreUserMutationResult{},
	}
	assert.Equal(t, "Discord", ctx.ProviderName)
	assert.Equal(t, OAuthFlowBind, ctx.Flow)
	assert.Equal(t, "a", ctx.Token.AccessToken)
	assert.Equal(t, "u", ctx.OAuthUser.ProviderUserID)
	assert.Equal(t, 7, ctx.CurrentUser.Id)
	assert.NotNil(t, ctx.Result)
}
