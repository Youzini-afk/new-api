package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withShortMsgExtraBilling swaps the global quota setting's short-msg extra
// billing config for the duration of a test, restoring the previous value
// on cleanup. Works for both shadow and enforce modes.
func withShortMsgExtraBilling(t *testing.T, cfg operation_setting.ShortMsgExtraBillingConfig) {
	t.Helper()
	qs := operation_setting.GetQuotaSetting()
	orig := qs.ShortMsgExtraBilling
	qs.ShortMsgExtraBilling = cfg
	t.Cleanup(func() { qs.ShortMsgExtraBilling = orig })
}

// newShortMsgRelayInfo builds a non-stream text-mode RelayInfo suitable for
// short-msg enforce tests. Mirrors newShadowRelayInfo from the Phase 10A
// test file but lives here so enforce tests are self-contained.
func newShortMsgRelayInfo(t *testing.T, modelName string, format types.RelayFormat, relayMode int) *relaycommon.RelayInfo {
	t.Helper()
	return &relaycommon.RelayInfo{
		OriginModelName: modelName,
		RelayFormat:     format,
		RelayMode:       relayMode,
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}
}

// frozenPreflight mirrors what PrepareShortMsgExtraBillingPreConsume would
// store on relayInfo for an enforce-mode eligible request.
func frozenPreflight(ruleID, model string, threshold, fee int, waive bool) *relaycommon.ShortMsgExtraBillingPreflight {
	return &relaycommon.ShortMsgExtraBillingPreflight{
		Mode:                          operation_setting.ShortMsgExtraBillingModeEnforce,
		TextMode:                      "chat_completions",
		RuleID:                        ruleID,
		Model:                         model,
		Trigger:                       operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow,
		Threshold:                     threshold,
		FeeQuota:                      fee,
		WaiveWhenCompletionTokensZero: waive,
		PotentialExtraQuota:           fee,
		Reason:                        "matched",
	}
}

// applyEnforce wraps applyShortMsgExtraBillingEnforce with the boilerplate
// summary pointer / origin usage so test bodies stay focused on outcomes.
func applyEnforce(relayInfo *relaycommon.RelayInfo, summary *textQuotaSummary, originUsage *dto.Usage, tieredApplied bool) {
	applyShortMsgExtraBillingEnforce(relayInfo, summary, originUsage, tieredApplied)
}

func enforceRule(id, model string, threshold, fee int, waive bool) operation_setting.ShortMsgExtraBillingRule {
	return operation_setting.ShortMsgExtraBillingRule{
		ID:                            id,
		Model:                         model,
		Trigger:                       operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow,
		Threshold:                     threshold,
		FeeQuota:                      fee,
		WaiveWhenCompletionTokensZero: waive,
	}
}

// ---------------------------------------------------------------------------
// PrepareShortMsgExtraBillingPreConsume (preflight) — service-level
// ---------------------------------------------------------------------------

func TestPrepareShortMsgExtraBillingPreConsume_EnforceEligibleReserves(t *testing.T) {
	withShortMsgExtraBilling(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeEnforce,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			enforceRule("r1", "gpt-4o-mini", 100, 500, false),
		},
	})

	relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)

	potential := PrepareShortMsgExtraBillingPreConsume(relayInfo, 50)

	require.Equal(t, 500, potential)
	require.NotNil(t, relayInfo.ShortMsgExtraBillingPreflight)
	require.Equal(t, "r1", relayInfo.ShortMsgExtraBillingPreflight.RuleID)
	require.Equal(t, 500, relayInfo.ShortMsgExtraBillingPreflight.PotentialExtraQuota)
	require.True(t, relayInfo.ForcePreConsume, "preflight must disable trust bypass")
	require.True(t, relayInfo.RequireCheckedPreConsume, "preflight must request checked preconsume")
}

func TestPrepareShortMsgExtraBillingPreConsume_ModeOffNoReservation(t *testing.T) {
	withShortMsgExtraBilling(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeOff,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			enforceRule("r1", "gpt-4o-mini", 100, 500, false),
		},
	})

	relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)

	potential := PrepareShortMsgExtraBillingPreConsume(relayInfo, 50)

	require.Equal(t, 0, potential)
	require.Nil(t, relayInfo.ShortMsgExtraBillingPreflight)
	require.False(t, relayInfo.ForcePreConsume)
	require.False(t, relayInfo.RequireCheckedPreConsume)
}

func TestPrepareShortMsgExtraBillingPreConsume_ShadowModeNoReservation(t *testing.T) {
	// Phase 10A shadow mode must remain audit-only: no reservation, no
	// ForcePreConsume / RequireCheckedPreConsume toggles.
	withShortMsgExtraBilling(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeShadow,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			enforceRule("r1", "gpt-4o-mini", 100, 500, false),
		},
	})

	relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)

	potential := PrepareShortMsgExtraBillingPreConsume(relayInfo, 50)

	require.Equal(t, 0, potential)
	require.Nil(t, relayInfo.ShortMsgExtraBillingPreflight)
	require.False(t, relayInfo.ForcePreConsume)
	require.False(t, relayInfo.RequireCheckedPreConsume)
}

func TestPrepareShortMsgExtraBillingPreConsume_StreamingNoReservation(t *testing.T) {
	withShortMsgExtraBilling(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeEnforce,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			enforceRule("r1", "gpt-4o-mini", 100, 500, false),
		},
	})

	relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	relayInfo.IsStream = true

	potential := PrepareShortMsgExtraBillingPreConsume(relayInfo, 50)

	require.Equal(t, 0, potential)
	require.Nil(t, relayInfo.ShortMsgExtraBillingPreflight)
}

func TestPrepareShortMsgExtraBillingPreConsume_NonTextModeNoReservation(t *testing.T) {
	withShortMsgExtraBilling(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeEnforce,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			enforceRule("r1", "gpt-4o-mini", 100, 500, false),
		},
	})

	relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatEmbedding, relayconstant.RelayModeEmbeddings)

	potential := PrepareShortMsgExtraBillingPreConsume(relayInfo, 50)

	require.Equal(t, 0, potential)
	require.Nil(t, relayInfo.ShortMsgExtraBillingPreflight)
}

