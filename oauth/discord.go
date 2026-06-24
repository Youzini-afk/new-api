package oauth

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

func init() {
	Register("discord", &DiscordProvider{})
}

// DiscordProvider implements OAuth for Discord
type DiscordProvider struct{}

type discordOAuthResponse struct {
	AccessToken  string `json:"access_token"`
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

type discordUser struct {
	UID  string `json:"id"`
	ID   string `json:"username"`
	Name string `json:"global_name"`
}

type discordGuildMember struct {
	Roles    []string   `json:"roles"`
	JoinedAt *time.Time `json:"joined_at"`
}

type discordGateDecision string

const (
	discordGateDecisionPass    discordGateDecision = "pass"
	discordGateDecisionDeny    discordGateDecision = "deny"
	discordGateDecisionBan     discordGateDecision = "ban"
	discordGateDecisionUnknown discordGateDecision = "unknown"

	discordGateDefaultFailMessage        = "Please join the required Discord server and complete role verification before continuing."
	discordGateDefaultBanMessage         = "This Discord account is not allowed to access this service."
	discordGateServiceUnavailableMessage = "Discord verification is temporarily unavailable. Please try again later."
	discordGateInvalidConfigMessage      = "Discord gate is not configured correctly. Please contact the administrator."
)

type discordGateResult struct {
	Decision discordGateDecision
	Message  string
	Reason   string
}

type discordMemberFetchResult struct {
	Member *discordGuildMember
	Status int
	Err    error
}

type discordRuleResult int

const (
	discordRuleFail discordRuleResult = iota
	discordRulePass
	discordRuleUnknown
)

var (
	discordAPIBaseURL = "https://discord.com/api/v10"
	discordHTTPClient = &http.Client{Timeout: 5 * time.Second}
)

func (p *DiscordProvider) GetName() string {
	return "Discord"
}

func (p *DiscordProvider) IsEnabled() bool {
	return system_setting.GetDiscordSettings().Enabled
}

func (p *DiscordProvider) ExchangeToken(ctx context.Context, code string, c *gin.Context) (*OAuthToken, error) {
	if code == "" {
		return nil, NewOAuthError(i18n.MsgOAuthInvalidCode, nil)
	}

	logger.LogDebug(ctx, "[OAuth-Discord] ExchangeToken: code=%s...", code[:min(len(code), 10)])

	settings := system_setting.GetDiscordSettings()
	redirectUri := fmt.Sprintf("%s/oauth/discord", system_setting.ServerAddress)
	values := url.Values{}
	values.Set("client_id", settings.ClientId)
	values.Set("client_secret", settings.ClientSecret)
	values.Set("code", code)
	values.Set("grant_type", "authorization_code")
	values.Set("redirect_uri", redirectUri)

	logger.LogDebug(ctx, "[OAuth-Discord] ExchangeToken: redirect_uri=%s", redirectUri)

	req, err := http.NewRequestWithContext(ctx, "POST", "https://discord.com/api/v10/oauth2/token", strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := http.Client{
		Timeout: 5 * time.Second,
	}
	res, err := client.Do(req)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Discord] ExchangeToken error: %s", err.Error()))
		return nil, NewOAuthErrorWithRaw(i18n.MsgOAuthConnectFailed, map[string]any{"Provider": "Discord"}, err.Error())
	}
	defer res.Body.Close()

	logger.LogDebug(ctx, "[OAuth-Discord] ExchangeToken response status: %d", res.StatusCode)

	var discordResponse discordOAuthResponse
	if err = common.DecodeJson(res.Body, &discordResponse); err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Discord] ExchangeToken decode error: %s", err.Error()))
		return nil, err
	}

	if discordResponse.AccessToken == "" {
		logger.LogError(ctx, "[OAuth-Discord] ExchangeToken failed: empty access token")
		return nil, NewOAuthError(i18n.MsgOAuthTokenFailed, map[string]any{"Provider": "Discord"})
	}

	logger.LogDebug(ctx, "[OAuth-Discord] ExchangeToken success: scope=%s", discordResponse.Scope)

	return &OAuthToken{
		AccessToken:  discordResponse.AccessToken,
		TokenType:    discordResponse.TokenType,
		RefreshToken: discordResponse.RefreshToken,
		ExpiresIn:    discordResponse.ExpiresIn,
		Scope:        discordResponse.Scope,
		IDToken:      discordResponse.IDToken,
	}, nil
}

