package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func relayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	switch info.RelayMode {
	case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
		err = relay.ImageHelper(c, info)
	case relayconstant.RelayModeAudioSpeech:
		fallthrough
	case relayconstant.RelayModeAudioTranslation:
		fallthrough
	case relayconstant.RelayModeAudioTranscription:
		err = relay.AudioHelper(c, info)
	case relayconstant.RelayModeRerank:
		err = relay.RerankHelper(c, info)
	case relayconstant.RelayModeEmbeddings:
		err = relay.EmbeddingHelper(c, info)
	case relayconstant.RelayModeResponses, relayconstant.RelayModeResponsesCompact:
		err = relay.ResponsesHelper(c, info)
	default:
		err = relay.TextHelper(c, info)
	}
	return err
}

// Phase 9A/B safe MVP — per-channel explicit fallback backend.
//
// The gin context keys and the admin-info append helper live in the service
// package (so both controller error logs and service.GenerateTextOtherInfo can
// reference them without an import cycle). The controller is responsible for
// scheduling the fallback (prepareNextChannelFallback) and consuming the
// override in getChannel.

const (
	channelFallbackReasonEmptyReply = service.ChannelFallbackReasonEmptyReply
	channelFallbackReasonError      = service.ChannelFallbackReasonError
)

// relayFallbackModeAllowed reports whether the relay format/mode is in the safe
// allowlist for fallback. Fallback is a non-stream, idempotent-style MVP, so
// we reject anything that mutates state (image gen/edit, audio speech/transcribe,
// realtime, unknown) and require non-stream for Gemini/Claude.
func relayFallbackModeAllowed(relayFormat types.RelayFormat, info *relaycommon.RelayInfo) bool {
	if info == nil {
		return false
	}
	if info.IsStream {
		return false
	}
	if relayFormat == types.RelayFormatOpenAIRealtime {
		return false
	}
	switch info.RelayMode {
	case relayconstant.RelayModeChatCompletions,
		relayconstant.RelayModeCompletions,
		relayconstant.RelayModeResponses,
		relayconstant.RelayModeResponsesCompact,
		relayconstant.RelayModeEmbeddings,
		relayconstant.RelayModeRerank,
		relayconstant.RelayModeGemini:
		return true
	default:
		return false
	}
}

// relayFallbackStateDenied reports whether the current request state forbids a
// fallback. We refuse fallback once any byte has been written to the client,
// once stream status is non-nil (safety: any stream interaction), once the
// client cancelled the request, once the error is marked skip-retry, once
// channel-affinity asked us not to retry, once a specific channel was
// requested, or once a fallback was already scheduled this request.
func relayFallbackStateDenied(c *gin.Context, info *relaycommon.RelayInfo, err *types.NewAPIError) bool {
	if c == nil || info == nil || err == nil {
		return true
	}
	if info.IsStream {
		return true
	}
	if info.StreamStatus != nil {
		return true
	}
	if info.SendResponseCount > 0 || info.ReceivedResponseCount > 0 {
		return true
	}
	if c.Writer != nil && c.Writer.Written() {
		return true
	}
	if c.Request != nil && c.Request.Context().Err() != nil {
		return true
	}
	if types.IsSkipRetryError(err) {
		return true
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return true
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return true
	}
	if service.IsChannelFallbackApplied(c) {
		return true
	}
	return false
}

// canFallbackChannelServeRequest validates that the fallback channel can serve
// the current request: enabled, not the same as the current channel, not
// already in use_channel, and able to serve the model for the effective group
// (or any user auto group when group is empty/auto). For Advanced Custom
// channels (type 58), also requires a route match for the request path.
func canFallbackChannelServeRequest(c *gin.Context, fallback *model.Channel, currentChannelID int, info *relaycommon.RelayInfo) bool {
	if fallback == nil || info == nil {
		return false
	}
	if fallback.Id <= 0 {
		return false
	}
	if fallback.Status != common.ChannelStatusEnabled {
		return false
	}
	if fallback.Id == currentChannelID {
		return false
	}
	fallbackIDStr := fmt.Sprintf("%d", fallback.Id)
	for _, usedID := range c.GetStringSlice("use_channel") {
		if usedID == fallbackIDStr {
			return false
		}
	}

	modelName := info.OriginModelName
	requestPath := ""
	if c.Request != nil && c.Request.URL != nil {
		requestPath = c.Request.URL.Path
	}

	// Advanced Custom channels are path-bound; require a matching route.
	if fallback.Type == constant.ChannelTypeAdvancedCustom {
		advancedConfig := fallback.GetOtherSettings().AdvancedCustom
		if advancedConfig == nil || !advancedConfig.SupportsPath(requestPath) {
			return false
		}
	}

	// Group/model eligibility. Use the effective group when available; for
	// "auto" or empty, fall back to user auto groups (conservative: allow
	// if the fallback serves any of them).
	usingGroup := strings.TrimSpace(info.UsingGroup)
	if usingGroup == "" {
		usingGroup = common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	}
	if usingGroup == "" {
		usingGroup = common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	}

	if usingGroup != "" && usingGroup != "auto" {
		if !model.IsChannelEnabledForGroupModel(usingGroup, modelName, fallback.Id) {
			return false
		}
		return true
	}

	// Auto/empty group: accept if the fallback serves any user auto group.
	userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	if userGroup == "" {
		userGroup = info.UserGroup
	}
	autoGroups := service.GetUserAutoGroup(userGroup)
	if len(autoGroups) == 0 {
		return false
	}
	return model.IsChannelEnabledForAnyGroupModel(autoGroups, modelName, fallback.Id)
}

