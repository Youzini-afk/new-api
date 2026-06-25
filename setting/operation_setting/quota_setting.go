package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

type QuotaSetting struct {
	EnableFreeModelPreConsume bool `json:"enable_free_model_pre_consume"` // 是否对免费模型启用预消耗

	// ShortMsgExtraBilling controls the short-message extra billing feature.
	// Phase 10A introduced the default-off "shadow" mode, which records what
	// *would* be charged into the consume log without altering the actual
	// settled quota. Phase 10B adds the "enforce" mode, which reserves the
	// potential extra quota before the upstream call (so the response can
	// never succeed while billing fails / overdrafts) and applies the extra
	// fee post-response when the actual usage confirms the rule fired.
	// See ShortMsgExtraBillingConfig for details.
	ShortMsgExtraBilling ShortMsgExtraBillingConfig `json:"short_msg_extra_billing"`
}

// ShortMsgExtraBillingConfig is the persisted configuration for the
// short-message extra billing feature.
//
// Mode governs behavior:
//   - "off" (default): feature disabled; nothing is computed or logged.
//   - "shadow": rules are evaluated and the result is written to the consume
//     log's `other` map under the `short_msg_extra_billing` key. No actual
//     quota is charged. (Phase 10A behavior, unchanged.)
//   - "enforce": in addition to the audit, the matched rule's FeeQuota is
//     reserved atomically before the upstream call (via the relay preflight
//     and the checked wallet preconsume) and applied to the final
//     summary.Quota post-response when the actual usage confirms the rule
//     fired. When the rule does not fire post-response, the pre-reserved
//     extra is refunded via SettleBilling's negative delta. (Phase 10B.)
//
// Any other value is treated as "off" (fail-closed).
type ShortMsgExtraBillingConfig struct {
	Mode  string                     `json:"mode"`
	Rules []ShortMsgExtraBillingRule `json:"rules"`
}

// ShortMsgExtraBillingRule defines a single short-message extra-billing rule.
//
// The only trigger supported is `input_tokens_below`, which fires when
// summary.PromptTokens < Threshold (equality does NOT trigger).
type ShortMsgExtraBillingRule struct {
	// ID is a stable, admin-supplied identifier used in audit logs.
	ID string `json:"id"`
	// Model is matched exactly against relayInfo.OriginModelName.
	Model string `json:"model"`
	// Trigger selects the rule condition. Only "input_tokens_below" is
	// supported.
	Trigger string `json:"trigger"`
	// Threshold is compared against summary.PromptTokens for the
	// input_tokens_below trigger. A rule fires when PromptTokens < Threshold.
	Threshold int `json:"threshold"`
	// FeeQuota is the extra quota that would be charged, expressed directly
	// in quota units (no USD conversion). Must be > 0.
	FeeQuota int `json:"fee_quota"`
	// WaiveWhenCompletionTokensZero, when true, causes a rule that otherwise
	// matches to record a "waived" result (would_apply=false) instead of an
	// applied candidate, when CompletionTokens==0.
	WaiveWhenCompletionTokensZero bool `json:"waive_when_completion_tokens_zero"`
	// ResponseModes optionally restricts a rule to specific text relay modes.
	// Stable internal values used by the service layer:
	//   "chat_completions", "completions", "responses",
	//   "responses_compact", "claude", "gemini".
	// When empty, the rule applies to any *text* mode request. Non-text
	// request paths (image, embedding, rerank, audio, ...) produce an empty
	// textMode and never match any rule regardless of this field.
	ResponseModes []string `json:"response_modes,omitempty"`
}

const (
	ShortMsgExtraBillingModeOff     = "off"
	ShortMsgExtraBillingModeShadow  = "shadow"
	ShortMsgExtraBillingModeEnforce = "enforce"

	// ShortMsgExtraBillingTriggerInputTokensBelow is the only trigger
	// supported.
	ShortMsgExtraBillingTriggerInputTokensBelow = "input_tokens_below"
)

