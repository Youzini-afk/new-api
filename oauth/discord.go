package oauth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
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
	UID           string `json:"id"`
	ID            string `json:"username"`
	Name          string `json:"global_name"`
	Discriminator string `json:"discriminator"`
	Avatar        string `json:"avatar"`
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

	discordUsernameMaxRunes      = 128
	discordGlobalNameMaxRunes    = 128
	discordDiscriminatorMaxRunes = 16
	discordAvatarHashMaxRunes    = 128

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

// discordResponseBodyLimit caps the number of bytes read from a Discord OAuth
// or API response body so a misbehaving upstream cannot exhaust memory. We
// read at most limit+1 bytes (the +1 lets us detect oversize without an
// unbounded read).
const discordResponseBodyLimit = 64 * 1024

// discordRetryAfterClamp is the upper bound we report for the Retry-After
// header. We never sleep on 429 (the caller decides what to do); the clamp
// just keeps the diagnostic value reasonable.
const discordRetryAfterClamp = 5 * time.Minute

// errDiscordBodyTooLarge is returned by readDiscordLimitedBody when the body
// exceeds discordResponseBodyLimit. Callers must treat this as a distinct
// category and never log the partial bytes they may have read.
var errDiscordBodyTooLarge = errors.New("discord response body exceeds limit")

// discordErrorPayload mirrors the subset of Discord OAuth/API error bodies we
// use for classification. The fields are parsed but never logged verbatim —
// only the derived category, status and Retry-After are surfaced.
type discordErrorPayload struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
	Message          string `json:"message"`
	RetryAfter       int    `json:"retry_after"`
	Global           bool   `json:"global"`
}

// discordErrorCategory is a stable, sanitized bucket for Discord HTTP failures.
// It is safe to log because it never carries tokens, the OAuth code,
// client_secret, or response bodies.
type discordErrorCategory string

const (
	discordCategoryInvalidGrant        discordErrorCategory = "invalid_grant"
	discordCategoryInvalidClient       discordErrorCategory = "invalid_client"
	discordCategoryRedirectURIMismatch discordErrorCategory = "redirect_uri_mismatch"
	discordCategoryRateLimited         discordErrorCategory = "rate_limited"
	discordCategoryUnauthorized        discordErrorCategory = "unauthorized"
	discordCategoryForbidden           discordErrorCategory = "forbidden"
	discordCategoryBodyTooLarge        discordErrorCategory = "body_too_large"
	discordCategoryTimeout             discordErrorCategory = "timeout"
	discordCategoryNetworkError        discordErrorCategory = "network_error"
	discordCategoryBadResponse         discordErrorCategory = "bad_response"
	discordCategoryDiscordError        discordErrorCategory = "discord_error"
)

// discordDiagnostic bundles the sanitized diagnostic output for one Discord
// HTTP failure. Raw() is the only string that may appear in logs or in
// OAuthError.RawError — it contains only the category, status and Retry-After
// hint, never secrets or response bodies.
type discordDiagnostic struct {
	Category   discordErrorCategory
	Status     int
	RetryAfter time.Duration
}

// Raw renders a compact, secret-free diagnostic string suitable for logs and
// OAuthError.RawError.
func (d discordDiagnostic) Raw() string {
	parts := []string{"category=" + string(d.Category)}
	if d.Status != 0 {
		parts = append(parts, fmt.Sprintf("status=%d", d.Status))
	}
	if d.RetryAfter > 0 {
		parts = append(parts, "retry_after="+d.RetryAfter.String())
	}
	return strings.Join(parts, " ")
}

// readDiscordLimitedBody reads at most limit+1 bytes from r. If the body
// exceeds the limit it returns errDiscordBodyTooLarge; the partially read
// prefix is intentionally discarded because callers must not log it.
func readDiscordLimitedBody(r io.Reader, limit int) ([]byte, error) {
	limited := io.LimitReader(r, int64(limit)+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, errDiscordBodyTooLarge
	}
	return data, nil
}

