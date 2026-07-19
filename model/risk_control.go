package model

import (
	"context"
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	RiskCaseStatusOpen      = "open"
	RiskCaseStatusReviewing = "reviewing"
	RiskCaseStatusActioned  = "actioned"
	RiskCaseStatusResolved  = "resolved"
	RiskCaseStatusDismissed = "dismissed"

	RiskActionNone           = "none"
	RiskActionObserve        = "observe"
	RiskActionRateLimit      = "rate_limit"
	RiskActionFreezeToken    = "freeze_token"
	RiskActionTemporaryBlock = "temporary_block"
	RiskActionPermanentBan   = "permanent_ban"
	RiskActionManualReview   = "manual_review"
	RiskActionClear          = "clear"

	RiskActionStatusActive    = "active"
	RiskActionStatusCompleted = "completed"
	RiskActionStatusExpired   = "expired"
	RiskActionStatusRevoked   = "revoked"
)

var ErrRiskCaseEvidenceChanged = errors.New("risk case evidence changed; analyze again")

type RiskCase struct {
	Id                         int64   `json:"id" gorm:"primaryKey"`
	Fingerprint                string  `json:"fingerprint" gorm:"type:char(64);uniqueIndex"`
	UserId                     int     `json:"user_id" gorm:"index"`
	Username                   string  `json:"username" gorm:"type:varchar(64);index"`
	TokenId                    int     `json:"token_id" gorm:"index"`
	TokenName                  string  `json:"token_name" gorm:"type:varchar(128)"`
	Status                     string  `json:"status" gorm:"type:varchar(32);index"`
	Verdict                    string  `json:"verdict" gorm:"type:varchar(32);index"`
	RuleVerdict                string  `json:"rule_verdict" gorm:"type:varchar(32);index"`
	RiskLevel                  string  `json:"risk_level" gorm:"type:varchar(16);index"`
	RuleScore                  int     `json:"rule_score"`
	AgentScore                 int     `json:"agent_score"`
	JudgeScore                 int     `json:"judge_score"`
	FinalScore                 int     `json:"final_score" gorm:"index"`
	Confidence                 float64 `json:"confidence"`
	PolicyViolation            bool    `json:"policy_violation"`
	Signals                    string  `json:"signals" gorm:"type:text"`
	SampleRequestIds           string  `json:"sample_request_ids" gorm:"type:text"`
	RuleReason                 string  `json:"rule_reason" gorm:"type:text"`
	AgentResult                string  `json:"agent_result" gorm:"type:text"`
	JudgeResult                string  `json:"judge_result" gorm:"type:text"`
	AgentModel                 string  `json:"agent_model" gorm:"type:varchar(128)"`
	JudgeModel                 string  `json:"judge_model" gorm:"type:varchar(128)"`
	AgentAnalyzedAt            int64   `json:"agent_analyzed_at" gorm:"index"`
	JudgeAnalyzedAt            int64   `json:"judge_analyzed_at" gorm:"index"`
	RuleRecommendedAction      string  `json:"rule_recommended_action" gorm:"type:varchar(32)"`
	RuleRecommendedDuration    int     `json:"rule_recommended_duration_minutes"`
	RecommendedAction          string  `json:"recommended_action" gorm:"type:varchar(32);index"`
	RecommendedDurationMinutes int     `json:"recommended_duration_minutes"`
	RecommendedReason          string  `json:"recommended_reason" gorm:"type:text"`
	RecommendedUserReason      string  `json:"recommended_user_reason" gorm:"type:text"`
	WindowHours                int     `json:"window_hours"`
	WindowStart                int64   `json:"window_start" gorm:"index"`
	WindowEnd                  int64   `json:"window_end" gorm:"index"`
	RepeatCount                int     `json:"repeat_count"`
	ActionId                   int64   `json:"action_id" gorm:"index"`
	ReviewedBy                 int     `json:"reviewed_by"`
	ReviewedByName             string  `json:"reviewed_by_name" gorm:"type:varchar(64)"`
	ReviewNote                 string  `json:"review_note" gorm:"type:text"`
	ReviewedAt                 int64   `json:"reviewed_at"`
	LastSeenAt                 int64   `json:"last_seen_at" gorm:"index"`
	CreatedAt                  int64   `json:"created_at" gorm:"index"`
	UpdatedAt                  int64   `json:"updated_at" gorm:"index"`
}

