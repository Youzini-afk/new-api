package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"gorm.io/gorm"
)

type ApplyRiskActionInput struct {
	CaseId          int64  `json:"case_id"`
	Action          string `json:"action"`
	Source          string `json:"source"`
	DurationMinutes int    `json:"duration_minutes"`
	RequestLimit    int    `json:"request_limit"`
	Reason          string `json:"reason"`
	UserMessage     string `json:"user_message"`
	OperatorUserId  int    `json:"operator_user_id"`
	OperatorName    string `json:"operator_name"`
}

type riskActionParameters struct {
	DurationMinutes int `json:"duration_minutes,omitempty"`
	RequestLimit    int `json:"request_limit,omitempty"`
}

func ApplyRiskAction(ctx context.Context, input ApplyRiskActionInput) (*model.RiskAction, error) {
	riskCase, err := model.GetRiskCaseById(ctx, input.CaseId)
	if err != nil {
		return nil, err
	}
	user, err := model.GetUserById(riskCase.UserId, true)
	if err != nil {
		return nil, err
	}
	if user.Role >= common.RoleAdminUser {
		return nil, errors.New("cannot apply risk action to admin/root user")
	}
	action := strings.TrimSpace(input.Action)
	if !validRiskAction(action) {
		return nil, errors.New("invalid risk action")
	}
	if input.Source == "" {
		input.Source = "manual"
	}
	if input.DurationMinutes < 0 {
		return nil, errors.New("duration_minutes cannot be negative")
	}
	if input.DurationMinutes > 43200 {
		return nil, errors.New("duration_minutes cannot exceed 43200")
	}
	if (action == model.RiskActionRateLimit || action == model.RiskActionTemporaryBlock || action == model.RiskActionObserve) && input.DurationMinutes <= 0 {
		return nil, errors.New("duration_minutes must be greater than zero for timed actions")
	}
	if action == model.RiskActionRateLimit && input.RequestLimit <= 0 {
		return nil, errors.New("request_limit is required for rate_limit")
	}
	if input.RequestLimit > 100000 {
		return nil, errors.New("request_limit cannot exceed 100000")
	}
	if action == model.RiskActionFreezeToken && riskCase.TokenId <= 0 {
		return nil, errors.New("risk case has no token to freeze")
	}
	if action != model.RiskActionRateLimit && action != model.RiskActionTemporaryBlock && action != model.RiskActionObserve {
		input.DurationMinutes = 0
	}
	if action != model.RiskActionRateLimit {
		input.RequestLimit = 0
	}
	input.UserMessage = strings.TrimSpace(input.UserMessage)
	if messageRunes := []rune(input.UserMessage); len(messageRunes) > 512 {
		input.UserMessage = string(messageRunes[:512])
	}

	now := common.GetTimestamp()
	expiresAt := int64(0)
	if input.DurationMinutes > 0 {
		expiresAt = now + int64(input.DurationMinutes*60)
	}
	params, err := common.Marshal(riskActionParameters{
		DurationMinutes: input.DurationMinutes,
		RequestLimit:    input.RequestLimit,
	})
	if err != nil {
		return nil, err
	}
	actionRecord := &model.RiskAction{
		CaseId:         riskCase.Id,
		UserId:         riskCase.UserId,
		TokenId:        riskCase.TokenId,
		Action:         action,
		Source:         strings.TrimSpace(input.Source),
		Parameters:     string(params),
		Reason:         strings.TrimSpace(input.Reason),
		UserMessage:    input.UserMessage,
		StartedAt:      now,
		ExpiresAt:      expiresAt,
		Status:         model.RiskActionStatusActive,
		OperatorUserId: input.OperatorUserId,
		OperatorName:   strings.TrimSpace(input.OperatorName),
	}

	// Case workflow decisions must not replace an unrelated active user
	// restriction. "none" and "manual_review" are completed audit events;
	// "observe" may remain active until its optional expiry, but it still does
	// not touch the relay hot-path summary or revoke enforcement actions.
	if action == model.RiskActionNone || action == model.RiskActionManualReview || action == model.RiskActionObserve {
		caseStatus := model.RiskCaseStatusActioned
		if action == model.RiskActionNone {
			caseStatus = model.RiskCaseStatusResolved
			actionRecord.Status = model.RiskActionStatusCompleted
		} else if action == model.RiskActionManualReview {
			caseStatus = model.RiskCaseStatusReviewing
			actionRecord.Status = model.RiskActionStatusCompleted
		}
		if err := model.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(actionRecord).Error; err != nil {
				return err
			}
			return tx.Model(&model.RiskCase{}).Where("id = ?", riskCase.Id).Updates(map[string]interface{}{
				"status":     caseStatus,
				"action_id":  actionRecord.Id,
				"updated_at": now,
			}).Error
		}); err != nil {
			return nil, err
		}
		recordRiskActionManageLog(actionRecord, riskCase)
		return actionRecord, nil
	}

	if action == model.RiskActionPermanentBan {
		// A permanent safety restriction must not depend on the action/audit row
		// being writable. The durable user+token ban commits first; action
		// persistence is attempted afterwards and a failure never rolls it back.
		reason := actionRecord.Reason
		if reason == "" {
			reason = fmt.Sprintf("Risk case #%d permanent ban", riskCase.Id)
		}
		if err := BanUserAndDisableTokens(user, reason); err != nil {
			return nil, err
		}
		if err := model.DB.WithContext(ctx).Create(actionRecord).Error; err != nil {
			return nil, err
		}
		if err := model.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return revokeSupersededRiskActions(tx, riskCase.UserId, actionRecord.Id, now)
		}); err != nil {
			common.SysLog("risk permanent ban failed to revoke prior action rows: " + err.Error())
		}
		_ = model.DB.WithContext(ctx).Model(&model.User{}).Where("id = ?", user.Id).Updates(map[string]interface{}{
			"risk_action":        model.RiskActionPermanentBan,
			"risk_until":         0,
			"risk_score":         riskCase.FinalScore,
			"risk_case_id":       riskCase.Id,
			"risk_action_id":     actionRecord.Id,
			"risk_request_limit": 0,
			"risk_message":       actionRecord.UserMessage,
		}).Error
		_ = markRiskCaseActioned(ctx, riskCase.Id, actionRecord.Id)
		_ = model.InvalidateUserCache(user.Id)
		recordRiskActionManageLog(actionRecord, riskCase)
		return actionRecord, nil
	}

	err = model.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(actionRecord).Error; err != nil {
			return err
		}
		if err := revokeSupersededRiskActions(tx, riskCase.UserId, actionRecord.Id, now); err != nil {
			return err
		}

		if action == model.RiskActionFreezeToken {
			var token model.Token
			if err := tx.Select("id", "status").Where("id = ? AND user_id = ?", riskCase.TokenId, riskCase.UserId).First(&token).Error; err != nil {
				return err
			}
			if token.Status != common.TokenStatusDisabled {
				if err := tx.Model(&model.Token{}).Where("id = ?", token.Id).Update("status", common.TokenStatusDisabled).Error; err != nil {
					return err
				}
			}
		}

		userAction := action
		userUntil := expiresAt
		requestLimit := input.RequestLimit
		userMessage := actionRecord.UserMessage
		if action == model.RiskActionClear {
			userAction = ""
			userUntil = 0
			requestLimit = 0
			userMessage = ""
			actionRecord.Status = model.RiskActionStatusCompleted
			if err := tx.Model(&model.RiskAction{}).Where("id = ?", actionRecord.Id).Updates(map[string]interface{}{
				"status":     model.RiskActionStatusCompleted,
				"updated_at": now,
			}).Error; err != nil {
				return err
			}
		}
		userUpdate := tx.Model(&model.User{}).
			Where("id = ? AND role < ?", riskCase.UserId, common.RoleAdminUser).
			Updates(map[string]interface{}{
				"risk_action":        userAction,
				"risk_until":         userUntil,
				"risk_score":         riskCase.FinalScore,
				"risk_case_id":       riskCase.Id,
				"risk_action_id":     actionRecord.Id,
				"risk_request_limit": requestLimit,
				"risk_message":       userMessage,
			})
		if userUpdate.Error != nil {
			return userUpdate.Error
		}
		if userUpdate.RowsAffected == 0 {
			return errors.New("risk target no longer exists or is now an admin/root user")
		}
		caseStatus := model.RiskCaseStatusActioned
		if action == model.RiskActionClear {
			caseStatus = model.RiskCaseStatusResolved
		}
		return tx.Model(&model.RiskCase{}).Where("id = ?", riskCase.Id).Updates(map[string]interface{}{
			"status":     caseStatus,
			"action_id":  actionRecord.Id,
			"updated_at": now,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	if err := model.InvalidateUserCache(riskCase.UserId); err != nil {
		common.SysLog("risk action user cache invalidate failed: " + err.Error())
	}
	if action == model.RiskActionFreezeToken {
		if err := model.InvalidateUserTokensCache(riskCase.UserId); err != nil {
			common.SysLog("risk action token cache invalidate failed: " + err.Error())
		}
	}
	recordRiskActionManageLog(actionRecord, riskCase)
	return actionRecord, nil
}

func revokeSupersededRiskActions(tx *gorm.DB, userId int, currentActionId, now int64) error {
	if tx == nil || userId <= 0 || currentActionId <= 0 {
		return nil
	}
	var priorActions []model.RiskAction
	if err := tx.Select("id", "case_id").
		Where("user_id = ? AND id <> ? AND status = ?", userId, currentActionId, model.RiskActionStatusActive).
		Find(&priorActions).Error; err != nil {
		return err
	}
	if len(priorActions) == 0 {
		return nil
	}
	actionIds := make([]int64, 0, len(priorActions))
	for _, action := range priorActions {
		actionIds = append(actionIds, action.Id)
	}
	if err := tx.Model(&model.RiskAction{}).
		Where("id IN ? AND status = ?", actionIds, model.RiskActionStatusActive).
		Updates(map[string]interface{}{
			"status":     model.RiskActionStatusRevoked,
			"updated_at": now,
		}).Error; err != nil {
		return err
	}
	return tx.Model(&model.RiskCase{}).
		Where("action_id IN ? AND status = ?", actionIds, model.RiskCaseStatusActioned).
		Updates(map[string]interface{}{
			"status":     model.RiskCaseStatusResolved,
			"updated_at": now,
		}).Error
}

func validRiskAction(action string) bool {
	switch action {
	case model.RiskActionNone,
		model.RiskActionObserve,
		model.RiskActionRateLimit,
		model.RiskActionFreezeToken,
		model.RiskActionTemporaryBlock,
		model.RiskActionPermanentBan,
		model.RiskActionManualReview,
		model.RiskActionClear:
		return true
	default:
		return false
	}
}

func markRiskCaseActioned(ctx context.Context, caseId, actionId int64) error {
	return model.DB.WithContext(ctx).Model(&model.RiskCase{}).Where("id = ?", caseId).Updates(map[string]interface{}{
		"status":     model.RiskCaseStatusActioned,
		"action_id":  actionId,
		"updated_at": common.GetTimestamp(),
	}).Error
}

func recordRiskActionManageLog(action *model.RiskAction, riskCase *model.RiskCase) {
	if action == nil {
		return
	}
	content := fmt.Sprintf("Risk action %s applied from case #%d", action.Action, action.CaseId)
	adminInfo := map[string]interface{}{
		"risk_case_id":   action.CaseId,
		"risk_action_id": action.Id,
		"action":         action.Action,
		"source":         action.Source,
		"expires_at":     action.ExpiresAt,
		"operator_id":    action.OperatorUserId,
		"operator_name":  action.OperatorName,
	}
	if riskCase != nil {
		adminInfo["verdict"] = riskCase.Verdict
		adminInfo["rule_score"] = riskCase.RuleScore
		adminInfo["agent_score"] = riskCase.AgentScore
		adminInfo["judge_score"] = riskCase.JudgeScore
		adminInfo["final_score"] = riskCase.FinalScore
		adminInfo["confidence"] = riskCase.Confidence
		adminInfo["policy_violation"] = riskCase.PolicyViolation
		adminInfo["repeat_count"] = riskCase.RepeatCount
		adminInfo["recommended_action"] = riskCase.RecommendedAction
	}
	model.RecordLogWithAdminInfo(action.UserId, model.LogTypeManage, content, adminInfo)
}

func ExpireRiskActions(ctx context.Context, now int64, limit int) (int, error) {
	if now <= 0 {
		now = common.GetTimestamp()
	}
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var actions []*model.RiskAction
	if err := model.DB.WithContext(ctx).
		Where("status = ? AND expires_at > 0 AND expires_at <= ?", model.RiskActionStatusActive, now).
		Order("expires_at asc, id asc").Limit(limit).Find(&actions).Error; err != nil {
		return 0, err
	}
	processed := 0
	for _, action := range actions {
		err := model.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			result := tx.Model(&model.RiskAction{}).
				Where("id = ? AND status = ?", action.Id, model.RiskActionStatusActive).
				Updates(map[string]interface{}{
					"status":     model.RiskActionStatusExpired,
					"updated_at": now,
				})
			if result.Error != nil || result.RowsAffected == 0 {
				if result.Error != nil {
					return result.Error
				}
				return gorm.ErrRecordNotFound
			}
			if err := tx.Model(&model.User{}).
				Where("id = ? AND risk_action_id = ?", action.UserId, action.Id).
				Updates(map[string]interface{}{
					"risk_action":        "",
					"risk_until":         0,
					"risk_request_limit": 0,
					"risk_message":       "",
				}).Error; err != nil {
				return err
			}
			// Once a temporary action ends, close the actioned case. If fresh
			// evidence later advances LastSeenAt, UpsertRiskCase reopens it and
			// clears ActionId, allowing a new decision in the same fingerprint
			// bucket instead of silently suppressing it until cooldown rollover.
			return tx.Model(&model.RiskCase{}).
				Where("id = ? AND action_id = ? AND status = ?", action.CaseId, action.Id, model.RiskCaseStatusActioned).
				Updates(map[string]interface{}{
					"status":     model.RiskCaseStatusResolved,
					"updated_at": now,
				}).Error
		})
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return processed, err
		}
		processed++
		_ = model.InvalidateUserCache(action.UserId)
	}
	return processed, nil
}

