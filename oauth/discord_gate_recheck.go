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
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

const (
	discordGateResultPass           = "pass"
	discordGateResultDeny           = "deny"
	discordGateResultBan            = "ban"
	discordGateResultUnknown        = "unknown"
	discordGateResultError          = "error"
	discordGateResultExempt         = "exempt"
	discordGateResultReauthRequired = "reauth_required"

	discordGateReauthMessage = "Please reconnect Discord OAuth authorization to refresh verification."

	discordGateReasonMaxRunes  = 128
	discordGateMessageMaxRunes = 1024
)

// DiscordGateRecheckOutcome is the API-safe result of a Discord gate operation.
type DiscordGateRecheckOutcome struct {
	UserID     int    `json:"user_id"`
	Username   string `json:"username"`
	Result     string `json:"result"`
	Reason     string `json:"reason"`
	Message    string `json:"message"`
	CheckedAt  int64  `json:"checked_at"`
	GatePassed bool   `json:"gate_passed"`
	Exempt     bool   `json:"exempt"`
}

type discordRefreshTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

// RecheckDiscordGate refreshes a user's Discord access token, evaluates the
// configured nested gate, and persists last-check state without disabling the
// user or touching tokens unrelated to Discord OAuth.
func RecheckDiscordGate(ctx context.Context, user *model.User) (DiscordGateRecheckOutcome, error) {
	outcome := newDiscordGateOutcome(user, discordGateResultUnknown, "", "")
	if user == nil || user.Id == 0 {
		return outcome, fmt.Errorf("user is required")
	}
	if user.DiscordGateExempt {
		outcome.Result = discordGateResultExempt
		outcome.Reason = "exempt"
		outcome.Message = "Discord gate exemption is enabled."
		return persistDiscordGateOutcome(user, outcome, false, nil)
	}
	if strings.TrimSpace(user.DiscordId) == "" {
		outcome.Result = discordGateResultReauthRequired
		outcome.Reason = "missing_discord_id"
		outcome.Message = discordGateReauthMessage
		outcome.GatePassed = false
		return persistDiscordGateOutcome(user, outcome, true, map[string]interface{}{"discord_refresh_token": ""})
	}
	if strings.TrimSpace(user.DiscordRefreshToken) == "" {
		outcome.Result = discordGateResultReauthRequired
		outcome.Reason = "missing_refresh_token"
		outcome.Message = discordGateReauthMessage
		outcome.GatePassed = false
		return persistDiscordGateOutcome(user, outcome, true, nil)
	}

	refreshToken, err := common.DecryptWithCryptoSecret(user.DiscordRefreshToken)
	if err != nil || strings.TrimSpace(refreshToken) == "" {
		outcome.Result = discordGateResultReauthRequired
		outcome.Reason = "refresh_token_decrypt_failed"
		outcome.Message = discordGateReauthMessage
		outcome.GatePassed = false
		return persistDiscordGateOutcome(user, outcome, true, map[string]interface{}{"discord_refresh_token": ""})
	}

	token, invalidGrant, err := refreshDiscordAccessToken(ctx, refreshToken)
	if invalidGrant {
		outcome.Result = discordGateResultReauthRequired
		outcome.Reason = "invalid_grant"
		outcome.Message = discordGateReauthMessage
		outcome.GatePassed = false
		return persistDiscordGateOutcome(user, outcome, true, map[string]interface{}{"discord_refresh_token": ""})
	}
	if err != nil || token == nil || strings.TrimSpace(token.AccessToken) == "" {
		outcome.Result = discordGateResultUnknown
		outcome.Reason = "refresh_failed"
		outcome.Message = discordGateServiceUnavailableMessage
		outcome.GatePassed = user.DiscordGatePassed
		return persistDiscordGateOutcome(user, outcome, false, nil)
	}

	extraUpdates := map[string]interface{}{}
	if strings.TrimSpace(token.RefreshToken) != "" && strings.TrimSpace(token.RefreshToken) != refreshToken {
		encrypted, err := common.EncryptWithCryptoSecret(token.RefreshToken)
		if err != nil {
			outcome.Result = discordGateResultReauthRequired
			outcome.Reason = "refresh_token_encrypt_failed"
			outcome.Message = discordGateReauthMessage
			outcome.GatePassed = false
			return persistDiscordGateOutcome(user, outcome, true, nil)
		}
		extraUpdates["discord_refresh_token"] = encrypted
	}
	cfg, cfgErr := normalizedDiscordGateConfig()
	if cfgErr != nil {
		outcome.Result = discordGateResultError
		outcome.Reason = "invalid_config"
		outcome.Message = discordGateInvalidConfigMessage
		outcome.GatePassed = false
		addDiscordProfileUpdates(ctx, token, extraUpdates)
		return persistDiscordGateOutcome(user, outcome, true, extraUpdates)
	}

	gateResult := evaluateDiscordGate(ctx, strings.TrimSpace(token.AccessToken), cfg)
	outcome.Result = string(gateResult.Decision)
	outcome.Reason = gateResult.Reason
	outcome.Message = discordGateUserMessage(gateResult, cfg)
	addDiscordProfileUpdates(ctx, token, extraUpdates)
	switch gateResult.Decision {
	case discordGateDecisionPass:
		outcome.GatePassed = true
		outcome.Message = ""
		return persistDiscordGateOutcome(user, outcome, true, extraUpdates)
	case discordGateDecisionDeny, discordGateDecisionBan:
		outcome.GatePassed = false
		return persistDiscordGateOutcome(user, outcome, true, extraUpdates)
	case discordGateDecisionUnknown:
		outcome.GatePassed = user.DiscordGatePassed
		return persistDiscordGateOutcome(user, outcome, false, extraUpdates)
	default:
		outcome.Result = discordGateResultError
		outcome.Reason = "unexpected_gate_result"
		outcome.Message = discordGateInvalidConfigMessage
		return persistDiscordGateOutcome(user, outcome, true, extraUpdates)
	}
}