func (c *RiskCase) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if c.CreatedAt == 0 {
		c.CreatedAt = now
	}
	if c.UpdatedAt == 0 {
		c.UpdatedAt = now
	}
	if c.LastSeenAt == 0 {
		c.LastSeenAt = now
	}
	if c.Status == "" {
		c.Status = RiskCaseStatusOpen
	}
	if c.RuleVerdict == "" {
		c.RuleVerdict = c.Verdict
	}
	if c.RuleRecommendedAction == "" {
		c.RuleRecommendedAction = c.RecommendedAction
	}
	if c.RuleRecommendedDuration == 0 {
		c.RuleRecommendedDuration = c.RecommendedDurationMinutes
	}
	if c.RepeatCount <= 0 {
		c.RepeatCount = 1
	}
	return nil
}

func (c *RiskCase) BeforeUpdate(_ *gorm.DB) error {
	c.UpdatedAt = common.GetTimestamp()
	return nil
}

type RiskAction struct {
	Id             int64  `json:"id" gorm:"primaryKey"`
	CaseId         int64  `json:"case_id" gorm:"index"`
	UserId         int    `json:"user_id" gorm:"index"`
	TokenId        int    `json:"token_id" gorm:"index"`
	Action         string `json:"action" gorm:"type:varchar(32);index"`
	Source         string `json:"source" gorm:"type:varchar(32);index"`
	Parameters     string `json:"parameters" gorm:"type:text"`
	Reason         string `json:"reason" gorm:"type:text"`
	UserMessage    string `json:"user_message" gorm:"type:text"`
	StartedAt      int64  `json:"started_at" gorm:"index"`
	ExpiresAt      int64  `json:"expires_at" gorm:"index"`
	Status         string `json:"status" gorm:"type:varchar(32);index"`
	OperatorUserId int    `json:"operator_user_id"`
	OperatorName   string `json:"operator_name" gorm:"type:varchar(64)"`
	CreatedAt      int64  `json:"created_at" gorm:"index"`
	UpdatedAt      int64  `json:"updated_at"`
}

func (a *RiskAction) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if a.StartedAt == 0 {
		a.StartedAt = now
	}
	if a.CreatedAt == 0 {
		a.CreatedAt = now
	}
	if a.UpdatedAt == 0 {
		a.UpdatedAt = now
	}
	if a.Status == "" {
		a.Status = RiskActionStatusActive
	}
	return nil
}

func (a *RiskAction) BeforeUpdate(_ *gorm.DB) error {
	a.UpdatedAt = common.GetTimestamp()
	return nil
}

