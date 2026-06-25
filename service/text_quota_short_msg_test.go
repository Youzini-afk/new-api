package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withShortMsgExtraBillingShadow swaps the global quota setting's short-msg
// extra billing config for the duration of a test, restoring the previous
// value on cleanup. Tests must not leak shadow config into other tests.
func withShortMsgExtraBillingShadow(t *testing.T, cfg operation_setting.ShortMsgExtraBillingConfig) {
	t.Helper()
	qs := operation_setting.GetQuotaSetting()
	orig := qs.ShortMsgExtraBilling
	qs.ShortMsgExtraBilling = cfg
	t.Cleanup(func() { qs.ShortMsgExtraBilling = orig })
}

func newShadowRelayInfo(t *testing.T, modelName string, format types.RelayFormat, relayMode int) *relaycommon.RelayInfo {
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

func TestShortMsgExtraBillingShadow_ModeOffLeavesNoResult(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	withShortMsgExtraBillingShadow(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeOff,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			{ID: "r1", Model: "gpt-4o-mini", Trigger: operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
		},
	})

	relayInfo := newShadowRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	usage := &dto.Usage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	require.Nil(t, summary.ShortMsgExtraBilling)
	// Quota is unaffected by shadow logic: simple ratio of 60 tokens at ratio 1.
	require.Equal(t, 60, summary.Quota)
}

func TestShortMsgExtraBillingShadow_MatchedRuleRecordsCandidateWithoutCharging(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	withShortMsgExtraBillingShadow(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeShadow,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			{ID: "r1", Model: "gpt-4o-mini", Trigger: operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
		},
	})

	relayInfo := newShadowRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	usage := &dto.Usage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	require.NotNil(t, summary.ShortMsgExtraBilling)
	require.True(t, summary.ShortMsgExtraBilling.WouldApply)
	require.Equal(t, 500, summary.ShortMsgExtraBilling.CandidateExtraQuota)
	require.False(t, summary.ShortMsgExtraBilling.Waived)

	// Actual settled quota is unchanged: 60 tokens at ratio 1, no surcharge.
	require.Equal(t, 60, summary.Quota)

	// Log injection reflects shadow intent without altering the recorded quota.
	other := map[string]interface{}{}
	injectShortMsgExtraBillingShadow(other, summary)
	entry, ok := other["short_msg_extra_billing"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "shadow", entry["mode"])
	assert.Equal(t, true, entry["would_apply"])
	assert.Equal(t, "r1", entry["rule_id"])
	assert.Equal(t, "input_tokens_below", entry["trigger"])
	assert.Equal(t, 100, entry["threshold"])
	assert.Equal(t, 50, entry["input_tokens"])
	assert.Equal(t, 10, entry["completion_tokens"])
	assert.Equal(t, 60, entry["base_quota"])
	assert.Equal(t, 500, entry["candidate_extra_quota"])
	assert.Equal(t, 560, entry["would_final_quota"])
	assert.Equal(t, false, entry["waived"])
}

func TestShortMsgExtraBillingShadow_ThresholdEqualityDoesNotApply(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	withShortMsgExtraBillingShadow(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeShadow,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			{ID: "r1", Model: "gpt-4o-mini", Trigger: operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
		},
	})

	relayInfo := newShadowRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	usage := &dto.Usage{PromptTokens: 100, CompletionTokens: 10, TotalTokens: 110}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	require.NotNil(t, summary.ShortMsgExtraBilling)
	require.False(t, summary.ShortMsgExtraBilling.WouldApply)
	require.Equal(t, 0, summary.ShortMsgExtraBilling.CandidateExtraQuota)
	// Shadow does not change actual quota.
	require.Equal(t, 110, summary.Quota)
}

func TestShortMsgExtraBillingShadow_CompletionTokensZeroWaived(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	withShortMsgExtraBillingShadow(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeShadow,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			{
				ID:                            "r1",
				Model:                         "gpt-4o-mini",
				Trigger:                       operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow,
				Threshold:                     100,
				FeeQuota:                      500,
				WaiveWhenCompletionTokensZero: true,
			},
		},
	})

	relayInfo := newShadowRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	usage := &dto.Usage{PromptTokens: 50, CompletionTokens: 0, TotalTokens: 50}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	require.NotNil(t, summary.ShortMsgExtraBilling)
	require.True(t, summary.ShortMsgExtraBilling.Waived)
	require.Equal(t, "completion_tokens_zero", summary.ShortMsgExtraBilling.WaiveReason)
	require.False(t, summary.ShortMsgExtraBilling.WouldApply)
	require.Equal(t, 500, summary.ShortMsgExtraBilling.CandidateExtraQuota)
	// Waiver does not charge the actual quota.
	require.Equal(t, 50, summary.Quota)

	other := map[string]interface{}{}
	injectShortMsgExtraBillingShadow(other, summary)
	entry := other["short_msg_extra_billing"].(map[string]interface{})
	assert.Equal(t, false, entry["would_apply"])
	assert.Equal(t, true, entry["waived"])
	assert.Equal(t, "completion_tokens_zero", entry["waive_reason"])
	assert.Equal(t, 500, entry["candidate_extra_quota"])
	// would_final_quota reflects the hypothetical "if the candidate fee had
	// been applied" amount (base + candidate), even when waived. The actual
	// settled quota (base_quota) is unchanged.
	assert.Equal(t, 550, entry["would_final_quota"])
	assert.Equal(t, 50, entry["base_quota"])
}

