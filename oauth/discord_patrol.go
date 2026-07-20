package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

const (
	DiscordPatrolOutcomePass           = "pass"
	DiscordPatrolOutcomeBanMatched     = "ban_matched"
	DiscordPatrolOutcomeAllowFailed    = "allow_failed"
	DiscordPatrolOutcomeReauthRequired = "reauth_required"
	DiscordPatrolOutcomeTransient      = "transient"
	DiscordPatrolOutcomeSkipped        = "skipped"
)

type DiscordPatrolOutcome struct {
	UserID     int           `json:"user_id"`
	Result     string        `json:"result"`
	Reason     string        `json:"reason"`
	Message    string        `json:"message,omitempty"`
	RetryAfter time.Duration `json:"-"`
}

type discordGuild struct {
	ID string `json:"id"`
}

type discordGuildListResult struct {
	GuildIDs   map[string]struct{}
	Diagnostic discordDiagnostic
	Err        error
}

func PatrolDiscordGate(ctx context.Context, user *model.User) (DiscordPatrolOutcome, error) {
	outcome := DiscordPatrolOutcome{Result: DiscordPatrolOutcomeSkipped}
	if user == nil || user.Id == 0 {
		return outcome, fmt.Errorf("user is required")
	}
	outcome.UserID = user.Id
	evaluatedDiscordID := strings.TrimSpace(user.DiscordId)
	originalEncryptedRefreshToken := user.DiscordRefreshToken
	currentEncryptedRefreshToken := originalEncryptedRefreshToken
	if user.Status != common.UserStatusEnabled || user.Role >= common.RoleAdminUser || user.DiscordGateExempt || !user.DiscordGatePassed || evaluatedDiscordID == "" {
		outcome.Reason = "not_eligible"
		return outcome, nil
	}
	refreshToken, err := common.DecryptWithCryptoSecret(user.DiscordRefreshToken)
	if err != nil || strings.TrimSpace(refreshToken) == "" {
		return markDiscordPatrolReauth(ctx, user, evaluatedDiscordID, originalEncryptedRefreshToken, "refresh_token_decrypt_failed")
	}
	token, invalidGrant, err := refreshDiscordAccessToken(ctx, refreshToken)
	if invalidGrant {
		outcome := DiscordPatrolOutcome{UserID: user.Id, Result: DiscordPatrolOutcomeReauthRequired, Reason: "invalid_grant", Message: discordGateReauthMessage}
		if err := service.MarkDiscordPatrolInvalidGrantAndDisableTokens(user, evaluatedDiscordID, originalEncryptedRefreshToken, false, common.GetTimestamp()); err != nil {
			if strings.Contains(err.Error(), "changed") {
				return discordPatrolStateChangedOutcome(user.Id), nil
			}
			return outcome, err
		}
		return outcome, nil
	}
	if err != nil || token == nil || strings.TrimSpace(token.AccessToken) == "" {
		return DiscordPatrolOutcome{UserID: user.Id, Result: DiscordPatrolOutcomeTransient, Reason: "refresh_failed", RetryAfter: retryAfterFromError(err)}, nil
	}
	updates := map[string]interface{}{}
	if strings.TrimSpace(token.RefreshToken) != "" && strings.TrimSpace(token.RefreshToken) != refreshToken {
		encrypted, err := common.EncryptWithCryptoSecret(token.RefreshToken)
		if err != nil {
			return markDiscordPatrolReauth(ctx, user, evaluatedDiscordID, originalEncryptedRefreshToken, "refresh_token_encrypt_failed")
		}
		updates["discord_refresh_token"] = encrypted
		currentEncryptedRefreshToken = encrypted
	}
	addDiscordScopeUpdates(token, updates)
	if len(updates) > 0 {
		updated, err := discordPatrolGuardedUserUpdate(ctx, user.Id, evaluatedDiscordID, originalEncryptedRefreshToken, updates)
		if err != nil {
			return DiscordPatrolOutcome{UserID: user.Id, Result: DiscordPatrolOutcomeTransient, Reason: "token_update_failed"}, err
		}
		if !updated {
			return discordPatrolStateChangedOutcome(user.Id), nil
		}
		_ = model.InvalidateUserCache(user.Id)
	}
	scopeSource := token.Scope
	if strings.TrimSpace(scopeSource) == "" {
		scopeSource = user.DiscordOAuthScopes
	}
	scopeStatus := model.DiscordGateScopeStatusForScopes(scopeSource)
	if scopeStatus != model.DiscordGateScopeStatusOK {
		return markDiscordPatrolReauth(ctx, user, evaluatedDiscordID, currentEncryptedRefreshToken, scopeStatus, scopeStatus)
	}
	cfg, err := normalizedDiscordPatrolGateConfig()
	if err != nil {
		return DiscordPatrolOutcome{UserID: user.Id, Result: DiscordPatrolOutcomeSkipped, Reason: "invalid_config", Message: discordGateInvalidConfigMessage}, nil
	}
	result := evaluateDiscordGateForPatrol(ctx, strings.TrimSpace(token.AccessToken), cfg)
	return persistDiscordPatrolOutcome(ctx, user, evaluatedDiscordID, currentEncryptedRefreshToken, cfg, result)
}

