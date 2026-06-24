package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"
)

// PromptBlockLogCreateInput captures the data needed to persist a prompt
// interception record. The relay chain writes these records via
// BuildPromptBlockedErrorAndRecord, which also runs local auto-ban when the
// rule is configured for it. No ban_sync.
type PromptBlockLogCreateInput struct {
	UserId            int
	Username          string
	IP                string
	RequestHeadersRaw string
	RequestParamsRaw  string
	RulePattern       string
	RuleMessage       string
	ErrorCode         string
	HTTPStatusCode    int
	RequestPath       string
	MatchMode         string
	AutoBanConfigured bool
	AutoBanned        bool
	BanReason         string
	MatchedAt         int64
}

// CreatePromptBlockLog persists a prompt interception record. No auto-ban,
// no ban_sync.
func CreatePromptBlockLog(ctx context.Context, input PromptBlockLogCreateInput) error {
	record := &model.PromptBlockLog{
		UserId:            input.UserId,
		Username:          input.Username,
		Ip:                input.IP,
		RequestHeadersRaw: input.RequestHeadersRaw,
		RequestParamsRaw:  input.RequestParamsRaw,
		RulePattern:       input.RulePattern,
		RuleMessage:       input.RuleMessage,
		ErrorCode:         input.ErrorCode,
		HTTPStatusCode:    input.HTTPStatusCode,
		RequestPath:       input.RequestPath,
		MatchMode:         input.MatchMode,
		AutoBanConfigured: input.AutoBanConfigured,
		AutoBanned:        input.AutoBanned,
		BanReason:         input.BanReason,
		MatchedAt:         input.MatchedAt,
	}
	return model.CreatePromptBlockLog(ctx, record)
}

// PromptBlockLogListFilter captures the query parameters accepted by the admin
// prompt-block-logs list endpoint.
type PromptBlockLogListFilter struct {
	UserId        int
	Username      string
	IP            string
	RulePattern   string
	RequestPath   string
	ErrorCode     string
	MatchMode     string
	AutoBanned    *bool
	StartTime     int64
	EndTime       int64
	StatusCodeMin int
	StatusCodeMax int
}

// PromptBlockLogItem is the list DTO for a prompt interception record.
type PromptBlockLogItem struct {
	Id                int    `json:"id"`
	UserId            int    `json:"user_id"`
	Username          string `json:"username"`
	DisplayName       string `json:"display_name"`
	Remark            string `json:"remark"`
	Ip                string `json:"ip"`
	RulePattern       string `json:"rule_pattern"`
	RuleMessage       string `json:"rule_message"`
	ErrorCode         string `json:"error_code"`
	HTTPStatusCode    int    `json:"http_status_code"`
	RequestPath       string `json:"request_path"`
	MatchMode         string `json:"match_mode"`
	AutoBanConfigured bool   `json:"auto_ban_configured"`
	AutoBanned        bool   `json:"auto_banned"`
	BanReason         string `json:"ban_reason"`
	MatchedAt         int64  `json:"matched_at"`
	CreatedAt         int64  `json:"created_at"`
	UpdatedAt         int64  `json:"updated_at"`
}

// PromptBlockLogDetail is the detail DTO; unlike the list item it includes the
// raw request headers / params for admin inspection.
type PromptBlockLogDetail struct {
	Id                int    `json:"id"`
	UserId            int    `json:"user_id"`
	Username          string `json:"username"`
	DisplayName       string `json:"display_name"`
	Remark            string `json:"remark"`
	Ip                string `json:"ip"`
	RequestHeadersRaw string `json:"request_headers_raw"`
	RequestParamsRaw  string `json:"request_params_raw"`
	RulePattern       string `json:"rule_pattern"`
	RuleMessage       string `json:"rule_message"`
	ErrorCode         string `json:"error_code"`
	HTTPStatusCode    int    `json:"http_status_code"`
	RequestPath       string `json:"request_path"`
	MatchMode         string `json:"match_mode"`
	AutoBanConfigured bool   `json:"auto_ban_configured"`
	AutoBanned        bool   `json:"auto_banned"`
	BanReason         string `json:"ban_reason"`
	MatchedAt         int64  `json:"matched_at"`
	CreatedAt         int64  `json:"created_at"`
	UpdatedAt         int64  `json:"updated_at"`
}