func UpsertRiskCase(ctx context.Context, incoming *RiskCase) (*RiskCase, bool, error) {
	if incoming == nil || strings.TrimSpace(incoming.Fingerprint) == "" {
		return nil, false, errors.New("risk case fingerprint is required")
	}
	var result RiskCase
	created := false
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing RiskCase
		err := tx.Where("fingerprint = ?", incoming.Fingerprint).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			now := common.GetTimestamp()
			lastSeenAt := incoming.LastSeenAt
			if lastSeenAt <= 0 {
				lastSeenAt = now
			}
			ruleVerdict := strings.TrimSpace(incoming.RuleVerdict)
			if ruleVerdict == "" {
				ruleVerdict = incoming.Verdict
			}
			var recentSimilar int64
			if countErr := tx.Model(&RiskCase{}).
				Where("user_id = ? AND window_hours = ? AND status <> ? AND last_seen_at >= ? AND created_at <= ?", incoming.UserId, incoming.WindowHours, RiskCaseStatusDismissed, lastSeenAt-7*86400, now-60).
				Where("rule_verdict = ? OR (rule_verdict = '' AND verdict = ?)", ruleVerdict, ruleVerdict).
				Count(&recentSimilar).Error; countErr != nil {
				return countErr
			}
			incoming.RepeatCount = int(recentSimilar) + 1
			if createErr := tx.Create(incoming).Error; createErr != nil {
				return createErr
			}
			result = *incoming
			created = true
			return nil
		}
		if err != nil {
			return err
		}

		newActivity := incoming.LastSeenAt > existing.LastSeenAt
		incomingRuleVerdict := strings.TrimSpace(incoming.RuleVerdict)
		if incomingRuleVerdict == "" {
			incomingRuleVerdict = incoming.Verdict
		}
		incomingRuleAction := strings.TrimSpace(incoming.RuleRecommendedAction)
		if incomingRuleAction == "" {
			incomingRuleAction = incoming.RecommendedAction
		}
		ruleChanged := existing.RuleScore != incoming.RuleScore ||
			existing.RuleVerdict != incomingRuleVerdict ||
			existing.Signals != incoming.Signals ||
			existing.SampleRequestIds != incoming.SampleRequestIds

		existing.Username = incoming.Username
		existing.TokenName = incoming.TokenName
		existing.RuleVerdict = incomingRuleVerdict
		existing.RuleScore = incoming.RuleScore
		existing.Signals = incoming.Signals
		existing.SampleRequestIds = incoming.SampleRequestIds
		existing.RuleReason = incoming.RuleReason
		existing.RuleRecommendedAction = incomingRuleAction
		existing.RuleRecommendedDuration = incoming.RuleRecommendedDuration
		existing.WindowHours = incoming.WindowHours
		existing.WindowStart = incoming.WindowStart
		existing.WindowEnd = incoming.WindowEnd
		existing.LastSeenAt = incoming.LastSeenAt

		// A scheduler pass over identical logs is not a repeat offence. Only new
		// traffic advances RepeatCount; material rule/evidence changes merely
		// invalidate the stale Agent decision so it can be recomputed.
		if newActivity {
			existing.RepeatCount++
		}
		if newActivity || ruleChanged || strings.TrimSpace(existing.AgentResult) == "" {
			existing.Verdict = incoming.Verdict
			existing.RiskLevel = incoming.RiskLevel
			existing.FinalScore = incoming.FinalScore
			existing.Confidence = incoming.Confidence
			existing.PolicyViolation = false
			existing.RecommendedAction = incoming.RecommendedAction
			existing.RecommendedDurationMinutes = incoming.RecommendedDurationMinutes
			existing.RecommendedReason = incoming.RecommendedReason
			existing.RecommendedUserReason = ""
		}
		if newActivity || ruleChanged {
			existing.AgentScore = 0
			existing.JudgeScore = 0
			existing.AgentResult = ""
			existing.JudgeResult = ""
			existing.AgentModel = ""
			existing.JudgeModel = ""
			existing.AgentAnalyzedAt = 0
			existing.JudgeAnalyzedAt = 0
		}
		if newActivity && (existing.Status == RiskCaseStatusDismissed || existing.Status == RiskCaseStatusResolved) {
			existing.Status = RiskCaseStatusOpen
			existing.ActionId = 0
		}
		if saveErr := tx.Save(&existing).Error; saveErr != nil {
			return saveErr
		}
		result = existing
		return nil
	})
	return &result, created, err
}

type RiskCaseListFilter struct {
	UserId    int
	TokenId   int
	Status    string
	Verdict   string
	RiskLevel string
	MinScore  int
	StartTime int64
	EndTime   int64
}