// ShortMsgExtraBillingResult is the structured outcome of evaluating
// short-message extra billing rules in shadow or enforce mode.
//
// It is a pure data record; the service layer decides whether to persist it
// into the consume log and (in enforce mode) whether to apply the charge.
// The actual settled quota is never altered by the evaluator itself.
//
// The Phase 10B enforce-mode fields (Enforced / ChargedExtraQuota /
// FinalQuota / EnforceSkippedReason) are populated by the service layer
// after the post-response apply decision is made; the evaluator only fills
// the candidate-decision fields (MatchedRule / WouldApply / Waived / ...).
type ShortMsgExtraBillingResult struct {
	// Mode echoes the active mode ("shadow" or "enforce" when evaluated).
	Mode string
	// TextMode is the stable internal text-mode label produced by the
	// service layer (e.g. "chat_completions"). Empty for non-text paths.
	TextMode string
	// MatchedRule is the first valid rule whose Model and ResponseModes
	// matched the request. It is nil when no rule matched.
	MatchedRule *ShortMsgExtraBillingRule
	// WouldApply is true when the rule fired and was not waived.
	WouldApply bool
	// CandidateExtraQuota is the FeeQuota that would be charged when
	// WouldApply is true (or would have been charged when Waived is true).
	CandidateExtraQuota int
	// Waived is true when a matched rule was suppressed because
	// CompletionTokens==0 and the rule opted into the waiver.
	Waived bool
	// WaiveReason is a stable machine-readable string explaining the waiver.
	// Currently only "completion_tokens_zero".
	WaiveReason string
	// SkippedInvalidRules counts rules that were skipped during validation
	// (empty id/model, unsupported trigger, threshold<=0, fee_quota<=0).
	SkippedInvalidRules int
	// Reason is a stable machine-readable evaluation outcome label, e.g.
	// "mode_disabled", "non_text_mode", "no_rule_matched", "matched",
	// "total_tokens_zero".
	Reason string

	// --- Phase 10B enforce-mode fields (populated by the service layer) ---

	// Enforced is true when the extra fee was actually charged to
	// summary.Quota (enforce mode + all apply conditions met). Always false
	// in shadow mode.
	Enforced bool
	// ChargedExtraQuota is the actual extra quota charged. 0 when not
	// enforced.
	ChargedExtraQuota int
	// FinalQuota is summary.Quota after applying (or not applying) the
	// extra. Set by the service layer so audit reflects the final settled
	// amount.
	FinalQuota int
	// EnforceSkippedReason is a stable machine-readable string explaining
	// why enforce mode did not charge (e.g. "completion_tokens_zero",
	// "threshold_not_met", "total_tokens_zero", "usage_unreliable",
	// "streaming", "tiered_expr", "non_text_mode", "no_preflight").
	// Empty when enforced or when nothing was reserved.
	EnforceSkippedReason string
}

// ShortMsgExtraBillingShadowResult is a backward-compatible alias for
// ShortMsgExtraBillingResult retained so Phase 10A callers/tests that
// reference the old name keep compiling. The struct is mode-agnostic.
type ShortMsgExtraBillingShadowResult = ShortMsgExtraBillingResult

// HasReportableInfo returns true when the evaluation produced information
// worth persisting into the consume log `other` map.
//
// A reportable result requires shadow or enforce mode AND a matched rule,
// and skips the total_tokens_zero case (where the request itself is
// non-billable) and the non_text_mode case (where the request reached
// PostTextConsumeQuota via a non-text path and must not be audited by this
// feature).
func (r ShortMsgExtraBillingResult) HasReportableInfo() bool {
	if r.Mode != ShortMsgExtraBillingModeShadow && r.Mode != ShortMsgExtraBillingModeEnforce {
		return false
	}
	if r.MatchedRule == nil {
		return false
	}
	if r.Reason == "total_tokens_zero" {
		return false
	}
	return true
}