// relayFallbackErrorPolicyAllowed reports whether the error's status-code /
// error-code policy permits a fallback. Fallback shares the same
// error/status-code retry policy as the global retry loop, so an always-skip
// status code (504/524), an always-skip error code (bad_response_body), a 2xx,
// or a 4xx/5xx outside the configured retry ranges never triggers a fallback
// even when FallbackOnError/FallbackOnEmptyReply is set.
//
// Unlike shouldRetry this does NOT consult the retry-times budget (fallback
// carries its own one-slot budget via IsChannelFallbackApplied) nor the
// affinity/specific-channel/stream/written state guards, which
// relayFallbackStateDenied already handles. The two helpers together mirror
// shouldRetry's full gate without coupling fallback to common.RetryTimes.
func relayFallbackErrorPolicyAllowed(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	if types.IsChannelError(err) {
		return true
	}
	if types.IsSkipRetryError(err) {
		return false
	}
	code := err.StatusCode
	if code >= 200 && code < 300 {
		return false
	}
	if code < 100 || code > 599 {
		return true
	}
	if operation_setting.IsAlwaysSkipRetryCode(err.GetErrorCode()) {
		return false
	}
	return operation_setting.ShouldRetryByStatusCode(code)
}

// prepareNextChannelFallback decides whether to schedule a per-channel
// explicit fallback after a non-stream relay failure that wrote nothing to the
// client. Returns true when a fallback has been scheduled; in that case the
// caller must call retryParam.ResetRetryNextTry() and continue the retry loop
// without consuming a global retry slot.
//
// Billing/body/affinity invariants are preserved: PreConsume/Refund defer and
// BodyStorage re-read remain in the outer Relay loop; SetupContextForSelectedChannel
// is re-run inside getChannel so the next attempt sees the fallback channel's
// settings/keys/overrides.
//
// Fallback is independent of the global retry-times budget but still obeys the
// same error/status-code retry policy as shouldRetry (via
// relayFallbackErrorPolicyAllowed), so always-skip status codes / error codes
// and 4xx outside the retry ranges cannot bypass the global policy.
func prepareNextChannelFallback(c *gin.Context, currentChannel *model.Channel, info *relaycommon.RelayInfo, openaiErr *types.NewAPIError) bool {
	if c == nil || currentChannel == nil || info == nil || openaiErr == nil {
		return false
	}
	if relayFallbackStateDenied(c, info, openaiErr) {
		return false
	}
	if !relayFallbackModeAllowed(info.RelayFormat, info) {
		return false
	}
	if !relayFallbackErrorPolicyAllowed(openaiErr) {
		return false
	}

	settings, ok := common.GetContextKeyType[dto.ChannelOtherSettings](c, constant.ContextKeyChannelOtherSetting)
	if !ok {
		return false
	}
	if settings.FallbackChannelID <= 0 || settings.FallbackChannelID == currentChannel.Id {
		return false
	}

	fallbackReason := ""
	switch openaiErr.GetErrorCode() {
	case types.ErrorCodeEmptyResponse:
		if !settings.FallbackOnEmptyReply {
			return false
		}
		fallbackReason = channelFallbackReasonEmptyReply
	default:
		if !settings.FallbackOnError {
			return false
		}
		fallbackReason = channelFallbackReasonError
	}

	fallback, err := model.GetChannelById(settings.FallbackChannelID, true)
	if err != nil || fallback == nil {
		if err != nil {
			logger.LogWarn(c, fmt.Sprintf("load fallback channel #%d failed: %s", settings.FallbackChannelID, err.Error()))
		}
		return false
	}
	if !canFallbackChannelServeRequest(c, fallback, currentChannel.Id, info) {
		logger.LogWarn(c, fmt.Sprintf("fallback channel #%d cannot serve current request (group/model/path), skip fallback", fallback.Id))
		return false
	}

	service.MarkChannelFallbackScheduled(c, fallback.Id, currentChannel.Id, fallbackReason)
	logger.LogInfo(c, fmt.Sprintf("channel fallback triggered: #%d -> #%d, reason: %s", currentChannel.Id, fallback.Id, fallbackReason))
	return true
}

func geminiRelayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	if strings.Contains(c.Request.URL.Path, "embed") {
		err = relay.GeminiEmbeddingHandler(c, info)
	} else {
		err = relay.GeminiHelper(c, info)
	}
	return err
}

func Relay(c *gin.Context, relayFormat types.RelayFormat) {

	requestId := c.GetString(common.RequestIdKey)
	//group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	//originalModel := common.GetContextKeyString(c, constant.ContextKeyOriginalModel)

	var (
		newAPIError *types.NewAPIError
		ws          *websocket.Conn
	)

	if relayFormat == types.RelayFormatOpenAIRealtime {
		var err error
		ws, err = upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			helper.WssError(c, ws, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry()).ToOpenAIError())
			return
		}
		defer ws.Close()
	}

	defer func() {
		if newAPIError != nil {
			logger.LogError(c, fmt.Sprintf("relay error: %s", common.LocalLogPreview(newAPIError.Error())))
			newAPIError.SetMessage(common.MessageWithRequestId(newAPIError.Error(), requestId))
			switch relayFormat {
			case types.RelayFormatOpenAIRealtime:
				helper.WssError(c, ws, newAPIError.ToOpenAIError())
			case types.RelayFormatClaude:
				c.JSON(newAPIError.StatusCode, gin.H{
					"type":  "error",
					"error": newAPIError.ToClaudeError(),
				})
			default:
				c.JSON(newAPIError.StatusCode, gin.H{
					"error": newAPIError.ToOpenAIError(),
				})
			}
		}
	}()

	request, err := helper.GetAndValidateRequest(c, relayFormat)
	if err != nil {
		// Map "request body too large" to 413 so clients can handle it correctly
		if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
			newAPIError = types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
		} else {
			newAPIError = types.NewError(err, types.ErrorCodeInvalidRequest)
		}
		return
	}

	relayInfo, err := relaycommon.GenRelayInfo(c, relayFormat, request, ws)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeGenRelayInfoFailed)
		return
	}

	needSensitiveCheck := setting.ShouldCheckPromptSensitive() || setting.ShouldCheckUASensitive()
	needCountToken := constant.CountToken
	// Admin/root bypass: never block or auto-ban admin/root users.
	skipSensitiveIntercept := false
	if needSensitiveCheck {
		userRole, roleErr := model.GetUserRoleById(relayInfo.UserId)
		if roleErr != nil {
			logger.LogWarn(c, fmt.Sprintf("failed to get user role for sensitive bypass: %v", roleErr))
		} else if userRole >= common.RoleAdminUser {
			skipSensitiveIntercept = true
		}
	}
	// OpenAIRealtime upgrades the websocket at function entry; its body is not a
	// plain JSON payload the prompt/UA interceptors can inspect. Skip the
	// interceptors for realtime in this phase (consistent with gy).
	isRealtime := relayFormat == types.RelayFormatOpenAIRealtime
	if needSensitiveCheck && (isRealtime || skipSensitiveIntercept) {
		// Still run the existing token-count meta path below; just skip the
		// prompt/UA interception side effects.
		needSensitiveCheck = false
	}
	// Avoid building huge CombineText (strings.Join) when token counting and sensitive check are both disabled.
	var meta *types.TokenCountMeta
	if needSensitiveCheck || needCountToken {
		meta = request.GetTokenCountMeta()
	} else {
		meta = fastTokenCountMetaForPricing(request)
	}

	if needSensitiveCheck && meta != nil {
		// Phase 5C/5D — prompt regex rules first, then basic sensitive words.
		if setting.ShouldCheckPromptSensitive() {
			hit, ok := service.MatchSensitivePromptRule(meta.CombineText)
			if ok && hit != nil {
				logger.LogWarn(c, fmt.Sprintf("prompt blocked by regex rule: %s", hit.Pattern))
				status, code, errMsg := service.BuildPromptBlockedErrorAndRecord(
					&promptBlockRecordContext{ctx: c},
					hit,
					setting.SensitivePromptBlockedMessage,
					"rule",
					hit.Pattern,
				)
				newAPIError = types.NewErrorWithStatusCode(
					errMsg,
					code,
					status,
					types.ErrOptionWithSkipRetry(),
				)
				return
			}

			contains, words := service.CheckSensitiveText(meta.CombineText)
			if contains {
				logger.LogWarn(c, fmt.Sprintf("user sensitive words detected: %s", strings.Join(words, ", ")))
				status, code, errMsg := service.BuildPromptBlockedErrorAndRecord(
					&promptBlockRecordContext{ctx: c},
					nil,
					setting.SensitivePromptBlockedMessage,
					"basic",
					strings.Join(words, ","),
				)
				newAPIError = types.NewErrorWithStatusCode(
					errMsg,
					code,
					status,
					types.ErrOptionWithSkipRetry(),
				)
				return
			}
		}
	}

	if !skipSensitiveIntercept && !isRealtime && setting.ShouldCheckUASensitive() {
		uaHit, uaOK := service.MatchSensitiveUARule(c.Request.UserAgent())
		if uaOK && uaHit != nil {
			logger.LogWarn(c, fmt.Sprintf("user agent blocked by regex rule: %s", uaHit.Pattern))
			status, code, errMsg := service.BuildUABlockedErrorAndRecord(
				&uaBlockRecordContext{ctx: c},
				uaHit,
			)
			newAPIError = types.NewErrorWithStatusCode(
				errMsg,
				code,
				status,
				types.ErrOptionWithSkipRetry(),
			)
			return
		}

		contains, hits := service.CheckSensitiveUA(c.Request.UserAgent())
		if contains {
			logger.LogWarn(c, fmt.Sprintf("user agent blocked by regex rules: %s", strings.Join(hits, ", ")))
			// Build a synthetic hit so the record+auto-ban path is uniform. The
			// line-regex block never auto-bans (AutoBan=false).
			syntheticHit := &service.SensitiveRuleHit{
				Pattern:        strings.Join(hits, ","),
				Message:        setting.SensitiveUABlockedMessage,
				ErrorCode:      types.ErrorCodeSensitiveWordsDetected,
				HTTPStatusCode: http.StatusBadRequest,
				AutoBan:        false,
				MatchMode:      "blocked_regex",
			}
			status, code, errMsg := service.BuildUABlockedErrorAndRecord(
				&uaBlockRecordContext{ctx: c},
				syntheticHit,
			)
			newAPIError = types.NewErrorWithStatusCode(
				errMsg,
				code,
				status,
				types.ErrOptionWithSkipRetry(),
			)
			return
		}
	}

	tokens, err := service.EstimateRequestToken(c, meta, relayInfo)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeCountTokenFailed)
		return
	}

	relayInfo.SetEstimatePromptTokens(tokens)

	priceData, err := helper.ModelPriceHelper(c, relayInfo, tokens, meta)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
		return
	}

	// common.SetContextKey(c, constant.ContextKeyTokenCountMeta, meta)

	if priceData.FreeModel {
		logger.LogInfo(c, fmt.Sprintf("模型 %s 免费，跳过预扣费", relayInfo.OriginModelName))
	} else {
		newAPIError = service.PreConsumeBilling(c, priceData.QuotaToPreConsume, relayInfo)
		if newAPIError != nil {
			return
		}
	}

	defer func() {
		// Only return quota if downstream failed and quota was actually pre-consumed
		if newAPIError != nil {
			newAPIError = service.NormalizeViolationFeeError(newAPIError)
			if relayInfo.Billing != nil {
				relayInfo.Billing.Refund(c)
			}
			service.ChargeViolationFeeIfNeeded(c, relayInfo, newAPIError)
		}
	}()

	retryParam := &service.RetryParam{
		Ctx:         c,
		TokenGroup:  relayInfo.TokenGroup,
		ModelName:   relayInfo.OriginModelName,
		RequestPath: c.Request.URL.Path,
		Retry:       common.GetPointer(0),
	}
	relayInfo.RetryIndex = 0
	relayInfo.LastError = nil

	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		relayInfo.RetryIndex = retryParam.GetRetry()
		channel, channelErr := getChannel(c, relayInfo, retryParam)
		if channelErr != nil {
			logger.LogError(c, channelErr.Error())
			newAPIError = channelErr
			break
		}

		addUsedChannel(c, channel.Id)
		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			// Ensure consistent 413 for oversized bodies even when error occurs later (e.g., retry path)
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
			} else {
				newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
			}
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)

		switch relayFormat {
		case types.RelayFormatOpenAIRealtime:
			newAPIError = relay.WssHelper(c, relayInfo)
		case types.RelayFormatClaude:
			newAPIError = relay.ClaudeHelper(c, relayInfo)
		case types.RelayFormatGemini:
			newAPIError = geminiRelayHandler(c, relayInfo)
		default:
			newAPIError = relayHandler(c, relayInfo)
		}

		if newAPIError == nil {
			relayInfo.LastError = nil
			return
		}

		newAPIError = service.NormalizeViolationFeeError(newAPIError)
		relayInfo.LastError = newAPIError

		// Phase 9A/B safe MVP: per-channel explicit fallback. Try fallback
		// BEFORE shouldRetry so it does not consume a global retry slot
		// (ResetRetryNextTry keeps the global counter stable). At most one
		// fallback per request (guarded by channelFallbackAppliedKey).
		fallbackScheduled := prepareNextChannelFallback(c, channel, relayInfo, newAPIError)

		processChannelError(c, *types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()), newAPIError)

		if fallbackScheduled {
			retryParam.ResetRetryNextTry()
			continue
		}

		if !shouldRetry(c, newAPIError, common.RetryTimes-retryParam.GetRetry()) {
			break
		}
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}
	if newAPIError != nil {
		gopool.Go(func() {
			perfmetrics.RecordRelaySample(relayInfo, false, 0)
		})
	}
}