func ListRiskCases(ctx context.Context, filter RiskCaseListFilter, offset, limit int) ([]*RiskCase, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	query := DB.WithContext(ctx).Model(&RiskCase{})
	if filter.UserId > 0 {
		query = query.Where("user_id = ?", filter.UserId)
	}
	if filter.TokenId > 0 {
		query = query.Where("token_id = ?", filter.TokenId)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.Verdict != "" {
		query = query.Where("verdict = ?", filter.Verdict)
	}
	if filter.RiskLevel != "" {
		query = query.Where("risk_level = ?", filter.RiskLevel)
	}
	if filter.MinScore > 0 {
		query = query.Where("final_score >= ?", filter.MinScore)
	}
	if filter.StartTime > 0 {
		query = query.Where("last_seen_at >= ?", filter.StartTime)
	}
	if filter.EndTime > 0 {
		query = query.Where("last_seen_at <= ?", filter.EndTime)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var cases []*RiskCase
	err := query.Order("last_seen_at desc, id desc").Offset(offset).Limit(limit).Find(&cases).Error
	return cases, total, err
}

func GetRiskCaseById(ctx context.Context, id int64) (*RiskCase, error) {
	if id <= 0 {
		return nil, errors.New("invalid risk case id")
	}
	var riskCase RiskCase
	if err := DB.WithContext(ctx).First(&riskCase, id).Error; err != nil {
		return nil, err
	}
	return &riskCase, nil
}

func ListRiskActionsByCase(ctx context.Context, caseId int64) ([]*RiskAction, error) {
	var actions []*RiskAction
	err := DB.WithContext(ctx).Where("case_id = ?", caseId).Order("id desc").Find(&actions).Error
	return actions, err
}

func UpdateRiskCaseAI(ctx context.Context, riskCase *RiskCase) error {
	if riskCase == nil || riskCase.Id <= 0 {
		return errors.New("invalid risk case")
	}
	result := DB.WithContext(ctx).Model(&RiskCase{}).
		Where("id = ? AND last_seen_at = ? AND rule_score = ? AND rule_verdict = ? AND signals = ? AND sample_request_ids = ? AND rule_recommended_action = ? AND rule_recommended_duration = ?",
			riskCase.Id,
			riskCase.LastSeenAt,
			riskCase.RuleScore,
			riskCase.RuleVerdict,
			riskCase.Signals,
			riskCase.SampleRequestIds,
			riskCase.RuleRecommendedAction,
			riskCase.RuleRecommendedDuration,
		).
		Updates(map[string]interface{}{
			"verdict":                      riskCase.Verdict,
			"risk_level":                   riskCase.RiskLevel,
			"agent_score":                  riskCase.AgentScore,
			"judge_score":                  riskCase.JudgeScore,
			"final_score":                  riskCase.FinalScore,
			"confidence":                   riskCase.Confidence,
			"policy_violation":             riskCase.PolicyViolation,
			"agent_result":                 riskCase.AgentResult,
			"judge_result":                 riskCase.JudgeResult,
			"agent_model":                  riskCase.AgentModel,
			"judge_model":                  riskCase.JudgeModel,
			"agent_analyzed_at":            riskCase.AgentAnalyzedAt,
			"judge_analyzed_at":            riskCase.JudgeAnalyzedAt,
			"recommended_action":           riskCase.RecommendedAction,
			"recommended_duration_minutes": riskCase.RecommendedDurationMinutes,
			"recommended_reason":           riskCase.RecommendedReason,
			"recommended_user_reason":      riskCase.RecommendedUserReason,
			"updated_at":                   common.GetTimestamp(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}

	// MySQL may report zero rows for a no-op update. Re-read the immutable
	// evidence identity before treating it as a conflict, while still rejecting
	// a stale Agent result if a scanner pass changed the case mid-analysis.
	var current RiskCase
	if err := DB.WithContext(ctx).
		Select("id, last_seen_at, rule_score, rule_verdict, signals, sample_request_ids, rule_recommended_action, rule_recommended_duration").
		First(&current, riskCase.Id).Error; err != nil {
		return err
	}
	if current.LastSeenAt != riskCase.LastSeenAt ||
		current.RuleScore != riskCase.RuleScore ||
		current.RuleVerdict != riskCase.RuleVerdict ||
		current.Signals != riskCase.Signals ||
		current.SampleRequestIds != riskCase.SampleRequestIds ||
		current.RuleRecommendedAction != riskCase.RuleRecommendedAction ||
		current.RuleRecommendedDuration != riskCase.RuleRecommendedDuration {
		return ErrRiskCaseEvidenceChanged
	}
	return nil
}

func ListRecentRiskCasesByUser(ctx context.Context, userId int, excludeId int64, limit int) ([]*RiskCase, error) {
	if userId <= 0 {
		return []*RiskCase{}, nil
	}
	if limit <= 0 || limit > 20 {
		limit = 8
	}
	var cases []*RiskCase
	err := DB.WithContext(ctx).Model(&RiskCase{}).
		Select("id, token_id, status, verdict, rule_verdict, risk_level, rule_score, agent_score, judge_score, final_score, confidence, policy_violation, recommended_action, repeat_count, action_id, last_seen_at, created_at").
		Where("user_id = ? AND id <> ?", userId, excludeId).
		Order("last_seen_at desc, id desc").
		Limit(limit).
		Find(&cases).Error
	return cases, err
}

func ListRecentRiskActionsByUser(ctx context.Context, userId int, limit int) ([]*RiskAction, error) {
	if userId <= 0 {
		return []*RiskAction{}, nil
	}
	if limit <= 0 || limit > 20 {
		limit = 8
	}
	var actions []*RiskAction
	err := DB.WithContext(ctx).Model(&RiskAction{}).
		Select("id, case_id, token_id, action, source, reason, started_at, expires_at, status, created_at").
		Where("user_id = ?", userId).
		Order("id desc").
		Limit(limit).
		Find(&actions).Error
	return actions, err
}

func HasActiveExpiringRiskActions() bool {
	if DB == nil {
		return false
	}
	var count int64
	err := DB.Model(&RiskAction{}).
		Where("status = ? AND expires_at > 0", RiskActionStatusActive).
		Count(&count).Error
	return err == nil && count > 0
}

func ReviewRiskCase(ctx context.Context, id int64, status string, reviewerId int, reviewerName, note string) error {
	if id <= 0 {
		return errors.New("invalid risk case id")
	}
	status = strings.TrimSpace(status)
	if status != RiskCaseStatusReviewing && status != RiskCaseStatusResolved && status != RiskCaseStatusDismissed && status != RiskCaseStatusOpen {
		return errors.New("invalid risk case status")
	}
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var riskCase RiskCase
		if err := tx.Select("id", "action_id").First(&riskCase, id).Error; err != nil {
			return err
		}
		if riskCase.ActionId > 0 {
			var activeActionCount int64
			if err := tx.Model(&RiskAction{}).
				Where("id = ? AND status = ?", riskCase.ActionId, RiskActionStatusActive).
				Count(&activeActionCount).Error; err != nil {
				return err
			}
			if activeActionCount > 0 {
				return errors.New("case has an active risk action; clear it or wait for expiry before changing review status")
			}
		}
		updates := map[string]interface{}{
			"status":           status,
			"reviewed_by":      reviewerId,
			"reviewed_by_name": strings.TrimSpace(reviewerName),
			"review_note":      strings.TrimSpace(note),
			"reviewed_at":      common.GetTimestamp(),
			"updated_at":       common.GetTimestamp(),
		}
		if status == RiskCaseStatusOpen {
			updates["action_id"] = 0
		}
		result := tx.Model(&RiskCase{}).Where("id = ? AND action_id = ?", id, riskCase.ActionId).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("risk case changed while review was being saved")
		}
		return nil
	})
}

func ResolveStaleOpenRiskCases(ctx context.Context, now int64) (int64, error) {
	if now <= 0 {
		return 0, nil
	}
	result := DB.WithContext(ctx).Model(&RiskCase{}).
		Where("status = ? AND action_id = 0 AND (last_seen_at + window_hours * ?) < ?", RiskCaseStatusOpen, int64(3600), now).
		Updates(map[string]interface{}{
			"status":     RiskCaseStatusResolved,
			"updated_at": common.GetTimestamp(),
		})
	return result.RowsAffected, result.Error
}