func (p *DiscordProvider) GetUserInfo(ctx context.Context, token *OAuthToken) (*OAuthUser, error) {
	logger.LogDebug(ctx, "[OAuth-Discord] GetUserInfo: fetching user info")

	req, err := http.NewRequestWithContext(ctx, "GET", "https://discord.com/api/v10/users/@me", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)

	client := http.Client{
		Timeout: 5 * time.Second,
	}
	res, err := client.Do(req)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Discord] GetUserInfo error: %s", err.Error()))
		return nil, NewOAuthErrorWithRaw(i18n.MsgOAuthConnectFailed, map[string]any{"Provider": "Discord"}, err.Error())
	}
	defer res.Body.Close()

	logger.LogDebug(ctx, "[OAuth-Discord] GetUserInfo response status: %d", res.StatusCode)

	if res.StatusCode != http.StatusOK {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Discord] GetUserInfo failed: status=%d", res.StatusCode))
		return nil, NewOAuthError(i18n.MsgOAuthGetUserErr, nil)
	}

	var discordUser discordUser
	if err = common.DecodeJson(res.Body, &discordUser); err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Discord] GetUserInfo decode error: %s", err.Error()))
		return nil, err
	}

	if discordUser.UID == "" || discordUser.ID == "" {
		logger.LogError(ctx, "[OAuth-Discord] GetUserInfo failed: empty user fields")
		return nil, NewOAuthError(i18n.MsgOAuthUserInfoEmpty, map[string]any{"Provider": "Discord"})
	}

	logger.LogDebug(ctx, "[OAuth-Discord] GetUserInfo success: uid=%s, username=%s, name=%s", discordUser.UID, discordUser.ID, discordUser.Name)

	return &OAuthUser{
		ProviderUserID: discordUser.UID,
		Username:       discordUser.ID,
		DisplayName:    discordUser.Name,
	}, nil
}

func (p *DiscordProvider) IsUserIDTaken(providerUserID string) bool {
	return model.IsDiscordIdAlreadyTaken(providerUserID)
}

func (p *DiscordProvider) FillUserByProviderID(user *model.User, providerUserID string) error {
	user.DiscordId = providerUserID
	return user.FillUserByDiscordId()
}

func (p *DiscordProvider) SetProviderUserID(user *model.User, providerUserID string) {
	user.DiscordId = providerUserID
}

func (p *DiscordProvider) GetProviderPrefix() string {
	return "discord_"
}

// PreUserMutation implements the optional Discord gate for OAuth create, bind
// and login flows. It only validates Discord OAuth flows; non-Discord providers
// do not implement this hook and remain unaffected.
func (p *DiscordProvider) PreUserMutation(ctx context.Context, preCtx PreUserMutationContext) error {
	settings := system_setting.GetDiscordSettings()
	gateEnabled := false
	switch preCtx.Flow {
	case OAuthFlowCreate, OAuthFlowBind:
		gateEnabled = settings.RegisterGateEnabled
	case OAuthFlowLogin, OAuthFlowExisting:
		gateEnabled = settings.LoginGateEnabled
	}
	if !gateEnabled {
		return nil
	}
	if preCtx.CurrentUser != nil && preCtx.CurrentUser.DiscordGateExempt {
		return nil
	}
	if preCtx.Token == nil || strings.TrimSpace(preCtx.Token.AccessToken) == "" {
		return &AccessDeniedError{Message: discordGateServiceUnavailableMessage}
	}

	cfg := settings.RegisterGate
	system_setting.NormalizeDiscordRegisterGate(&cfg)
	if err := system_setting.ValidateDiscordRegisterGate(cfg); err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Discord] invalid gate config: %s", err.Error()))
		return &AccessDeniedError{Message: discordGateInvalidConfigMessage}
	}

	gateResult := evaluateDiscordGate(ctx, strings.TrimSpace(preCtx.Token.AccessToken), cfg)
	if !discordGateAllowsFlow(preCtx.Flow, preCtx.CurrentUser, gateResult) {
		return &AccessDeniedError{Message: discordGateUserMessage(gateResult, cfg)}
	}
	if discordGateNeedsRefreshToken(preCtx) && strings.TrimSpace(preCtx.Token.RefreshToken) == "" {
		logger.LogError(ctx, "[OAuth-Discord] gate passed but refresh token was not returned; offline_access scope may be missing")
		return &AccessDeniedError{Message: discordGateReauthMessage}
	}

	if gateResult.Decision == discordGateDecisionPass && preCtx.Result != nil {
		preCtx.Result.DiscordGatePassed = true
		preCtx.Result.HasDiscordGateUpdate = true
	}
	if preCtx.Result != nil {
		preCtx.Result.DiscordLastCheckAt = time.Now().Unix()
		preCtx.Result.DiscordLastCheckResult = string(gateResult.Decision)
		preCtx.Result.DiscordLastCheckReason = truncateRunes(gateResult.Reason, discordGateReasonMaxRunes)
		preCtx.Result.DiscordGateMessage = truncateRunes(discordGateUserMessage(gateResult, cfg), discordGateMessageMaxRunes)
		if gateResult.Decision == discordGateDecisionPass {
			preCtx.Result.DiscordGateMessage = ""
		}
		preCtx.Result.HasDiscordCheckUpdate = true
	}
	if err := fillDiscordRefreshTokenResult(preCtx.Token, preCtx.Result); err != nil {
		return err
	}
	return nil
}