// GetPromptBlockLogDetail returns a single prompt interception record with raw
// request headers/params, enriched with the target user's display_name/remark.
func GetPromptBlockLogDetail(ctx context.Context, logId int) (*PromptBlockLogDetail, error) {
	if logId <= 0 {
		return nil, errors.New("invalid log id")
	}
	var record model.PromptBlockLog
	if err := model.DB.WithContext(ctx).First(&record, logId).Error; err != nil {
		return nil, err
	}
	detail := &PromptBlockLogDetail{
		Id:                record.Id,
		UserId:            record.UserId,
		Username:          record.Username,
		Ip:                record.Ip,
		RequestHeadersRaw: record.RequestHeadersRaw,
		RequestParamsRaw:  record.RequestParamsRaw,
		RulePattern:       record.RulePattern,
		RuleMessage:       record.RuleMessage,
		ErrorCode:         record.ErrorCode,
		HTTPStatusCode:    record.HTTPStatusCode,
		RequestPath:       record.RequestPath,
		MatchMode:         record.MatchMode,
		AutoBanConfigured: record.AutoBanConfigured,
		AutoBanned:        record.AutoBanned,
		BanReason:         record.BanReason,
		MatchedAt:         record.MatchedAt,
		CreatedAt:         record.CreatedAt,
		UpdatedAt:         record.UpdatedAt,
	}
	if record.UserId > 0 {
		username, displayName, remark, err := logScreeningFillUserIdentity(ctx, record.UserId)
		if err == nil {
			if strings.TrimSpace(detail.Username) == "" {
				detail.Username = username
			}
			detail.DisplayName = displayName
			detail.Remark = remark
		}
	}
	return detail, nil
}

// ListPromptBlockLogs returns a paginated, filtered list of prompt interception
// records enriched with the target user's display_name/remark.
func ListPromptBlockLogs(ctx context.Context, filter PromptBlockLogListFilter, startIdx int, pageSize int) (items []PromptBlockLogItem, total int64, err error) {
	if pageSize <= 0 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}
	if startIdx < 0 {
		startIdx = 0
	}

	query := model.DB.WithContext(ctx).Model(&model.PromptBlockLog{})
	if filter.UserId > 0 {
		query = query.Where("user_id = ?", filter.UserId)
	}
	if strings.TrimSpace(filter.Username) != "" {
		query = query.Where("username = ?", strings.TrimSpace(filter.Username))
	}
	if strings.TrimSpace(filter.IP) != "" {
		query = query.Where("ip = ?", strings.TrimSpace(filter.IP))
	}
	if strings.TrimSpace(filter.RulePattern) != "" {
		query = query.Where("rule_pattern LIKE ?", "%"+strings.TrimSpace(filter.RulePattern)+"%")
	}
	if strings.TrimSpace(filter.RequestPath) != "" {
		query = query.Where("request_path = ?", strings.TrimSpace(filter.RequestPath))
	}
	if strings.TrimSpace(filter.ErrorCode) != "" {
		query = query.Where("error_code = ?", strings.TrimSpace(filter.ErrorCode))
	}
	if strings.TrimSpace(filter.MatchMode) != "" {
		query = query.Where("match_mode = ?", strings.TrimSpace(filter.MatchMode))
	}
	if filter.AutoBanned != nil {
		query = query.Where("auto_banned = ?", *filter.AutoBanned)
	}
	if filter.StartTime > 0 {
		query = query.Where("matched_at >= ?", filter.StartTime)
	}
	if filter.EndTime > 0 {
		query = query.Where("matched_at <= ?", filter.EndTime)
	}
	if filter.StatusCodeMin > 0 {
		query = query.Where("http_status_code >= ?", filter.StatusCodeMin)
	}
	if filter.StatusCodeMax > 0 {
		query = query.Where("http_status_code <= ?", filter.StatusCodeMax)
	}

	if err = query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var records []model.PromptBlockLog
	if err = query.Order("matched_at desc, id desc").
		Limit(pageSize).
		Offset(startIdx).
		Find(&records).Error; err != nil {
		return nil, 0, err
	}
	if len(records) == 0 {
		return []PromptBlockLogItem{}, total, nil
	}

	userIds := make([]int, 0, len(records))
	for _, record := range records {
		if record.UserId > 0 {
			userIds = append(userIds, record.UserId)
		}
	}

	userMap := make(map[int]struct {
		DisplayName string
		Remark      string
	}, len(userIds))
	if len(userIds) > 0 {
		var users []struct {
			Id          int    `gorm:"column:id"`
			DisplayName string `gorm:"column:display_name"`
			Remark      string `gorm:"column:remark"`
		}
		if err = model.DB.WithContext(ctx).Table("users").
			Select("id, display_name, remark").
			Where("id IN ?", userIds).
			Find(&users).Error; err != nil {
			return nil, 0, err
		}
		for _, u := range users {
			userMap[u.Id] = struct {
				DisplayName string
				Remark      string
			}{
				DisplayName: u.DisplayName,
				Remark:      u.Remark,
			}
		}
	}

	items = make([]PromptBlockLogItem, 0, len(records))
	for _, record := range records {
		meta := userMap[record.UserId]
		items = append(items, PromptBlockLogItem{
			Id:                record.Id,
			UserId:            record.UserId,
			Username:          record.Username,
			DisplayName:       meta.DisplayName,
			Remark:            meta.Remark,
			Ip:                record.Ip,
			RulePattern:       record.RulePattern,
			RuleMessage:       record.RuleMessage,
			ErrorCode:         record.ErrorCode,
			HTTPStatusCode:    record.HTTPStatusCode,
			RequestPath:       record.RequestPath,
			MatchMode:         record.MatchMode,
			AutoBanConfigured: record.AutoBanConfigured,
			AutoBanned:        record.AutoBanned,
			BanReason:         record.BanReason,
			MatchedAt:         record.MatchedAt,
			CreatedAt:         record.CreatedAt,
			UpdatedAt:         record.UpdatedAt,
		})
	}
	return items, total, nil
}

