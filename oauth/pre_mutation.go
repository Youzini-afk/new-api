package oauth

import (
	"context"

	"github.com/QuantumNous/new-api/model"
)

// OAuthFlow labels the mutation flow a provider hook is invoked from.
type OAuthFlow string

const (
	// OAuthFlowLogin: an existing provider-bound user is logging in.
	OAuthFlowLogin OAuthFlow = "login"
	// OAuthFlowBind: the provider account is being bound to the current user.
	OAuthFlowBind OAuthFlow = "bind"
	// OAuthFlowCreate: a new local user is being created from the provider.
	OAuthFlowCreate OAuthFlow = "create"
	// OAuthFlowExisting: the provider user was found to already exist (alias
	// of login for providers that want to distinguish the lookup result).
	OAuthFlowExisting OAuthFlow = "existing"
)

// PreUserMutationContext carries the information available before an OAuth
// provider mutates the local user record (create / bind / login).
type PreUserMutationContext struct {
	// ProviderName is the display name of the provider (e.g. "Discord").
	ProviderName string
	// Flow indicates which mutation is about to happen.
	Flow OAuthFlow
	// Token is the OAuth access token exchanged from the provider.
	Token *OAuthToken
	// OAuthUser is the user info fetched from the provider.
	OAuthUser *OAuthUser
	// CurrentUser is the local user for bind/login flows; nil for create.
	CurrentUser *model.User
}

// PreUserMutationValidator is an OPTIONAL side-interface that OAuth providers
// may implement to run gate / verification logic BEFORE the controller
// mutates the local user record. Returning an error blocks the mutation.
//
// This is intentionally a separate interface from Provider so that providers
// which do not implement it (GitHub, OIDC, LinuxDo, custom OAuth, ...) are
// completely unaffected — the controller type-asserts and no-ops when the
// interface is absent.
type PreUserMutationValidator interface {
	PreUserMutation(ctx context.Context, preCtx PreUserMutationContext) error
}

// RunPreUserMutation type-asserts the provider and invokes the pre-mutation
// hook if implemented. Providers that do not implement
// PreUserMutationValidator are a no-op (nil error).
//
// This helper guarantees that non-hook providers are never affected by the
// gate skeleton: the type assertion simply fails and nil is returned.
func RunPreUserMutation(ctx context.Context, provider Provider, preCtx PreUserMutationContext) error {
	validator, ok := provider.(PreUserMutationValidator)
	if !ok {
		return nil
	}
	return validator.PreUserMutation(ctx, preCtx)
}