func TestPrepareShortMsgExtraBillingPreConsume_TieredExprNoReservation(t *testing.T) {
	withShortMsgExtraBilling(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeEnforce,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			enforceRule("r1", "gpt-4o-mini", 100, 500, false),
		},
	})

	relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	relayInfo.TieredBillingSnapshot = &billingexpr.BillingSnapshot{BillingMode: "tiered_expr"}

	potential := PrepareShortMsgExtraBillingPreConsume(relayInfo, 50)

	require.Equal(t, 0, potential)
	require.Nil(t, relayInfo.ShortMsgExtraBillingPreflight)
}

func TestPrepareShortMsgExtraBillingPreConsume_FreeModelNoReservation(t *testing.T) {
	withShortMsgExtraBilling(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeEnforce,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			enforceRule("r1", "gpt-4o-mini", 100, 500, false),
		},
	})

	relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	relayInfo.PriceData.FreeModel = true

	potential := PrepareShortMsgExtraBillingPreConsume(relayInfo, 50)

	require.Equal(t, 0, potential)
	require.Nil(t, relayInfo.ShortMsgExtraBillingPreflight)
}

func TestPrepareShortMsgExtraBillingPreConsume_ThresholdNotMetNoReservation(t *testing.T) {
	withShortMsgExtraBilling(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeEnforce,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			enforceRule("r1", "gpt-4o-mini", 100, 500, false),
		},
	})

	relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)

	// Estimated prompt tokens >= threshold => no reservation.
	potential := PrepareShortMsgExtraBillingPreConsume(relayInfo, 100)

	require.Equal(t, 0, potential)
	require.Nil(t, relayInfo.ShortMsgExtraBillingPreflight)
}

func TestPrepareShortMsgExtraBillingPreConsume_FirstMatchWins(t *testing.T) {
	withShortMsgExtraBilling(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeEnforce,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			enforceRule("first", "gpt-4o-mini", 100, 100, false),
			enforceRule("second", "gpt-4o-mini", 100, 200, false),
		},
	})

	relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)

	potential := PrepareShortMsgExtraBillingPreConsume(relayInfo, 50)

	require.Equal(t, 100, potential)
	require.Equal(t, "first", relayInfo.ShortMsgExtraBillingPreflight.RuleID)
}

func TestPrepareShortMsgExtraBillingPreConsume_ResponseModesRestrict(t *testing.T) {
	withShortMsgExtraBilling(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeEnforce,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			{
				ID:            "r1",
				Model:         "gpt-4o-mini",
				Trigger:       operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow,
				Threshold:     100,
				FeeQuota:      500,
				ResponseModes: []string{"responses"},
			},
		},
	})

	t.Run("matching mode reserves", func(t *testing.T) {
		relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAIResponses, relayconstant.RelayModeResponses)
		potential := PrepareShortMsgExtraBillingPreConsume(relayInfo, 50)
		require.Equal(t, 500, potential)
		require.Equal(t, "responses", relayInfo.ShortMsgExtraBillingPreflight.TextMode)
	})

	t.Run("non-matching mode does not reserve", func(t *testing.T) {
		relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
		potential := PrepareShortMsgExtraBillingPreConsume(relayInfo, 50)
		require.Equal(t, 0, potential)
		require.Nil(t, relayInfo.ShortMsgExtraBillingPreflight)
	})
}

// ---------------------------------------------------------------------------
// applyShortMsgExtraBillingEnforce (post-response) — service-level
// ---------------------------------------------------------------------------

func TestApplyShortMsgExtraBillingEnforce_AppliesWhenConditionsMet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, _ = gin.CreateTestContext(httptest.NewRecorder())

	withShortMsgExtraBilling(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeEnforce,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			enforceRule("r1", "gpt-4o-mini", 100, 500, false),
		},
	})

	relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	relayInfo.ShortMsgExtraBillingPreflight = frozenPreflight("r1", "gpt-4o-mini", 100, 500, false)

	usage := &dto.Usage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60}
	summary := textQuotaSummary{
		PromptTokens:     50,
		CompletionTokens: 10,
		TotalTokens:      60,
		ModelName:        "gpt-4o-mini",
		Quota:            60,
		ShortMsgExtraBilling: &operation_setting.ShortMsgExtraBillingResult{
			Mode:                operation_setting.ShortMsgExtraBillingModeEnforce,
			TextMode:            "chat_completions",
			MatchedRule:         &operation_setting.ShortMsgExtraBillingRule{ID: "r1", Model: "gpt-4o-mini", Trigger: operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
			Reason:              "matched",
			WouldApply:          true,
			CandidateExtraQuota: 500,
		},
	}

	applyEnforce(relayInfo, &summary, usage, false)

	// summary.Quota was bumped by the frozen fee.
	require.Equal(t, 560, summary.Quota)
	require.True(t, summary.ShortMsgExtraBilling.Enforced)
	require.Equal(t, 500, summary.ShortMsgExtraBilling.ChargedExtraQuota)
	require.Equal(t, 560, summary.ShortMsgExtraBilling.FinalQuota)
	require.Equal(t, "", summary.ShortMsgExtraBilling.EnforceSkippedReason)

	// Audit injection reflects the final split.
	other := map[string]interface{}{}
	injectShortMsgExtraBilling(other, summary)
	entry := other["short_msg_extra_billing"].(map[string]interface{})
	assert.Equal(t, true, entry["enforced"])
	assert.Equal(t, 500, entry["charged_extra_quota"])
	assert.Equal(t, 560, entry["final_quota"])
	assert.Equal(t, 60, entry["base_quota"])
	assert.Equal(t, "enforce", entry["mode"])
	assert.Equal(t, "r1", entry["rule_id"])
	assert.Equal(t, "chat_completions", entry["text_mode"])
}