// AppendPromptBlockLogRemark persists an admin remark for a prompt block log by
// appending it to the target user's remark field. No banning, no ban_sync.
func AppendPromptBlockLogRemark(ctx context.Context, logId int, operatorUserId int, operatorName string, remark string) error {
	remark = strings.TrimSpace(remark)
	if logId <= 0 {
		return errors.New("invalid log id")
	}
	if remark == "" {
		return errors.New("remark is empty")
	}
	var record model.PromptBlockLog
	if err := model.DB.WithContext(ctx).First(&record, logId).Error; err != nil {
		return err
	}
	if record.UserId <= 0 {
		return errors.New("user not found")
	}

	var user model.User
	if err := model.DB.WithContext(ctx).First(&user, record.UserId).Error; err != nil {
		return err
	}

	operatorDisplay := strings.TrimSpace(operatorName)
	if operatorDisplay == "" && operatorUserId > 0 {
		if username, displayName, _, err := logScreeningFillUserIdentity(ctx, operatorUserId); err == nil {
			if displayName != "" {
				operatorDisplay = displayName
			} else if username != "" {
				operatorDisplay = username
			}
		}
	}

	appendix := remark
	if operatorDisplay != "" {
		appendix = fmt.Sprintf("%s(%s)", remark, operatorDisplay)
	}
	if strings.TrimSpace(user.Remark) == "" {
		user.Remark = appendix
	} else {
		user.Remark = user.Remark + "\n" + appendix
	}
	if err := model.DB.WithContext(ctx).Model(&model.User{}).Where("id = ?", user.Id).Update("remark", user.Remark).Error; err != nil {
		return err
	}
	return model.InvalidateUserCache(user.Id)
}

// buildPromptAutoBanReason builds a human-readable local ban reason for a prompt
// regex rule hit. No ban_sync / external bot text.
func buildPromptAutoBanReason(ruleName string, pattern string) string {
	trimmedName := strings.TrimSpace(ruleName)
	trimmedPattern := strings.TrimSpace(pattern)
	if trimmedName != "" {
		if trimmedPattern != "" {
			return fmt.Sprintf("Prompt 拦截自动封禁：%s（%s）", trimmedName, trimmedPattern)
		}
		return fmt.Sprintf("Prompt 拦截自动封禁：%s", trimmedName)
	}
	if trimmedPattern != "" {
		return fmt.Sprintf("Prompt 拦截自动封禁：%s", trimmedPattern)
	}
	return "Prompt 拦截自动封禁"
}

