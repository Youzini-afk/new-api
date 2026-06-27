package service

import (
	"strings"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// PrepareShortMsgExtraBillingPreConsume runs the short-message extra billing
// enforce-mode preflight and returns the potential extra quota that must be
// reserved before the upstream call. It must be invoked after
// helper.ModelPriceHelper (so relayInfo.PriceData is populated) and before
// service.PreConsumeBilling in controller/relay.go.
//
// Returns 0 (and leaves relayInfo.ShortMsgExtraBillingPreflight == nil) when
// the request is not eligible for enforce-mode reservation. In that case the
// existing preconsume path is unchanged.
//
// When the returned potential extra is > 0, the caller MUST add it to
// priceData.QuotaToPreConsume (and re-store on relayInfo.PriceData) before
// calling PreConsumeBilling. The preflight snapshot is frozen on relayInfo so
// the post-response enforce decision uses preflight values, not the current
// config (which might change mid-request).
//
// Strict bounds — a non-zero potential is returned only when ALL of the
// following hold:
//  1. config mode == enforce
//  2. !relayInfo.IsStream
//  3. shortMsgExtraBillingTextMode(relayInfo) != "" (text chat path only)
//  4. relayInfo.TieredBillingSnapshot == nil (exclude tiered_expr)
//  5. !relayInfo.PriceData.FreeModel
//  6. a valid rule matches group + textMode (empty ResponseModes = any text
//     mode) AND estimatedPromptTokens < rule.Threshold (first match wins)
//
// The freeze is conservative: estimated < threshold at preflight time is
// enough to reserve. The actual charge decision is made post-response using
// the actual summary.PromptTokens against the FROZEN threshold.
func PrepareShortMsgExtraBillingPreConsume(relayInfo *relaycommon.RelayInfo, estimatedPromptTokens int) int {
	if relayInfo == nil {
		return 0
	}
	cfg := operation_setting.GetQuotaSetting().ShortMsgExtraBilling
	if cfg.Mode != operation_setting.ShortMsgExtraBillingModeEnforce {
		// Off and shadow modes never reserve. Shadow stays audit-only
		// (Phase 10A behavior unchanged).
		relayInfo.ShortMsgExtraBillingPreflight = nil
		return 0
	}

	// Boundary 2: streaming responses cannot safely apply post-response
	// enforce (usage may arrive late / unreliably).
	if relayInfo.IsStream {
		relayInfo.ShortMsgExtraBillingPreflight = nil
		return 0
	}

	// Boundary 3: only audit/charge text chat paths.
	textMode := shortMsgExtraBillingTextMode(relayInfo)
	if textMode == "" {
		relayInfo.ShortMsgExtraBillingPreflight = nil
		return 0
	}

	// Boundary 4: tiered_expr has its own settle path; do not stack the
	// short-message surcharge on top of it.
	if relayInfo.TieredBillingSnapshot != nil {
		relayInfo.ShortMsgExtraBillingPreflight = nil
		return 0
	}

	// Boundary 5: free models skip preconsume entirely; reserving an extra
	// would create a positive preconsume for a "free" request.
	if relayInfo.PriceData.FreeModel {
		relayInfo.ShortMsgExtraBillingPreflight = nil
		return 0
	}

	// Boundary 6: first valid matching rule whose group + response modes
	// match. Then check the trigger using the *estimated* prompt tokens
	// (conservative reservation; the post-response decision uses the actual
	// prompt tokens against the FROZEN threshold).
	rule := firstValidMatchingShortMsgRule(cfg, resolveShortMsgBillingGroup(relayInfo), textMode)
	if rule == nil {
		relayInfo.ShortMsgExtraBillingPreflight = nil
		return 0
	}
	if rule.Trigger != operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow {
		relayInfo.ShortMsgExtraBillingPreflight = nil
		return 0
	}
	if estimatedPromptTokens >= rule.Threshold {
		relayInfo.ShortMsgExtraBillingPreflight = nil
		return 0
	}

	// Reserve the full fee; post-response enforce may waive (e.g. completion
	// tokens zero) and refund the difference via SettleBilling's negative delta.
	relayInfo.ShortMsgExtraBillingPreflight = &relaycommon.ShortMsgExtraBillingPreflight{
		Mode:                          operation_setting.ShortMsgExtraBillingModeEnforce,
		TextMode:                      textMode,
		RuleID:                        rule.ID,
		Group:                         rule.Group,
		Model:                         relayInfo.OriginModelName,
		Trigger:                       rule.Trigger,
		Threshold:                     rule.Threshold,
		FeeQuota:                      rule.FeeQuota,
		WaiveWhenCompletionTokensZero: rule.WaiveWhenCompletionTokensZero,
		PotentialExtraQuota:           rule.FeeQuota,
		Reason:                        "matched",
	}
	// ForcePreConsume disables the trust bypass so the potential extra is
	// actually pre-deducted (no negative-quota race).
	relayInfo.ForcePreConsume = true
	// RequireCheckedPreConsume routes the wallet preconsume through the
	// atomic DecreaseUserQuotaIfEnough path (no BatchUpdate, immediate).
	relayInfo.RequireCheckedPreConsume = true
	return rule.FeeQuota
}

// firstValidMatchingShortMsgRule returns the first valid rule whose Group and
// ResponseModes match, or nil when none match. Mirrors the evaluator's
// rule-matching logic but returns the rule directly (no candidate/waive
// evaluation — the post-response evaluator handles that).
func firstValidMatchingShortMsgRule(cfg operation_setting.ShortMsgExtraBillingConfig, groupName, textMode string) *operation_setting.ShortMsgExtraBillingRule {
	groupName = strings.TrimSpace(groupName)
	for i := range cfg.Rules {
		rule := cfg.Rules[i]
		// Inline rule validation: non-empty ID/Group, supported trigger,
		// positive threshold and fee. Invalid rules are silently skipped.
		ruleGroup := strings.TrimSpace(rule.Group)
		if ruleGroup == "" {
			ruleGroup = strings.TrimSpace(rule.Model)
		}
		if rule.ID == "" || ruleGroup == "" {
			continue
		}
		if rule.Trigger != operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow {
			continue
		}
		if rule.Threshold <= 0 || rule.FeeQuota <= 0 {
			continue
		}
		if ruleGroup != groupName {
			continue
		}
		if len(rule.ResponseModes) > 0 && !containsShortMsgResponseMode(rule.ResponseModes, textMode) {
			continue
		}
		matched := rule
		matched.Group = ruleGroup
		matched.Model = ""
		return &matched
	}
	return nil
}

func resolveShortMsgBillingGroup(relayInfo *relaycommon.RelayInfo) string {
	if relayInfo == nil {
		return ""
	}
	if strings.TrimSpace(relayInfo.TokenGroup) != "" {
		return strings.TrimSpace(relayInfo.TokenGroup)
	}
	if strings.TrimSpace(relayInfo.UsingGroup) != "" {
		return strings.TrimSpace(relayInfo.UsingGroup)
	}
	if strings.TrimSpace(relayInfo.UserGroup) != "" {
		return strings.TrimSpace(relayInfo.UserGroup)
	}
	return strings.TrimSpace(relayInfo.OriginModelName)
}

func containsShortMsgResponseMode(list []string, target string) bool {
	for _, s := range list {
		if s == target {
			return true
		}
	}
	return false
}
