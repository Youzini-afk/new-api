package oauth

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

func PatrolDiscordBanOnly(ctx context.Context, user *model.User) (DiscordPatrolOutcome, error) {
	outcome := DiscordPatrolOutcome{Result: DiscordPatrolOutcomeSkipped}
	if user == nil || user.Id == 0 {
		return outcome, fmt.Errorf("user is required")
	}
	outcome.UserID = user.Id
	evaluatedDiscordID := strings.TrimSpace(user.DiscordId)
	originalEncryptedRefreshToken := user.DiscordRefreshToken
	currentEncryptedRefreshToken := originalEncryptedRefreshToken
	if user.Status != common.UserStatusEnabled || user.Role >= common.RoleAdminUser || user.DiscordGateExempt || evaluatedDiscordID == "" {
		outcome.Reason = "not_eligible"
		return persistDiscordBanPatrolCheck(ctx, user.Id, outcome)
	}
	refreshToken, err := common.DecryptWithCryptoSecret(user.DiscordRefreshToken)
	if err != nil || strings.TrimSpace(refreshToken) == "" {
		outcome.Result = DiscordPatrolOutcomeReauthRequired
		outcome.Reason = "refresh_token_decrypt_failed"
		return persistDiscordBanPatrolCheck(ctx, user.Id, outcome)
	}
	token, invalidGrant, err := refreshDiscordAccessToken(ctx, refreshToken)
	if invalidGrant {
		outcome.Result = DiscordPatrolOutcomeReauthRequired
		outcome.Reason = "invalid_grant"
		outcome.Message = discordGateReauthMessage
		if err := service.MarkDiscordPatrolInvalidGrantAndDisableTokens(user, evaluatedDiscordID, originalEncryptedRefreshToken, true, common.GetTimestamp()); err != nil {
			if strings.Contains(err.Error(), "changed") {
				return discordPatrolStateChangedOutcome(user.Id), nil
			}
			return outcome, err
		}
		return outcome, nil
	}
	if err != nil || token == nil || strings.TrimSpace(token.AccessToken) == "" {
		outcome.Result = DiscordPatrolOutcomeTransient
		outcome.Reason = "refresh_failed"
		outcome.RetryAfter = retryAfterFromError(err)
		return persistDiscordBanPatrolCheck(ctx, user.Id, outcome)
	}
	updates := map[string]interface{}{}
	if strings.TrimSpace(token.RefreshToken) != "" && strings.TrimSpace(token.RefreshToken) != refreshToken {
		encrypted, err := common.EncryptWithCryptoSecret(token.RefreshToken)
		if err != nil {
			outcome.Result = DiscordPatrolOutcomeTransient
			outcome.Reason = "refresh_token_encrypt_failed"
			return persistDiscordBanPatrolCheck(ctx, user.Id, outcome)
		}
		updates["discord_refresh_token"] = encrypted
		currentEncryptedRefreshToken = encrypted
	}
	addDiscordScopeUpdates(token, updates)
	if len(updates) > 0 {
		updated, err := discordPatrolGuardedUserUpdate(ctx, user.Id, evaluatedDiscordID, originalEncryptedRefreshToken, updates)
		if err != nil {
			outcome.Result = DiscordPatrolOutcomeTransient
			outcome.Reason = "token_update_failed"
			return persistDiscordBanPatrolCheck(ctx, user.Id, outcome)
		}
		if !updated {
			return discordPatrolStateChangedOutcome(user.Id), nil
		}
		_ = model.InvalidateUserCache(user.Id)
	}
	cfg, err := normalizedDiscordBanPatrolConfig()
	if err != nil {
		outcome.Result = DiscordPatrolOutcomeSkipped
		outcome.Reason = "ban_groups_empty"
		return persistDiscordBanPatrolCheck(ctx, user.Id, outcome)
	}
	scopeSource := token.Scope
	if strings.TrimSpace(scopeSource) == "" {
		scopeSource = user.DiscordOAuthScopes
	}
	outcome = evaluateDiscordBanPatrol(ctx, strings.TrimSpace(token.AccessToken), scopeSource, cfg)
	outcome.UserID = user.Id
	if outcome.Result == DiscordPatrolOutcomeBanMatched {
		if err := service.BanUserForDiscordBanPatrolAndDisableTokens(user, evaluatedDiscordID, currentEncryptedRefreshToken, "Discord ban patrol: banned guild matched", truncateRunes(outcome.Reason, discordGateReasonMaxRunes), common.GetTimestamp()); err != nil {
			return outcome, err
		}
		return outcome, nil
	}
	return persistDiscordBanPatrolCheck(ctx, user.Id, outcome)
}

