package model

import (
	"context"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetDiscordGatePatrolEligibilitySummary(t *testing.T) {
	truncateTables(t)
	now := common.GetTimestamp()
	users := []User{
		{Username: "eligible_scope_ok", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, DiscordId: "d1", DiscordRefreshToken: "rt", DiscordGatePassed: true, DiscordGateScopeStatus: DiscordGateScopeStatusOK},
		{Username: "eligible_scope_empty", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, DiscordId: "d2", DiscordRefreshToken: "rt", DiscordGatePassed: true, DiscordGateScopeStatus: ""},
		{Username: "retry_waiting", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, DiscordId: "d3", DiscordRefreshToken: "rt", DiscordGatePassed: true, DiscordGateScopeStatus: DiscordGateScopeStatusOK, DiscordPatrolRetryAt: now + 3600},
		{Username: "disabled", Status: common.UserStatusDisabled, Role: common.RoleCommonUser, DiscordId: "d4", DiscordRefreshToken: "rt", DiscordGatePassed: true, DiscordGateScopeStatus: DiscordGateScopeStatusOK},
		{Username: "admin", Status: common.UserStatusEnabled, Role: common.RoleAdminUser, DiscordId: "d5", DiscordRefreshToken: "rt", DiscordGatePassed: true, DiscordGateScopeStatus: DiscordGateScopeStatusOK},
		{Username: "root", Status: common.UserStatusEnabled, Role: common.RoleRootUser, DiscordId: "d6", DiscordRefreshToken: "rt", DiscordGatePassed: true, DiscordGateScopeStatus: DiscordGateScopeStatusOK},
		{Username: "exempt", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, DiscordId: "d7", DiscordRefreshToken: "rt", DiscordGatePassed: true, DiscordGateScopeStatus: DiscordGateScopeStatusOK, DiscordGateExempt: true},
		{Username: "missing_binding", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, DiscordRefreshToken: "rt", DiscordGatePassed: true, DiscordGateScopeStatus: DiscordGateScopeStatusOK},
		{Username: "missing_refresh", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, DiscordId: "d8", DiscordGatePassed: true, DiscordGateScopeStatus: DiscordGateScopeStatusOK},
		{Username: "gate_not_passed", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, DiscordId: "d9", DiscordRefreshToken: "rt", DiscordGateScopeStatus: DiscordGateScopeStatusOK},
		{Username: "scope_unknown", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, DiscordId: "d10", DiscordRefreshToken: "rt", DiscordGatePassed: true, DiscordGateScopeStatus: DiscordGateScopeStatusUnknown},
		{Username: "scope_missing_guilds", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, DiscordId: "d11", DiscordRefreshToken: "rt", DiscordGatePassed: true, DiscordGateScopeStatus: DiscordGateScopeStatusMissingGuilds},
		{Username: "scope_missing_members", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, DiscordId: "d12", DiscordRefreshToken: "rt", DiscordGatePassed: true, DiscordGateScopeStatus: DiscordGateScopeStatusMissingGuildsMembersRead},
	}
	for i := range users {
		users[i].AffCode = fmt.Sprintf("patrol-aff-%d", i)
		require.NoError(t, DB.Create(&users[i]).Error)
	}

	summary, err := GetDiscordGatePatrolEligibilitySummary(context.Background())
	require.NoError(t, err)
	eligible, err := CountDiscordGatePatrolEligibleUsers(context.Background())
	require.NoError(t, err)

	assert.Equal(t, int64(len(users)), summary.TotalUsers)
	assert.Equal(t, eligible, summary.Eligible)
	assert.Equal(t, int64(2), summary.Eligible)
	assert.Equal(t, int64(1), summary.Disabled)
	assert.Equal(t, int64(2), summary.AdminOrRoot)
	assert.Equal(t, int64(1), summary.Exempt)
	assert.Equal(t, int64(1), summary.MissingDiscordBinding)
	assert.Equal(t, int64(1), summary.MissingRefreshToken)
	assert.Equal(t, int64(1), summary.GateNotPassed)
	assert.Equal(t, int64(7), summary.ScopeOK)
	assert.Equal(t, int64(2), summary.ScopeUnknown)
	assert.Equal(t, int64(1), summary.ScopeMissingGuilds)
	assert.Equal(t, int64(1), summary.ScopeMissingGuildsMembersRead)
	assert.Equal(t, int64(1), summary.RetryWaiting)
}

func TestGetDiscordGatePatrolEligibilitySummaryIgnoresSoftDeletedUsers(t *testing.T) {
	truncateTables(t)
	user := User{Username: "soft_deleted", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, AffCode: "patrol-deleted", DiscordId: "deleted", DiscordRefreshToken: "rt", DiscordGatePassed: true, DiscordGateScopeStatus: DiscordGateScopeStatusOK}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Delete(&user).Error)

	summary, err := GetDiscordGatePatrolEligibilitySummary(context.Background())
	require.NoError(t, err)
	assert.Equal(t, DiscordGatePatrolEligibilitySummary{}, summary)
}

func TestDiscordGatePatrolEligibilitySummaryJSONContract(t *testing.T) {
	summary := DiscordGatePatrolEligibilitySummary{
		TotalUsers:                    1,
		Eligible:                      2,
		Disabled:                      3,
		AdminOrRoot:                   4,
		Exempt:                        5,
		MissingDiscordBinding:         6,
		MissingRefreshToken:           7,
		GateNotPassed:                 8,
		ScopeOK:                       9,
		ScopeUnknown:                  10,
		ScopeMissingGuilds:            11,
		ScopeMissingGuildsMembersRead: 12,
		RetryWaiting:                  13,
	}

	body, err := common.Marshal(summary)
	require.NoError(t, err)
	for _, key := range []string{
		"total_users",
		"eligible",
		"disabled",
		"admin_or_root",
		"exempt",
		"missing_discord_binding",
		"missing_refresh_token",
		"gate_not_passed",
		"scope_ok",
		"scope_unknown",
		"scope_missing_guilds",
		"scope_missing_guilds_members_read",
		"retry_waiting",
	} {
		assert.Contains(t, string(body), fmt.Sprintf("\"%s\"", key))
	}
}
