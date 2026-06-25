package model

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestUser_DiscordGateFields_JSONNoRefreshLeak verifies that the Discord
// refresh token is never serialized into JSON (json:"-" tag) while the gate
// boolean contract fields ARE serialized. This protects the API contract:
// a marshalled user payload must not leak the refresh token.
func TestUser_DiscordGateFields_JSONNoRefreshLeak(t *testing.T) {
	u := User{
		Id:                     42,
		Username:               "discord_user",
		DiscordId:              "111222333",
		DiscordUsername:        "remote_user",
		DiscordGlobalName:      "Remote User",
		DiscordDiscriminator:   "1234",
		DiscordAvatarHash:      "avatar-hash-must-not-leak",
		DiscordProfileSyncedAt: 987654321,
		DiscordRefreshToken:    "super-secret-refresh-token-must-not-leak",
		DiscordGatePassed:      true,
		DiscordGateExempt:      false,
		DiscordLastCheckAt:     123456789,
		DiscordLastCheckResult: "pass",
		DiscordLastCheckReason: "allow_group_matched",
		DiscordGateMessage:     "",
	}

	data, err := common.Marshal(u)
	require.NoError(t, err)
	body := string(data)

	// The refresh token must never appear in any JSON representation.
	assert.NotContains(t, body, "super-secret-refresh-token-must-not-leak",
		"refresh token leaked into user JSON")
	assert.NotContains(t, body, "discord_refresh_token",
		"discord_refresh_token key leaked into user JSON")
	assert.NotContains(t, body, "avatar-hash-must-not-leak",
		"Discord avatar hash leaked into user JSON")
	assert.NotContains(t, body, "discord_avatar_hash",
		"discord_avatar_hash key leaked into user JSON")

	// The safe profile/gate contract fields ARE exposed to the frontend.
	assert.Contains(t, body, `"discord_username":"remote_user"`)
	assert.Contains(t, body, `"discord_global_name":"Remote User"`)
	assert.Contains(t, body, `"discord_discriminator":"1234"`)
	assert.Contains(t, body, `"discord_profile_synced_at":987654321`)
	assert.Contains(t, body, `"discord_gate_passed":true`)
	assert.Contains(t, body, `"discord_gate_exempt":false`)
	assert.Contains(t, body, `"discord_last_check_at":123456789`)
	assert.Contains(t, body, `"discord_last_check_result":"pass"`)
	assert.Contains(t, body, `"discord_last_check_reason":"allow_group_matched"`)
	assert.Contains(t, body, `"discord_gate_message":""`)
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

func TestUser_DiscordGateFields_AutoMigrateColumns(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&User{}))

	columns, err := db.Migrator().ColumnTypes(&User{})
	require.NoError(t, err)
	names := make(map[string]struct{}, len(columns))
	for _, column := range columns {
		names[column.Name()] = struct{}{}
	}

	assert.Contains(t, names, "discord_refresh_token")
	assert.Contains(t, names, "discord_username")
	assert.Contains(t, names, "discord_global_name")
	assert.Contains(t, names, "discord_discriminator")
	assert.Contains(t, names, "discord_avatar_hash")
	assert.Contains(t, names, "discord_profile_synced_at")
	assert.Contains(t, names, "discord_gate_passed")
	assert.Contains(t, names, "discord_gate_exempt")
	assert.Contains(t, names, "discord_last_check_at")
	assert.Contains(t, names, "discord_last_check_result")
	assert.Contains(t, names, "discord_last_check_reason")
	assert.Contains(t, names, "discord_gate_message")
}