func addDiscordProfileUpdates(ctx context.Context, token *OAuthToken, updates map[string]interface{}) {
	if token == nil || strings.TrimSpace(token.AccessToken) == "" || updates == nil {
		return
	}
	oauthUser, err := (&DiscordProvider{}).GetUserInfo(ctx, token)
	if err != nil {
		return
	}
	profileResult := &PreUserMutationResult{}
	fillDiscordProfileResult(oauthUser, profileResult)
	if !profileResult.HasDiscordProfileUpdate {
		return
	}
	updates["discord_username"] = profileResult.DiscordUsername
	updates["discord_global_name"] = profileResult.DiscordGlobalName
	updates["discord_discriminator"] = profileResult.DiscordDiscriminator
	updates["discord_avatar_hash"] = profileResult.DiscordAvatarHash
	updates["discord_profile_synced_at"] = profileResult.DiscordProfileSyncedAt
}

// ForceDiscordGateReauth clears the Discord refresh token and marks the gate as
// requiring OAuth reauthorization without clearing the user's Discord binding.
func ForceDiscordGateReauth(user *model.User) (DiscordGateRecheckOutcome, error) {
	outcome := newDiscordGateOutcome(user, discordGateResultReauthRequired, "force_reauth", discordGateReauthMessage)
	outcome.GatePassed = false
	return persistDiscordGateOutcome(user, outcome, true, map[string]interface{}{"discord_refresh_token": ""})
}