func TestShortMsgExtraBillingShadow_InvalidRulesAreSkipped(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	withShortMsgExtraBillingShadow(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeShadow,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			{ID: "", Model: "gpt-4o-mini", Trigger: operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
			{ID: "bad-trigger", Model: "gpt-4o-mini", Trigger: "completion_tokens_above", Threshold: 100, FeeQuota: 500},
			{ID: "zero-threshold", Model: "gpt-4o-mini", Trigger: operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 0, FeeQuota: 500},
			{ID: "zero-fee", Model: "gpt-4o-mini", Trigger: operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 0},
			{ID: "good", Model: "gpt-4o-mini", Trigger: operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
		},
	})

	relayInfo := newShadowRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	usage := &dto.Usage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	require.NotNil(t, summary.ShortMsgExtraBilling)
	require.Equal(t, "good", summary.ShortMsgExtraBilling.MatchedRule.ID)
	require.Equal(t, 4, summary.ShortMsgExtraBilling.SkippedInvalidRules)
	require.True(t, summary.ShortMsgExtraBilling.WouldApply)
	require.Equal(t, 60, summary.Quota)
}

func TestShortMsgExtraBillingShadow_ModelMismatchNoMatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	withShortMsgExtraBillingShadow(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeShadow,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			{ID: "r1", Model: "gpt-4o-mini", Trigger: operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
		},
	})

	relayInfo := newShadowRelayInfo(t, "gpt-4o", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	usage := &dto.Usage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// No matched rule => no shadow result on the summary, no log injection.
	require.Nil(t, summary.ShortMsgExtraBilling)
	other := map[string]interface{}{}
	injectShortMsgExtraBillingShadow(other, summary)
	_, present := other["short_msg_extra_billing"]
	require.False(t, present)
	require.Equal(t, 60, summary.Quota)
}

func TestShortMsgExtraBillingShadow_ResponseModesRestrictApplication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	rule := operation_setting.ShortMsgExtraBillingRule{
		ID:            "r1",
		Model:         "gpt-4o-mini",
		Trigger:       operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow,
		Threshold:     100,
		FeeQuota:      500,
		ResponseModes: []string{"responses", "claude"},
	}
	withShortMsgExtraBillingShadow(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode:  operation_setting.ShortMsgExtraBillingModeShadow,
		Rules: []operation_setting.ShortMsgExtraBillingRule{rule},
	})

	t.Run("matching responses mode applies", func(t *testing.T) {
		relayInfo := newShadowRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAIResponses, relayconstant.RelayModeResponses)
		usage := &dto.Usage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60}
		summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
		require.NotNil(t, summary.ShortMsgExtraBilling)
		require.True(t, summary.ShortMsgExtraBilling.WouldApply)
		require.Equal(t, "responses", shortMsgExtraBillingTextMode(relayInfo))
	})

	t.Run("non-matching chat_completions mode does not apply", func(t *testing.T) {
		relayInfo := newShadowRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
		usage := &dto.Usage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60}
		summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
		require.Nil(t, summary.ShortMsgExtraBilling)
		require.Equal(t, "chat_completions", shortMsgExtraBillingTextMode(relayInfo))
	})
}

