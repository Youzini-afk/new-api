package model

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type DiscordGatePatrolEligibilitySummary struct {
	TotalUsers                    int64 `json:"total_users"`
	Eligible                      int64 `json:"eligible"`
	Disabled                      int64 `json:"disabled"`
	AdminOrRoot                   int64 `json:"admin_or_root"`
	Exempt                        int64 `json:"exempt"`
	MissingDiscordBinding         int64 `json:"missing_discord_binding"`
	MissingRefreshToken           int64 `json:"missing_refresh_token"`
	GateNotPassed                 int64 `json:"gate_not_passed"`
	ScopeOK                       int64 `json:"scope_ok"`
	ScopeUnknown                  int64 `json:"scope_unknown"`
	ScopeMissingGuilds            int64 `json:"scope_missing_guilds"`
	ScopeMissingGuildsMembersRead int64 `json:"scope_missing_guilds_members_read"`
	RetryWaiting                  int64 `json:"retry_waiting"`
}

func discordGatePatrolEligibleQuery(ctx context.Context, now int64) *gorm.DB {
	return DB.WithContext(ctx).Model(&User{}).Where(
		"status = ? AND role < ? AND discord_gate_exempt = ? AND discord_id <> ? AND discord_refresh_token <> ? AND discord_gate_passed = ? AND (discord_gate_scope_status IS NULL OR discord_gate_scope_status = ? OR discord_gate_scope_status = ?) AND (discord_patrol_retry_at = 0 OR discord_patrol_retry_at <= ?)",
		common.UserStatusEnabled,
		common.RoleAdminUser,
		false,
		"",
		"",
		true,
		DiscordGateScopeStatusOK,
		"",
		now,
	)
}

func CountDiscordGatePatrolEligibleUsers(ctx context.Context) (int64, error) {
	var total int64
	err := discordGatePatrolEligibleQuery(ctx, common.GetTimestamp()).Count(&total).Error
	return total, err
}

func GetDiscordGatePatrolEligibilitySummary(ctx context.Context) (DiscordGatePatrolEligibilitySummary, error) {
	now := common.GetTimestamp()
	summary := DiscordGatePatrolEligibilitySummary{}
	counts := []struct {
		dest  *int64
		query *gorm.DB
	}{
		{&summary.TotalUsers, DB.WithContext(ctx).Model(&User{})},
		{&summary.Eligible, discordGatePatrolEligibleQuery(ctx, now)},
		{&summary.Disabled, DB.WithContext(ctx).Model(&User{}).Where("status <> ?", common.UserStatusEnabled)},
		{&summary.AdminOrRoot, DB.WithContext(ctx).Model(&User{}).Where("role >= ?", common.RoleAdminUser)},
		{&summary.Exempt, DB.WithContext(ctx).Model(&User{}).Where("discord_gate_exempt = ?", true)},
		{&summary.MissingDiscordBinding, DB.WithContext(ctx).Model(&User{}).Where("discord_id = ?", "")},
		{&summary.MissingRefreshToken, DB.WithContext(ctx).Model(&User{}).Where("discord_id <> ? AND discord_refresh_token = ?", "", "")},
		{&summary.GateNotPassed, DB.WithContext(ctx).Model(&User{}).Where("discord_id <> ? AND discord_refresh_token <> ? AND discord_gate_passed = ?", "", "", false)},
		{&summary.ScopeOK, DB.WithContext(ctx).Model(&User{}).Where("discord_id <> ? AND discord_refresh_token <> ? AND discord_gate_scope_status = ?", "", "", DiscordGateScopeStatusOK)},
		{&summary.ScopeUnknown, DB.WithContext(ctx).Model(&User{}).Where("discord_id <> ? AND discord_refresh_token <> ? AND (discord_gate_scope_status IS NULL OR discord_gate_scope_status = ? OR discord_gate_scope_status = ?)", "", "", DiscordGateScopeStatusUnknown, "")},
		{&summary.ScopeMissingGuilds, DB.WithContext(ctx).Model(&User{}).Where("discord_id <> ? AND discord_refresh_token <> ? AND discord_gate_scope_status = ?", "", "", DiscordGateScopeStatusMissingGuilds)},
		{&summary.ScopeMissingGuildsMembersRead, DB.WithContext(ctx).Model(&User{}).Where("discord_id <> ? AND discord_refresh_token <> ? AND discord_gate_scope_status = ?", "", "", DiscordGateScopeStatusMissingGuildsMembersRead)},
		{&summary.RetryWaiting, DB.WithContext(ctx).Model(&User{}).Where(
			"status = ? AND role < ? AND discord_gate_exempt = ? AND discord_id <> ? AND discord_refresh_token <> ? AND discord_gate_passed = ? AND (discord_gate_scope_status IS NULL OR discord_gate_scope_status = ? OR discord_gate_scope_status = ?) AND discord_patrol_retry_at > ?",
			common.UserStatusEnabled,
			common.RoleAdminUser,
			false,
			"",
			"",
			true,
			DiscordGateScopeStatusOK,
			"",
			now,
		)},
	}

	for _, count := range counts {
		if err := count.query.Count(count.dest).Error; err != nil {
			return summary, err
		}
	}
	return summary, nil
}

func FindDiscordGatePatrolEligibleUsers(ctx context.Context, limit int) ([]*User, error) {
	if limit <= 0 {
		limit = 1
	}
	now := common.GetTimestamp()
	var users []*User
	err := DB.WithContext(ctx).Where(
		"status = ? AND role < ? AND discord_gate_exempt = ? AND discord_id <> ? AND discord_refresh_token <> ? AND discord_gate_passed = ? AND (discord_gate_scope_status IS NULL OR discord_gate_scope_status = ? OR discord_gate_scope_status = ?) AND (discord_patrol_retry_at = 0 OR discord_patrol_retry_at <= ?)",
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