func evaluateDiscordGateForPatrol(ctx context.Context, accessToken string, cfg system_setting.DiscordRegisterGateConfig) DiscordPatrolOutcome {
	guilds := fetchDiscordGuildList(ctx, accessToken)
	if guilds.Err != nil {
		if guilds.Diagnostic.Category == discordCategoryUnauthorized || guilds.Diagnostic.Category == discordCategoryForbidden {
			return DiscordPatrolOutcome{Result: DiscordPatrolOutcomeReauthRequired, Reason: "scope_missing_guilds"}
		}
		return DiscordPatrolOutcome{Result: DiscordPatrolOutcomeTransient, Reason: string(guilds.Diagnostic.Category), RetryAfter: guilds.Diagnostic.RetryAfter}
	}
	cache := map[string]discordMemberFetchResult{}
	banTransient := DiscordPatrolOutcome{}
	for _, group := range cfg.BanGroups {
		outcome := evaluateDiscordPatrolGroup(ctx, accessToken, group, true, guilds.GuildIDs, cache)
		if outcome.Result == DiscordPatrolOutcomeBanMatched {
			return outcome
		}
		if outcome.Result == DiscordPatrolOutcomeTransient && banTransient.Result == "" {
			banTransient = outcome
		}
	}
	if banTransient.Result != "" {
		return banTransient
	}
	if len(cfg.Groups) == 0 {
		return DiscordPatrolOutcome{Result: DiscordPatrolOutcomeAllowFailed, Reason: "allow_groups_empty"}
	}
	transient := false
	transientReason := "allow_group_unknown"
	var retryAfter time.Duration
	for _, group := range cfg.Groups {
		outcome := evaluateDiscordPatrolGroup(ctx, accessToken, group, false, guilds.GuildIDs, cache)
		switch outcome.Result {
		case DiscordPatrolOutcomePass:
			return outcome
		case DiscordPatrolOutcomeTransient:
			transient = true
			transientReason = outcome.Reason
			retryAfter = outcome.RetryAfter
		}
	}
	if transient {
		return DiscordPatrolOutcome{Result: DiscordPatrolOutcomeTransient, Reason: transientReason, RetryAfter: retryAfter}
	}
	return DiscordPatrolOutcome{Result: DiscordPatrolOutcomeAllowFailed, Reason: "allow_group_not_matched"}
}

func evaluateDiscordPatrolGroup(ctx context.Context, accessToken string, group system_setting.DiscordGateGroup, isBan bool, guildIDs map[string]struct{}, cache map[string]discordMemberFetchResult) DiscordPatrolOutcome {
	if len(group.Rules) == 0 {
		return DiscordPatrolOutcome{Result: DiscordPatrolOutcomeAllowFailed, Reason: "group_empty"}
	}
	for _, rule := range group.Rules {
		if _, ok := guildIDs[strings.TrimSpace(rule.GuildID)]; !ok {
			return DiscordPatrolOutcome{Result: DiscordPatrolOutcomeAllowFailed, Reason: "guild_not_present"}
		}
		needsMember := len(rule.RoleIDs) > 0 || rule.MinJoinHours > 0
		if !needsMember {
			continue
		}
		fetch := fetchDiscordGuildMember(ctx, accessToken, rule.GuildID, cache)
		if fetch.Err != nil || fetch.Status != http.StatusOK || fetch.Member == nil {
			if fetch.Status == http.StatusNotFound {
				return DiscordPatrolOutcome{Result: DiscordPatrolOutcomeTransient, Reason: "member_unknown"}
			}
			if fetch.Diagnostic.Category == discordCategoryUnauthorized || fetch.Diagnostic.Category == discordCategoryForbidden {
				return DiscordPatrolOutcome{Result: DiscordPatrolOutcomeReauthRequired, Reason: "scope_missing_guilds_members_read"}
			}
			retryAfter := fetch.Diagnostic.RetryAfter
			return DiscordPatrolOutcome{Result: DiscordPatrolOutcomeTransient, Reason: "member_unknown", RetryAfter: retryAfter}
		}
		if len(rule.RoleIDs) > 0 && !discordRolesMatch(fetch.Member.Roles, rule.RoleIDs, rule.RoleMatch) {
			return DiscordPatrolOutcome{Result: DiscordPatrolOutcomeAllowFailed, Reason: "role_not_matched"}
		}
		if !isBan && rule.MinJoinHours > 0 && !discordMemberJoinedLongEnough(fetch.Member, rule.MinJoinHours) {
			return DiscordPatrolOutcome{Result: DiscordPatrolOutcomeAllowFailed, Reason: "min_join_hours_not_met"}
		}
	}
	if isBan {
		return DiscordPatrolOutcome{Result: DiscordPatrolOutcomeBanMatched, Reason: "ban_group_matched"}
	}
	return DiscordPatrolOutcome{Result: DiscordPatrolOutcomePass, Reason: "allow_group_matched"}
}