// decodeDiscordJSONLimited parses a Discord response into v while enforcing
// the discordResponseBodyLimit cap. On oversize failure it returns
// errDiscordBodyTooLarge without leaving the reader open for unbounded reads.
func decodeDiscordJSONLimited(r io.Reader, v any) error {
	data, err := readDiscordLimitedBody(r, discordResponseBodyLimit)
	if err != nil {
		return err
	}
	return common.Unmarshal(data, v)
}

// parseDiscordRetryAfter parses the Retry-After header (delta-seconds form,
// RFC 7231 §7.1.3). Returns 0 when absent or unparseable. We never sleep on
// the value; callers only use it for diagnostics.
func parseDiscordRetryAfter(header string) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}
	seconds, err := strconv.ParseFloat(header, 64)
	if err != nil || seconds < 0 {
		return 0
	}
	d := time.Duration(seconds * float64(time.Second))
	if d > discordRetryAfterClamp {
		return discordRetryAfterClamp
	}
	return d
}

// classifyDiscordTransportError maps a transport-level error (no response
// received) to a diagnostic category. Timeouts become timeout; everything
// else becomes network_error.
func classifyDiscordTransportError(err error) discordDiagnostic {
	cat := discordCategoryNetworkError
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			cat = discordCategoryTimeout
		}
	}
	return discordDiagnostic{Category: cat}
}

// classifyDiscordPayload maps a parsed Discord error payload and HTTP status
// to a stable category. Payload-driven categories (invalid_grant,
// invalid_client, redirect_uri_mismatch) take precedence over the bare HTTP
// status because Discord sometimes returns these alongside unexpected codes.
func classifyDiscordPayload(status int, payload discordErrorPayload) discordErrorCategory {
	errCode := strings.ToLower(strings.TrimSpace(payload.Error))
	desc := strings.ToLower(payload.ErrorDescription)
	switch errCode {
	case "invalid_grant":
		return discordCategoryInvalidGrant
	case "invalid_client":
		return discordCategoryInvalidClient
	case "invalid_request":
		if strings.Contains(desc, "redirect_uri") {
			return discordCategoryRedirectURIMismatch
		}
	}
	if status == http.StatusTooManyRequests {
		return discordCategoryRateLimited
	}
	switch status {
	case http.StatusUnauthorized:
		return discordCategoryUnauthorized
	case http.StatusForbidden:
		return discordCategoryForbidden
	}
	return discordCategoryDiscordError
}

// classifyDiscordResponseError reads a bounded slice of the response body and
// classifies the failure based on the HTTP status and parsed Discord payload.
// The body is consumed (or capped) so a malicious upstream cannot pin the
// connection. An empty or non-JSON body never produces bad_response here — the
// status code alone is enough to classify non-200 responses, and a missing
// body is not itself a "bad response" at the HTTP level.
func classifyDiscordResponseError(res *http.Response) discordDiagnostic {
	diag := discordDiagnostic{
		Status:     res.StatusCode,
		RetryAfter: parseDiscordRetryAfter(res.Header.Get("Retry-After")),
	}
	body, err := readDiscordLimitedBody(res.Body, discordResponseBodyLimit)
	if err != nil {
		if errors.Is(err, errDiscordBodyTooLarge) {
			diag.Category = discordCategoryBodyTooLarge
			return diag
		}
		// Read failure (not oversize): classify by status with an empty
		// payload. The body is gone but the status code is still reliable.
		diag.Category = classifyDiscordPayload(res.StatusCode, discordErrorPayload{})
		return diag
	}
	var payload discordErrorPayload
	if uerr := common.Unmarshal(body, &payload); uerr != nil {
		// Non-JSON body (e.g., an HTML interstitial from a CDN): classify
		// by status, since the status code is authoritative even when the
		// body is not JSON.
		diag.Category = classifyDiscordPayload(res.StatusCode, discordErrorPayload{})
		return diag
	}
	diag.Category = classifyDiscordPayload(res.StatusCode, payload)
	return diag
}

