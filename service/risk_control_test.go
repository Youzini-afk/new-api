package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetRiskControlTables(t *testing.T) {
	t.Helper()
	require.NoError(t, model.DB.Exec("DELETE FROM risk_actions").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM risk_cases").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM tokens").Error)
	require.NoError(t, model.DB.Unscoped().Exec("DELETE FROM users").Error)
	require.NoError(t, model.LOG_DB.Exec("DELETE FROM logs").Error)
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM risk_actions")
		model.DB.Exec("DELETE FROM risk_cases")
		model.DB.Exec("DELETE FROM tokens")
		model.DB.Unscoped().Exec("DELETE FROM users")
		model.LOG_DB.Exec("DELETE FROM logs")
	})
}

func riskTestSubject(t *testing.T) (*model.User, *model.Token) {
	t.Helper()
	user := &model.User{
		Username: "risk-user",
		Password: "testpass123",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  "risk-user-aff",
	}
	require.NoError(t, model.DB.Create(user).Error)
	token := &model.Token{
		UserId:      user.Id,
		Key:         "risk-token-key-xxxxxxxxxxxxxxxx",
		Name:        "risk-token",
		Status:      common.TokenStatusEnabled,
		ExpiredTime: -1,
		Group:       "default",
	}
	require.NoError(t, model.DB.Create(token).Error)
	return user, token
}

func TestRunRiskScreeningCreatesAndDeduplicatesGatewayCase(t *testing.T) {
	resetRiskControlTables(t)
	user, token := riskTestSubject(t)
	original := *system_setting.GetRiskControlSetting()
	t.Cleanup(func() { *system_setting.GetRiskControlSetting() = original })
	cfg := system_setting.GetRiskControlSetting()
	cfg.Enabled = true
	cfg.WindowHours = []int{1}
	cfg.CandidateLimit = 20
	cfg.DetailLimit = 1000
	cfg.MaxSamples = 5
	cfg.MinRequests = 4
	cfg.CaseThreshold = 40
	cfg.HighRPM = 20
	cfg.CriticalRPM = 100
	cfg.IPFanoutThreshold = 5
	cfg.UAFanoutThreshold = 4
	cfg.ConcurrencyThreshold = 8
	cfg.CaseCooldownMinutes = 43200
	cfg.IncludeRequestContent = true
	cfg.RedactSensitive = true

	now := common.GetTimestamp()
	other := common.MapToJsonStr(map[string]interface{}{
		common.RequestParamsOtherKey: map[string]interface{}{
			"messages": []map[string]interface{}{{"role": "user", "content": "same shared request"}},
		},
	})
	for i := 0; i < 50; i++ {
		log := &model.Log{
			UserId:           user.Id,
			TokenId:          token.Id,
			Type:             model.LogTypeConsume,
			CreatedAt:        now - 30,
			PromptTokens:     10,
			CompletionTokens: 5,
			UseTime:          10,
			Ip:               fmt.Sprintf("10.0.0.%d", i%6+1),
			UserAgent:        fmt.Sprintf("client/%d", i%6+1),
			ModelName:        "test-model",
			RequestPath:      "/v1/chat/completions",
			RequestId:        fmt.Sprintf("risk-request-%d", i),
			Other:            other,
		}
		require.NoError(t, model.LOG_DB.Create(log).Error)
	}

	summary, err := RunRiskScreening(context.Background(), true)
	require.NoError(t, err)
	assert.Equal(t, "completed", summary.Status)
	assert.Equal(t, 1, summary.CasesCreated)
	require.Len(t, summary.CaseIds, 1)

	riskCase, err := model.GetRiskCaseById(context.Background(), summary.CaseIds[0])
	require.NoError(t, err)
	assert.Equal(t, "gateway_distribution", riskCase.Verdict)
	assert.GreaterOrEqual(t, riskCase.RuleScore, 40)
	assert.Contains(t, riskCase.Signals, "same shared request")
	assert.Equal(t, 1, riskCase.RepeatCount)
	require.NoError(t, model.DB.Model(&model.RiskCase{}).Where("id = ?", riskCase.Id).Updates(map[string]interface{}{
		"status":       model.RiskCaseStatusDismissed,
		"agent_result": `{"verdict":"gateway_distribution"}`,
		"agent_score":  90,
		"final_score":  88,
	}).Error)

	second, err := RunRiskScreening(context.Background(), true)
	require.NoError(t, err)
	assert.Equal(t, 0, second.CasesCreated)
	assert.Equal(t, 1, second.CasesUpdated)
	reloaded, err := model.GetRiskCaseById(context.Background(), riskCase.Id)
	require.NoError(t, err)
	assert.Equal(t, 1, reloaded.RepeatCount)
	assert.Equal(t, model.RiskCaseStatusDismissed, reloaded.Status)
	assert.NotEmpty(t, reloaded.AgentResult)

	newLog := &model.Log{
		UserId:           user.Id,
		TokenId:          token.Id,
		Type:             model.LogTypeConsume,
		CreatedAt:        now - 1,
		PromptTokens:     10,
		CompletionTokens: 5,
		UseTime:          2,
		Ip:               "10.0.0.7",
		UserAgent:        "client/7",
		ModelName:        "test-model",
		RequestPath:      "/v1/chat/completions",
		RequestId:        "risk-request-new-evidence",
		Other:            other,
	}
	require.NoError(t, model.LOG_DB.Create(newLog).Error)
	third, err := RunRiskScreening(context.Background(), true)
	require.NoError(t, err)
	assert.Equal(t, 1, third.CasesUpdated)
	reloaded, err = model.GetRiskCaseById(context.Background(), riskCase.Id)
	require.NoError(t, err)
	assert.Equal(t, 2, reloaded.RepeatCount)
	assert.Equal(t, model.RiskCaseStatusOpen, reloaded.Status)
	assert.Empty(t, reloaded.AgentResult)
}