func TestApplyShortMsgExtraBillingEnforce_CompletionZeroWaiveDoesNotCharge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, _ = gin.CreateTestContext(httptest.NewRecorder())

	withShortMsgExtraBilling(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeEnforce,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			enforceRule("r1", "gpt-4o-mini", 100, 500, true),
		},
	})

	relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	relayInfo.ShortMsgExtraBillingPreflight = frozenPreflight("r1", "gpt-4o-mini", 100, 500, true)

	usage := &dto.Usage{PromptTokens: 50, CompletionTokens: 0, TotalTokens: 50}
	summary := textQuotaSummary{
		PromptTokens:     50,
		CompletionTokens: 0,
		TotalTokens:      50,
		ModelName:        "gpt-4o-mini",
		Quota:            50,
		ShortMsgExtraBilling: &operation_setting.ShortMsgExtraBillingResult{
			Mode:                operation_setting.ShortMsgExtraBillingModeEnforce,
			TextMode:            "chat_completions",
			MatchedRule:         &operation_setting.ShortMsgExtraBillingRule{ID: "r1", Model: "gpt-4o-mini", Trigger: operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500, WaiveWhenCompletionTokensZero: true},
			Reason:              "matched",
			CandidateExtraQuota: 500,
			WouldApply:          false,
			Waived:              true,
			WaiveReason:         "completion_tokens_zero",
		},
	}

	applyEnforce(relayInfo, &summary, usage, false)

	// Quota stays at base; pre-reserved extra is returned via SettleBilling delta.
	require.Equal(t, 50, summary.Quota)
	require.False(t, summary.ShortMsgExtraBilling.Enforced)
	require.Equal(t, 0, summary.ShortMsgExtraBilling.ChargedExtraQuota)
	require.Equal(t, 50, summary.ShortMsgExtraBilling.FinalQuota)
	require.Equal(t, "completion_tokens_zero", summary.ShortMsgExtraBilling.EnforceSkippedReason)
	require.True(t, summary.ShortMsgExtraBilling.Waived)

	other := map[string]interface{}{}
	injectShortMsgExtraBilling(other, summary)
	entry := other["short_msg_extra_billing"].(map[string]interface{})
	assert.Equal(t, false, entry["enforced"])
	assert.Equal(t, 0, entry["charged_extra_quota"])
	assert.Equal(t, 50, entry["final_quota"])
	assert.Equal(t, "completion_tokens_zero", entry["enforce_skipped_reason"])
	assert.Equal(t, true, entry["waived"])
}

func TestApplyShortMsgExtraBillingEnforce_UsageNilDoesNotCharge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, _ = gin.CreateTestContext(httptest.NewRecorder())

	withShortMsgExtraBilling(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeEnforce,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			enforceRule("r1", "gpt-4o-mini", 100, 500, false),
		},
	})

	relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	relayInfo.ShortMsgExtraBillingPreflight = frozenPreflight("r1", "gpt-4o-mini", 100, 500, false)

	summary := textQuotaSummary{
		PromptTokens:     50,
		CompletionTokens: 10,
		TotalTokens:      60,
		ModelName:        "gpt-4o-mini",
		Quota:            60,
		ShortMsgExtraBilling: &operation_setting.ShortMsgExtraBillingResult{
			Mode:                operation_setting.ShortMsgExtraBillingModeEnforce,
			TextMode:            "chat_completions",
			MatchedRule:         &operation_setting.ShortMsgExtraBillingRule{ID: "r1", Model: "gpt-4o-mini", Trigger: operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
			CandidateExtraQuota: 500,
			WouldApply:          true,
		},
	}

	applyEnforce(relayInfo, &summary, nil, false)

	require.Equal(t, 60, summary.Quota)
	require.False(t, summary.ShortMsgExtraBilling.Enforced)
	require.Equal(t, "usage_unreliable", summary.ShortMsgExtraBilling.EnforceSkippedReason)
}

func TestApplyShortMsgExtraBillingEnforce_StreamingDoesNotCharge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, _ = gin.CreateTestContext(httptest.NewRecorder())

	withShortMsgExtraBilling(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeEnforce,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			enforceRule("r1", "gpt-4o-mini", 100, 500, false),
		},
	})

	relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	relayInfo.IsStream = true
	relayInfo.ShortMsgExtraBillingPreflight = frozenPreflight("r1", "gpt-4o-mini", 100, 500, false)

	usage := &dto.Usage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60}
	summary := textQuotaSummary{
		PromptTokens:     50,
		CompletionTokens: 10,
		TotalTokens:      60,
		ModelName:        "gpt-4o-mini",
		Quota:            60,
		ShortMsgExtraBilling: &operation_setting.ShortMsgExtraBillingResult{
			Mode:                operation_setting.ShortMsgExtraBillingModeEnforce,
			TextMode:            "chat_completions",
			MatchedRule:         &operation_setting.ShortMsgExtraBillingRule{ID: "r1", Model: "gpt-4o-mini", Trigger: operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
			CandidateExtraQuota: 500,
			WouldApply:          true,
		},
	}

	applyEnforce(relayInfo, &summary, usage, false)

	require.Equal(t, 60, summary.Quota)
	require.False(t, summary.ShortMsgExtraBilling.Enforced)
	require.Equal(t, "streaming", summary.ShortMsgExtraBilling.EnforceSkippedReason)
}

func TestApplyShortMsgExtraBillingEnforce_TieredExprDoesNotCharge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, _ = gin.CreateTestContext(httptest.NewRecorder())

	withShortMsgExtraBilling(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeEnforce,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			enforceRule("r1", "gpt-4o-mini", 100, 500, false),
		},
	})

	relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	relayInfo.TieredBillingSnapshot = &billingexpr.BillingSnapshot{BillingMode: "tiered_expr"}
	relayInfo.ShortMsgExtraBillingPreflight = frozenPreflight("r1", "gpt-4o-mini", 100, 500, false)

	usage := &dto.Usage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60}
	summary := textQuotaSummary{
		PromptTokens:     50,
		CompletionTokens: 10,
		TotalTokens:      60,
		ModelName:        "gpt-4o-mini",
		Quota:            60,
		ShortMsgExtraBilling: &operation_setting.ShortMsgExtraBillingResult{
			Mode:                operation_setting.ShortMsgExtraBillingModeEnforce,
			TextMode:            "chat_completions",
			MatchedRule:         &operation_setting.ShortMsgExtraBillingRule{ID: "r1", Model: "gpt-4o-mini", Trigger: operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
			CandidateExtraQuota: 500,
			WouldApply:          true,
		},
	}

	applyEnforce(relayInfo, &summary, usage, false)

	require.Equal(t, 60, summary.Quota)
	require.False(t, summary.ShortMsgExtraBilling.Enforced)
	require.Equal(t, "tiered_expr", summary.ShortMsgExtraBilling.EnforceSkippedReason)
}