var upgrader = websocket.Upgrader{
	Subprotocols: []string{"realtime"}, // WS 握手支持的协议，如果有使用 Sec-WebSocket-Protocol，则必须在此声明对应的 Protocol TODO add other protocol
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许跨域
	},
}

// promptBlockRecordContext adapts *gin.Context to the
// service.PromptBlockRecordContext interface so the relay can record a prompt
// block log + run local auto-ban without coupling the service layer to gin.
// Raw body reads go through service.BuildRawRequestParamsForInterceptLog which
// uses the io.Seeker body storage (5B) — no []byte GetRequestBody usage.
type promptBlockRecordContext struct {
	ctx *gin.Context
}

func (p *promptBlockRecordContext) RequestContext() context.Context {
	if p == nil || p.ctx == nil || p.ctx.Request == nil {
		return context.Background()
	}
	return p.ctx.Request.Context()
}

func (p *promptBlockRecordContext) UserID() int {
	if p == nil || p.ctx == nil {
		return 0
	}
	return p.ctx.GetInt("id")
}

func (p *promptBlockRecordContext) Username() string {
	if p == nil || p.ctx == nil {
		return ""
	}
	return p.ctx.GetString("username")
}

func (p *promptBlockRecordContext) ClientIP() string {
	if p == nil || p.ctx == nil {
		return ""
	}
	return p.ctx.ClientIP()
}

