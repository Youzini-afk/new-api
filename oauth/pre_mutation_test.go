package oauth

import (
	"context"
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noHookProvider is a minimal Provider implementation that does NOT implement
// PreUserMutationValidator. It proves non-hook providers are unaffected.
type noHookProvider struct{}

func (noHookProvider) GetName() string                                              { return "NoHook" }
func (noHookProvider) IsEnabled() bool                                              { return true }
func (noHookProvider) ExchangeToken(context.Context, string, *gin.Context) (*OAuthToken, error) {
	return nil, nil
}
func (noHookProvider) GetUserInfo(context.Context, *OAuthToken) (*OAuthUser, error) {
	return nil, nil
}
func (noHookProvider) IsUserIDTaken(string) bool                              { return false }
func (noHookProvider) FillUserByProviderID(*model.User, string) error         { return nil }
func (noHookProvider) SetProviderUserID(*model.User, string)                  {}
func (noHookProvider) GetProviderPrefix() string                              { return "nohook_" }

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
	settings := system_setting.GetDiscordSettings()
	original := *settings
	defer func() { *settings = original }()

	settings.RegisterGateEnabled = false
	settings.LoginGateEnabled = false

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

// TestDiscordPreUserMutation_RegisterGateEnabled_FailsClosedOnCreate verifies
// the fail-closed contract: when RegisterGateEnabled is true, the create/bind
// flows are rejected with a clear "evaluator not available" error.
func TestDiscordPreUserMutation_RegisterGateEnabled_FailsClosedOnCreate(t *testing.T) {
	settings := system_setting.GetDiscordSettings()
	original := *settings
	defer func() { *settings = original }()

	settings.RegisterGateEnabled = true
	settings.LoginGateEnabled = false

	p := &DiscordProvider{}
	err := p.PreUserMutation(context.Background(), PreUserMutationContext{
		Flow: OAuthFlowCreate,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "register gate is enabled but the evaluator is not available yet")

	err = p.PreUserMutation(context.Background(), PreUserMutationContext{
		Flow: OAuthFlowBind,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "register gate is enabled")

	// Login flow is NOT blocked by register gate.
	err = p.PreUserMutation(context.Background(), PreUserMutationContext{
		Flow: OAuthFlowLogin,
	})
	require.NoError(t, err)
}

// TestDiscordPreUserMutation_LoginGateEnabled_FailsClosedOnLogin verifies the
// fail-closed contract for the login gate.
func TestDiscordPreUserMutation_LoginGateEnabled_FailsClosedOnLogin(t *testing.T) {
	settings := system_setting.GetDiscordSettings()
	original := *settings
	defer func() { *settings = original }()

	settings.RegisterGateEnabled = false
	settings.LoginGateEnabled = true

	p := &DiscordProvider{}
	err := p.PreUserMutation(context.Background(), PreUserMutationContext{
		Flow: OAuthFlowLogin,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "login gate is enabled but the evaluator is not available yet")

	err = p.PreUserMutation(context.Background(), PreUserMutationContext{
		Flow: OAuthFlowExisting,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "login gate is enabled")

	// Create flow is NOT blocked by login gate.
	err = p.PreUserMutation(context.Background(), PreUserMutationContext{
		Flow: OAuthFlowCreate,
	})
	require.NoError(t, err)
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
	}
	assert.Equal(t, "Discord", ctx.ProviderName)
	assert.Equal(t, OAuthFlowBind, ctx.Flow)
	assert.Equal(t, "a", ctx.Token.AccessToken)
	assert.Equal(t, "u", ctx.OAuthUser.ProviderUserID)
	assert.Equal(t, 7, ctx.CurrentUser.Id)
}