func TestApplyShortMsgExtraBillingEnforce_NonTextModeAtPostDoesNotCharge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, _ = gin.CreateTestContext(httptest.NewRecorder())

	withShortMsgExtraBilling(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeEnforce,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			enforceRule("r1", "gpt-4o-mini", 100, 500, false),
		},
	})

	// Post-time textMode is "" (e.g. embedding path). Even though a preflight
	// exists, the non-text boundary must suppress the charge.
	relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatEmbedding, relayconstant.RelayModeEmbeddings)
	relayInfo.ShortMsgExtraBillingPreflight = frozenPreflight("r1", "gpt-4o-mini", 100, 500, false)

	usage := &dto.Usage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60}
	summary := textQuotaSummary{
		PromptTokens:     50,
		CompletionTokens: 10,
		TotalTokens:      60,
		ModelName:        "gpt-4o-mini",
		Quota:            60,
		ShortMsgExtraBilling: &operation_setting.ShortMsgExtraBillingResult{
			Mode:                operation_setting.ShortMsgExtraBillingModeEnforce,
			MatchedRule:         &operation_setting.ShortMsgExtraBillingRule{ID: "r1", Model: "gpt-4o-mini", Trigger: operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
			CandidateExtraQuota: 500,
			WouldApply:          true,
		},
	}

	applyEnforce(relayInfo, &summary, usage, false)

	require.Equal(t, 60, summary.Quota)
	require.False(t, summary.ShortMsgExtraBilling.Enforced)
	require.Equal(t, "non_text_mode", summary.ShortMsgExtraBilling.EnforceSkippedReason)
}

func TestApplyShortMsgExtraBillingEnforce_ThresholdNotMetDoesNotCharge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, _ = gin.CreateTestContext(httptest.NewRecorder())

	withShortMsgExtraBilling(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeEnforce,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			enforceRule("r1", "gpt-4o-mini", 100, 500, false),
		},
	})

	relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	relayInfo.ShortMsgExtraBillingPreflight = frozenPreflight("r1", "gpt-4o-mini", 100, 500, false)

	// Actual prompt tokens exceed the frozen threshold (estimate was conservative).
	usage := &dto.Usage{PromptTokens: 120, CompletionTokens: 10, TotalTokens: 130}
	summary := textQuotaSummary{
		PromptTokens:     120,
		CompletionTokens: 10,
		TotalTokens:      130,
		ModelName:        "gpt-4o-mini",
		Quota:            130,
		ShortMsgExtraBilling: &operation_setting.ShortMsgExtraBillingResult{
			Mode:                operation_setting.ShortMsgExtraBillingModeEnforce,
			TextMode:            "chat_completions",
			MatchedRule:         &operation_setting.ShortMsgExtraBillingRule{ID: "r1", Model: "gpt-4o-mini", Trigger: operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
			CandidateExtraQuota: 500,
			WouldApply:          false,
		},
	}

	applyEnforce(relayInfo, &summary, usage, false)

	require.Equal(t, 130, summary.Quota)
	require.False(t, summary.ShortMsgExtraBilling.Enforced)
	require.Equal(t, "threshold_not_met", summary.ShortMsgExtraBilling.EnforceSkippedReason)
}

func TestApplyShortMsgExtraBillingEnforce_TotalTokensZeroDoesNotCharge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, _ = gin.CreateTestContext(httptest.NewRecorder())

	withShortMsgExtraBilling(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeEnforce,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			enforceRule("r1", "gpt-4o-mini", 100, 500, false),
		},
	})

	relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	relayInfo.ShortMsgExtraBillingPreflight = frozenPreflight("r1", "gpt-4o-mini", 100, 500, false)

	usage := &dto.Usage{PromptTokens: 0, CompletionTokens: 0, TotalTokens: 0}
	summary := textQuotaSummary{
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
		ModelName:        "gpt-4o-mini",
		Quota:            0,
		ShortMsgExtraBilling: &operation_setting.ShortMsgExtraBillingResult{
			Mode:                operation_setting.ShortMsgExtraBillingModeEnforce,
			TextMode:            "chat_completions",
			MatchedRule:         &operation_setting.ShortMsgExtraBillingRule{ID: "r1", Model: "gpt-4o-mini", Trigger: operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
			CandidateExtraQuota: 0,
		},
	}

	applyEnforce(relayInfo, &summary, usage, false)

	require.Equal(t, 0, summary.Quota)
	require.False(t, summary.ShortMsgExtraBilling.Enforced)
	require.Equal(t, "total_tokens_zero", summary.ShortMsgExtraBilling.EnforceSkippedReason)
}

func TestApplyShortMsgExtraBillingEnforce_NoPreflightDropsResult(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, _ = gin.CreateTestContext(httptest.NewRecorder())

	withShortMsgExtraBilling(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeEnforce,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			enforceRule("r1", "gpt-4o-mini", 100, 500, false),
		},
	})

	relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	// No preflight (e.g. estimate was >= threshold at preflight). Even though
	// the evaluator returned a matched rule at post, the spec requires keeping
	// logs clean when no reservation was made.
	relayInfo.ShortMsgExtraBillingPreflight = nil

	usage := &dto.Usage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60}
	summary := textQuotaSummary{
		PromptTokens:     50,
		CompletionTokens: 10,
		TotalTokens:      60,
		ModelName:        "gpt-4o-mini",
		Quota:            60,
		ShortMsgExtraBilling: &operation_setting.ShortMsgExtraBillingResult{
			Mode:                operation_setting.ShortMsgExtraBillingModeEnforce,
			TextMode:            "chat_completions",
			MatchedRule:         &operation_setting.ShortMsgExtraBillingRule{ID: "r1", Model: "gpt-4o-mini", Trigger: operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
			CandidateExtraQuota: 500,
			WouldApply:          true,
		},
	}

	applyEnforce(relayInfo, &summary, usage, false)

	// Quota unchanged, result dropped (no audit entry).
	require.Equal(t, 60, summary.Quota)
	require.Nil(t, summary.ShortMsgExtraBilling)
}

