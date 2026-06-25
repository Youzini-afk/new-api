package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type textQuotaSummary struct {
	PromptTokens             int
	CompletionTokens         int
	TotalTokens              int
	CacheTokens              int
	CacheCreationTokens      int
	CacheCreationTokens5m    int
	CacheCreationTokens1h    int
	ImageTokens              int
	AudioTokens              int
	ModelName                string
	TokenName                string
	UseTimeSeconds           int64
	CompletionRatio          float64
	CacheRatio               float64
	ImageRatio               float64
	ModelRatio               float64
	GroupRatio               float64
	ModelPrice               float64
	CacheCreationRatio       float64
	CacheCreationRatio5m     float64
	CacheCreationRatio1h     float64
	Quota                    int
	IsClaudeUsageSemantic    bool
	UsageSemantic            string
	WebSearchPrice           float64
	WebSearchCallCount       int
	ClaudeWebSearchPrice     float64
	ClaudeWebSearchCallCount int
	FileSearchPrice          float64
	FileSearchCallCount      int
	AudioInputPrice          float64
	ImageGenerationCallPrice float64
	ToolCallSurchargeQuota   decimal.Decimal
	// ShortMsgExtraBilling is the Phase 10A/10B short-message extra billing
	// evaluation result. In shadow mode it records what *would* be charged
	// and never alters summary.Quota. In enforce mode the service layer
	// additionally applies the frozen preflight fee to summary.Quota when the
	// post-response conditions hold (see applyShortMsgExtraBillingEnforce).
	// Nil when the feature is disabled, no rule matched, or the request is
	// non-billable.
	ShortMsgExtraBilling *operation_setting.ShortMsgExtraBillingResult
}

func cacheWriteTokensTotal(summary textQuotaSummary) int {
	if summary.CacheCreationTokens5m > 0 || summary.CacheCreationTokens1h > 0 {
		splitCacheWriteTokens := summary.CacheCreationTokens5m + summary.CacheCreationTokens1h
		if summary.CacheCreationTokens > splitCacheWriteTokens {
			return summary.CacheCreationTokens
		}
		return splitCacheWriteTokens
	}
	return summary.CacheCreationTokens
}

func isLegacyClaudeDerivedOpenAIUsage(relayInfo *relaycommon.RelayInfo, usage *dto.Usage) bool {
	if relayInfo == nil || usage == nil {
		return false
	}
	if relayInfo.GetFinalRequestRelayFormat() == types.RelayFormatClaude {
		return false
	}
	if usage.UsageSource != "" || usage.UsageSemantic != "" {
		return false
	}
	return usage.ClaudeCacheCreation5mTokens > 0 || usage.ClaudeCacheCreation1hTokens > 0
}

func calculateTextToolCallSurcharge(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, summary *textQuotaSummary) decimal.Decimal {
	dGroupRatio := decimal.NewFromFloat(summary.GroupRatio)
	dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)

	var surcharge decimal.Decimal

	if relayInfo.ResponsesUsageInfo != nil {
		if webSearchTool, exists := relayInfo.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview]; exists && webSearchTool.CallCount > 0 {
			summary.WebSearchCallCount = webSearchTool.CallCount
			summary.WebSearchPrice = operation_setting.GetToolPriceForModel("web_search_preview", summary.ModelName)
			surcharge = surcharge.Add(decimal.NewFromFloat(summary.WebSearchPrice).
				Mul(decimal.NewFromInt(int64(webSearchTool.CallCount))).
				Div(decimal.NewFromInt(1000)).
				Mul(dGroupRatio).
				Mul(dQuotaPerUnit))
		}
	} else if strings.HasSuffix(summary.ModelName, "search-preview") {
		summary.WebSearchCallCount = 1
		summary.WebSearchPrice = operation_setting.GetToolPriceForModel("web_search_preview", summary.ModelName)
		surcharge = surcharge.Add(decimal.NewFromFloat(summary.WebSearchPrice).
			Div(decimal.NewFromInt(1000)).
			Mul(dGroupRatio).
			Mul(dQuotaPerUnit))
	}

	summary.ClaudeWebSearchCallCount = ctx.GetInt("claude_web_search_requests")
	if summary.ClaudeWebSearchCallCount > 0 {
		summary.ClaudeWebSearchPrice = operation_setting.GetToolPrice("web_search")
		surcharge = surcharge.Add(decimal.NewFromFloat(summary.ClaudeWebSearchPrice).
			Div(decimal.NewFromInt(1000)).
			Mul(dGroupRatio).
			Mul(dQuotaPerUnit).
			Mul(decimal.NewFromInt(int64(summary.ClaudeWebSearchCallCount))))
	}

	if relayInfo.ResponsesUsageInfo != nil {
		if fileSearchTool, exists := relayInfo.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolFileSearch]; exists && fileSearchTool.CallCount > 0 {
			summary.FileSearchCallCount = fileSearchTool.CallCount
			summary.FileSearchPrice = operation_setting.GetToolPrice("file_search")
			surcharge = surcharge.Add(decimal.NewFromFloat(summary.FileSearchPrice).
				Mul(decimal.NewFromInt(int64(fileSearchTool.CallCount))).
				Div(decimal.NewFromInt(1000)).
				Mul(dGroupRatio).
				Mul(dQuotaPerUnit))
		}
	}

	if ctx.GetBool("image_generation_call") {
		summary.ImageGenerationCallPrice = operation_setting.GetGPTImage1PriceOnceCall(ctx.GetString("image_generation_call_quality"), ctx.GetString("image_generation_call_size"))
		surcharge = surcharge.Add(decimal.NewFromFloat(summary.ImageGenerationCallPrice).
			Mul(dGroupRatio).
			Mul(dQuotaPerUnit))
	}

	return surcharge
}