func MaybeApplyAutomaticRiskAction(ctx context.Context, riskCase *model.RiskCase, alreadyApplied int) (*model.RiskAction, bool, error) {
	if riskCase == nil {
		return nil, false, nil
	}
	cfg := system_setting.GetRiskControlSetting()
	if !cfg.AutoActionEnabled || alreadyApplied >= cfg.MaxAutoActionsPerRun {
		return nil, false, nil
	}
	if riskCase.ActionId > 0 || riskCase.Status != model.RiskCaseStatusOpen {
		return nil, false, nil
	}
	if strings.TrimSpace(riskCase.AgentResult) == "" || !riskCase.PolicyViolation || riskCase.FinalScore < cfg.AutoActionMinScore || riskCase.Confidence < cfg.AutoActionMinConfidence {
		return nil, false, nil
	}
	action := riskCase.RecommendedAction
	if !RiskActionCompatibleWithVerdict(riskCase.Verdict, action, riskCase.TokenId) {
		return nil, false, nil
	}
	switch action {
	case model.RiskActionRateLimit:
		if !cfg.AutoRateLimitEnabled {
			return nil, false, nil
		}
	case model.RiskActionFreezeToken:
		if !cfg.AutoFreezeTokenEnabled || riskCase.TokenId <= 0 {
			return nil, false, nil
		}
	case model.RiskActionTemporaryBlock:
		if !cfg.AutoTempBlockEnabled {
			return nil, false, nil
		}
	case model.RiskActionPermanentBan:
		if !cfg.AutoPermanentBanEnabled || riskCase.FinalScore < cfg.AutoPermanentMinScore || riskCase.JudgeScore <= 0 || strings.TrimSpace(riskCase.JudgeResult) == "" || riskCase.RepeatCount < 2 {
			return nil, false, nil
		}
	default:
		return nil, false, nil
	}
	duration := riskCase.RecommendedDurationMinutes
	if action == model.RiskActionTemporaryBlock && duration <= 0 {
		duration = cfg.TemporaryBlockMinutes
	}
	requestLimit := 0
	if action == model.RiskActionRateLimit {
		requestLimit = cfg.RateLimitPerMinute
		if duration <= 0 {
			duration = 60
		}
	}
	actionRecord, err := ApplyRiskAction(ctx, ApplyRiskActionInput{
		CaseId:          riskCase.Id,
		Action:          action,
		Source:          "auto_agent",
		DurationMinutes: duration,
		RequestLimit:    requestLimit,
		Reason:          riskCase.RecommendedReason,
		UserMessage:     automaticRiskUserMessage(riskCase),
		OperatorName:    "risk-agent",
	})
	if err != nil {
		return nil, false, err
	}
	return actionRecord, true, nil
}

// RiskActionCompatibleWithVerdict is the local policy matrix used to keep
// Agent recommendations advisory. High-impact actions are only eligible for
// the incident classes they can safely address.
func RiskActionCompatibleWithVerdict(verdict, action string, tokenId int) bool {
	switch action {
	case model.RiskActionNone, model.RiskActionObserve, model.RiskActionManualReview:
		return true
	}
	switch verdict {
	case "key_leak":
		return action == model.RiskActionRateLimit || (action == model.RiskActionFreezeToken && tokenId > 0) || action == model.RiskActionTemporaryBlock
	case "gateway_distribution", "multi_node_gateway", "commercial_resale", "forbidden_paid_client":
		return action == model.RiskActionRateLimit || action == model.RiskActionTemporaryBlock || action == model.RiskActionPermanentBan
	default:
		return false
	}
}

func automaticRiskUserMessage(riskCase *model.RiskCase) string {
	if riskCase != nil && strings.TrimSpace(riskCase.RecommendedUserReason) != "" {
		message := strings.TrimSpace(common.MaskSensitiveInfo(riskCase.RecommendedUserReason))
		if message != "" {
			return message
		}
	}
	return "API 访问因异常使用特征被限制，请联系管理员核查。"
}