func TestApplyShortMsgExtraBillingEnforce_ConfigChangedStillHonorsFrozenRule(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, _ = gin.CreateTestContext(httptest.NewRecorder())

	// At post-time, the config has a different rule ("r2", fee 200). But the
	// preflight froze "r1" with fee 500. The enforce decision must use the
	// frozen rule, not the current config.
	withShortMsgExtraBilling(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeEnforce,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			enforceRule("r2", "gpt-4o-mini", 100, 200, false),
		},
	})

	relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	relayInfo.ShortMsgExtraBillingPreflight = frozenPreflight("r1", "gpt-4o-mini", 100, 500, false)

	usage := &dto.Usage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60}
	summary := textQuotaSummary{
		PromptTokens:     50,
		CompletionTokens: 10,
		TotalTokens:      60,
		ModelName:        "gpt-4o-mini",
		Quota:            60,
		// Evaluator returned r2 (current config), but the frozen preflight is r1.
		ShortMsgExtraBilling: &operation_setting.ShortMsgExtraBillingResult{
			Mode:                operation_setting.ShortMsgExtraBillingModeEnforce,
			TextMode:            "chat_completions",
			MatchedRule:         &operation_setting.ShortMsgExtraBillingRule{ID: "r2", Model: "gpt-4o-mini", Trigger: operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 200},
			CandidateExtraQuota: 200,
			WouldApply:          true,
		},
	}

	applyEnforce(relayInfo, &summary, usage, false)

	// The frozen fee (500) is charged, not the current config fee (200).
	require.Equal(t, 560, summary.Quota)
	require.True(t, summary.ShortMsgExtraBilling.Enforced)
	require.Equal(t, 500, summary.ShortMsgExtraBilling.ChargedExtraQuota)
	require.Equal(t, "r1", summary.ShortMsgExtraBilling.MatchedRule.ID)
	require.Equal(t, 500, summary.ShortMsgExtraBilling.CandidateExtraQuota)
}

func TestApplyShortMsgExtraBillingEnforce_LiveConfigShadowStillHonorsFrozenPreflight(t *testing.T) {
	// Must-fix #1: even if the global config is flipped to shadow after the
	// preflight was frozen, the post logic must still apply the frozen enforce
	// rule. Shadow is real-time audit only and did not reserve, so it cannot
	// override a frozen enforce reservation.
	gin.SetMode(gin.TestMode)
	_, _ = gin.CreateTestContext(httptest.NewRecorder())

	withShortMsgExtraBilling(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeShadow,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			enforceRule("r1", "gpt-4o-mini", 100, 500, false),
		},
	})

	relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	relayInfo.ShortMsgExtraBillingPreflight = frozenPreflight("r1", "gpt-4o-mini", 100, 500, false)

	usage := &dto.Usage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60}
	summary := textQuotaSummary{
		PromptTokens:     50,
		CompletionTokens: 10,
		TotalTokens:      60,
		ModelName:        "gpt-4o-mini",
		Quota:            60,
		// Live evaluator returned a shadow result (current config is shadow).
		ShortMsgExtraBilling: &operation_setting.ShortMsgExtraBillingResult{
			Mode:                operation_setting.ShortMsgExtraBillingModeShadow,
			TextMode:            "chat_completions",
			MatchedRule:         &operation_setting.ShortMsgExtraBillingRule{ID: "r1", Model: "gpt-4o-mini", Trigger: operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
			CandidateExtraQuota: 500,
			WouldApply:          true,
		},
	}

	applyEnforce(relayInfo, &summary, usage, false)

	// Frozen enforce semantics win over the shadow live config: charge the
	// frozen fee, audit reflects the frozen rule.
	require.Equal(t, 560, summary.Quota)
	require.NotNil(t, summary.ShortMsgExtraBilling)
	require.True(t, summary.ShortMsgExtraBilling.Enforced)
	require.Equal(t, 500, summary.ShortMsgExtraBilling.ChargedExtraQuota)
	require.Equal(t, 560, summary.ShortMsgExtraBilling.FinalQuota)
	require.Equal(t, operation_setting.ShortMsgExtraBillingModeEnforce, summary.ShortMsgExtraBilling.Mode)
	require.Equal(t, "r1", summary.ShortMsgExtraBilling.MatchedRule.ID)
	require.Equal(t, 500, summary.ShortMsgExtraBilling.MatchedRule.FeeQuota)
	require.Equal(t, "", summary.ShortMsgExtraBilling.EnforceSkippedReason)
}