func TestApplyAndExpireRiskRateLimit(t *testing.T) {
	resetRiskControlTables(t)
	user, token := riskTestSubject(t)
	riskCase := &model.RiskCase{
		Fingerprint:       "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		UserId:            user.Id,
		Username:          user.Username,
		TokenId:           token.Id,
		TokenName:         token.Name,
		RuleScore:         80,
		FinalScore:        88,
		Verdict:           "gateway_distribution",
		RiskLevel:         "high",
		RecommendedAction: model.RiskActionRateLimit,
	}
	require.NoError(t, model.DB.Create(riskCase).Error)

	action, err := ApplyRiskAction(context.Background(), ApplyRiskActionInput{
		CaseId:          riskCase.Id,
		Action:          model.RiskActionRateLimit,
		Source:          "manual",
		DurationMinutes: 1,
		RequestLimit:    7,
		Reason:          "confirmed distributed use",
		UserMessage:     "temporary station restriction",
		OperatorUserId:  1,
		OperatorName:    "root",
	})
	require.NoError(t, err)
	assert.Equal(t, model.RiskActionRateLimit, action.Action)

	var restricted model.User
	require.NoError(t, model.DB.First(&restricted, user.Id).Error)
	assert.Equal(t, model.RiskActionRateLimit, restricted.RiskAction)
	assert.Equal(t, 7, restricted.RiskRequestLimit)
	assert.Equal(t, action.Id, restricted.RiskActionId)
	assert.Equal(t, "temporary station restriction", restricted.RiskMessage)

	processed, err := ExpireRiskActions(context.Background(), action.ExpiresAt, 10)
	require.NoError(t, err)
	assert.Equal(t, 1, processed)
	require.NoError(t, model.DB.First(&restricted, user.Id).Error)
	assert.Empty(t, restricted.RiskAction)
	assert.Zero(t, restricted.RiskRequestLimit)
	assert.Empty(t, restricted.RiskMessage)

	var expired model.RiskAction
	require.NoError(t, model.DB.First(&expired, action.Id).Error)
	assert.Equal(t, model.RiskActionStatusExpired, expired.Status)

	var resolvedCase model.RiskCase
	require.NoError(t, model.DB.First(&resolvedCase, riskCase.Id).Error)
	assert.Equal(t, model.RiskCaseStatusResolved, resolvedCase.Status)
}