func composeTieredTextQuota(relayInfo *relaycommon.RelayInfo, summary textQuotaSummary, tieredQuota int, tieredResult *billingexpr.TieredResult) int {
	if summary.ToolCallSurchargeQuota.IsZero() {
		return tieredQuota
	}

	if tieredResult != nil {
		if snap := relayInfo.TieredBillingSnapshot; snap != nil {
			return int(decimal.NewFromFloat(tieredResult.ActualQuotaBeforeGroup).
				Mul(decimal.NewFromFloat(snap.GroupRatio)).
				Add(summary.ToolCallSurchargeQuota).
				Round(0).
				IntPart())
		}
	}

	return tieredQuota + int(summary.ToolCallSurchargeQuota.Round(0).IntPart())
}

func calculateTextQuotaSummary(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.Usage) textQuotaSummary {
	summary := textQuotaSummary{
		ModelName:            relayInfo.OriginModelName,
		TokenName:            ctx.GetString("token_name"),
		UseTimeSeconds:       time.Now().Unix() - relayInfo.StartTime.Unix(),
		CompletionRatio:      relayInfo.PriceData.CompletionRatio,
		CacheRatio:           relayInfo.PriceData.CacheRatio,
		ImageRatio:           relayInfo.PriceData.ImageRatio,
		ModelRatio:           relayInfo.PriceData.ModelRatio,
		GroupRatio:           relayInfo.PriceData.GroupRatioInfo.GroupRatio,
		ModelPrice:           relayInfo.PriceData.ModelPrice,
		CacheCreationRatio:   relayInfo.PriceData.CacheCreationRatio,
		CacheCreationRatio5m: relayInfo.PriceData.CacheCreation5mRatio,
		CacheCreationRatio1h: relayInfo.PriceData.CacheCreation1hRatio,
		UsageSemantic:        usageSemanticFromUsage(relayInfo, usage),
	}
	summary.IsClaudeUsageSemantic = summary.UsageSemantic == "anthropic"

	if usage == nil {
		usage = &dto.Usage{
			PromptTokens:     relayInfo.GetEstimatePromptTokens(),
			CompletionTokens: 0,
			TotalTokens:      relayInfo.GetEstimatePromptTokens(),
		}
	}

	summary.PromptTokens = usage.PromptTokens
	summary.CompletionTokens = usage.CompletionTokens
	summary.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	summary.CacheTokens = usage.PromptTokensDetails.CachedTokens
	summary.CacheCreationTokens = usage.PromptTokensDetails.CachedCreationTokens
	summary.CacheCreationTokens5m = usage.ClaudeCacheCreation5mTokens
	summary.CacheCreationTokens1h = usage.ClaudeCacheCreation1hTokens
	summary.ImageTokens = usage.PromptTokensDetails.ImageTokens
	summary.AudioTokens = usage.PromptTokensDetails.AudioTokens
	legacyClaudeDerived := isLegacyClaudeDerivedOpenAIUsage(relayInfo, usage)
	isOpenRouterClaudeBilling := relayInfo.ChannelMeta != nil &&
		relayInfo.ChannelType == constant.ChannelTypeOpenRouter &&
		summary.IsClaudeUsageSemantic

	if isOpenRouterClaudeBilling {
		summary.PromptTokens -= summary.CacheTokens
		isUsingCustomSettings := relayInfo.PriceData.UsePrice || hasCustomModelRatio(summary.ModelName, relayInfo.PriceData.ModelRatio)
		if summary.CacheCreationTokens == 0 && relayInfo.PriceData.CacheCreationRatio != 1 && usage.Cost != 0 && !isUsingCustomSettings {
			maybeCacheCreationTokens := CalcOpenRouterCacheCreateTokens(*usage, relayInfo.PriceData)
			if maybeCacheCreationTokens >= 0 && summary.PromptTokens >= maybeCacheCreationTokens {
				summary.CacheCreationTokens = maybeCacheCreationTokens
			}
		}
		summary.PromptTokens -= summary.CacheCreationTokens
	}

	dPromptTokens := decimal.NewFromInt(int64(summary.PromptTokens))
	dCacheTokens := decimal.NewFromInt(int64(summary.CacheTokens))
	dImageTokens := decimal.NewFromInt(int64(summary.ImageTokens))
	dAudioTokens := decimal.NewFromInt(int64(summary.AudioTokens))
	dCompletionTokens := decimal.NewFromInt(int64(summary.CompletionTokens))
	dCachedCreationTokens := decimal.NewFromInt(int64(summary.CacheCreationTokens))
	dCompletionRatio := decimal.NewFromFloat(summary.CompletionRatio)
	dCacheRatio := decimal.NewFromFloat(summary.CacheRatio)
	dImageRatio := decimal.NewFromFloat(summary.ImageRatio)
	dModelRatio := decimal.NewFromFloat(summary.ModelRatio)
	dGroupRatio := decimal.NewFromFloat(summary.GroupRatio)
	dModelPrice := decimal.NewFromFloat(summary.ModelPrice)
	dCacheCreationRatio := decimal.NewFromFloat(summary.CacheCreationRatio)
	dCacheCreationRatio5m := decimal.NewFromFloat(summary.CacheCreationRatio5m)
	dCacheCreationRatio1h := decimal.NewFromFloat(summary.CacheCreationRatio1h)
	dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)

	ratio := dModelRatio.Mul(dGroupRatio)
	summary.ToolCallSurchargeQuota = calculateTextToolCallSurcharge(ctx, relayInfo, &summary)

	var audioInputQuota decimal.Decimal
	if !relayInfo.PriceData.UsePrice {
		baseTokens := dPromptTokens

		var cachedTokensWithRatio decimal.Decimal
		if !dCacheTokens.IsZero() {
			if !summary.IsClaudeUsageSemantic && !legacyClaudeDerived {
				baseTokens = baseTokens.Sub(dCacheTokens)
			}
			cachedTokensWithRatio = dCacheTokens.Mul(dCacheRatio)
		}

		var cachedCreationTokensWithRatio decimal.Decimal
		hasSplitCacheCreationTokens := summary.CacheCreationTokens5m > 0 || summary.CacheCreationTokens1h > 0
		if !dCachedCreationTokens.IsZero() || hasSplitCacheCreationTokens {
			if !summary.IsClaudeUsageSemantic && !legacyClaudeDerived {
				baseTokens = baseTokens.Sub(dCachedCreationTokens)
				cachedCreationTokensWithRatio = dCachedCreationTokens.Mul(dCacheCreationRatio)
			} else {
				remaining := summary.CacheCreationTokens - summary.CacheCreationTokens5m - summary.CacheCreationTokens1h
				if remaining < 0 {
					remaining = 0
				}
				cachedCreationTokensWithRatio = decimal.NewFromInt(int64(remaining)).Mul(dCacheCreationRatio)
				cachedCreationTokensWithRatio = cachedCreationTokensWithRatio.Add(decimal.NewFromInt(int64(summary.CacheCreationTokens5m)).Mul(dCacheCreationRatio5m))
				cachedCreationTokensWithRatio = cachedCreationTokensWithRatio.Add(decimal.NewFromInt(int64(summary.CacheCreationTokens1h)).Mul(dCacheCreationRatio1h))
			}
		}

		var imageTokensWithRatio decimal.Decimal
		if !dImageTokens.IsZero() {
			baseTokens = baseTokens.Sub(dImageTokens)
			imageTokensWithRatio = dImageTokens.Mul(dImageRatio)
		}

		if !dAudioTokens.IsZero() {
			summary.AudioInputPrice = operation_setting.GetGeminiInputAudioPricePerMillionTokens(summary.ModelName)
			if summary.AudioInputPrice > 0 {
				baseTokens = baseTokens.Sub(dAudioTokens)
				audioInputQuota = decimal.NewFromFloat(summary.AudioInputPrice).
					Div(decimal.NewFromInt(1000000)).Mul(dAudioTokens).Mul(dGroupRatio).Mul(dQuotaPerUnit)
			}
		}

		promptQuota := baseTokens.Add(cachedTokensWithRatio).Add(imageTokensWithRatio).Add(cachedCreationTokensWithRatio)
		completionQuota := dCompletionTokens.Mul(dCompletionRatio)
		quotaCalculateDecimal := promptQuota.Add(completionQuota).Mul(ratio)
		quotaCalculateDecimal = quotaCalculateDecimal.Add(summary.ToolCallSurchargeQuota)
		quotaCalculateDecimal = quotaCalculateDecimal.Add(audioInputQuota)

		if len(relayInfo.PriceData.OtherRatios) > 0 {
			for _, otherRatio := range relayInfo.PriceData.OtherRatios {
				quotaCalculateDecimal = quotaCalculateDecimal.Mul(decimal.NewFromFloat(otherRatio))
			}
		}

		if !ratio.IsZero() && quotaCalculateDecimal.LessThanOrEqual(decimal.Zero) {
			quotaCalculateDecimal = decimal.NewFromInt(1)
		}
		summary.Quota = int(quotaCalculateDecimal.Round(0).IntPart())
	} else {
		quotaCalculateDecimal := dModelPrice.Mul(dQuotaPerUnit).Mul(dGroupRatio)
		quotaCalculateDecimal = quotaCalculateDecimal.Add(summary.ToolCallSurchargeQuota)
		quotaCalculateDecimal = quotaCalculateDecimal.Add(audioInputQuota)
		if len(relayInfo.PriceData.OtherRatios) > 0 {
			for _, otherRatio := range relayInfo.PriceData.OtherRatios {
				quotaCalculateDecimal = quotaCalculateDecimal.Mul(decimal.NewFromFloat(otherRatio))
			}
		}
		summary.Quota = int(quotaCalculateDecimal.Round(0).IntPart())
	}

	if summary.TotalTokens == 0 {
		summary.Quota = 0
	} else if !ratio.IsZero() && summary.Quota == 0 {
		summary.Quota = 1
	}

	// Phase 10A/10B: evaluate short-message extra billing for both shadow
	// and enforce modes. The result only records what *would* be charged;
	// the enforce-mode charge (when applicable) is applied post-tiered in
	// PostTextConsumeQuota using the frozen preflight.
	summary.ShortMsgExtraBilling = evaluateShortMsgExtraBilling(relayInfo, summary)

	return summary
}

