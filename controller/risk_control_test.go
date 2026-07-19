package controller

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRiskAgentDecisionValidatesContract(t *testing.T) {
	decision, raw, err := parseRiskAgentDecision(`{
		"verdict":"gateway_distribution",
		"risk_score":91,
		"confidence":0.94,
		"policy_violation":true,
		"evidence":[{"signal_id":"max_concurrency","strength":92,"summary":"high concurrency","request_ids":["req-1"]}],
		"counter_evidence":["short window"],
		"recommended_action":"temporary_block",
		"recommended_duration_minutes":360,
		"admin_reason":"confirmed gateway pattern",
		"user_reason":"abnormal shared usage"
	}`)
	require.NoError(t, err)
	assert.Equal(t, "gateway_distribution", decision.Verdict)
	assert.Equal(t, 91, decision.RiskScore)
	assert.Equal(t, "temporary_block", decision.RecommendedAction)
	require.Len(t, decision.Evidence, 1)
	assert.Equal(t, "max_concurrency", decision.Evidence[0].SignalID)
	assert.Contains(t, raw, "confirmed gateway pattern")
	require.NoError(t, validateRiskAgentEvidence(decision, riskAgentInput{
		AllowedSignalIDs:  map[string]struct{}{"max_concurrency": {}},
		AllowedRequestIDs: map[string]struct{}{"req-1": {}},
	}))

	_, _, err = parseRiskAgentDecision(`{"verdict":"invented","risk_score":90,"confidence":0.9,"recommended_action":"permanent_ban"}`)
	require.Error(t, err)

	decision.Evidence[0].RequestIDs = []string{"not-in-case"}
	require.Error(t, validateRiskAgentEvidence(decision, riskAgentInput{
		AllowedSignalIDs:  map[string]struct{}{"max_concurrency": {}},
		AllowedRequestIDs: map[string]struct{}{"req-1": {}},
	}))
}

func TestRedactRiskAgentPromptMasksCredentials(t *testing.T) {
	redacted := redactRiskAgentPrompt(`{"authorization":"Bearer secret-token","api_key":"sk-abcdefghijklmnopqrstuvwxyz","ip":"10.0.0.1"}`)
	assert.NotContains(t, redacted, "secret-token")
	assert.NotContains(t, redacted, "sk-abcdefghijklmnopqrstuvwxyz")
	assert.NotContains(t, redacted, "10.0.0.1")
	assert.Contains(t, redacted, "[REDACTED]")
}

func TestNormalizeRiskAgentRecommendationUsesLocalMatrix(t *testing.T) {
	decision := normalizeRiskAgentRecommendation(riskAgentDecision{
		Verdict:           "key_leak",
		RecommendedAction: model.RiskActionFreezeToken,
		AdminReason:       "model requested token freeze",
	}, &model.RiskCase{TokenId: 0})

	assert.Equal(t, model.RiskActionManualReview, decision.RecommendedAction)
	assert.Contains(t, decision.AdminReason, "本地动作矩阵")
}

func TestValidateRiskAgentJSONParamsRejectsPromptOverrides(t *testing.T) {
	allowed := json.RawMessage(`{"response_format":{"type":"json_object"},"temperature":0}`)
	require.NoError(t, validateRiskAgentJSONParams(allowed, map[string]interface{}{
		"response_format": map[string]interface{}{"type": "json_object"},
		"temperature":     float64(0),
	}))

	reserved := json.RawMessage(`{"messages":[],"response_format":{"type":"json_object"}}`)
	require.Error(t, validateRiskAgentJSONParams(reserved, map[string]interface{}{
		"messages":        []interface{}{},
		"response_format": map[string]interface{}{"type": "json_object"},
	}))
}