func TestApplyShortMsgExtraBillingEnforce_LiveConfigOffStillHonorsFrozenPreflight(t *testing.T) {
	// Must-fix #1: even if the global config is flipped to off after the
	// preflight was frozen, the post logic must still apply the frozen
	// enforce rule. The reservation was already made; turning the feature
	// off mid-request cannot retroactively waive the reserved extra.
	gin.SetMode(gin.TestMode)
	_, _ = gin.CreateTestContext(httptest.NewRecorder())

	withShortMsgExtraBilling(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeOff,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			enforceRule("r1", "gpt-4o-mini", 100, 500, false),
		},
	})

	relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	relayInfo.ShortMsgExtraBillingPreflight = frozenPreflight("r1", "gpt-4o-mini", 100, 500, false)

	usage := &dto.Usage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60}
	summary := textQuotaSummary{
		PromptTokens:     50,
		CompletionTokens: 10,
		TotalTokens:      60,
		ModelName:        "gpt-4o-mini",
		Quota:            60,
		// Live evaluator returned nil (config is off), so summary starts
		// with no audit entry. The frozen preflight must rebuild it.
		ShortMsgExtraBilling: nil,
	}

	applyEnforce(relayInfo, &summary, usage, false)

	require.Equal(t, 560, summary.Quota)
	require.NotNil(t, summary.ShortMsgExtraBilling)
	require.True(t, summary.ShortMsgExtraBilling.Enforced)
	require.Equal(t, 500, summary.ShortMsgExtraBilling.ChargedExtraQuota)
	require.Equal(t, "r1", summary.ShortMsgExtraBilling.MatchedRule.ID)
	require.Equal(t, 500, summary.ShortMsgExtraBilling.MatchedRule.FeeQuota)
}

func TestApplyShortMsgExtraBillingEnforce_LiveConfigSameIdDifferentFeeStillHonorsFrozen(t *testing.T) {
	// Must-fix #3: even when the live config has a rule with the SAME id as
	// the frozen preflight but a mutated fee / threshold / waive flag, the
	// enforce audit and charge must use the frozen fields. The live
	// evaluator result is discarded entirely.
	gin.SetMode(gin.TestMode)
	_, _ = gin.CreateTestContext(httptest.NewRecorder())

	withShortMsgExtraBilling(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeEnforce,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			{
				ID:                            "r1",
				Model:                         "gpt-4o-mini",
				Trigger:                       operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow,
				Threshold:                     200,  // mutated from frozen 100
				FeeQuota:                      200,  // mutated from frozen 500
				WaiveWhenCompletionTokensZero: true, // mutated from frozen false
			},
		},
	})

	relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	relayInfo.ShortMsgExtraBillingPreflight = frozenPreflight("r1", "gpt-4o-mini", 100, 500, false)

	usage := &dto.Usage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60}
	summary := textQuotaSummary{
		PromptTokens:     50,
		CompletionTokens: 10,
		TotalTokens:      60,
		ModelName:        "gpt-4o-mini",
		Quota:            60,
		// Live evaluator returned the mutated rule (fee 200, threshold 200,
		// waive=true). The frozen preflight must override it.
		ShortMsgExtraBilling: &operation_setting.ShortMsgExtraBillingResult{
			Mode:                operation_setting.ShortMsgExtraBillingModeEnforce,
			TextMode:            "chat_completions",
			MatchedRule:         &operation_setting.ShortMsgExtraBillingRule{ID: "r1", Model: "gpt-4o-mini", Trigger: operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 200, FeeQuota: 200, WaiveWhenCompletionTokensZero: true},
			CandidateExtraQuota: 200,
			WouldApply:          true,
		},
	}

	applyEnforce(relayInfo, &summary, usage, false)

	// Frozen fee (500) is charged, not the mutated live fee (200). Frozen
	// threshold (100) is used, not the mutated threshold (200) — and since
	// prompt=50 < frozen threshold=100, the rule fires. Frozen waive=false
	// means completion=10 does not trigger a waiver.
	require.Equal(t, 560, summary.Quota)
	require.True(t, summary.ShortMsgExtraBilling.Enforced)
	require.Equal(t, 500, summary.ShortMsgExtraBilling.ChargedExtraQuota)
	require.Equal(t, "r1", summary.ShortMsgExtraBilling.MatchedRule.ID)
	require.Equal(t, 100, summary.ShortMsgExtraBilling.MatchedRule.Threshold)
	require.Equal(t, 500, summary.ShortMsgExtraBilling.MatchedRule.FeeQuota)
	require.False(t, summary.ShortMsgExtraBilling.MatchedRule.WaiveWhenCompletionTokensZero)
	require.False(t, summary.ShortMsgExtraBilling.Waived)
}

func TestApplyShortMsgExtraBillingEnforce_TextModeMismatchDoesNotCharge(t *testing.T) {
	// Must-fix #2: when the post textMode does not match the frozen
	// TextMode, the enforce must skip with reason "text_mode_mismatch".
	// This simulates a relay format change between preflight and post
	// (should not happen in practice, but must fail-closed).
	gin.SetMode(gin.TestMode)
	_, _ = gin.CreateTestContext(httptest.NewRecorder())

	withShortMsgExtraBilling(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeEnforce,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			enforceRule("r1", "gpt-4o-mini", 100, 500, false),
		},
	})

	relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatClaude, relayconstant.RelayModeChatCompletions)
	// Frozen TextMode is "chat_completions"; post is "claude".
	relayInfo.ShortMsgExtraBillingPreflight = frozenPreflight("r1", "gpt-4o-mini", 100, 500, false)

	usage := &dto.Usage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60}
	summary := textQuotaSummary{
		PromptTokens:     50,
		CompletionTokens: 10,
		TotalTokens:      60,
		ModelName:        "gpt-4o-mini",
		Quota:            60,
		ShortMsgExtraBilling: &operation_setting.ShortMsgExtraBillingResult{
			Mode:                operation_setting.ShortMsgExtraBillingModeEnforce,
			TextMode:            "chat_completions",
			MatchedRule:         &operation_setting.ShortMsgExtraBillingRule{ID: "r1", Model: "gpt-4o-mini", Trigger: operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
			CandidateExtraQuota: 500,
			WouldApply:          true,
		},
	}

	applyEnforce(relayInfo, &summary, usage, false)

	// No charge; audit records the skip reason; the rebuilt MatchedRule
	// comes from the frozen preflight (not the live evaluator result).
	require.Equal(t, 60, summary.Quota)
	require.False(t, summary.ShortMsgExtraBilling.Enforced)
	require.Equal(t, 0, summary.ShortMsgExtraBilling.ChargedExtraQuota)
	require.Equal(t, "text_mode_mismatch", summary.ShortMsgExtraBilling.EnforceSkippedReason)
	require.Equal(t, "r1", summary.ShortMsgExtraBilling.MatchedRule.ID)

	other := map[string]interface{}{}
	injectShortMsgExtraBilling(other, summary)
	entry := other["short_msg_extra_billing"].(map[string]interface{})
	assert.Equal(t, "text_mode_mismatch", entry["enforce_skipped_reason"])
	assert.Equal(t, false, entry["enforced"])
	assert.Equal(t, 0, entry["charged_extra_quota"])
}

