package model

import (
	"context"

	"github.com/QuantumNous/new-api/common"
)

func CountDiscordGatePatrolEligibleUsers(ctx context.Context) (int64, error) {
	var total int64
	err := DB.WithContext(ctx).Model(&User{}).Where(
		"status = ? AND role < ? AND discord_gate_exempt = ? AND discord_id <> ? AND discord_refresh_token <> ? AND discord_gate_passed = ? AND (discord_gate_scope_status = ? OR discord_gate_scope_status = ?) AND (discord_patrol_retry_at = 0 OR discord_patrol_retry_at <= ?)",
		common.UserStatusEnabled,
		common.RoleAdminUser,
		false,
		"",
		"",
		true,
		DiscordGateScopeStatusOK,
		"",
		common.GetTimestamp(),
	).Count(&total).Error
	return total, err
}

func FindDiscordGatePatrolEligibleUsers(ctx context.Context, limit int) ([]*User, error) {
	if limit <= 0 {
		limit = 1
	}
	now := common.GetTimestamp()
	var users []*User
	err := DB.WithContext(ctx).Where(
		"status = ? AND role < ? AND discord_gate_exempt = ? AND discord_id <> ? AND discord_refresh_token <> ? AND discord_gate_passed = ? AND (discord_gate_scope_status = ? OR discord_gate_scope_status = ?) AND (discord_patrol_retry_at = 0 OR discord_patrol_retry_at <= ?)",
		common.UserStatusEnabled,
		common.RoleAdminUser,
		false,
		"",
		"",
		true,
		DiscordGateScopeStatusOK,
		"",
		now,
	).Order("CASE WHEN discord_patrol_retry_at > 0 THEN 0 ELSE 1 END asc, discord_patrol_retry_at asc, discord_last_check_at asc, id asc").Limit(limit).Find(&users).Error
	return users, err
}