func (p *promptBlockRecordContext) RequestPath() string {
	if p == nil || p.ctx == nil || p.ctx.Request == nil || p.ctx.Request.URL == nil {
		return ""
	}
	return p.ctx.Request.URL.Path
}

func (p *promptBlockRecordContext) RequestHeadersRaw() string {
	if p == nil {
		return ""
	}
	return service.BuildRawRequestHeadersForInterceptLog(p.ctx)
}

func (p *promptBlockRecordContext) RequestParamsRaw() string {
	if p == nil {
		return ""
	}
	return service.BuildRawRequestParamsForInterceptLog(p.ctx, nil)
}

// uaBlockRecordContext is the UA-block analogue of promptBlockRecordContext;
// it also exposes the request User-Agent.
type uaBlockRecordContext struct {
	ctx *gin.Context
}

func (u *uaBlockRecordContext) RequestContext() context.Context {
	if u == nil || u.ctx == nil || u.ctx.Request == nil {
		return context.Background()
	}
	return u.ctx.Request.Context()
}

func (u *uaBlockRecordContext) UserID() int {
	if u == nil || u.ctx == nil {
		return 0
	}
	return u.ctx.GetInt("id")
}

func (u *uaBlockRecordContext) Username() string {
	if u == nil || u.ctx == nil {
		return ""
	}
	return u.ctx.GetString("username")
}

func (u *uaBlockRecordContext) ClientIP() string {
	if u == nil || u.ctx == nil {
		return ""
	}
	return u.ctx.ClientIP()
}

func (u *uaBlockRecordContext) RequestPath() string {
	if u == nil || u.ctx == nil || u.ctx.Request == nil || u.ctx.Request.URL == nil {
		return ""
	}
	return u.ctx.Request.URL.Path
}

func (u *uaBlockRecordContext) UserAgent() string {
	if u == nil || u.ctx == nil || u.ctx.Request == nil {
		return ""
	}
	return u.ctx.Request.UserAgent()
}

func (u *uaBlockRecordContext) RequestHeadersRaw() string {
	if u == nil {
		return ""
	}
	return service.BuildRawRequestHeadersForInterceptLog(u.ctx)
}

func (u *uaBlockRecordContext) RequestParamsRaw() string {
	if u == nil {
		return ""
	}
	return service.BuildRawRequestParamsForInterceptLog(u.ctx, nil)
}