func TestApplyShortMsgExtraBillingEnforce_OriginUsageTotalTokensZeroButPromptCompletionNonZeroDoesNotCharge(t *testing.T) {
	// Must-fix #4: the reliable-usage boundary is originUsage.TotalTokens,
	// not summary.TotalTokens (which is prompt+completion). When upstream
	// reports TotalTokens==0 (e.g. malformed payload) even though prompt
	// and completion are non-zero, the enforce extra must not charge.
	gin.SetMode(gin.TestMode)
	_, _ = gin.CreateTestContext(httptest.NewRecorder())

	withShortMsgExtraBilling(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeEnforce,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			enforceRule("r1", "gpt-4o-mini", 100, 500, false),
		},
	})

	relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	relayInfo.ShortMsgExtraBillingPreflight = frozenPreflight("r1", "gpt-4o-mini", 100, 500, false)

	// Origin usage has TotalTokens==0 but non-zero prompt+completion.
	usage := &dto.Usage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 0}
	summary := textQuotaSummary{
		PromptTokens:     50,
		CompletionTokens: 10,
		// summary.TotalTokens is prompt+completion == 60, which under the
		// old check would NOT skip. Must-fix #4 requires using
		// originUsage.TotalTokens (0) instead, so the charge is suppressed.
		TotalTokens: 60,
		ModelName:   "gpt-4o-mini",
		Quota:       60,
		ShortMsgExtraBilling: &operation_setting.ShortMsgExtraBillingResult{
			Mode:                operation_setting.ShortMsgExtraBillingModeEnforce,
			TextMode:            "chat_completions",
			MatchedRule:         &operation_setting.ShortMsgExtraBillingRule{ID: "r1", Model: "gpt-4o-mini", Trigger: operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
			CandidateExtraQuota: 500,
			WouldApply:          true,
		},
	}

	applyEnforce(relayInfo, &summary, usage, false)

	require.Equal(t, 60, summary.Quota)
	require.False(t, summary.ShortMsgExtraBilling.Enforced)
	require.Equal(t, 0, summary.ShortMsgExtraBilling.ChargedExtraQuota)
	require.Equal(t, "total_tokens_zero", summary.ShortMsgExtraBilling.EnforceSkippedReason)
}

func TestApplyShortMsgExtraBillingEnforce_FrozenModeMismatchDoesNotCharge(t *testing.T) {
	// Must-fix #5: a frozen preflight whose Mode is not "enforce" must not
	// trigger the enforce path. This is a defensive case (the preflight
	// builder only sets Mode="enforce") — the function must fall through
	// to the non-frozen path and drop the evaluator result (no charge, no
	// audit entry) when the live config is enforce-without-reservation.
	gin.SetMode(gin.TestMode)
	_, _ = gin.CreateTestContext(httptest.NewRecorder())

	withShortMsgExtraBilling(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeEnforce,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			enforceRule("r1", "gpt-4o-mini", 100, 500, false),
		},
	})

	relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	relayInfo.ShortMsgExtraBillingPreflight = &relaycommon.ShortMsgExtraBillingPreflight{
		Mode:                "shadow", // not "enforce" — entry gate must reject
		TextMode:            "chat_completions",
		RuleID:              "r1",
		Model:               "gpt-4o-mini",
		Trigger:             operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow,
		Threshold:           100,
		FeeQuota:            500,
		PotentialExtraQuota: 500,
		Reason:              "matched",
	}

	usage := &dto.Usage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60}
	summary := textQuotaSummary{
		PromptTokens:     50,
		CompletionTokens: 10,
		TotalTokens:      60,
		ModelName:        "gpt-4o-mini",
		Quota:            60,
		ShortMsgExtraBilling: &operation_setting.ShortMsgExtraBillingResult{
			Mode:                operation_setting.ShortMsgExtraBillingModeEnforce,
			TextMode:            "chat_completions",
			MatchedRule:         &operation_setting.ShortMsgExtraBillingRule{ID: "r1", Model: "gpt-4o-mini", Trigger: operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
			CandidateExtraQuota: 500,
			WouldApply:          true,
		},
	}

	applyEnforce(relayInfo, &summary, usage, false)

	// Falls through to non-frozen path; live config is enforce without a
	// valid reservation, so the evaluator result is dropped (no charge, no
	// audit entry).
	require.Equal(t, 60, summary.Quota)
	require.Nil(t, summary.ShortMsgExtraBilling)
}

func TestApplyShortMsgExtraBillingEnforce_ModelMismatchDoesNotCharge(t *testing.T) {
	// Must-fix #5: when summary.ModelName != frozen.Model the enforce must
	// skip with reason "model_mismatch" and not charge. This catches a
	// model rewrite between preflight and post (should not happen in
	// practice, but must fail-closed).
	gin.SetMode(gin.TestMode)
	_, _ = gin.CreateTestContext(httptest.NewRecorder())

	withShortMsgExtraBilling(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeEnforce,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			enforceRule("r1", "gpt-4o-mini", 100, 500, false),
		},
	})

	relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	// Frozen Model is "gpt-4o-mini"; summary.ModelName is "gpt-4o" (mismatch).
	relayInfo.ShortMsgExtraBillingPreflight = frozenPreflight("r1", "gpt-4o-mini", 100, 500, false)

	usage := &dto.Usage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60}
	summary := textQuotaSummary{
		PromptTokens:     50,
		CompletionTokens: 10,
		TotalTokens:      60,
		ModelName:        "gpt-4o", // mismatch
		Quota:            60,
		ShortMsgExtraBilling: &operation_setting.ShortMsgExtraBillingResult{
			Mode:                operation_setting.ShortMsgExtraBillingModeEnforce,
			TextMode:            "chat_completions",
			MatchedRule:         &operation_setting.ShortMsgExtraBillingRule{ID: "r1", Model: "gpt-4o-mini", Trigger: operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
			CandidateExtraQuota: 500,
			WouldApply:          true,
		},
	}

	applyEnforce(relayInfo, &summary, usage, false)

	require.Equal(t, 60, summary.Quota)
	require.False(t, summary.ShortMsgExtraBilling.Enforced)
	require.Equal(t, 0, summary.ShortMsgExtraBilling.ChargedExtraQuota)
	require.Equal(t, "model_mismatch", summary.ShortMsgExtraBilling.EnforceSkippedReason)
}

