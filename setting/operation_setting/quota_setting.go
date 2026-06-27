package operation_setting

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

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
	ID                            string   `json:"id"`
	Group                         string   `json:"group"`
	Model                         string   `json:"model,omitempty"`
	Trigger                       string   `json:"trigger"`
	Threshold                     int      `json:"threshold"`
	FeeQuota                      int      `json:"fee_quota"`
	WaiveWhenCompletionTokensZero bool     `json:"waive_when_completion_tokens_zero"`
	ResponseModes                 []string `json:"response_modes,omitempty"`
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
	groupName string,
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

	groupName = strings.TrimSpace(groupName)

	// Find the first valid rule that matches the request's group and
	// (optionally) response mode. Only the first match is used to avoid
	// stacking multiple surcharges.
	for i := range cfg.Rules {
		rule := cfg.Rules[i]
		if !isShortMsgExtraBillingRuleValid(rule) {
			result.SkippedInvalidRules++
			continue
		}
		ruleGroup := shortMsgExtraBillingRuleGroup(rule)
		if ruleGroup != groupName {
			continue
		}
		if len(rule.ResponseModes) > 0 && !containsString(rule.ResponseModes, textMode) {
			continue
		}
		// Matched. Use a stable copy so later config mutations can't alias.
		matched := rule
		matched.Group = ruleGroup
		matched.Model = ""
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
	groupName string,
	promptTokens int,
	completionTokens int,
	totalTokens int,
	textMode string,
) ShortMsgExtraBillingResult {
	return EvaluateShortMsgExtraBilling(cfg, groupName, promptTokens, completionTokens, totalTokens, textMode)
}