func TestManualReviewDoesNotReplaceActiveRestriction(t *testing.T) {
	resetRiskControlTables(t)
	user, token := riskTestSubject(t)
	restrictedCase := &model.RiskCase{
		Fingerprint: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		UserId:      user.Id,
		TokenId:     token.Id,
		FinalScore:  88,
		Verdict:     "gateway_distribution",
	}
	reviewCase := &model.RiskCase{
		Fingerprint: "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		UserId:      user.Id,
		TokenId:     token.Id,
		FinalScore:  60,
		Verdict:     "uncertain",
	}
	require.NoError(t, model.DB.Create(restrictedCase).Error)
	require.NoError(t, model.DB.Create(reviewCase).Error)

	restriction, err := ApplyRiskAction(context.Background(), ApplyRiskActionInput{
		CaseId:          restrictedCase.Id,
		Action:          model.RiskActionRateLimit,
		DurationMinutes: 60,
		RequestLimit:    5,
	})
	require.NoError(t, err)

	review, err := ApplyRiskAction(context.Background(), ApplyRiskActionInput{
		CaseId: reviewCase.Id,
		Action: model.RiskActionManualReview,
		Reason: "needs a human decision",
	})
	require.NoError(t, err)
	assert.Equal(t, model.RiskActionStatusCompleted, review.Status)

	var currentUser model.User
	require.NoError(t, model.DB.First(&currentUser, user.Id).Error)
	assert.Equal(t, model.RiskActionRateLimit, currentUser.RiskAction)
	assert.Equal(t, restriction.Id, currentUser.RiskActionId)

	var currentRestriction model.RiskAction
	require.NoError(t, model.DB.First(&currentRestriction, restriction.Id).Error)
	assert.Equal(t, model.RiskActionStatusActive, currentRestriction.Status)

	var currentReviewCase model.RiskCase
	require.NoError(t, model.DB.First(&currentReviewCase, reviewCase.Id).Error)
	assert.Equal(t, model.RiskCaseStatusReviewing, currentReviewCase.Status)
}

func TestClearRestrictionResolvesSupersededCase(t *testing.T) {
	resetRiskControlTables(t)
	user, token := riskTestSubject(t)
	restrictedCase := &model.RiskCase{
		Fingerprint: "abababababababababababababababababababababababababababababababab",
		UserId:      user.Id,
		TokenId:     token.Id,
		FinalScore:  88,
		Verdict:     "gateway_distribution",
	}
	clearCase := &model.RiskCase{
		Fingerprint: "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd",
		UserId:      user.Id,
		TokenId:     token.Id,
		FinalScore:  40,
		Verdict:     "uncertain",
	}
	require.NoError(t, model.DB.Create(restrictedCase).Error)
	require.NoError(t, model.DB.Create(clearCase).Error)

	restriction, err := ApplyRiskAction(context.Background(), ApplyRiskActionInput{
		CaseId:          restrictedCase.Id,
		Action:          model.RiskActionTemporaryBlock,
		DurationMinutes: 60,
	})
	require.NoError(t, err)
	require.Error(t, model.ReviewRiskCase(context.Background(), restrictedCase.Id, model.RiskCaseStatusResolved, 1, "root", "premature close"))
	_, err = ApplyRiskAction(context.Background(), ApplyRiskActionInput{
		CaseId: clearCase.Id,
		Action: model.RiskActionClear,
	})
	require.NoError(t, err)

	var revoked model.RiskAction
	require.NoError(t, model.DB.First(&revoked, restriction.Id).Error)
	assert.Equal(t, model.RiskActionStatusRevoked, revoked.Status)

	var previousCase model.RiskCase
	require.NoError(t, model.DB.First(&previousCase, restrictedCase.Id).Error)
	assert.Equal(t, model.RiskCaseStatusResolved, previousCase.Status)
	require.NoError(t, model.ReviewRiskCase(context.Background(), restrictedCase.Id, model.RiskCaseStatusOpen, 1, "root", "reopen after clear"))
	require.NoError(t, model.DB.First(&previousCase, restrictedCase.Id).Error)
	assert.Equal(t, model.RiskCaseStatusOpen, previousCase.Status)
	assert.Zero(t, previousCase.ActionId)

	var currentUser model.User
	require.NoError(t, model.DB.First(&currentUser, user.Id).Error)
	assert.Empty(t, currentUser.RiskAction)
}

func TestClassifyRiskUserAgentSeparatesGatewayAndForbiddenClient(t *testing.T) {
	cfg := system_setting.NormalizeRiskControlSetting(system_setting.RiskControlSetting{
		GatewayUAMarkers:       []string{"sub2api", "axon"},
		ForbiddenClientMarkers: []string{"paid-client"},
	})

	gateway, forbidden := classifyRiskUserAgent("sub2api/1.2", cfg)
	assert.True(t, gateway)
	assert.False(t, forbidden)

	gateway, forbidden = classifyRiskUserAgent("paid-client/9.0", cfg)
	assert.False(t, gateway)
	assert.True(t, forbidden)
}