func fetchDiscordGuildList(ctx context.Context, accessToken string) discordGuildListResult {
	endpoint := strings.TrimRight(discordAPIBaseURL, "/") + "/users/@me/guilds"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return discordGuildListResult{Err: err, Diagnostic: discordDiagnostic{Category: discordCategoryBadResponse}}
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	client := discordHTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	if err := waitDiscordPatrolLimiter(ctx); err != nil {
		return discordGuildListResult{Err: err, Diagnostic: discordDiagnostic{Category: discordCategoryTimeout}}
	}
	res, err := client.Do(req)
	if err != nil {
		diag := classifyDiscordTransportError(err)
		return discordGuildListResult{Err: err, Diagnostic: diag}
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		diag := classifyDiscordResponseError(res)
		recordDiscordPatrolDiagnostic(ctx, diag)
		return discordGuildListResult{Err: fmt.Errorf("discord guild list: %s", diag.Raw()), Diagnostic: diag}
	}
	var guilds []discordGuild
	if err := decodeDiscordJSONLimited(res.Body, &guilds); err != nil {
		diag := discordDecodeDiagnostic(res.StatusCode, err)
		return discordGuildListResult{Err: err, Diagnostic: diag}
	}
	ids := make(map[string]struct{}, len(guilds))
	for _, guild := range guilds {
		if strings.TrimSpace(guild.ID) != "" {
			ids[strings.TrimSpace(guild.ID)] = struct{}{}
		}
	}
	return discordGuildListResult{GuildIDs: ids}
}

func persistDiscordPatrolOutcome(ctx context.Context, user *model.User, evaluatedDiscordID, currentEncryptedRefreshToken string, cfg system_setting.DiscordRegisterGateConfig, outcome DiscordPatrolOutcome) (DiscordPatrolOutcome, error) {
	outcome.UserID = user.Id
	now := common.GetTimestamp()
	updates := map[string]interface{}{
		"discord_last_check_at":      now,
		"discord_last_check_result":  outcome.Result,
		"discord_last_check_reason":  truncateRunes(outcome.Reason, discordGateReasonMaxRunes),
		"discord_patrol_retry_at":    0,
		"discord_patrol_retry_count": 0,
		"discord_patrol_last_error":  "",
	}
	switch outcome.Result {
	case DiscordPatrolOutcomePass:
		updates["discord_gate_message"] = ""
	case DiscordPatrolOutcomeBanMatched:
		message := discordGateUserMessage(discordGateResult{Decision: discordGateDecisionBan, Reason: outcome.Reason}, cfg)
		if err := service.BanUserForDiscordPatrolAndDisableTokens(
			user,
			evaluatedDiscordID,
			currentEncryptedRefreshToken,
			"Discord gate patrol: banned guild matched",
			truncateRunes(outcome.Reason, discordGateReasonMaxRunes),
			truncateRunes(message, discordGateMessageMaxRunes),
			now,
		); err != nil {
			return outcome, err
		}
		return outcome, nil
	case DiscordPatrolOutcomeAllowFailed:
		message := discordGateUserMessage(discordGateResult{Decision: discordGateDecisionDeny, Reason: outcome.Reason}, cfg)
		if err := service.MarkDiscordGateFailedAndDisableTokens(user, evaluatedDiscordID, currentEncryptedRefreshToken, truncateRunes(outcome.Reason, discordGateReasonMaxRunes), truncateRunes(message, discordGateMessageMaxRunes), now); err != nil {
			return outcome, err
		}
		return outcome, nil
	case DiscordPatrolOutcomeReauthRequired:
		updates["discord_gate_passed"] = false
		updates["discord_gate_message"] = discordGateReauthMessage
		if outcome.Reason == model.DiscordGateScopeStatusMissingGuilds || outcome.Reason == "scope_missing_guilds" {
			updates["discord_gate_scope_status"] = model.DiscordGateScopeStatusMissingGuilds
		} else if outcome.Reason == model.DiscordGateScopeStatusMissingGuildsMembersRead || outcome.Reason == "scope_missing_guilds_members_read" {
			updates["discord_gate_scope_status"] = model.DiscordGateScopeStatusMissingGuildsMembersRead
		}
	case DiscordPatrolOutcomeTransient:
		return outcome, nil
	}
	updated, err := discordPatrolGuardedUserUpdate(ctx, user.Id, evaluatedDiscordID, currentEncryptedRefreshToken, updates)
	if err != nil {
		return outcome, err
	}
	if !updated {
		return discordPatrolStateChangedOutcome(user.Id), nil
	}
	_ = model.InvalidateUserCache(user.Id)
	return outcome, nil
}