// buildPromptAutoBanRemark builds the single remark line appended to the user
// when a prompt regex rule triggers local auto-ban.
func buildPromptAutoBanRemark(ruleName string, pattern string) string {
	trimmedName := strings.TrimSpace(ruleName)
	trimmedPattern := strings.TrimSpace(pattern)
	if trimmedName != "" {
		if trimmedPattern != "" {
			return fmt.Sprintf("[Prompt命中]%s(%s)", trimmedName, trimmedPattern)
		}
		return fmt.Sprintf("[Prompt命中]%s", trimmedName)
	}
	if trimmedPattern != "" {
		return fmt.Sprintf("[Prompt命中]%s", trimmedPattern)
	}
	return "[Prompt命中]"
}

// AutoBanUserForPromptBlock performs the LOCAL auto-ban for a prompt regex hit:
// bans the user + disables tokens, appends a remark line, records a manage log,
// and marks the client IP suspicious. Admin/root users are protected (no ban,
// no token disable, no suspicious mark). Returns (banned, reason, err):
//   - banned=true when the user was actually disabled this call;
//   - banned=false with a reason describing the existing-disabled state when
//     the user was already disabled (tokens still disabled for convergence);
//   - banned=false with empty reason when the user is admin/root or userId<=0.
//
// clientIP, when non-empty, drives a local suspicious-IP mark
// (source=prompt_auto_ban). No ban_sync / external bot integration.
func AutoBanUserForPromptBlock(ctx context.Context, userId int, ruleName string, rulePattern string, clientIP string) (bool, string, error) {
	if userId <= 0 {
		return false, "", nil
	}
	user, err := model.GetUserById(userId, true)
	if err != nil {
		return false, "", err
	}
	if user == nil || user.Id == 0 {
		return false, "", nil
	}
	if user.Role >= common.RoleAdminUser {
		return false, "", nil
	}
	reason := buildPromptAutoBanReason(ruleName, rulePattern)
	remarkLine := buildPromptAutoBanRemark(ruleName, rulePattern)
	trimmedIP := strings.TrimSpace(clientIP)
	sourceLabel := "prompt_auto_ban"
	banContext := fmt.Sprintf("source=%s; rule=%s; pattern=%s", sourceLabel, strings.TrimSpace(ruleName), strings.TrimSpace(rulePattern))
	markContext := fmt.Sprintf("Prompt 自动封禁命中：%s", strings.TrimSpace(rulePattern))
	if user.Status == common.UserStatusDisabled {
		reason = reason + "（已处于封禁状态）"
		if _, appendErr := AppendUserRemarkLine(userId, remarkLine); appendErr != nil {
			common.SysLog("append prompt autoban remark failed: " + appendErr.Error())
		}
		if err := model.DisableAllUserTokens(userId); err != nil {
			return false, reason, err
		}
		// Already-disabled users still get a suspicious-IP mark + manage log
		// for consistency (the hit is still a policy violation).
		markSuspiciousIP(ctx, user.Id, user.Username, trimmedIP, sourceLabel, markContext, banContext, reason)
		return false, reason, nil
	}
	if err := BanUserAndDisableTokens(user, reason); err != nil {
		return false, "", err
	}
	if _, appendErr := AppendUserRemarkLine(userId, remarkLine); appendErr != nil {
		common.SysLog("append prompt autoban remark failed: " + appendErr.Error())
	}
	markSuspiciousIP(ctx, user.Id, user.Username, trimmedIP, sourceLabel, markContext, banContext, reason)
	model.RecordLog(userId, model.LogTypeManage, reason)
	return true, reason, nil
}

// markSuspiciousIP is the shared suspicious-IP mark helper for the local
// auto-ban paths. It upserts a SuspiciousIPMark (atomically incrementing
// trigger_count on repeat hits). No-op when clientIP is empty. The caller
// is responsible for calling model.RecordLog separately if a manage log is
// needed (this helper does NOT record a manage log).
func markSuspiciousIP(ctx context.Context, userId int, username, clientIP, sourceLabel, markContext, banContext, reason string) {
	trimmedIP := strings.TrimSpace(clientIP)
	if trimmedIP == "" {
		return
	}
	if _, _, markErr := MarkSuspiciousIP(ctx, MarkSuspiciousIPInput{
		UserID:      userId,
		Username:    username,
		IP:          trimmedIP,
		Source:      sourceLabel,
		Context:     markContext,
		BanContext:  banContext,
		BanReason:   reason,
		TriggeredAt: common.GetTimestamp(),
	}); markErr != nil {
		common.SysLog("mark suspicious ip from " + sourceLabel + " failed: " + markErr.Error())
	}
}