func usageSemanticFromUsage(relayInfo *relaycommon.RelayInfo, usage *dto.Usage) string {
	if usage != nil && usage.UsageSemantic != "" {
		return usage.UsageSemantic
	}
	if relayInfo != nil && relayInfo.GetFinalRequestRelayFormat() == types.RelayFormatClaude {
		return "anthropic"
	}
	return "openai"
}

// shortMsgExtraBillingTextMode maps the current request's relay format/mode
// to a stable internal text-mode label used by short-message extra billing
// rules' response_modes filter.
//
// Returns "" for non-text paths (image, embedding, rerank, audio, realtime,
// task, mj_proxy) and for OpenAI-format sub-modes that are not chat/legacy
// completions. The evaluator treats an empty textMode as fail-closed no-op
// (Reason "non_text_mode") regardless of rule ResponseModes, so those paths
// never produce a shadow audit record even when PostTextConsumeQuota is
// reached via image / embedding / rerank / audio fallback callers.
func shortMsgExtraBillingTextMode(relayInfo *relaycommon.RelayInfo) string {
	if relayInfo == nil {
		return ""
	}
	format := relayInfo.GetFinalRequestRelayFormat()
	switch format {
	case types.RelayFormatOpenAI:
		// Distinguish chat completions from legacy /v1/completions. Other
		// OpenAI-format text sub-modes are treated conservatively as unknown.
		switch relayInfo.RelayMode {
		case relayconstant.RelayModeChatCompletions:
			return "chat_completions"
		case relayconstant.RelayModeCompletions:
			return "completions"
		}
		return ""
	case types.RelayFormatOpenAIResponses:
		return "responses"
	case types.RelayFormatOpenAIResponsesCompaction:
		return "responses_compact"
	case types.RelayFormatClaude:
		return "claude"
	case types.RelayFormatGemini:
		return "gemini"
	}
	return ""
}