// discordDecodeDiagnostic converts an error returned by decodeDiscordJSONLimited
// into a diagnostic category, distinguishing oversize bodies from generic
// decode failures.
func discordDecodeDiagnostic(status int, err error) discordDiagnostic {
	diag := discordDiagnostic{Status: status, Category: discordCategoryBadResponse}
	if errors.Is(err, errDiscordBodyTooLarge) {
		diag.Category = discordCategoryBodyTooLarge
	}
	return diag
}

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

	settings := system_setting.GetDiscordSettings()
	redirectUri := fmt.Sprintf("%s/oauth/discord", system_setting.ServerAddress)
	values := url.Values{}
	values.Set("client_id", settings.ClientId)
	values.Set("client_secret", settings.ClientSecret)
	values.Set("code", code)
	values.Set("grant_type", "authorization_code")
	values.Set("redirect_uri", redirectUri)

	logger.LogDebug(ctx, "[OAuth-Discord] ExchangeToken: redirect_uri=%s", redirectUri)

	endpoint := strings.TrimRight(discordAPIBaseURL, "/") + "/oauth2/token"
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
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
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Discord] token exchange failed: %s", diag.Raw()))
		return nil, NewOAuthErrorWithRaw(i18n.MsgOAuthConnectFailed, map[string]any{"Provider": "Discord"}, diag.Raw())
	}
	defer res.Body.Close()

	logger.LogDebug(ctx, "[OAuth-Discord] ExchangeToken response status: %d", res.StatusCode)

	if res.StatusCode != http.StatusOK {
		diag := classifyDiscordResponseError(res)
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Discord] token exchange failed: %s", diag.Raw()))
		return nil, NewOAuthErrorWithRaw(i18n.MsgOAuthTokenFailed, map[string]any{"Provider": "Discord"}, diag.Raw())
	}

	var discordResponse discordOAuthResponse
	if err = decodeDiscordJSONLimited(res.Body, &discordResponse); err != nil {
		diag := discordDecodeDiagnostic(res.StatusCode, err)
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Discord] token exchange decode failed: %s", diag.Raw()))
		return nil, NewOAuthErrorWithRaw(i18n.MsgOAuthTokenFailed, map[string]any{"Provider": "Discord"}, diag.Raw())
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

	endpoint := strings.TrimRight(discordAPIBaseURL, "/") + "/users/@me"
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)

	client := discordHTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		diag := classifyDiscordTransportError(err)
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Discord] GetUserInfo failed: %s", diag.Raw()))
		return nil, NewOAuthErrorWithRaw(i18n.MsgOAuthConnectFailed, map[string]any{"Provider": "Discord"}, diag.Raw())
	}
	defer res.Body.Close()

	logger.LogDebug(ctx, "[OAuth-Discord] GetUserInfo response status: %d", res.StatusCode)

	if res.StatusCode != http.StatusOK {
		diag := classifyDiscordResponseError(res)
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Discord] GetUserInfo failed: %s", diag.Raw()))
		return nil, NewOAuthErrorWithRaw(i18n.MsgOAuthGetUserErr, map[string]any{"Provider": "Discord"}, diag.Raw())
	}

	var discordUser discordUser
	if err = decodeDiscordJSONLimited(res.Body, &discordUser); err != nil {
		diag := discordDecodeDiagnostic(res.StatusCode, err)
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Discord] GetUserInfo decode failed: %s", diag.Raw()))
		return nil, NewOAuthErrorWithRaw(i18n.MsgOAuthGetUserErr, map[string]any{"Provider": "Discord"}, diag.Raw())
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
		Extra: map[string]any{
			"discord_username":      discordUser.ID,
			"discord_global_name":   discordUser.Name,
			"discord_discriminator": discordUser.Discriminator,
			"discord_avatar_hash":   discordUser.Avatar,
		},
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
	fillDiscordProfileResult(preCtx.OAuthUser, preCtx.Result)

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
		logger.LogError(ctx, "[OAuth-Discord] gate passed but refresh token was not returned by Discord")
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

