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
		{Username: "eligible_scope_null", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, DiscordId: "d13", DiscordRefreshToken: "rt", DiscordGatePassed: true, DiscordGateScopeStatus: ""},
		{Username: "eligible_exempt_null", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, DiscordId: "d14", DiscordRefreshToken: "rt", DiscordGatePassed: true, DiscordGateScopeStatus: DiscordGateScopeStatusOK},
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
	nullScopeUpdate := DB.Model(&User{}).Where("username = ?", "eligible_scope_null").Update("discord_gate_scope_status", nil)
	require.NoError(t, nullScopeUpdate.Error)
	require.Equal(t, int64(1), nullScopeUpdate.RowsAffected)
	nullExemptUpdate := DB.Model(&User{}).Where("username = ?", "eligible_exempt_null").Update("discord_gate_exempt", nil)
	require.NoError(t, nullExemptUpdate.Error)
	require.Equal(t, int64(1), nullExemptUpdate.RowsAffected)

	summary, err := GetDiscordGatePatrolEligibilitySummary(context.Background())
	require.NoError(t, err)
	eligible, err := CountDiscordGatePatrolEligibleUsers(context.Background())
	require.NoError(t, err)
	candidates, err := FindDiscordGatePatrolEligibleUsers(context.Background(), 10)
	require.NoError(t, err)
	candidateNames := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		candidateNames[candidate.Username] = struct{}{}
	}

	assert.Equal(t, int64(len(users)), summary.TotalUsers)
	assert.Equal(t, eligible, summary.Eligible)
	assert.Equal(t, int64(4), summary.Eligible)
	assert.Contains(t, candidateNames, "eligible_scope_null")
	assert.Contains(t, candidateNames, "eligible_exempt_null")
	assert.Equal(t, int64(1), summary.Disabled)
	assert.Equal(t, int64(2), summary.AdminOrRoot)
	assert.Equal(t, int64(1), summary.Exempt)
	assert.Equal(t, int64(1), summary.MissingDiscordBinding)
	assert.Equal(t, int64(1), summary.MissingRefreshToken)
	assert.Equal(t, int64(1), summary.GateNotPassed)
	assert.Equal(t, int64(8), summary.ScopeOK)
	assert.Equal(t, int64(3), summary.ScopeUnknown)
	assert.Equal(t, int64(1), summary.ScopeMissingGuilds)
	assert.Equal(t, int64(1), summary.ScopeMissingGuildsMembersRead)
	assert.Equal(t, int64(1), summary.RetryWaiting)
}

func TestDiscordGatePatrolEligibilitySummaryClassifiesNullBuckets(t *testing.T) {
	truncateTables(t)
	users := []User{
		{Username: "null_status", Role: common.RoleCommonUser, AffCode: "null-status-aff", DiscordId: "d1", DiscordRefreshToken: "rt", DiscordGatePassed: true, DiscordGateScopeStatus: DiscordGateScopeStatusOK},
		{Username: "null_discord", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, AffCode: "null-discord-aff", DiscordRefreshToken: "rt", DiscordGatePassed: true, DiscordGateScopeStatus: DiscordGateScopeStatusOK},
		{Username: "null_refresh", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, AffCode: "null-refresh-aff", DiscordId: "d2", DiscordGatePassed: true, DiscordGateScopeStatus: DiscordGateScopeStatusOK},
		{Username: "null_gate_passed", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, AffCode: "null-gate-aff", DiscordId: "d3", DiscordRefreshToken: "rt", DiscordGateScopeStatus: DiscordGateScopeStatusOK},
	}
	for i := range users {
		require.NoError(t, DB.Create(&users[i]).Error)
	}
	require.NoError(t, DB.Model(&User{}).Where("username = ?", "null_status").Update("status", nil).Error)
	require.NoError(t, DB.Model(&User{}).Where("username = ?", "null_discord").Update("discord_id", nil).Error)
	require.NoError(t, DB.Model(&User{}).Where("username = ?", "null_refresh").Update("discord_refresh_token", nil).Error)
	require.NoError(t, DB.Model(&User{}).Where("username = ?", "null_gate_passed").Update("discord_gate_passed", nil).Error)

	summary, err := GetDiscordGatePatrolEligibilitySummary(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1), summary.Disabled)
	assert.Equal(t, int64(1), summary.MissingDiscordBinding)
	assert.Equal(t, int64(1), summary.MissingRefreshToken)
	assert.Equal(t, int64(1), summary.GateNotPassed)
	assert.Equal(t, int64(2), summary.ScopeOK)
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

func TestDiscordGatePatrolEligibleTreatsNullRetryAsReady(t *testing.T) {
	truncateTables(t)
	user := User{
		Username:               "null_retry_full_patrol",
		Status:                 common.UserStatusEnabled,
		Role:                   common.RoleCommonUser,
		AffCode:                "full-patrol-null-retry-aff",
		DiscordId:              "discord-null-retry",
		DiscordRefreshToken:    "rt",
		DiscordGatePassed:      true,
		DiscordGateScopeStatus: DiscordGateScopeStatusOK,
	}
	require.NoError(t, DB.Create(&user).Error)
	nullRetryUpdate := DB.Model(&User{}).Where("id = ?", user.Id).Update("discord_patrol_retry_at", nil)
	require.NoError(t, nullRetryUpdate.Error)
	require.Equal(t, int64(1), nullRetryUpdate.RowsAffected)

	count, err := CountDiscordGatePatrolEligibleUsers(context.Background())
	require.NoError(t, err)
	candidates, err := FindDiscordGatePatrolEligibleUsers(context.Background(), 10)
	require.NoError(t, err)
	summary, err := GetDiscordGatePatrolEligibilitySummary(context.Background())
	require.NoError(t, err)

	require.Equal(t, int64(1), count)
	require.Len(t, candidates, 1)
	assert.Equal(t, "null_retry_full_patrol", candidates[0].Username)
	assert.Equal(t, count, summary.Eligible)
	assert.Equal(t, int64(0), summary.RetryWaiting)
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