func addUsedChannel(c *gin.Context, channelId int) {
	useChannel := c.GetStringSlice("use_channel")
	useChannel = append(useChannel, fmt.Sprintf("%d", channelId))
	c.Set("use_channel", useChannel)
}

func fastTokenCountMetaForPricing(request dto.Request) *types.TokenCountMeta {
	if request == nil {
		return &types.TokenCountMeta{}
	}
	meta := &types.TokenCountMeta{
		TokenType: types.TokenTypeTokenizer,
	}
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		maxCompletionTokens := lo.FromPtrOr(r.MaxCompletionTokens, uint(0))
		maxTokens := lo.FromPtrOr(r.MaxTokens, uint(0))
		if maxCompletionTokens > maxTokens {
			meta.MaxTokens = int(maxCompletionTokens)
		} else {
			meta.MaxTokens = int(maxTokens)
		}
	case *dto.OpenAIResponsesRequest:
		meta.MaxTokens = int(lo.FromPtrOr(r.MaxOutputTokens, uint(0)))
	case *dto.ClaudeRequest:
		meta.MaxTokens = int(lo.FromPtr(r.MaxTokens))
	case *dto.ImageRequest:
		// Pricing for image requests depends on ImagePriceRatio; safe to compute even when CountToken is disabled.
		return r.GetTokenCountMeta()
	default:
		// Best-effort: leave CombineText empty to avoid large allocations.
	}
	return meta
}