func discordGateNeedsRefreshToken(preCtx PreUserMutationContext) bool {
	if preCtx.Flow == OAuthFlowCreate || preCtx.Flow == OAuthFlowBind {
		return true
	}
	return preCtx.CurrentUser == nil || strings.TrimSpace(preCtx.CurrentUser.DiscordRefreshToken) == ""
}

func fillDiscordRefreshTokenResult(token *OAuthToken, result *PreUserMutationResult) error {
	if token == nil || result == nil {
		return nil
	}
	refreshToken := strings.TrimSpace(token.RefreshToken)
	if refreshToken == "" {
		return nil
	}
	encrypted, err := common.EncryptWithCryptoSecret(refreshToken)
	if err != nil {
		return fmt.Errorf("encrypt Discord refresh token: %w", err)
	}
	result.EncryptedDiscordRefreshToken = encrypted
	result.HasDiscordRefreshTokenUpdate = true
	return nil
}

func discordGateAllowsFlow(flow OAuthFlow, currentUser *model.User, result discordGateResult) bool {
	if result.Decision == discordGateDecisionPass {
		return true
	}
	if flow == OAuthFlowLogin || flow == OAuthFlowExisting {
		return result.Decision == discordGateDecisionUnknown && currentUser != nil && currentUser.DiscordGatePassed
	}
	return false
}

func discordGateUserMessage(result discordGateResult, cfg system_setting.DiscordRegisterGateConfig) string {
	message := strings.TrimSpace(result.Message)
	if message != "" {
		return message
	}
	switch result.Decision {
	case discordGateDecisionBan:
		if msg := strings.TrimSpace(cfg.BanMessage); msg != "" {
			return msg
		}
		return discordGateDefaultBanMessage
	case discordGateDecisionUnknown:
		return discordGateServiceUnavailableMessage
	default:
		if msg := strings.TrimSpace(cfg.FailMessage); msg != "" {
			return msg
		}
		return discordGateDefaultFailMessage
	}
}

func evaluateDiscordGate(ctx context.Context, accessToken string, cfg system_setting.DiscordRegisterGateConfig) discordGateResult {
	if len(cfg.Groups) == 0 && len(cfg.BanGroups) == 0 {
		return discordGateResult{
			Decision: discordGateDecisionDeny,
			Message:  discordGateInvalidConfigMessage,
			Reason:   "config_empty",
		}
	}
	cache := make(map[string]discordMemberFetchResult)
	banUnknown := false
	for _, group := range cfg.BanGroups {
		result := evaluateDiscordGateGroup(ctx, accessToken, group, true, cache)
		switch result {
		case discordRulePass:
			message := strings.TrimSpace(cfg.BanMessage)
			if message == "" {
				message = discordGateDefaultBanMessage
			}
			return discordGateResult{Decision: discordGateDecisionBan, Message: message, Reason: "ban_group_matched"}
		case discordRuleUnknown:
			banUnknown = true
		}
	}
	if banUnknown {
		return discordGateResult{Decision: discordGateDecisionUnknown, Message: discordGateServiceUnavailableMessage, Reason: "ban_group_unknown"}
	}

	if len(cfg.Groups) == 0 {
		return discordGateResult{
			Decision: discordGateDecisionDeny,
			Message:  discordGateInvalidConfigMessage,
			Reason:   "allow_groups_empty",
		}
	}
	unknown := false
	for _, group := range cfg.Groups {
		result := evaluateDiscordGateGroup(ctx, accessToken, group, false, cache)
		switch result {
		case discordRulePass:
			return discordGateResult{Decision: discordGateDecisionPass, Reason: "allow_group_matched"}
		case discordRuleUnknown:
			unknown = true
		}
	}
	if unknown {
		return discordGateResult{Decision: discordGateDecisionUnknown, Message: discordGateServiceUnavailableMessage, Reason: "allow_group_unknown"}
	}
	return discordGateResult{
		Decision: discordGateDecisionDeny,
		Message:  discordGateUserMessage(discordGateResult{Decision: discordGateDecisionDeny}, cfg),
		Reason:   "allow_group_not_matched",
	}
}

