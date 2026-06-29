package model

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

func discordBanPatrolCandidateQuery(ctx context.Context, now int64) *gorm.DB {
	return DB.WithContext(ctx).Model(&User{}).Where(
		"status = ? AND role < ? AND discord_gate_exempt = ? AND discord_id <> ? AND discord_refresh_token <> ? AND (discord_ban_patrol_retry_at IS NULL OR discord_ban_patrol_retry_at = 0 OR discord_ban_patrol_retry_at <= ?)",
		common.UserStatusEnabled,
		common.RoleAdminUser,
		false,
		"",
		"",
		now,
	)
}

func CountDiscordBanPatrolCandidateUsers(ctx context.Context) (int64, error) {
	var total int64
	err := discordBanPatrolCandidateQuery(ctx, common.GetTimestamp()).Count(&total).Error
	return total, err
}

func FindDiscordBanPatrolCandidateUsers(ctx context.Context, limit int) ([]*User, error) {
	if limit <= 0 {
		limit = 1
	}
	var users []*User
	err := discordBanPatrolCandidateQuery(ctx, common.GetTimestamp()).
		Order("CASE WHEN discord_ban_patrol_retry_at > 0 THEN 0 ELSE 1 END asc, CASE WHEN discord_ban_patrol_retry_at IS NULL THEN 0 ELSE discord_ban_patrol_retry_at END asc, discord_ban_patrol_last_check_at asc, id asc").
		Limit(limit).
		Find(&users).Error
	return users, err
}