// evaluateShortMsgExtraBilling runs the short-message extra billing
// evaluator against the current request for both shadow and enforce modes.
// The result is informational: in shadow mode it surfaces what *would* be
// charged; in enforce mode the actual charge decision is made by
// applyShortMsgExtraBillingEnforce using the frozen preflight (so config
// changes mid-request cannot break accounting). Returns nil when the feature
// is disabled, no rule matched, or the request is non-billable.
func evaluateShortMsgExtraBilling(relayInfo *relaycommon.RelayInfo, summary textQuotaSummary) *operation_setting.ShortMsgExtraBillingResult {
	cfg := operation_setting.GetQuotaSetting().ShortMsgExtraBilling
	if cfg.Mode != operation_setting.ShortMsgExtraBillingModeShadow && cfg.Mode != operation_setting.ShortMsgExtraBillingModeEnforce {
		return nil
	}
	textMode := shortMsgExtraBillingTextMode(relayInfo)
	result := operation_setting.EvaluateShortMsgExtraBilling(
		cfg,
		summary.ModelName,
		summary.PromptTokens,
		summary.CompletionTokens,
		summary.TotalTokens,
		textMode,
	)
	if !result.HasReportableInfo() {
		return nil
	}
	return &result
}

// injectShortMsgExtraBilling writes the short-message extra billing
// evaluation (shadow or enforce) into the consume log `other` map. The map
// field is only added when the result is reportable; otherwise no field is
// added so unrelated requests keep their log payload clean.
//
// Shadow mode (Phase 10A): audit-only, never alters summary.Quota.
// Enforce mode (Phase 10B): records both the candidate decision and the
// actual charge decision (enforced / charged_extra_quota / final_quota /
// enforce_skipped_reason).
//
// base_quota is derived as summary.Quota - ChargedExtraQuota so it always
// reflects the pre-extra amount regardless of whether enforce already added
// the fee. final_quota is summary.Quota (post-extra). would_final_quota is
// the hypothetical base + candidate (preserved from Phase 10A for shadow
// observability).
func injectShortMsgExtraBilling(other map[string]interface{}, summary textQuotaSummary) {
	if other == nil || summary.ShortMsgExtraBilling == nil {
		return
	}
	res := summary.ShortMsgExtraBilling
	if !res.HasReportableInfo() {
		return
	}
	baseQuota := summary.Quota - res.ChargedExtraQuota
	entry := map[string]interface{}{
		"mode":                   res.Mode,
		"rule_id":                res.MatchedRule.ID,
		"trigger":                res.MatchedRule.Trigger,
		"threshold":              res.MatchedRule.Threshold,
		"text_mode":              res.TextMode,
		"input_tokens":           summary.PromptTokens,
		"completion_tokens":      summary.CompletionTokens,
		"total_tokens":           summary.TotalTokens,
		"base_quota":             baseQuota,
		"candidate_extra_quota":  res.CandidateExtraQuota,
		"would_final_quota":      baseQuota + res.CandidateExtraQuota,
		"would_apply":            res.WouldApply,
		"waived":                 res.Waived,
		"waive_reason":           res.WaiveReason,
		"charged_extra_quota":    res.ChargedExtraQuota,
		"final_quota":            summary.Quota,
		"enforced":               res.Enforced,
		"enforce_skipped_reason": res.EnforceSkippedReason,
	}
	other["short_msg_extra_billing"] = entry
}