// isShortMsgExtraBillingRuleValid reports whether a rule has all required
// fields set to a meaningful value. Invalid rules are skipped silently.
func isShortMsgExtraBillingRuleValid(rule ShortMsgExtraBillingRule) bool {
	if rule.ID == "" || shortMsgExtraBillingRuleGroup(rule) == "" {
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

func shortMsgExtraBillingRuleGroup(rule ShortMsgExtraBillingRule) string {
	if strings.TrimSpace(rule.Group) != "" {
		return strings.TrimSpace(rule.Group)
	}
	return strings.TrimSpace(rule.Model)
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

// ShortMsgExtraBillingOptionKey is the option-store key under which the
// short-message extra billing config is persisted (registered as the
// `quota_setting` config module with the `short_msg_extra_billing` leaf).
const ShortMsgExtraBillingOptionKey = "quota_setting.short_msg_extra_billing"

// shortMsgExtraBillingAllowedResponseModes lists the stable internal text
// modes that a rule's ResponseModes may restrict to. Unknown values are
// rejected at validation time to prevent typos silently widening or
// narrowing a rule's scope. The set mirrors shortMsgExtraBillingTextMode in
// the service layer.
var shortMsgExtraBillingAllowedResponseModes = map[string]struct{}{
	"chat_completions":  {},
	"completions":       {},
	"responses":         {},
	"responses_compact": {},
	"claude":            {},
	"gemini":            {},
}

// ParseAndValidateShortMsgExtraBillingConfig parses, normalizes and validates
// a raw JSON config string for the ShortMsgExtraBillingConfig. On success it
// returns the normalized config and a stable JSON serialization suitable for
// persistence. On error it returns the zero config, an empty string, and the
// error.
//
// Validation mirrors the Phase 10B evaluator/preflight behavior so that the
// frontend can never be the only line of defense:
//
//   - mode must be "off", "shadow", or "enforce"; an empty mode is
//     normalized to "off" (the fail-closed default).
//   - Any mode may carry an empty rules list (no rule matches => no-op).
//   - When rules are present, each rule must satisfy:
//   - id non-empty (trimmed), model non-empty (trimmed),
//   - trigger == "input_tokens_below" (the only supported trigger),
//   - threshold > 0,
//   - fee_quota > 0.
//   - response_modes (when present) may only contain values from
//     shortMsgExtraBillingAllowedResponseModes. Entries are trimmed and
//     de-duplicated while preserving first-seen order. Unknown values are
//     rejected.
//   - duplicate rule ids are rejected to avoid audit ambiguity.
//   - negative threshold / fee_quota are rejected (the > 0 check covers this).
//
// Empty/whitespace-only input is normalized to the disabled default. The
// persisted format is produced via common.Marshal so field order is stable;
// empty rules are normalized to nil so the output matches the default config
// shape. The function never mutates the input raw string.
//
// All JSON (un)marshal calls go through the common wrapper per the project
// JSON rule (no direct encoding/json in business code).
func ParseAndValidateShortMsgExtraBillingConfig(raw string) (ShortMsgExtraBillingConfig, string, error) {
	var cfg ShortMsgExtraBillingConfig
	trimmedRaw := strings.TrimSpace(raw)
	if trimmedRaw == "" {
		// Empty input is the disabled default. Reuse the same shape as the
		// package-level quotaSetting initializer so normalized output is
		// byte-stable for the same logical config.
		cfg = ShortMsgExtraBillingConfig{Mode: ShortMsgExtraBillingModeOff, Rules: nil}
	} else {
		if err := common.Unmarshal([]byte(trimmedRaw), &cfg); err != nil {
			return ShortMsgExtraBillingConfig{}, "", fmt.Errorf("short_msg_extra_billing: 无效 JSON: %w", err)
		}
	}

	// Normalize mode: empty => off.
	cfg.Mode = strings.TrimSpace(cfg.Mode)
	if cfg.Mode == "" {
		cfg.Mode = ShortMsgExtraBillingModeOff
	}

	switch cfg.Mode {
	case ShortMsgExtraBillingModeOff, ShortMsgExtraBillingModeShadow, ShortMsgExtraBillingModeEnforce:
		// valid mode
	default:
		return ShortMsgExtraBillingConfig{}, "", fmt.Errorf("short_msg_extra_billing: mode 必须是 off/shadow/enforce, 实际为 %q", cfg.Mode)
	}

	// Normalize rules. Validation applies regardless of mode: an invalid rule
	// saved under "off" today would silently activate when an admin flips the
	// mode to "shadow" tomorrow. Empty rules are normalized to nil so the
	// persisted JSON shape matches the default config.
	if len(cfg.Rules) > 0 {
		normalized := make([]ShortMsgExtraBillingRule, 0, len(cfg.Rules))
		seenIDs := make(map[string]struct{}, len(cfg.Rules))
		for i := range cfg.Rules {
			rule := cfg.Rules[i]
			rule.ID = strings.TrimSpace(rule.ID)
			rule.Group = strings.TrimSpace(rule.Group)
			rule.Model = strings.TrimSpace(rule.Model)
			if rule.Group == "" && rule.Model != "" {
				rule.Group = rule.Model
			}
			rule.Model = ""
			rule.Trigger = strings.TrimSpace(rule.Trigger)
			modes, modeErr := normalizeShortMsgExtraBillingResponseModes(rule.ResponseModes)
			if modeErr != nil {
				return ShortMsgExtraBillingConfig{}, "", fmt.Errorf("short_msg_extra_billing: rule[%d] (%q) response_modes 无效: %w", i, rule.ID, modeErr)
			}
			rule.ResponseModes = modes

			if err := validateShortMsgExtraBillingRule(rule, i); err != nil {
				return ShortMsgExtraBillingConfig{}, "", err
			}
			if _, dup := seenIDs[rule.ID]; dup {
				return ShortMsgExtraBillingConfig{}, "", fmt.Errorf("short_msg_extra_billing: 重复 rule id %q", rule.ID)
			}
			seenIDs[rule.ID] = struct{}{}
			normalized = append(normalized, rule)
		}
		cfg.Rules = normalized
	} else {
		cfg.Rules = nil
	}

	normalized, err := common.Marshal(cfg)
	if err != nil {
		return ShortMsgExtraBillingConfig{}, "", fmt.Errorf("short_msg_extra_billing: 序列化失败: %w", err)
	}
	return cfg, string(normalized), nil
}

// validateShortMsgExtraBillingRule validates a single normalized rule. It
// returns an error covering empty id/model, unsupported trigger, and
// non-positive threshold/fee_quota. The caller is expected to have already
// trimmed whitespace and normalized response_modes.
func validateShortMsgExtraBillingRule(rule ShortMsgExtraBillingRule, idx int) error {
	if rule.ID == "" {
		return fmt.Errorf("short_msg_extra_billing: rule[%d] id 不能为空", idx)
	}
	if rule.Group == "" {
		return fmt.Errorf("short_msg_extra_billing: rule[%d] (%q) group 不能为空", idx, rule.ID)
	}
	if rule.Trigger != ShortMsgExtraBillingTriggerInputTokensBelow {
		return fmt.Errorf("short_msg_extra_billing: rule[%d] (%q) trigger 必须是 %q, 实际为 %q",
			idx, rule.ID, ShortMsgExtraBillingTriggerInputTokensBelow, rule.Trigger)
	}
	if rule.Threshold <= 0 {
		return fmt.Errorf("short_msg_extra_billing: rule[%d] (%q) threshold 必须 > 0, 实际为 %d",
			idx, rule.ID, rule.Threshold)
	}
	if rule.FeeQuota <= 0 {
		return fmt.Errorf("short_msg_extra_billing: rule[%d] (%q) fee_quota 必须 > 0, 实际为 %d",
			idx, rule.ID, rule.FeeQuota)
	}
	return nil
}

// normalizeShortMsgExtraBillingResponseModes trims, validates and de-duplicates
// the response_modes list. Entries are trimmed of surrounding whitespace; the
// first-seen occurrence wins for de-duplication so the original order is
// preserved. Unknown or blank values produce an error.
//
// A nil input means the field was omitted/null and intentionally applies to any
// text mode. An explicit empty array is rejected so API/UI callers cannot
// accidentally represent "all text modes" as "no selected modes".
func normalizeShortMsgExtraBillingResponseModes(modes []string) ([]string, error) {
	if modes == nil {
		return nil, nil
	}
	if len(modes) == 0 {
		return nil, fmt.Errorf("response_modes 不能为空；如需匹配全部文本模式，请省略该字段或使用 null")
	}
	out := make([]string, 0, len(modes))
	seen := make(map[string]struct{}, len(modes))
	for _, m := range modes {
		trimmed := strings.TrimSpace(m)
		if trimmed == "" {
			return nil, fmt.Errorf("response_modes 不能包含空白值")
		}
		if _, ok := shortMsgExtraBillingAllowedResponseModes[trimmed]; !ok {
			return nil, fmt.Errorf("未知值 %q (允许: chat_completions, completions, responses, responses_compact, claude, gemini)", trimmed)
		}
		if _, dup := seen[trimmed]; dup {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out, nil
}

// NormalizeShortMsgExtraBillingOption validates and normalizes the persisted
// value for the ShortMsgExtraBillingOptionKey option. For any other key it
// returns (value, nil) untouched so callers can route every option through
// this helper without an extra branch. For the owned key it parses, validates
// and re-marshals the value to a stable JSON string suitable for persistence.
//
// This is the canonical entry point used by both controller.UpdateOption and
// model.UpdateOption / model.UpdateOptionsBulk so that internal callers cannot
// bypass the controller and persist an invalid config: any validation failure
// surfaces as a Go error, and successful callers should substitute the
// returned normalized string for the original before persisting.
func NormalizeShortMsgExtraBillingOption(key, value string) (string, error) {
	if key != ShortMsgExtraBillingOptionKey {
		return value, nil
	}
	_, normalized, err := ParseAndValidateShortMsgExtraBillingConfig(value)
	if err != nil {
		return "", err
	}
	return normalized, nil
}