func TestRunRiskScreeningCreatesUserLevelCaseAcrossMultipleTokens(t *testing.T) {
	resetRiskControlTables(t)
	user, firstToken := riskTestSubject(t)
	secondToken := &model.Token{UserId: user.Id, Key: "risk-token-key-second-xxxxxxxx", Name: "second", Status: common.TokenStatusEnabled, ExpiredTime: -1, Group: "default"}
	thirdToken := &model.Token{UserId: user.Id, Key: "risk-token-key-third-xxxxxxxxx", Name: "third", Status: common.TokenStatusEnabled, ExpiredTime: -1, Group: "default"}
	require.NoError(t, model.DB.Create(secondToken).Error)
	require.NoError(t, model.DB.Create(thirdToken).Error)

	original := *system_setting.GetRiskControlSetting()
	t.Cleanup(func() { *system_setting.GetRiskControlSetting() = original })
	cfg := system_setting.GetRiskControlSetting()
	cfg.Enabled = true
	cfg.WindowHours = []int{1}
	cfg.CandidateLimit = 20
	cfg.DetailLimit = 1000
	cfg.MaxSamples = 4
	cfg.MinRequests = 20
	cfg.CaseThreshold = 15
	cfg.HighRPM = 1000
	cfg.CriticalRPM = 2000
	cfg.IPFanoutThreshold = 5
	cfg.UAFanoutThreshold = 10
	cfg.ConcurrencyThreshold = 100
	cfg.GatewayUAMarkers = []string{}
	cfg.ForbiddenClientMarkers = []string{}
	cfg.CaseCooldownMinutes = 43200

	now := common.GetTimestamp()
	tokens := []*model.Token{firstToken, secondToken, thirdToken}
	for tokenIndex, token := range tokens {
		for requestIndex := 0; requestIndex < 10; requestIndex++ {
			require.NoError(t, model.LOG_DB.Create(&model.Log{
				UserId:           user.Id,
				TokenId:          token.Id,
				Type:             model.LogTypeConsume,
				CreatedAt:        now - int64(requestIndex+1),
				UseTime:          1,
				Ip:               fmt.Sprintf("10.10.0.%d", (tokenIndex*2+requestIndex)%6+1),
				UserAgent:        "personal-client/1.0",
				ModelName:        "test-model",
				RequestPath:      "/v1/chat/completions",
				RequestId:        fmt.Sprintf("multi-token-%d-%d", tokenIndex, requestIndex),
				PromptTokens:     1,
				CompletionTokens: 1,
			}).Error)
		}
	}

	summary, err := RunRiskScreening(context.Background(), true)
	require.NoError(t, err)
	require.Len(t, summary.CaseIds, 1)
	riskCase, err := model.GetRiskCaseById(context.Background(), summary.CaseIds[0])
	require.NoError(t, err)
	assert.Zero(t, riskCase.TokenId)
	assert.Equal(t, "gateway_distribution", riskCase.RuleVerdict)
	assert.Contains(t, riskCase.Signals, `"distinct_tokens":3`)
}

func TestAutomaticRiskActionMatrixRejectsIncompatibleAgentAdvice(t *testing.T) {
	assert.True(t, RiskActionCompatibleWithVerdict("key_leak", model.RiskActionFreezeToken, 1))
	assert.False(t, RiskActionCompatibleWithVerdict("key_leak", model.RiskActionFreezeToken, 0))
	assert.False(t, RiskActionCompatibleWithVerdict("key_leak", model.RiskActionPermanentBan, 1))
	assert.True(t, RiskActionCompatibleWithVerdict("commercial_resale", model.RiskActionPermanentBan, 0))
	assert.False(t, RiskActionCompatibleWithVerdict("normal", model.RiskActionTemporaryBlock, 1))
	assert.False(t, RiskActionCompatibleWithVerdict("uncertain", model.RiskActionRateLimit, 1))
}

func TestMaxRiskConcurrencyUsesHalfOpenIntervals(t *testing.T) {
	assert.Equal(t, 1, maxRiskConcurrency([]riskConcurrencyEvent{
		{at: 10, delta: 1},
		{at: 11, delta: -1},
		{at: 11, delta: 1},
		{at: 12, delta: -1},
	}))
	assert.Equal(t, 2, maxRiskConcurrency([]riskConcurrencyEvent{
		{at: 10, delta: 1},
		{at: 10, delta: 1},
		{at: 11, delta: -1},
		{at: 11, delta: -1},
	}))
}