// applyShortMsgExtraBillingEnforce applies (or skips) the enforce-mode
// short-message extra charge to summary.Quota based on the frozen preflight
// and the actual post-response usage. It must be called after the base quota
// is calculated and the tiered override is resolved.
//
// Phase 10B must-fix contract:
//
//  1. When a frozen enforce preflight exists
//     (relayInfo.ShortMsgExtraBillingPreflight != nil && frozen.Mode ==
//     "enforce"), the post logic uses the frozen preflight as the
//     authoritative source for charge/waive/skip semantics. The live global
//     config is NOT consulted for the enforce decision — even if it has been
//     changed to off/shadow or the matched rule has been mutated (different
//     fee/threshold/waive/response_modes, or a different rule sharing the
//     same ID), the frozen fields govern.
//
//  2. When no frozen enforce preflight exists, shadow mode is real-time
//     audit (no reservation was made, so it is safe to keep the live
//     evaluator result that calculateTextQuotaSummary already populated).
//     Off mode and stray enforce-without-preflight cases drop the evaluator
//     result to keep logs clean.
//
// Shadow mode is otherwise a no-op here (Phase 10A behavior is preserved by
// leaving summary.ShortMsgExtraBilling untouched).
func applyShortMsgExtraBillingEnforce(
	relayInfo *relaycommon.RelayInfo,
	summary *textQuotaSummary,
	originUsage *dto.Usage,
	tieredBillingApplied bool,
) {
	if summary == nil || relayInfo == nil {
		return
	}

	frozen := relayInfo.ShortMsgExtraBillingPreflight

	// Must-fix #1: when a frozen enforce preflight exists, the post logic
	// must use it as the authoritative source. The live global config is
	// NOT consulted for the enforce decision; even off/shadow or a mutated
	// rule cannot change this request's charge/waive/skip semantics.
	if frozen != nil && frozen.Mode == operation_setting.ShortMsgExtraBillingModeEnforce {
		applyShortMsgEnforceFromFrozen(summary, originUsage, relayInfo, tieredBillingApplied, frozen)
		return
	}

	// No frozen enforce preflight. Shadow mode is real-time audit (no
	// reservation was made), so it is safe to keep the live evaluator
	// result that calculateTextQuotaSummary already populated. Off mode
	// and stray enforce-without-preflight cases drop the evaluator result
	// so no audit entry is written for a request that did not reserve.
	if summary.ShortMsgExtraBilling != nil && summary.ShortMsgExtraBilling.Mode == operation_setting.ShortMsgExtraBillingModeShadow {
		return
	}
	summary.ShortMsgExtraBilling = nil
}

