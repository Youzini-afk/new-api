package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDiscordGateScopeStatusForScopes(t *testing.T) {
	tests := []struct {
		name   string
		scopes string
		want   string
	}{
		{name: "unknown", scopes: "", want: DiscordGateScopeStatusUnknown},
		{name: "missing guilds", scopes: "identify openid guilds.members.read", want: DiscordGateScopeStatusMissingGuilds},
		{name: "missing members read", scopes: "identify GUILDS", want: DiscordGateScopeStatusMissingGuildsMembersRead},
		{name: "ok", scopes: " identify guilds guilds.members.read guilds ", want: DiscordGateScopeStatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, DiscordGateScopeStatusForScopes(tt.scopes))
		})
	}
}

func TestNormalizeDiscordOAuthScopes(t *testing.T) {
	assert.Equal(t, "identify guilds guilds.members.read", NormalizeDiscordOAuthScopes("Identify  guilds GUILDS guilds.members.read"))
}