func getChannel(c *gin.Context, info *relaycommon.RelayInfo, retryParam *service.RetryParam) (*model.Channel, *types.NewAPIError) {
	// Phase 9A/B safe MVP: per-channel explicit fallback. If a fallback was
	// scheduled, force getChannel to return the fallback channel instead of
	// going through CacheGetRandomSatisfiedChannel. The override is consumed
	// once so subsequent retries in the same request revert to normal selection.
	if overrideID, ok := service.ConsumeChannelFallbackOverride(c); ok && overrideID > 0 {
		fallback, err := model.GetChannelById(overrideID, true)
		if err != nil || fallback == nil {
			return nil, types.NewError(fmt.Errorf("获取回退渠道 #%d 失败: %s", overrideID, errOrEmpty(err)), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		}
		info.PriceData.GroupRatioInfo = helper.HandleGroupRatio(c, info)
		if setupErr := middleware.SetupContextForSelectedChannel(c, fallback, info.OriginModelName); setupErr != nil {
			return nil, setupErr
		}
		return fallback, nil
	}
	if info.ChannelMeta == nil {
		autoBan := c.GetBool("auto_ban")
		autoBanInt := 1
		if !autoBan {
			autoBanInt = 0
		}
		return &model.Channel{
			Id:      c.GetInt("channel_id"),
			Type:    c.GetInt("channel_type"),
			Name:    c.GetString("channel_name"),
			AutoBan: &autoBanInt,
		}, nil
	}
	channel, selectGroup, err := service.CacheGetRandomSatisfiedChannel(retryParam)

	info.PriceData.GroupRatioInfo = helper.HandleGroupRatio(c, info)

	if err != nil {
		return nil, types.NewError(fmt.Errorf("获取分组 %s 下模型 %s 的可用渠道失败（retry）: %s", selectGroup, info.OriginModelName, err.Error()), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	if channel == nil {
		return nil, types.NewError(fmt.Errorf("分组 %s 下模型 %s 的可用渠道不存在（retry）", selectGroup, info.OriginModelName), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}

	newAPIError := middleware.SetupContextForSelectedChannel(c, channel, info.OriginModelName)
	if newAPIError != nil {
		return nil, newAPIError
	}
	return channel, nil
}

// errOrEmpty returns err.Error() or "" when err is nil. Used for fallback channel
// load error messages where the error may be nil but the channel lookup still
// failed (returned nil without an error).
func errOrEmpty(err error) string {
	if err == nil {
		return "channel not found"
	}
	return err.Error()
}

func shouldRetry(c *gin.Context, openaiErr *types.NewAPIError, retryTimes int) bool {
	if openaiErr == nil {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return false
	}
	if types.IsChannelError(openaiErr) {
		return true
	}
	if types.IsSkipRetryError(openaiErr) {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	code := openaiErr.StatusCode
	if code >= 200 && code < 300 {
		return false
	}
	if code < 100 || code > 599 {
		return true
	}
	if operation_setting.IsAlwaysSkipRetryCode(openaiErr.GetErrorCode()) {
		return false
	}
	return operation_setting.ShouldRetryByStatusCode(code)
}

func processChannelError(c *gin.Context, channelError types.ChannelError, err *types.NewAPIError) {
	logger.LogError(c, fmt.Sprintf("channel error (channel #%d, status code: %d): %s", channelError.ChannelId, err.StatusCode, common.LocalLogPreview(err.Error())))
	// 不要使用context获取渠道信息，异步处理时可能会出现渠道信息不一致的情况
	// do not use context to get channel info, there may be inconsistent channel info when processing asynchronously
	if service.ShouldDisableChannel(err) && channelError.AutoBan {
		gopool.Go(func() {
			service.DisableChannel(channelError, err.ErrorWithStatusCode())
		})
	}

	if constant.ErrorLogEnabled && types.IsRecordErrorLog(err) {
		// 保存错误日志到mysql中
		userId := c.GetInt("id")
		tokenName := c.GetString("token_name")
		modelName := c.GetString("original_model")
		tokenId := c.GetInt("token_id")
		userGroup := c.GetString("group")
		channelId := c.GetInt("channel_id")
		other := make(map[string]interface{})
		if c.Request != nil && c.Request.URL != nil {
			other["request_path"] = c.Request.URL.Path
		}
		other["error_type"] = err.GetErrorType()
		other["error_code"] = err.GetErrorCode()
		other["status_code"] = err.StatusCode
		other["channel_id"] = channelId
		other["channel_name"] = c.GetString("channel_name")
		other["channel_type"] = c.GetInt("channel_type")
		adminInfo := make(map[string]interface{})
		adminInfo["use_channel"] = c.GetStringSlice("use_channel")
		isMultiKey := common.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey)
		if isMultiKey {
			adminInfo["is_multi_key"] = true
			adminInfo["multi_key_index"] = common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
		}
		service.AppendChannelAffinityAdminInfo(c, adminInfo)
		service.AppendChannelFallbackAdminInfo(c, adminInfo)
		other["admin_info"] = adminInfo
		startTime := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
		if startTime.IsZero() {
			startTime = time.Now()
		}
		useTimeSeconds := int(time.Since(startTime).Seconds())
		model.RecordErrorLog(c, userId, channelId, modelName, tokenName, err.MaskSensitiveErrorWithStatusCode(), tokenId, useTimeSeconds, common.GetContextKeyBool(c, constant.ContextKeyIsStream), userGroup, other)
	}

}

func RelayMidjourney(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatMjProxy, nil, nil)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"description": fmt.Sprintf("failed to generate relay info: %s", err.Error()),
			"type":        "upstream_error",
			"code":        4,
		})
		return
	}

	var mjErr *dto.MidjourneyResponse
	switch relayInfo.RelayMode {
	case relayconstant.RelayModeMidjourneyNotify:
		mjErr = relay.RelayMidjourneyNotify(c)
	case relayconstant.RelayModeMidjourneyTaskFetch, relayconstant.RelayModeMidjourneyTaskFetchByCondition:
		mjErr = relay.RelayMidjourneyTask(c, relayInfo.RelayMode)
	case relayconstant.RelayModeMidjourneyTaskImageSeed:
		mjErr = relay.RelayMidjourneyTaskImageSeed(c)
	case relayconstant.RelayModeSwapFace:
		mjErr = relay.RelaySwapFace(c, relayInfo)
	default:
		mjErr = relay.RelayMidjourneySubmit(c, relayInfo)
	}
	//err = relayMidjourneySubmit(c, relayMode)
	log.Println(mjErr)
	if mjErr != nil {
		statusCode := http.StatusBadRequest
		if mjErr.Code == 30 {
			mjErr.Result = "当前分组负载已饱和，请稍后再试，或升级账户以提升服务质量。"
			statusCode = http.StatusTooManyRequests
		}
		c.JSON(statusCode, gin.H{
			"description": fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result),
			"type":        "upstream_error",
			"code":        mjErr.Code,
		})
		channelId := c.GetInt("channel_id")
		logger.LogError(c, fmt.Sprintf("relay error (channel #%d, status code %d): %s", channelId, statusCode, fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result)))
	}
}

func RelayNotImplemented(c *gin.Context) {
	err := types.OpenAIError{
		Message: "API not implemented",
		Type:    "new_api_error",
		Param:   "",
		Code:    "api_not_implemented",
	}
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": err,
	})
}

func RelayNotFound(c *gin.Context) {
	err := types.OpenAIError{
		Message: fmt.Sprintf("Invalid URL (%s %s)", c.Request.Method, c.Request.URL.Path),
		Type:    "invalid_request_error",
		Param:   "",
		Code:    "",
	}
	c.JSON(http.StatusNotFound, gin.H{
		"error": err,
	})
}

func RelayTaskFetch(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &dto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}
	if taskErr := relay.RelayTaskFetch(c, relayInfo.RelayMode); taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

