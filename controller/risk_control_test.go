package controller

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
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

func TestParseRiskAgentDecisionAcceptsFirstJSONValue(t *testing.T) {
	decision, _, err := parseRiskAgentDecision("```json\n" + validRiskAgentDecisionJSON + "\n```\nThis trailing explanation should be ignored.")
	require.NoError(t, err)
	assert.Equal(t, "uncertain", decision.Verdict)
}

func TestRequestRiskAgentDecisionRetriesWithoutResponseFormat(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/api/risk-control/cases/1/analyze", nil)
	cfg := &system_setting.RiskControlSetting{
		ChannelID:        1,
		AgentRetryCount:  1,
		JSONOutputParams: json.RawMessage(`{"response_format":{"type":"json_object"},"temperature":0}`),
	}
	input := riskAgentInput{
		AllowedSignalIDs:  map[string]struct{}{},
		AllowedRequestIDs: map[string]struct{}{},
	}
	calls := 0
	decision, _, err := requestRiskAgentDecision(c, cfg, "test-model", "base prompt", input, func(
		_ *gin.Context,
		_ int,
		_ string,
		params json.RawMessage,
		prompt string,
	) (string, error) {
		calls++
		var decoded map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(params, &decoded))
		if calls == 1 {
			assert.Contains(t, decoded, "response_format")
			return "not json", nil
		}
		assert.NotContains(t, decoded, "response_format")
		assert.Contains(t, decoded, "temperature")
		assert.Contains(t, prompt, "LOCAL VALIDATION RETRY")
		return validRiskAgentDecisionJSON, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
	assert.Equal(t, "uncertain", decision.Verdict)
}

func TestRiskAgentLimiterEnforcesConfiguredConcurrency(t *testing.T) {
	limiter := newRiskAgentLimiter()
	releaseFirst, err := limiter.acquire(context.Background(), 2)
	require.NoError(t, err)
	releaseSecond, err := limiter.acquire(context.Background(), 2)
	require.NoError(t, err)

	blockedContext, cancelBlocked := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancelBlocked()
	_, err = limiter.acquire(blockedContext, 2)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	releaseFirst()
	releaseThird, err := limiter.acquire(context.Background(), 2)
	require.NoError(t, err)
	releaseThird()
	releaseSecond()
}

const validRiskAgentDecisionJSON = `{
	"verdict":"uncertain",
	"risk_score":40,
	"confidence":0.5,
	"policy_violation":false,
	"evidence":[],
	"counter_evidence":[],
	"recommended_action":"manual_review",
	"recommended_duration_minutes":0,
	"admin_reason":"manual review required",
	"user_reason":"",
	"suggested_fingerprint":{"kind":"none","pattern":"","reason":""}
}`

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