func normalizedDiscordBanPatrolConfig() (system_setting.DiscordRegisterGateConfig, error) {
	cfg := system_setting.GetDiscordPatrolGateConfig()
	system_setting.NormalizeDiscordRegisterGate(&cfg)
	cfg.Groups = nil
	if err := system_setting.ValidateDiscordRegisterGate(cfg); err != nil {
		return cfg, err
	}
	if len(cfg.BanGroups) == 0 {
		return cfg, fmt.Errorf("discord ban patrol config has no ban groups")
	}
	return cfg, nil
}

func evaluateDiscordBanPatrol(ctx context.Context, accessToken, scopes string, cfg system_setting.DiscordRegisterGateConfig) DiscordPatrolOutcome {
	normalizedScopes := model.NormalizeDiscordOAuthScopes(scopes)
	knownScope := strings.TrimSpace(normalizedScopes) != ""
	hasGuilds := discordScopeSetHas(normalizedScopes, model.DiscordRequiredScopeGuilds)
	hasMembersRead := discordScopeSetHas(normalizedScopes, model.DiscordRequiredScopeGuildsMembersRead)
	var guildIDs map[string]struct{}
	if hasGuilds {
		guilds := fetchDiscordGuildList(ctx, accessToken)
		if guilds.Err != nil {
			if guilds.Diagnostic.Category == discordCategoryUnauthorized || guilds.Diagnostic.Category == discordCategoryForbidden {
				return DiscordPatrolOutcome{Result: DiscordPatrolOutcomeReauthRequired, Reason: "scope_missing_guilds"}
			}
			return DiscordPatrolOutcome{Result: DiscordPatrolOutcomeTransient, Reason: string(guilds.Diagnostic.Category), RetryAfter: guilds.Diagnostic.RetryAfter}
		}
		guildIDs = guilds.GuildIDs
	}
	if !hasGuilds && !hasMembersRead && knownScope {
		return DiscordPatrolOutcome{Result: DiscordPatrolOutcomeReauthRequired, Reason: "scope_missing_discord_ban_patrol"}
	}
	allowMemberProbe := hasMembersRead || !knownScope
	cache := map[string]discordMemberFetchResult{}
	transient := DiscordPatrolOutcome{}
	reauth := DiscordPatrolOutcome{}
	for _, group := range cfg.BanGroups {
		outcome := evaluateDiscordBanPatrolGroup(ctx, accessToken, group, hasGuilds, allowMemberProbe, guildIDs, cache)
		switch outcome.Result {
		case DiscordPatrolOutcomeBanMatched:
			return outcome
		case DiscordPatrolOutcomeTransient:
			if transient.Result == "" {
				transient = outcome
			}
		case DiscordPatrolOutcomeReauthRequired:
			if reauth.Result == "" {
				reauth = outcome
			}
		}
	}
	if transient.Result != "" {
		return transient
	}
	if reauth.Result != "" {
		return reauth
	}
	return DiscordPatrolOutcome{Result: DiscordPatrolOutcomePass, Reason: "ban_group_not_matched"}
}

func evaluateDiscordBanPatrolGroup(ctx context.Context, accessToken string, group system_setting.DiscordGateGroup, hasGuilds, allowMemberProbe bool, guildIDs map[string]struct{}, cache map[string]discordMemberFetchResult) DiscordPatrolOutcome {
	if len(group.Rules) == 0 {
		return DiscordPatrolOutcome{Result: DiscordPatrolOutcomePass, Reason: "ban_group_empty"}
	}
	for _, rule := range group.Rules {
		outcome := evaluateDiscordBanPatrolRule(ctx, accessToken, rule, hasGuilds, allowMemberProbe, guildIDs, cache)
		if outcome.Result != DiscordPatrolOutcomeBanMatched {
			return outcome
		}
	}
	return DiscordPatrolOutcome{Result: DiscordPatrolOutcomeBanMatched, Reason: "ban_group_matched"}
}

