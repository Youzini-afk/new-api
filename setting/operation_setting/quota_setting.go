package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

type QuotaSetting struct {
	EnableFreeModelPreConsume bool `json:"enable_free_model_pre_consume"` // 是否对免费模型启用预消耗

	// ShortMsgExtraBilling controls the short-message extra billing feature.
	// Phase 10A only supports the default-off "shadow" mode, which records
	// what *would* be charged into the consume log without altering the
	// actual settled quota. See ShortMsgExtraBillingConfig for details.
	ShortMsgExtraBilling ShortMsgExtraBillingConfig `json:"short_msg_extra_billing"`
}

// ShortMsgExtraBillingConfig is the persisted configuration for the
// short-message extra billing feature.
//
// Mode governs behavior:
//   - "off" (default): feature disabled; nothing is computed or logged.
//   - "shadow": rules are evaluated and the result is written to the consume
//     log's `other` map under the `short_msg_extra_billing` key. No actual
//     quota is charged.
//
// Any other value is treated as "off" (fail-closed).
type ShortMsgExtraBillingConfig struct {
	Mode  string                     `json:"mode"`
	Rules []ShortMsgExtraBillingRule `json:"rules"`
}

// ShortMsgExtraBillingRule defines a single short-message extra-billing rule.
//
// Phase 10A only supports the `input_tokens_below` trigger, which fires when
// summary.PromptTokens < Threshold (equality does NOT trigger).
type ShortMsgExtraBillingRule struct {
	// ID is a stable, admin-supplied identifier used in audit logs.
	ID string `json:"id"`
	// Model is matched exactly against relayInfo.OriginModelName.
	Model string `json:"model"`
	// Trigger selects the rule condition. Only "input_tokens_below" is
	// supported in Phase 10A.
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
	ShortMsgExtraBillingModeOff    = "off"
	ShortMsgExtraBillingModeShadow = "shadow"

	// ShortMsgExtraBillingTriggerInputTokensBelow is the only trigger
	// supported in Phase 10A.
	ShortMsgExtraBillingTriggerInputTokensBelow = "input_tokens_below"
)

// ShortMsgExtraBillingShadowResult is the structured outcome of evaluating
// short-message extra billing rules in shadow mode.
//
// It is a pure data record; the service layer decides whether to persist it
// into the consume log. The actual settled quota is never altered here.
type ShortMsgExtraBillingShadowResult struct {
	// Mode echoes the active mode ("shadow" when evaluated).
	Mode string
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
	// "mode_disabled", "no_rule_matched", "matched", "total_tokens_zero".
	Reason string
}

// HasReportableInfo returns true when the shadow evaluation produced
// information worth persisting into the consume log `other` map.
//
// A reportable result requires shadow mode and a matched rule, and skips the
// total_tokens_zero case (where the request itself is non-billable) and the
// non_text_mode case (where the request reached PostTextConsumeQuota via a
// non-text path and must not be audited by Phase 10A).
func (r ShortMsgExtraBillingShadowResult) HasReportableInfo() bool {
	return r.Mode == ShortMsgExtraBillingModeShadow &&
		r.MatchedRule != nil &&
		r.Reason != "total_tokens_zero"
}

// EvaluateShortMsgExtraBillingShadow deterministically evaluates the
// short-message extra billing rules against a single request.
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
//     task, mj_proxy, unknown OpenAI sub-modes, ...). Phase 10A only
//     audits text chat paths, so an empty textMode is always no-op:
//     the function returns a non-reportable result with Reason
//     "non_text_mode" regardless of whether any rule has an empty
//     ResponseModes list. This prevents PostTextConsumeQuota's image /
//     embedding / rerank / audio-fallback callers from being audited.
func EvaluateShortMsgExtraBillingShadow(
	cfg ShortMsgExtraBillingConfig,
	modelName string,
	promptTokens int,
	completionTokens int,
	totalTokens int,
	textMode string,
) ShortMsgExtraBillingShadowResult {
	result := ShortMsgExtraBillingShadowResult{Mode: cfg.Mode}

	if cfg.Mode != ShortMsgExtraBillingModeShadow {
		// Fail-closed: any non-shadow mode is treated as off.
		result.Mode = ShortMsgExtraBillingModeOff
		result.Reason = "mode_disabled"
		return result
	}

	// Phase 10A boundary: only audit text chat paths. An empty textMode
	// means the request reached PostTextConsumeQuota via a non-text path
	// (image, embedding, rerank, audio fallback, ...) or carries an
	// unknown relay format. Even when a rule has an empty ResponseModes
	// list, it must NOT match such paths; fail-closed to no report.
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