func TestAutomaticRiskActionRequiresExplicitPolicyViolation(t *testing.T) {
	original := *system_setting.GetRiskControlSetting()
	t.Cleanup(func() { *system_setting.GetRiskControlSetting() = original })
	cfg := system_setting.GetRiskControlSetting()
	cfg.AutoActionEnabled = true
	cfg.MaxAutoActionsPerRun = 10
	cfg.AutoActionMinScore = 80
	cfg.AutoActionMinConfidence = 0.9

	_, applied, err := MaybeApplyAutomaticRiskAction(context.Background(), &model.RiskCase{
		AgentResult:       `{"policy_violation":false}`,
		FinalScore:        95,
		Confidence:        0.99,
		PolicyViolation:   false,
		Verdict:           "gateway_distribution",
		RecommendedAction: model.RiskActionTemporaryBlock,
	}, 0)
	require.NoError(t, err)
	assert.False(t, applied)
}

func TestAutomaticRiskActionHonorsHumanReviewHold(t *testing.T) {
	original := *system_setting.GetRiskControlSetting()
	t.Cleanup(func() { *system_setting.GetRiskControlSetting() = original })
	cfg := system_setting.GetRiskControlSetting()
	cfg.AutoActionEnabled = true
	cfg.MaxAutoActionsPerRun = 10
	cfg.AutoActionMinScore = 80
	cfg.AutoActionMinConfidence = 0.9
	cfg.AutoTempBlockEnabled = true

	_, applied, err := MaybeApplyAutomaticRiskAction(context.Background(), &model.RiskCase{
		Status:            model.RiskCaseStatusReviewing,
		AgentResult:       `{"policy_violation":true}`,
		FinalScore:        95,
		Confidence:        0.99,
		PolicyViolation:   true,
		Verdict:           "gateway_distribution",
		RecommendedAction: model.RiskActionTemporaryBlock,
	}, 0)
	require.NoError(t, err)
	assert.False(t, applied)
}

func TestResolveStaleRiskCasesUsesEachCaseWindow(t *testing.T) {
	resetRiskControlTables(t)
	now := common.GetTimestamp()
	stale := &model.RiskCase{
		Fingerprint: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		UserId:      1,
		Status:      model.RiskCaseStatusOpen,
		WindowHours: 1,
		LastSeenAt:  now - 3601,
	}
	recent := &model.RiskCase{
		Fingerprint: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		UserId:      2,
		Status:      model.RiskCaseStatusOpen,
		WindowHours: 24,
		LastSeenAt:  now - 3601,
	}
	require.NoError(t, model.DB.Create(stale).Error)
	require.NoError(t, model.DB.Create(recent).Error)

	resolved, err := model.ResolveStaleOpenRiskCases(context.Background(), now)
	require.NoError(t, err)
	assert.EqualValues(t, 1, resolved)
	require.NoError(t, model.DB.First(stale, stale.Id).Error)
	require.NoError(t, model.DB.First(recent, recent.Id).Error)
	assert.Equal(t, model.RiskCaseStatusResolved, stale.Status)
	assert.Equal(t, model.RiskCaseStatusOpen, recent.Status)
}

func TestUpdateRiskCaseAIRejectsStaleEvidence(t *testing.T) {
	resetRiskControlTables(t)
	riskCase := &model.RiskCase{
		Fingerprint:                "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		UserId:                     1,
		Status:                     model.RiskCaseStatusOpen,
		RuleScore:                  70,
		RuleVerdict:                "gateway_distribution",
		Verdict:                    "gateway_distribution",
		Signals:                    `{"request_count":50}`,
		SampleRequestIds:           `["old-request"]`,
		RuleRecommendedAction:      model.RiskActionRateLimit,
		RuleRecommendedDuration:    60,
		RecommendedAction:          model.RiskActionRateLimit,
		RecommendedDurationMinutes: 60,
		LastSeenAt:                 common.GetTimestamp() - 10,
	}
	require.NoError(t, model.DB.Create(riskCase).Error)

	stale, err := model.GetRiskCaseById(context.Background(), riskCase.Id)
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(&model.RiskCase{}).Where("id = ?", riskCase.Id).Updates(map[string]interface{}{
		"signals":            `{"request_count":51}`,
		"sample_request_ids": `["new-request"]`,
		"last_seen_at":       riskCase.LastSeenAt + 1,
	}).Error)

	stale.AgentResult = `{"verdict":"gateway_distribution"}`
	stale.AgentScore = 95
	stale.FinalScore = 90
	err = model.UpdateRiskCaseAI(context.Background(), stale)
	require.ErrorIs(t, err, model.ErrRiskCaseEvidenceChanged)

	current, err := model.GetRiskCaseById(context.Background(), riskCase.Id)
	require.NoError(t, err)
	assert.Empty(t, current.AgentResult)
}