// applyShortMsgEnforceFromFrozen runs the enforce apply using the frozen
// preflight as the exclusive source of truth for the audit and the charge.
// The live evaluator result (if any) is discarded so a mid-request config
// mutation cannot leak into the enforce audit or charge decision.
func applyShortMsgEnforceFromFrozen(
	summary *textQuotaSummary,
	originUsage *dto.Usage,
	relayInfo *relaycommon.RelayInfo,
	tieredBillingApplied bool,
	frozen *relaycommon.ShortMsgExtraBillingPreflight,
) {
	// No reservation was actually made at preflight. Per spec, keep logs
	// clean: drop any evaluator result so no audit entry is written.
	if frozen.PotentialExtraQuota <= 0 {
		summary.ShortMsgExtraBilling = nil
		return
	}

	// Must-fix #3: always rebuild the audit/charge result from the frozen
	// preflight. Never use the live evaluator result for enforce audit or
	// charge decisions — the live rule may have changed fee/threshold/
	// waive/response_modes even when its ID matches the frozen RuleID.
	result := &operation_setting.ShortMsgExtraBillingResult{
		Mode:     operation_setting.ShortMsgExtraBillingModeEnforce,
		TextMode: frozen.TextMode,
		MatchedRule: &operation_setting.ShortMsgExtraBillingRule{
			ID:                            frozen.RuleID,
			Model:                         frozen.Model,
			Trigger:                       frozen.Trigger,
			Threshold:                     frozen.Threshold,
			FeeQuota:                      frozen.FeeQuota,
			WaiveWhenCompletionTokensZero: frozen.WaiveWhenCompletionTokensZero,
		},
		Reason:              "matched",
		CandidateExtraQuota: frozen.FeeQuota,
	}
	summary.ShortMsgExtraBilling = result

	skipReason := computeShortMsgEnforceSkipReason(frozen, summary, originUsage, relayInfo, tieredBillingApplied)
	if skipReason == "" {
		summary.Quota += frozen.FeeQuota
		result.Enforced = true
		result.ChargedExtraQuota = frozen.FeeQuota
		result.FinalQuota = summary.Quota
		result.EnforceSkippedReason = ""
		result.WouldApply = true
		return
	}

	// Not applied: leave summary.Quota unchanged so SettleBilling refunds
	// the pre-reserved extra via negative delta.
	result.Enforced = false
	result.ChargedExtraQuota = 0
	result.FinalQuota = summary.Quota
	result.EnforceSkippedReason = skipReason
	if skipReason == "completion_tokens_zero" {
		// Completion-zero waiver: only the extra is waived; base quota is
		// still charged (Phase 10A waiver semantics, preserved).
		result.Waived = true
		result.WaiveReason = "completion_tokens_zero"
		result.WouldApply = false
	}
}

// computeShortMsgEnforceSkipReason returns "" when the enforce charge should
// apply, or a stable machine-readable skip reason explaining why it should
// not. Conditions mirror the Phase 10B must-fix spec ordering (most reliable
// signal first; later conditions assume earlier ones hold).
func computeShortMsgEnforceSkipReason(
	frozen *relaycommon.ShortMsgExtraBillingPreflight,
	summary *textQuotaSummary,
	originUsage *dto.Usage,
	relayInfo *relaycommon.RelayInfo,
	tieredBillingApplied bool,
) string {
	// Must-fix #5: explicit frozen model / trigger / mode validation. The
	// entry gate already ensures frozen.Mode == "enforce"; this is a
	// defense-in-depth check that documents the requirement and guards
	// against future callers bypassing the gate.
	if frozen.Mode != operation_setting.ShortMsgExtraBillingModeEnforce {
		return "frozen_mode_mismatch"
	}
	if summary.ModelName != frozen.Model {
		return "model_mismatch"
	}
	if frozen.Trigger != operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow {
		return "trigger_mismatch"
	}

	// Must-fix #4: reliable usage boundary based on the original usage, not
	// summary.TotalTokens (which is prompt+completion and may diverge from
	// upstream's reported total). The base quota calculation is unaffected;
	// this only gates the enforce extra.
	if originUsage == nil {
		return "usage_unreliable"
	}
	if originUsage.TotalTokens <= 0 {
		return "total_tokens_zero"
	}

	if relayInfo.IsStream {
		return "streaming"
	}

	// Must-fix #2: post text mode must match the frozen text mode. An empty
	// postTextMode means the request reached PostTextConsumeQuota via a
	// non-text path (image / embedding / rerank / audio / ...); a non-empty
	// but mismatched postTextMode means the relay format changed between
	// preflight and post (should not happen in practice, but fail-closed).
	postTextMode := shortMsgExtraBillingTextMode(relayInfo)
	if postTextMode == "" {
		return "non_text_mode"
	}
	if postTextMode != frozen.TextMode {
		return "text_mode_mismatch"
	}

	if tieredBillingApplied || relayInfo.TieredBillingSnapshot != nil {
		return "tiered_expr"
	}

	// Use the actual post-response prompt tokens against the FROZEN
	// threshold (the preflight used a conservative estimate).
	if summary.PromptTokens >= frozen.Threshold {
		return "threshold_not_met"
	}

	if summary.CompletionTokens == 0 && frozen.WaiveWhenCompletionTokensZero {
		return "completion_tokens_zero"
	}

	if frozen.FeeQuota > frozen.PotentialExtraQuota {
		// Safety net: should never trigger because PotentialExtraQuota is
		// set to FeeQuota at preflight. Surfaces a mis-reservation rather
		// than silently overdrafting.
		return "no_preflight"
	}
	return ""
}

// injectShortMsgExtraBillingShadow is a backward-compatible wrapper for
// injectShortMsgExtraBilling retained so Phase 10A callers/tests keep
// compiling. The injection is mode-agnostic.
func injectShortMsgExtraBillingShadow(other map[string]interface{}, summary textQuotaSummary) {
	injectShortMsgExtraBilling(other, summary)
}

