package operation_setting

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvaluateShortMsgExtraBillingShadow_ModeOffNeverApplies(t *testing.T) {
	cfg := ShortMsgExtraBillingConfig{
		Mode: ShortMsgExtraBillingModeOff,
		Rules: []ShortMsgExtraBillingRule{
			{
				ID:        "r1",
				Model:     "gpt-4o-mini",
				Trigger:   ShortMsgExtraBillingTriggerInputTokensBelow,
				Threshold: 100,
				FeeQuota:  500,
			},
		},
	}

	res := EvaluateShortMsgExtraBillingShadow(cfg, "gpt-4o-mini", 10, 5, 15, "chat_completions")

	require.Equal(t, ShortMsgExtraBillingModeOff, res.Mode)
	require.Nil(t, res.MatchedRule)
	require.False(t, res.WouldApply)
	require.Equal(t, 0, res.CandidateExtraQuota)
	require.False(t, res.HasReportableInfo())
}

func TestEvaluateShortMsgExtraBillingShadow_UnknownModeFailsClosed(t *testing.T) {
	// Phase 10B recognizes "enforce" as a valid mode; use a genuinely
	// unknown value here so the fail-closed branch is still exercised.
	cfg := ShortMsgExtraBillingConfig{
		Mode: "bogus", // unsupported => fail closed as off
		Rules: []ShortMsgExtraBillingRule{
			{ID: "r1", Model: "gpt-4o-mini", Trigger: ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
		},
	}

	res := EvaluateShortMsgExtraBillingShadow(cfg, "gpt-4o-mini", 10, 5, 15, "chat_completions")

	require.Equal(t, ShortMsgExtraBillingModeOff, res.Mode)
	require.False(t, res.WouldApply)
	require.False(t, res.HasReportableInfo())
}

// TestEvaluateShortMsgExtraBilling_EnforceModeRecognized locks Phase 10B:
// "enforce" is now a first-class mode (no longer fail-closed to off). A
// matching rule under enforce produces a reportable candidate with
// Mode="enforce" and the same candidate-decision fields as shadow.
func TestEvaluateShortMsgExtraBilling_EnforceModeRecognized(t *testing.T) {
	cfg := ShortMsgExtraBillingConfig{
		Mode: ShortMsgExtraBillingModeEnforce,
		Rules: []ShortMsgExtraBillingRule{
			{ID: "r1", Model: "gpt-4o-mini", Trigger: ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
		},
	}

	res := EvaluateShortMsgExtraBilling(cfg, "gpt-4o-mini", 50, 10, 60, "chat_completions")

	require.Equal(t, ShortMsgExtraBillingModeEnforce, res.Mode)
	require.Equal(t, "chat_completions", res.TextMode)
	require.NotNil(t, res.MatchedRule)
	require.Equal(t, "r1", res.MatchedRule.ID)
	require.True(t, res.WouldApply)
	require.Equal(t, 500, res.CandidateExtraQuota)
	require.False(t, res.Waived)
	require.True(t, res.HasReportableInfo())

	// Enforce-mode specific audit fields are left blank by the evaluator;
	// they are populated by the service layer after the post-response apply
	// decision.
	require.False(t, res.Enforced)
	require.Equal(t, 0, res.ChargedExtraQuota)
	require.Equal(t, 0, res.FinalQuota)
	require.Equal(t, "", res.EnforceSkippedReason)
}

// TestEvaluateShortMsgExtraBilling_EnforceModeEmptyTextModeNoOp ensures
// enforce mode still respects the non-text boundary: an empty textMode
// (image / embedding / rerank / audio / ...) is fail-closed no-op even when
// the rule has an empty ResponseModes list.
func TestEvaluateShortMsgExtraBilling_EnforceModeEmptyTextModeNoOp(t *testing.T) {
	cfg := ShortMsgExtraBillingConfig{
		Mode: ShortMsgExtraBillingModeEnforce,
		Rules: []ShortMsgExtraBillingRule{
			{ID: "r1", Model: "gpt-4o-mini", Trigger: ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
		},
	}

	res := EvaluateShortMsgExtraBilling(cfg, "gpt-4o-mini", 50, 10, 60, "")

	require.Nil(t, res.MatchedRule)
	require.Equal(t, "non_text_mode", res.Reason)
	require.False(t, res.HasReportableInfo())
}

func TestEvaluateShortMsgExtraBillingShadow_ExactModelBelowThresholdApplies(t *testing.T) {
	cfg := ShortMsgExtraBillingConfig{
		Mode: ShortMsgExtraBillingModeShadow,
		Rules: []ShortMsgExtraBillingRule{
			{ID: "r1", Model: "gpt-4o-mini", Trigger: ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
		},
	}

	res := EvaluateShortMsgExtraBillingShadow(cfg, "gpt-4o-mini", 50, 10, 60, "chat_completions")

	require.Equal(t, ShortMsgExtraBillingModeShadow, res.Mode)
	require.NotNil(t, res.MatchedRule)
	require.Equal(t, "r1", res.MatchedRule.ID)
	require.True(t, res.WouldApply)
	require.Equal(t, 500, res.CandidateExtraQuota)
	require.False(t, res.Waived)
	require.True(t, res.HasReportableInfo())
}

func TestEvaluateShortMsgExtraBillingShadow_ThresholdEqualityDoesNotApply(t *testing.T) {
	cfg := ShortMsgExtraBillingConfig{
		Mode: ShortMsgExtraBillingModeShadow,
		Rules: []ShortMsgExtraBillingRule{
			{ID: "r1", Model: "gpt-4o-mini", Trigger: ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
		},
	}

	res := EvaluateShortMsgExtraBillingShadow(cfg, "gpt-4o-mini", 100, 10, 110, "chat_completions")

	require.NotNil(t, res.MatchedRule)
	require.False(t, res.WouldApply)
	require.Equal(t, 0, res.CandidateExtraQuota)
	// Matched but not triggered is still reportable for shadow observability.
	require.True(t, res.HasReportableInfo())
}

func TestEvaluateShortMsgExtraBillingShadow_ThresholdAboveDoesNotApply(t *testing.T) {
	cfg := ShortMsgExtraBillingConfig{
		Mode: ShortMsgExtraBillingModeShadow,
		Rules: []ShortMsgExtraBillingRule{
			{ID: "r1", Model: "gpt-4o-mini", Trigger: ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
		},
	}

	res := EvaluateShortMsgExtraBillingShadow(cfg, "gpt-4o-mini", 200, 10, 210, "chat_completions")

	require.False(t, res.WouldApply)
	require.Equal(t, 0, res.CandidateExtraQuota)
}

func TestEvaluateShortMsgExtraBillingShadow_WaiveWhenCompletionTokensZero(t *testing.T) {
	cfg := ShortMsgExtraBillingConfig{
		Mode: ShortMsgExtraBillingModeShadow,
		Rules: []ShortMsgExtraBillingRule{
			{
				ID:                            "r1",
				Model:                         "gpt-4o-mini",
				Trigger:                       ShortMsgExtraBillingTriggerInputTokensBelow,
				Threshold:                     100,
				FeeQuota:                      500,
				WaiveWhenCompletionTokensZero: true,
			},
		},
	}

	res := EvaluateShortMsgExtraBillingShadow(cfg, "gpt-4o-mini", 50, 0, 50, "chat_completions")

	require.True(t, res.Waived)
	require.Equal(t, "completion_tokens_zero", res.WaiveReason)
	require.False(t, res.WouldApply)
	// Waiver still surfaces the candidate fee that *would* have been charged
	// so auditors can size the suppressed charge.
	require.Equal(t, 500, res.CandidateExtraQuota)
	require.True(t, res.HasReportableInfo())
}

func TestEvaluateShortMsgExtraBillingShadow_NoWaiveWhenFlagDisabled(t *testing.T) {
	cfg := ShortMsgExtraBillingConfig{
		Mode: ShortMsgExtraBillingModeShadow,
		Rules: []ShortMsgExtraBillingRule{
			{
				ID:                            "r1",
				Model:                         "gpt-4o-mini",
				Trigger:                       ShortMsgExtraBillingTriggerInputTokensBelow,
				Threshold:                     100,
				FeeQuota:                      500,
				WaiveWhenCompletionTokensZero: false,
			},
		},
	}

	res := EvaluateShortMsgExtraBillingShadow(cfg, "gpt-4o-mini", 50, 0, 50, "chat_completions")

	require.False(t, res.Waived)
	require.True(t, res.WouldApply)
	require.Equal(t, 500, res.CandidateExtraQuota)
}

func TestEvaluateShortMsgExtraBillingShadow_TotalTokensZeroProducesNoCandidate(t *testing.T) {
	cfg := ShortMsgExtraBillingConfig{
		Mode: ShortMsgExtraBillingModeShadow,
		Rules: []ShortMsgExtraBillingRule{
			{ID: "r1", Model: "gpt-4o-mini", Trigger: ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
		},
	}

	res := EvaluateShortMsgExtraBillingShadow(cfg, "gpt-4o-mini", 50, 0, 0, "chat_completions")

	// Matched but the request itself is non-billable (upstream returned no usage),
	// so no candidate is produced and the result is not reportable.
	require.NotNil(t, res.MatchedRule)
	require.False(t, res.WouldApply)
	require.False(t, res.Waived)
	require.Equal(t, 0, res.CandidateExtraQuota)
	require.False(t, res.HasReportableInfo())
}

func TestEvaluateShortMsgExtraBillingShadow_ModelMismatchNoMatch(t *testing.T) {
	cfg := ShortMsgExtraBillingConfig{
		Mode: ShortMsgExtraBillingModeShadow,
		Rules: []ShortMsgExtraBillingRule{
			{ID: "r1", Model: "gpt-4o-mini", Trigger: ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
		},
	}

	res := EvaluateShortMsgExtraBillingShadow(cfg, "gpt-4o", 50, 10, 60, "chat_completions")

	require.Nil(t, res.MatchedRule)
	require.False(t, res.WouldApply)
	require.False(t, res.HasReportableInfo())
}

func TestEvaluateShortMsgExtraBillingShadow_InvalidRulesAreSkipped(t *testing.T) {
	cfg := ShortMsgExtraBillingConfig{
		Mode: ShortMsgExtraBillingModeShadow,
		Rules: []ShortMsgExtraBillingRule{
			// Invalid: empty ID.
			{ID: "", Model: "gpt-4o-mini", Trigger: ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
			// Invalid: empty model.
			{ID: "r2", Model: "", Trigger: ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
			// Invalid: unsupported trigger.
			{ID: "r3", Model: "gpt-4o-mini", Trigger: "completion_tokens_above", Threshold: 100, FeeQuota: 500},
			// Invalid: threshold <= 0.
			{ID: "r4", Model: "gpt-4o-mini", Trigger: ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 0, FeeQuota: 500},
			// Invalid: fee_quota <= 0.
			{ID: "r5", Model: "gpt-4o-mini", Trigger: ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 0},
			// Valid: should be picked.
			{ID: "r6", Model: "gpt-4o-mini", Trigger: ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
		},
	}

	res := EvaluateShortMsgExtraBillingShadow(cfg, "gpt-4o-mini", 50, 10, 60, "chat_completions")

	require.Equal(t, 5, res.SkippedInvalidRules)
	require.NotNil(t, res.MatchedRule)
	require.Equal(t, "r6", res.MatchedRule.ID)
	require.True(t, res.WouldApply)
	require.Equal(t, 500, res.CandidateExtraQuota)
}

func TestEvaluateShortMsgExtraBillingShadow_AllInvalidRulesYieldNoMatch(t *testing.T) {
	cfg := ShortMsgExtraBillingConfig{
		Mode: ShortMsgExtraBillingModeShadow,
		Rules: []ShortMsgExtraBillingRule{
			{ID: "", Model: "gpt-4o-mini", Trigger: ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
		},
	}

	res := EvaluateShortMsgExtraBillingShadow(cfg, "gpt-4o-mini", 50, 10, 60, "chat_completions")

	require.Nil(t, res.MatchedRule)
	require.Equal(t, 1, res.SkippedInvalidRules)
	require.False(t, res.HasReportableInfo())
}

func TestEvaluateShortMsgExtraBillingShadow_FirstMatchWins(t *testing.T) {
	cfg := ShortMsgExtraBillingConfig{
		Mode: ShortMsgExtraBillingModeShadow,
		Rules: []ShortMsgExtraBillingRule{
			{ID: "first", Model: "gpt-4o-mini", Trigger: ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 100},
			{ID: "second", Model: "gpt-4o-mini", Trigger: ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 200},
		},
	}

	res := EvaluateShortMsgExtraBillingShadow(cfg, "gpt-4o-mini", 50, 10, 60, "chat_completions")

	require.NotNil(t, res.MatchedRule)
	require.Equal(t, "first", res.MatchedRule.ID)
	require.Equal(t, 100, res.CandidateExtraQuota)
}

func TestEvaluateShortMsgExtraBillingShadow_ResponseModesRestrictApplication(t *testing.T) {
	rule := ShortMsgExtraBillingRule{
		ID:            "r1",
		Model:         "gpt-4o-mini",
		Trigger:       ShortMsgExtraBillingTriggerInputTokensBelow,
		Threshold:     100,
		FeeQuota:      500,
		ResponseModes: []string{"responses", "claude"},
	}

	t.Run("matching mode applies", func(t *testing.T) {
		cfg := ShortMsgExtraBillingConfig{Mode: ShortMsgExtraBillingModeShadow, Rules: []ShortMsgExtraBillingRule{rule}}
		res := EvaluateShortMsgExtraBillingShadow(cfg, "gpt-4o-mini", 50, 10, 60, "responses")
		require.NotNil(t, res.MatchedRule)
		require.True(t, res.WouldApply)
		require.Equal(t, 500, res.CandidateExtraQuota)
	})

	t.Run("non-matching mode does not apply", func(t *testing.T) {
		cfg := ShortMsgExtraBillingConfig{Mode: ShortMsgExtraBillingModeShadow, Rules: []ShortMsgExtraBillingRule{rule}}
		res := EvaluateShortMsgExtraBillingShadow(cfg, "gpt-4o-mini", 50, 10, 60, "chat_completions")
		require.Nil(t, res.MatchedRule)
		require.False(t, res.WouldApply)
		require.False(t, res.HasReportableInfo())
	})

	t.Run("empty response modes applies to any text mode", func(t *testing.T) {
		openRule := rule
		openRule.ResponseModes = nil
		cfg := ShortMsgExtraBillingConfig{Mode: ShortMsgExtraBillingModeShadow, Rules: []ShortMsgExtraBillingRule{openRule}}
		res := EvaluateShortMsgExtraBillingShadow(cfg, "gpt-4o-mini", 50, 10, 60, "gemini")
		require.NotNil(t, res.MatchedRule)
		require.True(t, res.WouldApply)
	})
}

func TestEvaluateShortMsgExtraBillingShadow_EmptyRulesYieldsNoMatch(t *testing.T) {
	cfg := ShortMsgExtraBillingConfig{Mode: ShortMsgExtraBillingModeShadow, Rules: nil}

	res := EvaluateShortMsgExtraBillingShadow(cfg, "gpt-4o-mini", 50, 10, 60, "chat_completions")

	require.Nil(t, res.MatchedRule)
	require.False(t, res.WouldApply)
	require.False(t, res.HasReportableInfo())
}

func TestEvaluateShortMsgExtraBillingShadow_DoesNotMutateConfig(t *testing.T) {
	rule := ShortMsgExtraBillingRule{
		ID:            "r1",
		Model:         "gpt-4o-mini",
		Trigger:       ShortMsgExtraBillingTriggerInputTokensBelow,
		Threshold:     100,
		FeeQuota:      500,
		ResponseModes: []string{"chat_completions"},
	}
	cfg := ShortMsgExtraBillingConfig{Mode: ShortMsgExtraBillingModeShadow, Rules: []ShortMsgExtraBillingRule{rule}}

	_ = EvaluateShortMsgExtraBillingShadow(cfg, "gpt-4o-mini", 50, 10, 60, "chat_completions")

	// Config slice contents are unchanged.
	require.Len(t, cfg.Rules, 1)
	assert.Equal(t, "r1", cfg.Rules[0].ID)
	assert.Equal(t, "gpt-4o-mini", cfg.Rules[0].Model)
	assert.Equal(t, []string{"chat_completions"}, cfg.Rules[0].ResponseModes)
}

// TestEvaluateShortMsgExtraBillingShadow_EmptyTextModeIsNoOp locks the Phase
// 10A boundary: PostTextConsumeQuota is also called by image, embedding,
// rerank, and audio-fallback paths, which the service layer maps to an empty
// textMode. Even when a rule has an empty ResponseModes list and the model
// matches exactly, an empty textMode must never produce a reportable shadow
// result (no audit entry is written).
func TestEvaluateShortMsgExtraBillingShadow_EmptyTextModeIsNoOp(t *testing.T) {
	// Rule has no ResponseModes restriction and model matches exactly: under
	// the old behavior this would match an empty textMode and produce a
	// reportable shadow record. The Phase 10A fix must reject it.
	cfg := ShortMsgExtraBillingConfig{
		Mode: ShortMsgExtraBillingModeShadow,
		Rules: []ShortMsgExtraBillingRule{
			{ID: "r1", Model: "gpt-4o-mini", Trigger: ShortMsgExtraBillingTriggerInputTokensBelow, Threshold: 100, FeeQuota: 500},
		},
	}

	res := EvaluateShortMsgExtraBillingShadow(cfg, "gpt-4o-mini", 50, 10, 60, "")

	require.Equal(t, ShortMsgExtraBillingModeShadow, res.Mode)
	require.Nil(t, res.MatchedRule)
	require.False(t, res.WouldApply)
	require.False(t, res.Waived)
	require.Equal(t, 0, res.CandidateExtraQuota)
	require.Equal(t, "non_text_mode", res.Reason)
	require.False(t, res.HasReportableInfo())
}

// TestEvaluateShortMsgExtraBillingShadow_EmptyTextModeFailsClosedEvenWithEmptyResponseModes
// is a tighter regression guard: the rule explicitly omits ResponseModes and
// the model still matches, yet the non-text path must remain no-op. It
// complements TestEvaluateShortMsgExtraBillingShadow_EmptyTextModeIsNoOp by
// varying the input shape (nil rules, waived rule, would-trigger usage) to
// prove the boundary holds across rule shapes.
func TestEvaluateShortMsgExtraBillingShadow_EmptyTextModeFailsClosedEvenWithEmptyResponseModes(t *testing.T) {
	t.Run("nil response modes with waive flag stays no-op", func(t *testing.T) {
		cfg := ShortMsgExtraBillingConfig{
			Mode: ShortMsgExtraBillingModeShadow,
			Rules: []ShortMsgExtraBillingRule{
				{
					ID:                            "r1",
					Model:                         "gpt-4o-mini",
					Trigger:                       ShortMsgExtraBillingTriggerInputTokensBelow,
					Threshold:                     100,
					FeeQuota:                      500,
					WaiveWhenCompletionTokensZero: true,
				},
			},
		}

		// Completion tokens zero + waive flag would normally surface a
		// waived candidate; the non-text boundary must suppress it.
		res := EvaluateShortMsgExtraBillingShadow(cfg, "gpt-4o-mini", 50, 0, 50, "")

		require.Nil(t, res.MatchedRule)
		require.False(t, res.Waived)
		require.Equal(t, "non_text_mode", res.Reason)
		require.False(t, res.HasReportableInfo())
	})

	t.Run("explicit nil response_modes stays no-op", func(t *testing.T) {
		cfg := ShortMsgExtraBillingConfig{
			Mode: ShortMsgExtraBillingModeShadow,
			Rules: []ShortMsgExtraBillingRule{
				{
					ID:            "r1",
					Model:         "gpt-4o-mini",
					Trigger:       ShortMsgExtraBillingTriggerInputTokensBelow,
					Threshold:     100,
					FeeQuota:      500,
					ResponseModes: nil,
				},
			},
		}

		res := EvaluateShortMsgExtraBillingShadow(cfg, "gpt-4o-mini", 50, 10, 60, "")

		require.Nil(t, res.MatchedRule)
		require.Equal(t, "non_text_mode", res.Reason)
		require.False(t, res.HasReportableInfo())
	})
}

// ---------------------------------------------------------------------------
// ParseAndValidateShortMsgExtraBillingConfig — Phase 10C server-side guard
// ---------------------------------------------------------------------------

// validRuleJSON returns a JSON-encoded rule with whitespace padding around
// id/model/response_modes so the normalizer's trim/dedupe behavior is
// observable.
const validRuleJSON = `{"id":"  r1  ","model":"  gpt-4o-mini  ","trigger":"input_tokens_below","threshold":100,"fee_quota":500,"response_modes":["  claude ","gemini","claude"]}`

func TestParseAndValidateShortMsgExtraBillingConfig_ValidModesAndNormalize(t *testing.T) {
	t.Run("off with no rules", func(t *testing.T) {
		cfg, normalized, err := ParseAndValidateShortMsgExtraBillingConfig(`{"mode":"off"}`)
		require.NoError(t, err)
		require.Equal(t, ShortMsgExtraBillingModeOff, cfg.Mode)
		require.Nil(t, cfg.Rules)
		// Stable JSON shape: rules field is null (matches the package-level
		// default initializer).
		assert.JSONEq(t, `{"mode":"off","rules":null}`, normalized)
	})

	t.Run("shadow with one valid rule", func(t *testing.T) {
		cfg, normalized, err := ParseAndValidateShortMsgExtraBillingConfig(`{"mode":"shadow","rules":[` + validRuleJSON + `]}`)
		require.NoError(t, err)
		require.Equal(t, ShortMsgExtraBillingModeShadow, cfg.Mode)
		require.Len(t, cfg.Rules, 1)
		assert.Equal(t, "r1", cfg.Rules[0].ID, "id should be trimmed")
		assert.Equal(t, "gpt-4o-mini", cfg.Rules[0].Model, "model should be trimmed")
		// Response modes are trimmed and de-duplicated, first-seen order kept.
		assert.Equal(t, []string{"claude", "gemini"}, cfg.Rules[0].ResponseModes)
		// The persisted value parses back to the same logical config.
		var reparsed ShortMsgExtraBillingConfig
		require.NoError(t, common.Unmarshal([]byte(normalized), &reparsed))
		assert.Equal(t, cfg, reparsed)
	})

	t.Run("enforce with one valid rule", func(t *testing.T) {
		cfg, _, err := ParseAndValidateShortMsgExtraBillingConfig(`{"mode":"enforce","rules":[{"id":"r1","model":"gpt-4o-mini","trigger":"input_tokens_below","threshold":10,"fee_quota":1}]}`)
		require.NoError(t, err)
		require.Equal(t, ShortMsgExtraBillingModeEnforce, cfg.Mode)
		require.Len(t, cfg.Rules, 1)
	})

	t.Run("empty rules under shadow is allowed", func(t *testing.T) {
		cfg, normalized, err := ParseAndValidateShortMsgExtraBillingConfig(`{"mode":"shadow","rules":[]}`)
		require.NoError(t, err)
		require.Equal(t, ShortMsgExtraBillingModeShadow, cfg.Mode)
		require.Nil(t, cfg.Rules, "empty rules should normalize to nil for stable storage")
		assert.JSONEq(t, `{"mode":"shadow","rules":null}`, normalized)
	})

	t.Run("rule with missing response_modes applies to all text modes", func(t *testing.T) {
		cfg, _, err := ParseAndValidateShortMsgExtraBillingConfig(`{"mode":"shadow","rules":[{"id":"r1","model":"gpt-4o-mini","trigger":"input_tokens_below","threshold":1,"fee_quota":1}]}`)
		require.NoError(t, err)
		require.Len(t, cfg.Rules, 1)
		assert.Nil(t, cfg.Rules[0].ResponseModes)
	})
}

func TestParseAndValidateShortMsgExtraBillingConfig_EmptyModeNormalizesToOff(t *testing.T) {
	cfg, normalized, err := ParseAndValidateShortMsgExtraBillingConfig(`{"mode":"","rules":[]}`)
	require.NoError(t, err)
	require.Equal(t, ShortMsgExtraBillingModeOff, cfg.Mode, "empty mode must normalize to off")
	assert.JSONEq(t, `{"mode":"off","rules":null}`, normalized)
}

func TestParseAndValidateShortMsgExtraBillingConfig_EmptyInputDefaultsToOff(t *testing.T) {
	for _, raw := range []string{"", "   ", "\n\t"} {
		cfg, normalized, err := ParseAndValidateShortMsgExtraBillingConfig(raw)
		require.NoError(t, err, "raw %q should not error", raw)
		require.Equal(t, ShortMsgExtraBillingModeOff, cfg.Mode)
		assert.JSONEq(t, `{"mode":"off","rules":null}`, normalized)
	}
}

func TestParseAndValidateShortMsgExtraBillingConfig_Rejections(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantMsg string
	}{
		{"invalid JSON", `{not-json`, "无效 JSON"},
		{"unknown mode", `{"mode":"bogus"}`, "mode 必须是"},
		{"unknown trigger", `{"mode":"shadow","rules":[{"id":"r1","model":"m","trigger":"output_tokens_above","threshold":1,"fee_quota":1}]}`, "trigger 必须是"},
		{"threshold zero", `{"mode":"shadow","rules":[{"id":"r1","model":"m","trigger":"input_tokens_below","threshold":0,"fee_quota":1}]}`, "threshold 必须 > 0"},
		{"threshold negative", `{"mode":"shadow","rules":[{"id":"r1","model":"m","trigger":"input_tokens_below","threshold":-5,"fee_quota":1}]}`, "threshold 必须 > 0"},
		{"fee_quota zero", `{"mode":"shadow","rules":[{"id":"r1","model":"m","trigger":"input_tokens_below","threshold":1,"fee_quota":0}]}`, "fee_quota 必须 > 0"},
		{"fee_quota negative", `{"mode":"shadow","rules":[{"id":"r1","model":"m","trigger":"input_tokens_below","threshold":1,"fee_quota":-1}]}`, "fee_quota 必须 > 0"},
		{"empty model", `{"mode":"shadow","rules":[{"id":"r1","model":"","trigger":"input_tokens_below","threshold":1,"fee_quota":1}]}`, "model 不能为空"},
		{"empty id", `{"mode":"shadow","rules":[{"id":"","model":"m","trigger":"input_tokens_below","threshold":1,"fee_quota":1}]}`, "id 不能为空"},
		{"unknown response mode", `{"mode":"shadow","rules":[{"id":"r1","model":"m","trigger":"input_tokens_below","threshold":1,"fee_quota":1,"response_modes":["chat_completions","bogus"]}]}`, "response_modes 无效"},
		{"empty response modes array", `{"mode":"shadow","rules":[{"id":"r1","model":"m","trigger":"input_tokens_below","threshold":1,"fee_quota":1,"response_modes":[]}]}`, "response_modes 无效"},
		{"blank response mode", `{"mode":"shadow","rules":[{"id":"r1","model":"m","trigger":"input_tokens_below","threshold":1,"fee_quota":1,"response_modes":["   "]}]}`, "response_modes 无效"},
		{"duplicate rule id", `{"mode":"shadow","rules":[{"id":"r1","model":"m","trigger":"input_tokens_below","threshold":1,"fee_quota":1},{"id":"r1","model":"m2","trigger":"input_tokens_below","threshold":1,"fee_quota":1}]}`, "重复 rule id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, normalized, err := ParseAndValidateShortMsgExtraBillingConfig(tc.raw)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantMsg)
			// On error the function returns the zero config + empty string so
			// callers never accidentally persist a half-validated value.
			assert.Equal(t, ShortMsgExtraBillingConfig{}, cfg)
			assert.Equal(t, "", normalized)
		})
	}
}

func TestParseAndValidateShortMsgExtraBillingConfig_ResponseModesTrimDedupe(t *testing.T) {
	cfg, normalized, err := ParseAndValidateShortMsgExtraBillingConfig(`{"mode":"shadow","rules":[{"id":"r1","model":"m","trigger":"input_tokens_below","threshold":1,"fee_quota":1,"response_modes":["  claude ","claude"," gemini ","claude"]}]}`)
	require.NoError(t, err)
	require.Len(t, cfg.Rules, 1)
	// Whitespace stripped; first-seen order kept; dups dropped.
	assert.Equal(t, []string{"claude", "gemini"}, cfg.Rules[0].ResponseModes)

	// The normalized JSON round-trips back to the same logical value.
	var reparsed ShortMsgExtraBillingConfig
	require.NoError(t, common.Unmarshal([]byte(normalized), &reparsed))
	assert.Equal(t, cfg, reparsed)
}

func TestParseAndValidateShortMsgExtraBillingConfig_DoesNotMutateInput(t *testing.T) {
	raw := `{"mode":"shadow","rules":[{"id":"  r1  ","model":"  m  ","trigger":"input_tokens_below","threshold":1,"fee_quota":1,"response_modes":[" claude ","claude"]}]}`

	_, _, err := ParseAndValidateShortMsgExtraBillingConfig(raw)
	require.NoError(t, err)
	// The input string is untouched.
	assert.Equal(t, `{"mode":"shadow","rules":[{"id":"  r1  ","model":"  m  ","trigger":"input_tokens_below","threshold":1,"fee_quota":1,"response_modes":[" claude ","claude"]}]}`, raw)
}

func TestNormalizeShortMsgExtraBillingOption_OwnedKeyNormalizes(t *testing.T) {
	normalized, err := NormalizeShortMsgExtraBillingOption(ShortMsgExtraBillingOptionKey, `{"mode":"shadow","rules":[]}`)
	require.NoError(t, err)
	assert.JSONEq(t, `{"mode":"shadow","rules":null}`, normalized)
}

func TestNormalizeShortMsgExtraBillingOption_RejectsInvalid(t *testing.T) {
	_, err := NormalizeShortMsgExtraBillingOption(ShortMsgExtraBillingOptionKey, `{"mode":"bogus"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mode 必须是")
}

func TestNormalizeShortMsgExtraBillingOption_NonOwnedKeyPassThrough(t *testing.T) {
	// Any other key is returned untouched so callers can route every option
	// through this helper without an extra branch.
	normalized, err := NormalizeShortMsgExtraBillingOption("SomeOther.Key", `{"foo":"bar"}`)
	require.NoError(t, err)
	assert.Equal(t, `{"foo":"bar"}`, normalized)
}

// keep strings import referenced when this file is compiled standalone.
var _ = strings.TrimSpace