func TestApplyShortMsgExtraBillingEnforce_TriggerMismatchDoesNotCharge(t *testing.T) {
	// Must-fix #5: when frozen.Trigger != "input_tokens_below" the enforce
	// must skip with reason "trigger_mismatch". The preflight builder only
	// sets Trigger="input_tokens_below", so this is a defensive case.
	gin.SetMode(gin.TestMode)
	_, _ = gin.CreateTestContext(httptest.NewRecorder())

	withShortMsgExtraBilling(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeEnforce,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			enforceRule("r1", "gpt-4o-mini", 100, 500, false),
		},
	})

	relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	relayInfo.ShortMsgExtraBillingPreflight = &relaycommon.ShortMsgExtraBillingPreflight{
		Mode:                          operation_setting.ShortMsgExtraBillingModeEnforce,
		TextMode:                      "chat_completions",
		RuleID:                        "r1",
		Model:                         "gpt-4o-mini",
		Trigger:                       "completion_tokens_above", // not supported
		Threshold:                     100,
		FeeQuota:                      500,
		WaiveWhenCompletionTokensZero: false,
		PotentialExtraQuota:           500,
		Reason:                        "matched",
	}

	usage := &dto.Usage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60}
	summary := textQuotaSummary{
		PromptTokens:     50,
		CompletionTokens: 10,
		TotalTokens:      60,
		ModelName:        "gpt-4o-mini",
		Quota:            60,
		ShortMsgExtraBilling: &operation_setting.ShortMsgExtraBillingResult{
			Mode:                operation_setting.ShortMsgExtraBillingModeEnforce,
			TextMode:            "chat_completions",
			MatchedRule:         &operation_setting.ShortMsgExtraBillingRule{ID: "r1", Model: "gpt-4o-mini", Trigger: operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
			CandidateExtraQuota: 500,
			WouldApply:          true,
		},
	}

	applyEnforce(relayInfo, &summary, usage, false)

	require.Equal(t, 60, summary.Quota)
	require.False(t, summary.ShortMsgExtraBilling.Enforced)
	require.Equal(t, 0, summary.ShortMsgExtraBilling.ChargedExtraQuota)
	require.Equal(t, "trigger_mismatch", summary.ShortMsgExtraBilling.EnforceSkippedReason)
	// The rebuilt MatchedRule carries the frozen trigger, not the live one.
	require.Equal(t, "completion_tokens_above", summary.ShortMsgExtraBilling.MatchedRule.Trigger)
}

// ---------------------------------------------------------------------------
// End-to-end: calculateTextQuotaSummary -> applyShortMsgExtraBillingEnforce
// ---------------------------------------------------------------------------

func TestEnforceEndToEnd_AppliesExtraCharge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	withShortMsgExtraBilling(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeEnforce,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			enforceRule("r1", "gpt-4o-mini", 100, 500, false),
		},
	})

	relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	relayInfo.ShortMsgExtraBillingPreflight = frozenPreflight("r1", "gpt-4o-mini", 100, 500, false)

	usage := &dto.Usage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60}
	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
	applyEnforce(relayInfo, &summary, usage, false)

	// 60 base + 500 extra = 560.
	require.Equal(t, 560, summary.Quota)
	require.NotNil(t, summary.ShortMsgExtraBilling)
	require.True(t, summary.ShortMsgExtraBilling.Enforced)
	require.Equal(t, 500, summary.ShortMsgExtraBilling.ChargedExtraQuota)

	other := map[string]interface{}{}
	injectShortMsgExtraBilling(other, summary)
	entry := other["short_msg_extra_billing"].(map[string]interface{})
	assert.Equal(t, 560, entry["final_quota"])
	assert.Equal(t, 60, entry["base_quota"])
	assert.Equal(t, 500, entry["charged_extra_quota"])
}

func TestShadowEndToEnd_UnchangedByEnforceCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	withShortMsgExtraBilling(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeShadow,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			enforceRule("r1", "gpt-4o-mini", 100, 500, false),
		},
	})

	relayInfo := newShortMsgRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	// Shadow never has a preflight.
	require.Nil(t, relayInfo.ShortMsgExtraBillingPreflight)

	usage := &dto.Usage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60}
	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
	applyEnforce(relayInfo, &summary, usage, false)

	// Shadow: quota unchanged, candidate recorded, no enforce fields set.
	require.Equal(t, 60, summary.Quota)
	require.NotNil(t, summary.ShortMsgExtraBilling)
	require.True(t, summary.ShortMsgExtraBilling.WouldApply)
	require.Equal(t, 500, summary.ShortMsgExtraBilling.CandidateExtraQuota)
	require.False(t, summary.ShortMsgExtraBilling.Enforced)
	require.Equal(t, 0, summary.ShortMsgExtraBilling.ChargedExtraQuota)
	require.Equal(t, "", summary.ShortMsgExtraBilling.EnforceSkippedReason)

	other := map[string]interface{}{}
	injectShortMsgExtraBilling(other, summary)
	entry := other["short_msg_extra_billing"].(map[string]interface{})
	assert.Equal(t, "shadow", entry["mode"])
	assert.Equal(t, false, entry["enforced"])
	assert.Equal(t, 0, entry["charged_extra_quota"])
	assert.Equal(t, 60, entry["final_quota"])
	assert.Equal(t, 60, entry["base_quota"])
	assert.Equal(t, 560, entry["would_final_quota"])
}