// EvaluateShortMsgExtraBilling deterministically evaluates the short-message
// extra billing rules against a single request.
//
// The function is fail-closed: any malformed/invalid rule is skipped (and
// counted in SkippedInvalidRules); an unknown mode is treated as "off".
// It never mutates the input config and never panics.
//
// Parameters:
//   - cfg: the persisted configuration.
//   - modelName: relayInfo.OriginModelName (exact match required).
//   - promptTokens, completionTokens, totalTokens: usage summary values.
//   - textMode: stable internal text-mode label produced by the service
//     layer (e.g. "chat_completions"). An empty textMode indicates a
//     non-text request path (image, embedding, rerank, audio, realtime,
//     task, mj_proxy, unknown OpenAI sub-modes, ...). The evaluator only
//     audits text chat paths, so an empty textMode is always no-op:
//     the function returns a non-reportable result with Reason
//     "non_text_mode" regardless of whether any rule has an empty
//     ResponseModes list. This prevents PostTextConsumeQuota's image /
//     embedding / rerank / audio-fallback callers from being audited.
func EvaluateShortMsgExtraBilling(
	cfg ShortMsgExtraBillingConfig,
	modelName string,
	promptTokens int,
	completionTokens int,
	totalTokens int,
	textMode string,
) ShortMsgExtraBillingResult {
	result := ShortMsgExtraBillingResult{Mode: cfg.Mode, TextMode: textMode}

	if cfg.Mode != ShortMsgExtraBillingModeShadow && cfg.Mode != ShortMsgExtraBillingModeEnforce {
		// Fail-closed: any unknown mode is treated as off.
		result.Mode = ShortMsgExtraBillingModeOff
		result.Reason = "mode_disabled"
		return result
	}

	// Boundary: only audit text chat paths. An empty textMode means the
	// request reached PostTextConsumeQuota via a non-text path (image,
	// embedding, rerank, audio fallback, ...) or carries an unknown relay
	// format. Even when a rule has an empty ResponseModes list, it must NOT
	// match such paths; fail-closed to no report.
	if textMode == "" {
		result.Reason = "non_text_mode"
		return result
	}

	// Find the first valid rule that matches the request's model and
	// (optionally) response mode. Only the first match is used to avoid
	// stacking multiple surcharges.
	for i := range cfg.Rules {
		rule := cfg.Rules[i]
		if !isShortMsgExtraBillingRuleValid(rule) {
			result.SkippedInvalidRules++
			continue
		}
		if rule.Model != modelName {
			continue
		}
		if len(rule.ResponseModes) > 0 && !containsString(rule.ResponseModes, textMode) {
			continue
		}
		// Matched. Use a stable copy so later config mutations can't alias.
		matched := rule
		result.MatchedRule = &matched
		break
	}

	if result.MatchedRule == nil {
		result.Reason = "no_rule_matched"
		return result
	}

	// total_tokens == 0 means upstream did not report billable usage; the
	// request itself is non-billable, so no candidate is produced.
	if totalTokens == 0 {
		result.Reason = "total_tokens_zero"
		return result
	}

	result.Reason = "matched"

	switch result.MatchedRule.Trigger {
	case ShortMsgExtraBillingTriggerInputTokensBelow:
		// Equality does not trigger.
		triggered := promptTokens < result.MatchedRule.Threshold

		if triggered && completionTokens == 0 && result.MatchedRule.WaiveWhenCompletionTokensZero {
			result.Waived = true
			result.WaiveReason = "completion_tokens_zero"
			result.CandidateExtraQuota = result.MatchedRule.FeeQuota
			result.WouldApply = false
			return result
		}

		if triggered {
			result.WouldApply = true
			result.CandidateExtraQuota = result.MatchedRule.FeeQuota
		}
		// When not triggered, WouldApply stays false and CandidateExtraQuota 0.
		return result
	default:
		// Should be unreachable because isShortMsgExtraBillingRuleValid
		// rejects unsupported triggers, but stay fail-closed.
		result.MatchedRule = nil
		result.Reason = "no_rule_matched"
		return result
	}
}

// EvaluateShortMsgExtraBillingShadow is a backward-compatible wrapper for
// EvaluateShortMsgExtraBilling retained so Phase 10A callers/tests keep
// compiling. The evaluation is mode-agnostic.
func EvaluateShortMsgExtraBillingShadow(
	cfg ShortMsgExtraBillingConfig,
	modelName string,
	promptTokens int,
	completionTokens int,
	totalTokens int,
	textMode string,
) ShortMsgExtraBillingResult {
	return EvaluateShortMsgExtraBilling(cfg, modelName, promptTokens, completionTokens, totalTokens, textMode)
}

// isShortMsgExtraBillingRuleValid reports whether a rule has all required
// fields set to a meaningful value. Invalid rules are skipped silently.
func isShortMsgExtraBillingRuleValid(rule ShortMsgExtraBillingRule) bool {
	if rule.ID == "" || rule.Model == "" {
		return false
	}
	if rule.Trigger != ShortMsgExtraBillingTriggerInputTokensBelow {
		return false
	}
	if rule.Threshold <= 0 || rule.FeeQuota <= 0 {
		return false
	}
	return true
}

func containsString(list []string, target string) bool {
	for _, s := range list {
		if s == target {
			return true
		}
	}
	return false
}

// 默认配置
var quotaSetting = QuotaSetting{
	EnableFreeModelPreConsume: true,
	ShortMsgExtraBilling: ShortMsgExtraBillingConfig{
		Mode:  ShortMsgExtraBillingModeOff,
		Rules: nil,
	},
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("quota_setting", &quotaSetting)
}

func GetQuotaSetting() *QuotaSetting {
	return &quotaSetting
}