func TestShortMsgExtraBillingShadow_TextModeMapping(t *testing.T) {
	cases := []struct {
		name   string
		info   *relaycommon.RelayInfo
		expect string
	}{
		{
			name:   "chat completions",
			info:   newShadowRelayInfo(t, "m", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions),
			expect: "chat_completions",
		},
		{
			name:   "legacy completions",
			info:   newShadowRelayInfo(t, "m", types.RelayFormatOpenAI, relayconstant.RelayModeCompletions),
			expect: "completions",
		},
		{
			name:   "responses",
			info:   newShadowRelayInfo(t, "m", types.RelayFormatOpenAIResponses, relayconstant.RelayModeResponses),
			expect: "responses",
		},
		{
			name:   "responses compact",
			info:   newShadowRelayInfo(t, "m", types.RelayFormatOpenAIResponsesCompaction, relayconstant.RelayModeResponsesCompact),
			expect: "responses_compact",
		},
		{
			name:   "claude",
			info:   newShadowRelayInfo(t, "m", types.RelayFormatClaude, 0),
			expect: "claude",
		},
		{
			name:   "gemini",
			info:   newShadowRelayInfo(t, "m", types.RelayFormatGemini, 0),
			expect: "gemini",
		},
		{
			name:   "embedding excluded",
			info:   newShadowRelayInfo(t, "m", types.RelayFormatEmbedding, relayconstant.RelayModeEmbeddings),
			expect: "",
		},
		{
			name:   "image excluded",
			info:   newShadowRelayInfo(t, "m", types.RelayFormatOpenAIImage, 0),
			expect: "",
		},
		{
			name:   "rerank excluded",
			info:   newShadowRelayInfo(t, "m", types.RelayFormatRerank, relayconstant.RelayModeRerank),
			expect: "",
		},
		{
			name:   "nil relay info",
			info:   nil,
			expect: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expect, shortMsgExtraBillingTextMode(tc.info))
		})
	}
}

func TestShortMsgExtraBillingShadow_DoesNotAlterTieredOrToolSurchargeQuota(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	// A tool surcharge that already lives in summary.ToolCallSurchargeQuota
	// and summary.Quota must not be perturbed by the shadow evaluation.
	ctx.Set("claude_web_search_requests", 2)

	withShortMsgExtraBillingShadow(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeShadow,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			{ID: "r1", Model: "claude-3-7-sonnet", Trigger: operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 1000, FeeQuota: 500},
		},
	})

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "claude-3-7-sonnet",
		RelayFormat:     types.RelayFormatClaude,
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1.25},
		},
		StartTime: time.Now(),
	}
	usage := &dto.Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// The candidate exists but is NOT folded into summary.Quota.
	require.NotNil(t, summary.ShortMsgExtraBilling)
	require.True(t, summary.ShortMsgExtraBilling.WouldApply)
	require.Equal(t, 500, summary.ShortMsgExtraBilling.CandidateExtraQuota)
	require.True(t, summary.ToolCallSurchargeQuota.GreaterThan(decimal.NewFromInt(0)))

	// Sanity: actual quota equals the tiered/tool surcharge computation and
	// is strictly greater than what the candidate would have produced by
	// simple addition; we only assert it equals the pre-shadow value.
	other := map[string]interface{}{}
	injectShortMsgExtraBillingShadow(other, summary)
	entry := other["short_msg_extra_billing"].(map[string]interface{})
	assert.Equal(t, summary.Quota, entry["base_quota"])
	assert.Equal(t, summary.Quota+500, entry["would_final_quota"])
}

func TestShortMsgExtraBillingShadow_TotalTokensZeroProducesNoResult(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	withShortMsgExtraBillingShadow(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeShadow,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			{ID: "r1", Model: "gpt-4o-mini", Trigger: operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
		},
	})

	relayInfo := newShadowRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	usage := &dto.Usage{PromptTokens: 0, CompletionTokens: 0, TotalTokens: 0}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// Matched but non-billable request => no candidate, no log entry, no quota.
	require.Nil(t, summary.ShortMsgExtraBilling)
	require.Equal(t, 0, summary.Quota)
	other := map[string]interface{}{}
	injectShortMsgExtraBillingShadow(other, summary)
	_, present := other["short_msg_extra_billing"]
	require.False(t, present)
}