// SetDiscordGateExempt toggles the operational exemption flag. Exemption is a
// bypass and does not forge a passed state.
func SetDiscordGateExempt(user *model.User, exempt bool) (DiscordGateRecheckOutcome, error) {
	result := discordGateResultDeny
	reason := "exempt_disabled"
	message := "Discord gate exemption is disabled."
	if exempt {
		result = discordGateResultExempt
		reason = "exempt_enabled"
		message = "Discord gate exemption is enabled."
	}
	outcome := newDiscordGateOutcome(user, result, reason, message)
	if user != nil {
		outcome.Exempt = exempt
		outcome.GatePassed = user.DiscordGatePassed
	}
	return persistDiscordGateOutcome(user, outcome, false, map[string]interface{}{"discord_gate_exempt": exempt})
}

func refreshDiscordAccessToken(ctx context.Context, refreshToken string) (*OAuthToken, bool, error) {
	settings := system_setting.GetDiscordSettings()
	values := url.Values{}
	values.Set("client_id", settings.ClientId)
	values.Set("client_secret", settings.ClientSecret)
	values.Set("grant_type", "refresh_token")
	values.Set("refresh_token", strings.TrimSpace(refreshToken))

	endpoint := strings.TrimRight(discordAPIBaseURL, "/") + "/oauth2/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := discordHTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		diag := classifyDiscordTransportError(err)
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Discord] refresh token failed: %s", diag.Raw()))
		// Network/timeout failures must NOT clear the refresh token or the
		// existing gate pass — only an explicit invalid_grant does.
		return nil, false, fmt.Errorf("discord refresh token: %s", diag.Raw())
	}
	defer res.Body.Close()

	// Read a bounded body regardless of status. Discord returns invalid_grant
	// on 400 with a JSON error body, but edge cases (proxies, CDNs) may also
	// surface errors on other codes; decoding once keeps the invalid_grant
	// detection uniform without ever reading an unbounded body.
	body, readErr := readDiscordLimitedBody(res.Body, discordResponseBodyLimit)
	retryAfter := parseDiscordRetryAfter(res.Header.Get("Retry-After"))
	if readErr != nil {
		diag := discordDiagnostic{Status: res.StatusCode, RetryAfter: retryAfter, Category: discordCategoryBadResponse}
		if errors.Is(readErr, errDiscordBodyTooLarge) {
			diag.Category = discordCategoryBodyTooLarge
		}
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Discord] refresh token body read failed: %s", diag.Raw()))
		// Body-too-large / read failure must NOT clear the refresh token.
		return nil, false, fmt.Errorf("discord refresh token: %s", diag.Raw())
	}

	var tokenResponse discordRefreshTokenResponse
	if uerr := common.Unmarshal(body, &tokenResponse); uerr != nil {
		diag := discordDiagnostic{Status: res.StatusCode, RetryAfter: retryAfter, Category: discordCategoryBadResponse}
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Discord] refresh token decode failed: %s", diag.Raw()))
		// Unparseable body must NOT clear the refresh token.
		return nil, false, fmt.Errorf("discord refresh token: %s", diag.Raw())
	}

	// Only an explicit invalid_grant clears the refresh token and forces
	// reauthorization. invalid_client, 429, 5xx, body_too_large, timeout and
	// network errors all leave the token intact so the existing gate pass
	// survives transient outages.
	if tokenResponse.Error == "invalid_grant" {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Discord] refresh token rejected: category=invalid_grant status=%d", res.StatusCode))
		return nil, true, nil
	}

	if res.StatusCode != http.StatusOK || strings.TrimSpace(tokenResponse.AccessToken) == "" {
		payload := discordErrorPayload{Error: tokenResponse.Error, ErrorDescription: tokenResponse.Description}
		diag := discordDiagnostic{
			Status:     res.StatusCode,
			RetryAfter: retryAfter,
			Category:   classifyDiscordPayload(res.StatusCode, payload),
		}
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Discord] refresh token failed: %s", diag.Raw()))
		return nil, false, fmt.Errorf("discord refresh token: %s", diag.Raw())
	}

	return &OAuthToken{
		AccessToken:  tokenResponse.AccessToken,
		RefreshToken: tokenResponse.RefreshToken,
		TokenType:    tokenResponse.TokenType,
		ExpiresIn:    tokenResponse.ExpiresIn,
		Scope:        tokenResponse.Scope,
	}, false, nil
}