func evaluateDiscordGateGroup(ctx context.Context, accessToken string, group system_setting.DiscordGateGroup, isBan bool, cache map[string]discordMemberFetchResult) discordRuleResult {
	if len(group.Rules) == 0 {
		return discordRuleFail
	}
	unknown := false
	for _, rule := range group.Rules {
		result := evaluateDiscordGateRule(ctx, accessToken, rule, isBan, cache)
		switch result {
		case discordRuleFail:
			return result
		case discordRuleUnknown:
			unknown = true
		}
	}
	if unknown {
		return discordRuleUnknown
	}
	return discordRulePass
}

func evaluateDiscordGateRule(ctx context.Context, accessToken string, rule system_setting.DiscordGateRule, isBan bool, cache map[string]discordMemberFetchResult) discordRuleResult {
	fetch := fetchDiscordGuildMember(ctx, accessToken, rule.GuildID, cache)
	if fetch.Err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Discord] guild member fetch failed: guild=%s err=%s", rule.GuildID, fetch.Err.Error()))
		return discordRuleUnknown
	}
	switch fetch.Status {
	case http.StatusOK:
		// continue below
	case http.StatusNotFound:
		return discordRuleFail
	default:
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Discord] guild member fetch unexpected status: guild=%s status=%d", rule.GuildID, fetch.Status))
		return discordRuleUnknown
	}
	if fetch.Member == nil {
		return discordRuleUnknown
	}
	if len(rule.RoleIDs) > 0 && !discordRolesMatch(fetch.Member.Roles, rule.RoleIDs, rule.RoleMatch) {
		return discordRuleFail
	}
	if isBan {
		return discordRulePass
	}
	if rule.MinJoinHours > 0 && !discordMemberJoinedLongEnough(fetch.Member, rule.MinJoinHours) {
		return discordRuleFail
	}
	return discordRulePass
}

func fetchDiscordGuildMember(ctx context.Context, accessToken, guildID string, cache map[string]discordMemberFetchResult) discordMemberFetchResult {
	guildID = strings.TrimSpace(guildID)
	if cached, ok := cache[guildID]; ok {
		return cached
	}
	result := discordMemberFetchResult{}
	defer func() { cache[guildID] = result }()

	endpoint := strings.TrimRight(discordAPIBaseURL, "/") + "/users/@me/guilds/" + url.PathEscape(guildID) + "/member"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		result.Err = err
		return result
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	client := discordHTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		result.Err = err
		return result
	}
	defer res.Body.Close()
	result.Status = res.StatusCode
	if res.StatusCode != http.StatusOK {
		return result
	}
	var member discordGuildMember
	if err := common.DecodeJson(res.Body, &member); err != nil {
		result.Err = err
		return result
	}
	result.Member = &member
	return result
}

func discordRolesMatch(userRoles, requiredRoles []string, roleMatch string) bool {
	if len(requiredRoles) == 0 {
		return false
	}
	userRoleSet := make(map[string]struct{}, len(userRoles))
	for _, role := range userRoles {
		userRoleSet[strings.TrimSpace(role)] = struct{}{}
	}
	roleMatch = strings.ToLower(strings.TrimSpace(roleMatch))
	if roleMatch == "" {
		roleMatch = "any"
	}
	switch roleMatch {
	case "all":
		for _, required := range requiredRoles {
			if _, ok := userRoleSet[strings.TrimSpace(required)]; !ok {
				return false
			}
		}
		return true
	default:
		for _, required := range requiredRoles {
			if _, ok := userRoleSet[strings.TrimSpace(required)]; ok {
				return true
			}
		}
		return false
	}
}

func discordMemberJoinedLongEnough(member *discordGuildMember, minJoinHours int) bool {
	if minJoinHours <= 0 {
		return true
	}
	if member == nil || member.JoinedAt == nil || member.JoinedAt.IsZero() {
		return false
	}
	return time.Since(member.JoinedAt.UTC()) >= time.Duration(minJoinHours)*time.Hour
}