func PostTextConsumeQuota(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.Usage, extraContent []string) {
	originUsage := usage
	if usage == nil {
		extraContent = append(extraContent, "上游无计费信息")
	}
	if originUsage != nil {
		ObserveChannelAffinityUsageCacheByRelayFormat(ctx, usage, relayInfo.GetFinalRequestRelayFormat())
	}

	adminRejectReason := common.GetContextKeyString(ctx, constant.ContextKeyAdminRejectReason)
	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	var tieredResult *billingexpr.TieredResult
	tieredBillingApplied := false
	if originUsage != nil {
		var tieredUsedVars map[string]bool
		if snap := relayInfo.TieredBillingSnapshot; snap != nil {
			tieredUsedVars = billingexpr.UsedVars(snap.ExprString)
		}
		tieredOk, tieredQuota, tieredRes := TryTieredSettle(relayInfo, BuildTieredTokenParams(usage, summary.IsClaudeUsageSemantic, tieredUsedVars))
		if tieredOk {
			tieredBillingApplied = true
			tieredResult = tieredRes
			summary.Quota = composeTieredTextQuota(relayInfo, summary, tieredQuota, tieredRes)
		}
	}

	// Phase 10B: apply (or skip) the enforce-mode short-message extra charge
	// using the frozen preflight. Shadow mode is a no-op here. Must run after
	// the tiered override so summary.Quota reflects the base amount the
	// extra is added to (and so tiered_expr requests are correctly skipped).
	applyShortMsgExtraBillingEnforce(relayInfo, &summary, originUsage, tieredBillingApplied)

	if summary.WebSearchCallCount > 0 {
		extraContent = append(extraContent, fmt.Sprintf("Web Search 调用 %d 次，调用花费 %s", summary.WebSearchCallCount, decimal.NewFromFloat(summary.WebSearchPrice).Mul(decimal.NewFromInt(int64(summary.WebSearchCallCount))).Div(decimal.NewFromInt(1000)).Mul(decimal.NewFromFloat(summary.GroupRatio)).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).String()))
	}
	if summary.ClaudeWebSearchCallCount > 0 {
		extraContent = append(extraContent, fmt.Sprintf("Claude Web Search 调用 %d 次，调用花费 %s", summary.ClaudeWebSearchCallCount, decimal.NewFromFloat(summary.ClaudeWebSearchPrice).Div(decimal.NewFromInt(1000)).Mul(decimal.NewFromFloat(summary.GroupRatio)).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).Mul(decimal.NewFromInt(int64(summary.ClaudeWebSearchCallCount))).String()))
	}
	if summary.FileSearchCallCount > 0 {
		extraContent = append(extraContent, fmt.Sprintf("File Search 调用 %d 次，调用花费 %s", summary.FileSearchCallCount, decimal.NewFromFloat(summary.FileSearchPrice).Mul(decimal.NewFromInt(int64(summary.FileSearchCallCount))).Div(decimal.NewFromInt(1000)).Mul(decimal.NewFromFloat(summary.GroupRatio)).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).String()))
	}
	if summary.AudioInputPrice > 0 && summary.AudioTokens > 0 {
		extraContent = append(extraContent, fmt.Sprintf("Audio Input 花费 %s", decimal.NewFromFloat(summary.AudioInputPrice).Div(decimal.NewFromInt(1000000)).Mul(decimal.NewFromInt(int64(summary.AudioTokens))).Mul(decimal.NewFromFloat(summary.GroupRatio)).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).String()))
	}
	if summary.ImageGenerationCallPrice > 0 {
		extraContent = append(extraContent, fmt.Sprintf("Image Generation Call 花费 %s", decimal.NewFromFloat(summary.ImageGenerationCallPrice).Mul(decimal.NewFromFloat(summary.GroupRatio)).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).String()))
	}

	if summary.TotalTokens == 0 {
		extraContent = append(extraContent, "上游没有返回计费信息，无法扣费（可能是上游超时）")
		logger.LogError(ctx, fmt.Sprintf("total tokens is 0, cannot consume quota, userId %d, channelId %d, tokenId %d, model %s， pre-consumed quota %d", relayInfo.UserId, relayInfo.ChannelId, relayInfo.TokenId, summary.ModelName, relayInfo.FinalPreConsumedQuota))
	} else {
		model.UpdateUserUsedQuotaAndRequestCount(relayInfo.UserId, summary.Quota)
		model.UpdateChannelUsedQuota(relayInfo.ChannelId, summary.Quota)
	}

	if err := SettleBilling(ctx, relayInfo, summary.Quota); err != nil {
		logger.LogError(ctx, "error settling billing: "+err.Error())
	}

	logModel := summary.ModelName
	if strings.HasPrefix(logModel, "gpt-4-gizmo") {
		logModel = "gpt-4-gizmo-*"
		extraContent = append(extraContent, fmt.Sprintf("模型 %s", summary.ModelName))
	}
	if strings.HasPrefix(logModel, "gpt-4o-gizmo") {
		logModel = "gpt-4o-gizmo-*"
		extraContent = append(extraContent, fmt.Sprintf("模型 %s", summary.ModelName))
	}

	logContent := strings.Join(extraContent, ", ")
	var other map[string]interface{}
	if summary.IsClaudeUsageSemantic {
		other = GenerateClaudeOtherInfo(ctx, relayInfo,
			summary.ModelRatio, summary.GroupRatio, summary.CompletionRatio,
			summary.CacheTokens, summary.CacheRatio,
			summary.CacheCreationTokens, summary.CacheCreationRatio,
			summary.CacheCreationTokens5m, summary.CacheCreationRatio5m,
			summary.CacheCreationTokens1h, summary.CacheCreationRatio1h,
			summary.ModelPrice, relayInfo.PriceData.GroupRatioInfo.GroupSpecialRatio)
		other["usage_semantic"] = "anthropic"
	} else {
		other = GenerateTextOtherInfo(ctx, relayInfo, summary.ModelRatio, summary.GroupRatio, summary.CompletionRatio, summary.CacheTokens, summary.CacheRatio, summary.ModelPrice, relayInfo.PriceData.GroupRatioInfo.GroupSpecialRatio)
	}
	if adminRejectReason != "" {
		other["reject_reason"] = adminRejectReason
	}
	if summary.ImageTokens != 0 {
		other["image"] = true
		other["image_ratio"] = summary.ImageRatio
		other["image_output"] = summary.ImageTokens
	}
	if summary.WebSearchCallCount > 0 {
		other["web_search"] = true
		other["web_search_call_count"] = summary.WebSearchCallCount
		other["web_search_price"] = summary.WebSearchPrice
	} else if summary.ClaudeWebSearchCallCount > 0 {
		other["web_search"] = true
		other["web_search_call_count"] = summary.ClaudeWebSearchCallCount
		other["web_search_price"] = summary.ClaudeWebSearchPrice
	}
	if summary.FileSearchCallCount > 0 {
		other["file_search"] = true
		other["file_search_call_count"] = summary.FileSearchCallCount
		other["file_search_price"] = summary.FileSearchPrice
	}
	if summary.AudioInputPrice > 0 && summary.AudioTokens > 0 {
		other["audio_input_seperate_price"] = true
		other["audio_input_token_count"] = summary.AudioTokens
		other["audio_input_price"] = summary.AudioInputPrice
	}
	if summary.ImageGenerationCallPrice > 0 {
		other["image_generation_call"] = true
		other["image_generation_call_price"] = summary.ImageGenerationCallPrice
	}
	if summary.CacheCreationTokens > 0 {
		other["cache_creation_tokens"] = summary.CacheCreationTokens
		other["cache_creation_ratio"] = summary.CacheCreationRatio
	}
	if summary.CacheCreationTokens5m > 0 {
		other["cache_creation_tokens_5m"] = summary.CacheCreationTokens5m
		other["cache_creation_ratio_5m"] = summary.CacheCreationRatio5m
	}
	if summary.CacheCreationTokens1h > 0 {
		other["cache_creation_tokens_1h"] = summary.CacheCreationTokens1h
		other["cache_creation_ratio_1h"] = summary.CacheCreationRatio1h
	}
	cacheWriteTokens := cacheWriteTokensTotal(summary)
	if cacheWriteTokens > 0 {
		// cache_write_tokens: normalized cache creation total for UI display.
		// If split 5m/1h values are present, this is their sum; otherwise it falls back
		// to cache_creation_tokens.
		other["cache_write_tokens"] = cacheWriteTokens
	}
	if relayInfo.GetFinalRequestRelayFormat() != types.RelayFormatClaude && usage != nil && usage.UsageSource != "" && usage.InputTokens > 0 {
		// input_tokens_total: explicit normalized total input used by the usage log UI.
		// Only write this field when upstream/current conversion has already provided a
		// reliable total input value and tagged the usage source. Do not infer it from
		// prompt/cache fields here, otherwise old upstream payloads may be double-counted.
		other["input_tokens_total"] = usage.InputTokens
	}
	if tieredBillingApplied {
		InjectTieredBillingInfo(other, relayInfo, tieredResult)
	}
	injectShortMsgExtraBillingShadow(other, summary)

	model.RecordConsumeLog(ctx, relayInfo.UserId, model.RecordConsumeLogParams{
		ChannelId:        relayInfo.ChannelId,
		PromptTokens:     summary.PromptTokens,
		CompletionTokens: summary.CompletionTokens,
		ModelName:        logModel,
		TokenName:        summary.TokenName,
		Quota:            summary.Quota,
		Content:          logContent,
		TokenId:          relayInfo.TokenId,
		UseTimeSeconds:   int(summary.UseTimeSeconds),
		IsStream:         relayInfo.IsStream,
		Group:            relayInfo.UsingGroup,
		Other:            other,
	})
	gopool.Go(func() {
		perfmetrics.RecordRelaySample(relayInfo, true, int64(summary.CompletionTokens))
	})
}