func markDiscordPatrolReauth(ctx context.Context, user *model.User, evaluatedDiscordID, currentEncryptedRefreshToken, reason string, scopeStatusOverride ...string) (DiscordPatrolOutcome, error) {
	outcome := DiscordPatrolOutcome{UserID: user.Id, Result: DiscordPatrolOutcomeReauthRequired, Reason: reason, Message: discordGateReauthMessage}
	scopeStatus := model.DiscordGateScopeStatusForScopes(user.DiscordOAuthScopes)
	if len(scopeStatusOverride) > 0 && strings.TrimSpace(scopeStatusOverride[0]) != "" {
		scopeStatus = strings.TrimSpace(scopeStatusOverride[0])
	}
	updates := map[string]interface{}{
		"discord_gate_scope_status":  scopeStatus,
		"discord_last_check_at":      common.GetTimestamp(),
		"discord_last_check_result":  outcome.Result,
		"discord_last_check_reason":  truncateRunes(reason, discordGateReasonMaxRunes),
		"discord_gate_message":       discordGateReauthMessage,
		"discord_gate_passed":        false,
		"discord_patrol_retry_at":    0,
		"discord_patrol_retry_count": 0,
		"discord_patrol_last_error":  "",
	}
	if reason == "invalid_grant" {
		updates["discord_gate_scope_status"] = model.DiscordGateScopeStatusUnknown
		updates["discord_refresh_token"] = ""
	}
	updated, err := discordPatrolGuardedUserUpdate(ctx, user.Id, evaluatedDiscordID, currentEncryptedRefreshToken, updates)
	if err != nil {
		return outcome, err
	}
	if !updated {
		return discordPatrolStateChangedOutcome(user.Id), nil
	}
	_ = model.InvalidateUserCache(user.Id)
	return outcome, nil
}

func discordPatrolGuardedUserUpdate(ctx context.Context, userID int, evaluatedDiscordID, encryptedRefreshToken string, updates map[string]interface{}) (bool, error) {
	result := model.DB.WithContext(ctx).Model(&model.User{}).Where(
		"id = ? AND status = ? AND role < ? AND (discord_gate_exempt IS NULL OR discord_gate_exempt = ?) AND discord_id = ? AND discord_refresh_token = ?",
		userID,
		common.UserStatusEnabled,
		common.RoleAdminUser,
		false,
		evaluatedDiscordID,
		encryptedRefreshToken,
	).Updates(updates)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func discordPatrolStateChangedOutcome(userID int) DiscordPatrolOutcome {
	return DiscordPatrolOutcome{UserID: userID, Result: DiscordPatrolOutcomeSkipped, Reason: "discord_oauth_state_changed"}
}

func retryAfterFromError(err error) time.Duration {
	if err == nil {
		return 0
	}
	var diagErr discordDiagnosticError
	if errors.As(err, &diagErr) {
		return diagErr.Diagnostic.RetryAfter
	}
	text := err.Error()
	marker := "retry_after="
	idx := strings.Index(text, marker)
	if idx < 0 {
		return 0
	}
	d, _ := time.ParseDuration(strings.TrimSpace(text[idx+len(marker):]))
	return d
}

func DiscordPatrolAuthorizeScope() string {
	return "identify openid guilds guilds.members.read"
}

func DiscordPatrolAuthorizeURL(clientID, state, redirectURI string) string {
	u := url.URL{Scheme: "https", Host: "discord.com", Path: "/oauth2/authorize"}
	q := u.Query()
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("scope", DiscordPatrolAuthorizeScope())
	q.Set("state", state)
	u.RawQuery = q.Encode()
	return u.String()
}