func evaluateDiscordBanPatrolRule(ctx context.Context, accessToken string, rule system_setting.DiscordGateRule, hasGuilds, allowMemberProbe bool, guildIDs map[string]struct{}, cache map[string]discordMemberFetchResult) DiscordPatrolOutcome {
	guildID := strings.TrimSpace(rule.GuildID)
	if guildID == "" {
		return DiscordPatrolOutcome{Result: DiscordPatrolOutcomePass, Reason: "ban_rule_empty_guild"}
	}
	needsMember := len(rule.RoleIDs) > 0
	if hasGuilds {
		if _, ok := guildIDs[guildID]; !ok {
			return DiscordPatrolOutcome{Result: DiscordPatrolOutcomePass, Reason: "ban_guild_not_present"}
		}
		if !needsMember {
			return DiscordPatrolOutcome{Result: DiscordPatrolOutcomeBanMatched, Reason: "ban_guild_matched"}
		}
	}
	if !allowMemberProbe {
		return DiscordPatrolOutcome{Result: DiscordPatrolOutcomeReauthRequired, Reason: "scope_missing_guilds_members_read"}
	}
	fetch := fetchDiscordGuildMember(ctx, accessToken, guildID, cache)
	if fetch.Err != nil {
		return DiscordPatrolOutcome{Result: DiscordPatrolOutcomeTransient, Reason: string(fetch.Diagnostic.Category), RetryAfter: fetch.Diagnostic.RetryAfter}
	}
	switch fetch.Status {
	case http.StatusOK:
		if fetch.Member == nil {
			return DiscordPatrolOutcome{Result: DiscordPatrolOutcomeTransient, Reason: "member_unknown"}
		}
		if needsMember && !discordRolesMatch(fetch.Member.Roles, rule.RoleIDs, rule.RoleMatch) {
			return DiscordPatrolOutcome{Result: DiscordPatrolOutcomePass, Reason: "ban_role_not_matched"}
		}
		return DiscordPatrolOutcome{Result: DiscordPatrolOutcomeBanMatched, Reason: "ban_member_matched"}
	case http.StatusNotFound:
		return DiscordPatrolOutcome{Result: DiscordPatrolOutcomePass, Reason: "ban_member_not_found"}
	case http.StatusUnauthorized, http.StatusForbidden:
		return DiscordPatrolOutcome{Result: DiscordPatrolOutcomeReauthRequired, Reason: "scope_missing_guilds_members_read"}
	default:
		return DiscordPatrolOutcome{Result: DiscordPatrolOutcomeTransient, Reason: string(fetch.Diagnostic.Category), RetryAfter: fetch.Diagnostic.RetryAfter}
	}
}

func persistDiscordBanPatrolCheck(ctx context.Context, userID int, outcome DiscordPatrolOutcome) (DiscordPatrolOutcome, error) {
	if userID <= 0 {
		return outcome, fmt.Errorf("user is required")
	}
	updates := map[string]interface{}{
		"discord_ban_patrol_last_check_at":     common.GetTimestamp(),
		"discord_ban_patrol_last_check_result": outcome.Result,
		"discord_ban_patrol_last_check_reason": truncateRunes(outcome.Reason, discordGateReasonMaxRunes),
	}
	if outcome.Result != DiscordPatrolOutcomeTransient {
		updates["discord_ban_patrol_retry_at"] = 0
		updates["discord_ban_patrol_retry_count"] = 0
		updates["discord_ban_patrol_last_error"] = ""
	}
	if err := model.DB.WithContext(ctx).Model(&model.User{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
		return outcome, err
	}
	_ = model.InvalidateUserCache(userID)
	return outcome, nil
}

func discordScopeSetHas(normalizedScopes, want string) bool {
	for _, scope := range strings.Fields(normalizedScopes) {
		if scope == want {
			return true
		}
	}
	return false
}
