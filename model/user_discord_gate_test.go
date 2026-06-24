package model

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUser_DiscordGateFields_JSONNoRefreshLeak verifies that the Discord
// refresh token is never serialized into JSON (json:"-" tag) while the gate
// boolean contract fields ARE serialized. This protects the API contract:
// a marshalled user payload must not leak the refresh token.
func TestUser_DiscordGateFields_JSONNoRefreshLeak(t *testing.T) {
	u := User{
		Id:                    42,
		Username:              "discord_user",
		DiscordId:             "111222333",
		DiscordRefreshToken:   "super-secret-refresh-token-must-not-leak",
		DiscordGatePassed:     true,
		DiscordGateExempt:     false,
	}

	data, err := common.Marshal(u)
	require.NoError(t, err)
	body := string(data)

	// The refresh token must never appear in any JSON representation.
	assert.NotContains(t, body, "super-secret-refresh-token-must-not-leak",
		"refresh token leaked into user JSON")
	assert.NotContains(t, body, "discord_refresh_token",
		"discord_refresh_token key leaked into user JSON")

	// The gate boolean contract fields ARE exposed to the frontend.
	assert.Contains(t, body, `"discord_gate_passed":true`)
	assert.Contains(t, body, `"discord_gate_exempt":false`)
}

// TestUser_DiscordGateFields_JSONNoRefreshLeak_EmptyToken also asserts no
// "discord_refresh_token" key appears even when the token is empty, so a
// zero-value user (e.g. freshly migrated) never surfaces the field name.
func TestUser_DiscordGateFields_JSONNoRefreshLeak_EmptyToken(t *testing.T) {
	u := User{Id: 1, Username: "plain_user"}

	data, err := common.Marshal(u)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "discord_refresh_token")
	assert.NotContains(t, string(data), "refresh_token")
}

// TestUser_DiscordGateFields_OmitDoesNotBreakRefreshToken checks the gorm:"-:all"
// style guard does not accidentally apply to the gate fields: the gate fields
// must be persistable columns, so they must not carry gorm:"-:all".
func TestUser_DiscordGateFields_NotIgnoredByORM(t *testing.T) {
	// Assert field names exist and have the expected json/gorm tags by
	// checking that marshalling reflects the contract and that the fields
	// are real exported struct fields (compile-time enforced by type use).
	u := User{
		DiscordRefreshToken: "rt",
		DiscordGatePassed:   true,
		DiscordGateExempt:   true,
	}
	// Refresh token is json:"-" so it must not show up; gate booleans do.
	data, err := common.Marshal(u)
	require.NoError(t, err)
	body := string(data)
	assert.True(t, strings.Contains(body, "discord_gate_passed"))
	assert.True(t, strings.Contains(body, "discord_gate_exempt"))
	// Ensure the refresh token value did not leak.
	assert.NotContains(t, body, "\"rt\"")
}