func RelayTask(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &dto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}

	if taskErr := relay.ResolveOriginTask(c, relayInfo); taskErr != nil {
		respondTaskError(c, taskErr)
		return
	}

	var result *relay.TaskSubmitResult
	var taskErr *dto.TaskError
	defer func() {
		if taskErr != nil && relayInfo.Billing != nil {
			relayInfo.Billing.Refund(c)
		}
	}()

	retryParam := &service.RetryParam{
		Ctx:         c,
		TokenGroup:  relayInfo.TokenGroup,
		ModelName:   relayInfo.OriginModelName,
		RequestPath: c.Request.URL.Path,
		Retry:       common.GetPointer(0),
	}

	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		var channel *model.Channel

		if lockedCh, ok := relayInfo.LockedChannel.(*model.Channel); ok && lockedCh != nil {
			channel = lockedCh
			if retryParam.GetRetry() > 0 {
				if setupErr := middleware.SetupContextForSelectedChannel(c, channel, relayInfo.OriginModelName); setupErr != nil {
					taskErr = service.TaskErrorWrapperLocal(setupErr.Err, "setup_locked_channel_failed", http.StatusInternalServerError)
					break
				}
			}
		} else {
			var channelErr *types.NewAPIError
			channel, channelErr = getChannel(c, relayInfo, retryParam)
			if channelErr != nil {
				logger.LogError(c, channelErr.Error())
				taskErr = service.TaskErrorWrapperLocal(channelErr.Err, "get_channel_failed", http.StatusInternalServerError)
				break
			}
		}

		addUsedChannel(c, channel.Id)
		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusRequestEntityTooLarge)
			} else {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusBadRequest)
			}
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)

		result, taskErr = relay.RelayTaskSubmit(c, relayInfo)
		if taskErr == nil {
			break
		}

		if !taskErr.LocalError {
			processChannelError(c,
				*types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey,
					common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()),
				types.NewOpenAIError(taskErr.Error, types.ErrorCodeBadResponseStatusCode, taskErr.StatusCode))
		}

		if !shouldRetryTaskRelay(c, channel.Id, taskErr, common.RetryTimes-retryParam.GetRetry()) {
			break
		}
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}

	// ── 成功：结算 + 日志 + 插入任务 ──
	if taskErr == nil {
		if settleErr := service.SettleBilling(c, relayInfo, result.Quota); settleErr != nil {
			common.SysError("settle task billing error: " + settleErr.Error())
		}
		service.LogTaskConsumption(c, relayInfo)

		task := model.InitTask(result.Platform, relayInfo)
		task.PrivateData.UpstreamTaskID = result.UpstreamTaskID
		task.PrivateData.BillingSource = relayInfo.BillingSource
		task.PrivateData.SubscriptionId = relayInfo.SubscriptionId
		task.PrivateData.TokenId = relayInfo.TokenId
		task.PrivateData.BillingContext = &model.TaskBillingContext{
			ModelPrice:      relayInfo.PriceData.ModelPrice,
			GroupRatio:      relayInfo.PriceData.GroupRatioInfo.GroupRatio,
			ModelRatio:      relayInfo.PriceData.ModelRatio,
			OtherRatios:     relayInfo.PriceData.OtherRatios,
			OriginModelName: relayInfo.OriginModelName,
			PerCallBilling:  common.StringsContains(constant.TaskPricePatches, relayInfo.OriginModelName) || relayInfo.PriceData.UsePrice,
		}
		task.Quota = result.Quota
		task.Data = result.TaskData
		task.Action = relayInfo.Action
		if insertErr := task.Insert(); insertErr != nil {
			common.SysError("insert task error: " + insertErr.Error())
		}
	}

	if taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

// respondTaskError 统一输出 Task 错误响应（含 429 限流提示改写）
func respondTaskError(c *gin.Context, taskErr *dto.TaskError) {
	if taskErr.StatusCode == http.StatusTooManyRequests {
		taskErr.Message = "当前分组上游负载已饱和，请稍后再试"
	}
	c.JSON(taskErr.StatusCode, taskErr)
}

func shouldRetryTaskRelay(c *gin.Context, channelId int, taskErr *dto.TaskError, retryTimes int) bool {
	if taskErr == nil {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	if taskErr.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if taskErr.StatusCode == 307 {
		return true
	}
	if taskErr.StatusCode/100 == 5 {
		// 超时不重试
		if operation_setting.IsAlwaysSkipRetryStatusCode(taskErr.StatusCode) {
			return false
		}
		return true
	}
	if taskErr.StatusCode == http.StatusBadRequest {
		return false
	}
	if taskErr.StatusCode == 408 {
		// azure处理超时不重试
		return false
	}
	if taskErr.LocalError {
		return false
	}
	if taskErr.StatusCode/100 == 2 {
		return false
	}
	return true
}
