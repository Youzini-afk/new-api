package model

import (
	"context"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscordBanPatrolCandidates(t *testing.T) {
	truncateTables(t)
	now := common.GetTimestamp()
	users := []User{
		{Username: "gate_failed", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, DiscordId: "d1", DiscordRefreshToken: "rt", DiscordGatePassed: false, DiscordGateScopeStatus: DiscordGateScopeStatusMissingGuilds},
		{Username: "scope_missing_members", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, DiscordId: "d2", DiscordRefreshToken: "rt", DiscordGatePassed: true, DiscordGateScopeStatus: DiscordGateScopeStatusMissingGuildsMembersRead},
		{Username: "scope_null", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, DiscordId: "d3", DiscordRefreshToken: "rt", DiscordGatePassed: true},
		{Username: "disabled", Status: common.UserStatusDisabled, Role: common.RoleCommonUser, DiscordId: "d4", DiscordRefreshToken: "rt"},
		{Username: "admin", Status: common.UserStatusEnabled, Role: common.RoleAdminUser, DiscordId: "d5", DiscordRefreshToken: "rt"},
		{Username: "exempt", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, DiscordId: "d6", DiscordRefreshToken: "rt", DiscordGateExempt: true},
		{Username: "missing_refresh", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, DiscordId: "d7"},
		{Username: "missing_discord", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, DiscordRefreshToken: "rt"},
		{Username: "retry_waiting", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, DiscordId: "d8", DiscordRefreshToken: "rt", DiscordBanPatrolRetryAt: now + 3600},
	}
	for i := range users {
		users[i].AffCode = fmt.Sprintf("ban-patrol-aff-%d", i)
		require.NoError(t, DB.Create(&users[i]).Error)
	}
	nullScopeUpdate := DB.Model(&User{}).Where("username = ?", "scope_null").Update("discord_gate_scope_status", nil)
	require.NoError(t, nullScopeUpdate.Error)
	require.Equal(t, int64(1), nullScopeUpdate.RowsAffected)

	count, err := CountDiscordBanPatrolCandidateUsers(context.Background())
	require.NoError(t, err)
	candidates, err := FindDiscordBanPatrolCandidateUsers(context.Background(), 10)
	require.NoError(t, err)
	names := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		names[candidate.Username] = struct{}{}
	}

	assert.Equal(t, int64(3), count)
	assert.Len(t, candidates, 3)
	assert.Contains(t, names, "gate_failed")
	assert.Contains(t, names, "scope_missing_members")
	assert.Contains(t, names, "scope_null")
	assert.NotContains(t, names, "disabled")
	assert.NotContains(t, names, "admin")
	assert.NotContains(t, names, "exempt")
	assert.NotContains(t, names, "missing_refresh")
	assert.NotContains(t, names, "missing_discord")
	assert.NotContains(t, names, "retry_waiting")
}