// PromptBlockRecordContext is the minimal interface the relay controller
// exposes so the service can record a prompt block log + run local auto-ban
// without taking a *gin.Context (testable, no relay coupling). All raw-body
// reads go through BuildRawRequestParamsForInterceptLog which uses the
// io.Seeker body storage (5B).
type PromptBlockRecordContext interface {
	RequestContext() context.Context
	UserID() int
	Username() string
	ClientIP() string
	RequestPath() string
	RequestHeadersRaw() string
	RequestParamsRaw() string
}

// BuildPromptBlockedErrorAndRecord derives the (status, code, message) triple
// for a blocked prompt, runs local auto-ban when the rule is configured for it,
// and persists a PromptBlockLog (with raw headers/params). matchMode is "rule"
// for regex-rule hits, "basic" for plain sensitive-word hits (basic never
// auto-bans). fallbackPattern is used when hit is nil (basic path) or to pin
// the recorded pattern. No ban_sync / AutoBanSync.
func BuildPromptBlockedErrorAndRecord(c PromptBlockRecordContext, hit *SensitiveRuleHit, fallbackMessage string, matchMode string, fallbackPattern string) (status int, code types.ErrorCode, errMsg error) {
	statusCode, errorCode, messageErr := BuildSensitiveBlockedError(hit, fallbackMessage)
	if c == nil {
		return statusCode, errorCode, messageErr
	}

	pattern := strings.TrimSpace(fallbackPattern)
	ruleMessage := fallbackMessage
	if hit != nil {
		if strings.TrimSpace(hit.Pattern) != "" {
			pattern = strings.TrimSpace(hit.Pattern)
		}
		if strings.TrimSpace(hit.Message) != "" {
			ruleMessage = strings.TrimSpace(hit.Message)
		}
	}
	if pattern == "" {
		pattern = "<prompt_blocked>"
	}
	// Local auto-ban: ONLY for regex-rule hits with AutoBan=true. Basic
	// sensitive-word hits (matchMode != "rule") never auto-ban. No AutoBanSync.
	autoBanConfigured := hit != nil &&
		strings.EqualFold(strings.TrimSpace(matchMode), "rule") &&
		hit.AutoBan
	autoBanned := false
	banReason := ""
	if autoBanConfigured {
		ruleName := ""
		if hit != nil {
			ruleName = strings.TrimSpace(hit.RuleName)
		}
		banned, reason, err := AutoBanUserForPromptBlock(c.RequestContext(), c.UserID(), ruleName, pattern, c.ClientIP())
		if err != nil {
			common.SysLog("prompt auto ban failed: " + err.Error())
		} else {
			autoBanned = banned
			banReason = reason
		}
	}

	if err := CreatePromptBlockLog(c.RequestContext(), PromptBlockLogCreateInput{
		UserId:            c.UserID(),
		Username:          c.Username(),
		IP:                c.ClientIP(),
		RequestHeadersRaw: c.RequestHeadersRaw(),
		RequestParamsRaw:  c.RequestParamsRaw(),
		RulePattern:       pattern,
		RuleMessage:       ruleMessage,
		ErrorCode:         string(errorCode),
		HTTPStatusCode:    statusCode,
		RequestPath:       c.RequestPath(),
		MatchMode:         strings.TrimSpace(matchMode),
		AutoBanConfigured: autoBanConfigured,
		AutoBanned:        autoBanned,
		BanReason:         banReason,
		MatchedAt:         common.GetTimestamp(),
	}); err != nil {
		common.SysLog("create prompt block log failed: " + err.Error())
	}

	return statusCode, errorCode, messageErr
}

// keep setting + types imports referenced (used by BuildPromptBlockedErrorAndRecord).
var _ = setting.DefaultSensitiveErrorCode
var _ = types.ErrorCodeSensitiveWordsDetected