func normalizedDiscordGateConfig() (system_setting.DiscordRegisterGateConfig, error) {
	cfg := system_setting.GetDiscordSettings().RegisterGate
	system_setting.NormalizeDiscordRegisterGate(&cfg)
	if err := system_setting.ValidateDiscordRegisterGate(cfg); err != nil {
		return cfg, err
	}
	if len(cfg.Groups) == 0 && len(cfg.BanGroups) == 0 {
		return cfg, fmt.Errorf("discord gate config is empty")
	}
	return cfg, nil
}

func newDiscordGateOutcome(user *model.User, result, reason, message string) DiscordGateRecheckOutcome {
	outcome := DiscordGateRecheckOutcome{
		Result:    result,
		Reason:    truncateRunes(reason, discordGateReasonMaxRunes),
		Message:   truncateRunes(message, discordGateMessageMaxRunes),
		CheckedAt: time.Now().Unix(),
	}
	if user != nil {
		outcome.UserID = user.Id
		outcome.Username = user.Username
		outcome.GatePassed = user.DiscordGatePassed
		outcome.Exempt = user.DiscordGateExempt
	}
	return outcome
}

func persistDiscordGateOutcome(user *model.User, outcome DiscordGateRecheckOutcome, updateGatePassed bool, extraUpdates map[string]interface{}) (DiscordGateRecheckOutcome, error) {
	if user == nil || user.Id == 0 {
		return outcome, fmt.Errorf("user is required")
	}
	outcome.Reason = truncateRunes(outcome.Reason, discordGateReasonMaxRunes)
	outcome.Message = truncateRunes(outcome.Message, discordGateMessageMaxRunes)
	updates := map[string]interface{}{
		"discord_last_check_at":     outcome.CheckedAt,
		"discord_last_check_result": outcome.Result,
		"discord_last_check_reason": outcome.Reason,
		"discord_gate_message":      outcome.Message,
	}
	if updateGatePassed {
		updates["discord_gate_passed"] = outcome.GatePassed
	}
	for key, value := range extraUpdates {
		updates[key] = value
	}
	if err := model.DB.Model(&model.User{}).Where("id = ?", user.Id).Updates(updates).Error; err != nil {
		return outcome, err
	}
	user.DiscordLastCheckAt = outcome.CheckedAt
	user.DiscordLastCheckResult = outcome.Result
	user.DiscordLastCheckReason = outcome.Reason
	user.DiscordGateMessage = outcome.Message
	if updateGatePassed {
		user.DiscordGatePassed = outcome.GatePassed
	}
	if value, ok := extraUpdates["discord_refresh_token"].(string); ok {
		user.DiscordRefreshToken = value
	}
	if value, ok := extraUpdates["discord_gate_exempt"].(bool); ok {
		user.DiscordGateExempt = value
		outcome.Exempt = value
	}
	if value, ok := extraUpdates["discord_username"].(string); ok {
		user.DiscordUsername = value
	}
	if value, ok := extraUpdates["discord_global_name"].(string); ok {
		user.DiscordGlobalName = value
	}
	if value, ok := extraUpdates["discord_discriminator"].(string); ok {
		user.DiscordDiscriminator = value
	}
	if value, ok := extraUpdates["discord_avatar_hash"].(string); ok {
		user.DiscordAvatarHash = value
	}
	if value, ok := extraUpdates["discord_profile_synced_at"].(int64); ok {
		user.DiscordProfileSyncedAt = value
	}
	outcome.GatePassed = user.DiscordGatePassed
	outcome.Exempt = user.DiscordGateExempt
	if err := model.InvalidateUserCache(user.Id); err != nil {
		return outcome, err
	}
	return outcome, nil
}

func truncateRunes(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes])
}