func fillDiscordProfileResult(oauthUser *OAuthUser, result *PreUserMutationResult) {
	if oauthUser == nil || result == nil {
		return
	}
	result.DiscordUsername = truncateDiscordProfileField(oauthUser.Username, discordUsernameMaxRunes)
	result.DiscordGlobalName = truncateDiscordProfileField(oauthUser.DisplayName, discordGlobalNameMaxRunes)
	if oauthUser.Extra != nil {
		if value, ok := oauthUser.Extra["discord_username"].(string); ok {
			result.DiscordUsername = truncateDiscordProfileField(value, discordUsernameMaxRunes)
		}
		if value, ok := oauthUser.Extra["discord_global_name"].(string); ok {
			result.DiscordGlobalName = truncateDiscordProfileField(value, discordGlobalNameMaxRunes)
		}
		if value, ok := oauthUser.Extra["discord_discriminator"].(string); ok {
			result.DiscordDiscriminator = truncateDiscordProfileField(value, discordDiscriminatorMaxRunes)
		}
		if value, ok := oauthUser.Extra["discord_avatar_hash"].(string); ok {
			result.DiscordAvatarHash = truncateDiscordProfileField(value, discordAvatarHashMaxRunes)
		}
	}
	if result.DiscordUsername == "" {
		return
	}
	result.DiscordProfileSyncedAt = time.Now().Unix()
	result.HasDiscordProfileUpdate = true
}

func truncateDiscordProfileField(value string, maxRunes int) string {
	trimmed := strings.TrimSpace(value)
	if maxRunes <= 0 {
		return ""
	}
	if len([]rune(trimmed)) <= maxRunes {
		return trimmed
	}
	buf := make([]rune, 0, maxRunes)
	for _, r := range trimmed {
		if len(buf) == maxRunes {
			break
		}
		buf = append(buf, r)
	}
	return string(buf)
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
		// fetchDiscordGuildMember already logged the diagnostic detail.
		return discordRuleUnknown
	}
	switch fetch.Status {
	case http.StatusOK:
		// continue below
	case http.StatusNotFound:
		// 404 means the user is genuinely not a member; this is a real
		// rule failure, not a transient outage.
		return discordRuleFail
	default:
		// 429/401/403/5xx/body_too_large are all transient or unknown —
		// never treat as "not a member". fetchDiscordGuildMember already
		// logged the categorized diagnostic.
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
		diag := classifyDiscordTransportError(err)
		result.Err = fmt.Errorf("discord guild member fetch: %s", diag.Raw())
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Discord] guild member fetch failed: guild=%s %s", guildID, diag.Raw()))
		return result
	}
	defer res.Body.Close()
	result.Status = res.StatusCode

	if res.StatusCode == http.StatusOK {
		var member discordGuildMember
		if err := decodeDiscordJSONLimited(res.Body, &member); err != nil {
			diag := discordDecodeDiagnostic(res.StatusCode, err)
			result.Err = fmt.Errorf("discord guild member decode: %s", diag.Raw())
			logger.LogError(ctx, fmt.Sprintf("[OAuth-Discord] guild member decode failed: guild=%s %s", guildID, diag.Raw()))
			return result
		}
		result.Member = &member
		return result
	}

	// Non-200: classify for diagnostics without consuming an unbounded body.
	// 404 stays a deliberate "not a member" failure for the rule evaluator;
	// every other status (429/401/403/5xx/body_too_large) stays unknown so
	// we never accidentally treat a transient outage as "not a member".
	diag := classifyDiscordResponseError(res)
	logger.LogError(ctx, fmt.Sprintf("[OAuth-Discord] guild member fetch non-ok: guild=%s %s", guildID, diag.Raw()))
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