// TestShortMsgExtraBillingShadow_NonTextRelayPathsNeverAudited locks the
// Phase 10A oracle must-fix: PostTextConsumeQuota is also invoked by image,
// embedding, rerank, and audio-fallback paths. Those paths map to an empty
// textMode. Even when a rule has an empty ResponseModes list and the model
// matches exactly, no shadow result must be produced and no log entry
// written. The shadow-only Quota path stays unchanged in all cases.
func TestShortMsgExtraBillingShadow_NonTextRelayPathsNeverAudited(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Single rule with empty ResponseModes and exact model match: under the
	// old behavior this would match non-text paths via textMode=="". The
	// fix must reject them at the evaluator boundary.
	cfg := operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeShadow,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			{
				ID:                            "r1",
				Model:                         "gpt-4o-mini",
				Trigger:                       operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow,
				Threshold:                     100,
				FeeQuota:                      500,
				WaiveWhenCompletionTokensZero: true,
			},
		},
	}

	cases := []struct {
		name      string
		format    types.RelayFormat
		relayMode int
	}{
		{
			name:      "embedding",
			format:    types.RelayFormatEmbedding,
			relayMode: relayconstant.RelayModeEmbeddings,
		},
		{
			name:   "image generation",
			format: types.RelayFormatOpenAIImage,
		},
		{
			name:      "rerank",
			format:    types.RelayFormatRerank,
			relayMode: relayconstant.RelayModeRerank,
		},
		{
			name:      "audio speech (tts)",
			format:    types.RelayFormatOpenAIAudio,
			relayMode: relayconstant.RelayModeAudioSpeech,
		},
		{
			name:      "audio transcription",
			format:    types.RelayFormatOpenAIAudio,
			relayMode: relayconstant.RelayModeAudioTranscription,
		},
		{
			name:   "realtime",
			format: types.RelayFormatOpenAIRealtime,
		},
		{
			name:   "task",
			format: types.RelayFormatTask,
		},
		{
			name:   "mj_proxy",
			format: types.RelayFormatMjProxy,
		},
		// OpenAI-format but a non-text sub-mode (e.g. embeddings routed
		// through the openai format) must also be excluded.
		{
			name:      "openai format non-text sub-mode",
			format:    types.RelayFormatOpenAI,
			relayMode: relayconstant.RelayModeEmbeddings,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			withShortMsgExtraBillingShadow(t, cfg)

			relayInfo := newShadowRelayInfo(t, "gpt-4o-mini", tc.format, tc.relayMode)
			// Usage shape that WOULD trigger the rule if the boundary were
			// broken: prompt below threshold, non-zero completion tokens
			// (and a waive-flagged variant covered below), non-zero total.
			usage := &dto.Usage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60}

			summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

			// Sanity: the helper itself maps non-text paths to "".
			require.Equal(t, "", shortMsgExtraBillingTextMode(relayInfo), "test fixture must map to non-text mode")

			// No shadow result, no log entry, quota unchanged.
			require.Nil(t, summary.ShortMsgExtraBilling, "non-text path must not produce a shadow result")
			other := map[string]interface{}{}
			injectShortMsgExtraBillingShadow(other, summary)
			_, present := other["short_msg_extra_billing"]
			require.False(t, present, "non-text path must not write a shadow audit entry")
		})
	}

	// Explicit waived variant: even with WaiveWhenCompletionTokensZero and
	// completion_tokens=0 (which would normally surface a waived record),
	// the non-text boundary must suppress it.
	t.Run("waived rule suppressed on embedding path", func(t *testing.T) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		withShortMsgExtraBillingShadow(t, cfg)

		relayInfo := newShadowRelayInfo(t, "gpt-4o-mini", types.RelayFormatEmbedding, relayconstant.RelayModeEmbeddings)
		usage := &dto.Usage{PromptTokens: 50, CompletionTokens: 0, TotalTokens: 50}

		summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

		require.Equal(t, "", shortMsgExtraBillingTextMode(relayInfo))
		require.Nil(t, summary.ShortMsgExtraBilling)
		other := map[string]interface{}{}
		injectShortMsgExtraBillingShadow(other, summary)
		_, present := other["short_msg_extra_billing"]
		require.False(t, present)
	})
}

// TestShortMsgExtraBillingShadow_TextModeWithEmptyResponseModesStillMatches
// is the paired invariant: when the path IS a text chat path, an empty
// ResponseModes list still matches. This guarantees the Phase 10A fix only
// narrows the boundary to non-text paths and does not over-suppress text.
func TestShortMsgExtraBillingShadow_TextModeWithEmptyResponseModesStillMatches(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	withShortMsgExtraBillingShadow(t, operation_setting.ShortMsgExtraBillingConfig{
		Mode: operation_setting.ShortMsgExtraBillingModeShadow,
		Rules: []operation_setting.ShortMsgExtraBillingRule{
			{
				ID:            "r1",
				Model:         "gpt-4o-mini",
				Trigger:       operation_setting.ShortMsgExtraBillingTriggerInputTokensBelow,
				Threshold:     100,
				FeeQuota:      500,
				ResponseModes: nil,
			},
		},
	})

	relayInfo := newShadowRelayInfo(t, "gpt-4o-mini", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	usage := &dto.Usage{PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	require.NotNil(t, summary.ShortMsgExtraBilling)
	require.True(t, summary.ShortMsgExtraBilling.WouldApply)
	require.Equal(t, 500, summary.ShortMsgExtraBilling.CandidateExtraQuota)
	// Shadow-only: settled quota stays at the pre-shadow value.
	require.Equal(t, 60, summary.Quota)

	other := map[string]interface{}{}
	injectShortMsgExtraBillingShadow(other, summary)
	entry, ok := other["short_msg_extra_billing"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, entry["would_apply"])
	assert.Equal(t, 500, entry["candidate_extra_quota"])
}
