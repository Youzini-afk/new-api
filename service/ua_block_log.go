package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"
)

const uaBlockAuditWriteTimeout = 2 * time.Second

// UABlockLogCreateInput captures the data needed to persist a UA interception
// record. The relay chain writes these records via BuildUABlockedErrorAndRecord,
// which also runs local auto-ban when the rule is configured for it. No ban_sync.
type UABlockLogCreateInput struct {
	UserId            int
	Username          string
	IP                string
	UserAgent         string
	RequestHeadersRaw string
	RequestParamsRaw  string
	RulePattern       string
	RuleMessage       string
	ErrorCode         string
	HTTPStatusCode    int
	RequestPath       string
	IsEmptyUA         bool
	AutoBanConfigured bool
	AutoBanned        bool
	BanReason         string
	MatchedAt         int64
}

func createUABlockLogRecord(ctx context.Context, input UABlockLogCreateInput) (int, error) {
	record := &model.UABlockLog{
		UserId:            input.UserId,
		Username:          input.Username,
		Ip:                input.IP,
		UserAgent:         input.UserAgent,
		RequestHeadersRaw: input.RequestHeadersRaw,
		RequestParamsRaw:  input.RequestParamsRaw,
		RulePattern:       input.RulePattern,
		RuleMessage:       input.RuleMessage,
		ErrorCode:         input.ErrorCode,
		HTTPStatusCode:    input.HTTPStatusCode,
		RequestPath:       input.RequestPath,
		IsEmptyUA:         input.IsEmptyUA,
		AutoBanConfigured: input.AutoBanConfigured,
		AutoBanned:        input.AutoBanned,
		BanReason:         input.BanReason,
		MatchedAt:         input.MatchedAt,
	}
	if err := model.CreateUABlockLog(ctx, record); err != nil {
		return 0, err
	}
	return record.Id, nil
}

// CreateUABlockLog persists a UA interception record. No auto-ban, no ban_sync.
func CreateUABlockLog(ctx context.Context, input UABlockLogCreateInput) error {
	_, err := createUABlockLogRecord(ctx, input)
	return err
}

type uaBlockLogPersistence struct {
	LogId    int
	Degraded bool
	Err      error
}

func createUABlockLogWithTimeout(baseCtx context.Context, input UABlockLogCreateInput) (int, error) {
	ctx, cancel := context.WithTimeout(baseCtx, uaBlockAuditWriteTimeout)
	defer cancel()
	return createUABlockLogRecord(ctx, input)
}

// persistUABlockLogForIntercept writes a UA audit independently from client
// cancellation. If the full raw-request record fails, it retries once with the
// core UA evidence only. Audit failure is reported to the caller but never
// affects the blocking response or the durable user ban.
func persistUABlockLogForIntercept(requestCtx context.Context, input UABlockLogCreateInput) uaBlockLogPersistence {
	baseCtx := context.Background()
	if requestCtx != nil {
		baseCtx = context.WithoutCancel(requestCtx)
	}

	logId, err := createUABlockLogWithTimeout(baseCtx, input)
	if err == nil {
		return uaBlockLogPersistence{LogId: logId}
	}

	fullErr := err
	input.RequestHeadersRaw = ""
	input.RequestParamsRaw = ""
	logId, err = createUABlockLogWithTimeout(baseCtx, input)
	if err == nil {
		return uaBlockLogPersistence{LogId: logId, Degraded: true, Err: fullErr}
	}

	return uaBlockLogPersistence{Err: errors.Join(fullErr, err)}
}

// UABlockLogListFilter captures the query parameters accepted by the admin
// ua-block-logs list endpoint.
type UABlockLogListFilter struct {
	UserId        int
	Username      string
	IP            string
	UserAgent     string
	RulePattern   string
	RequestPath   string
	ErrorCode     string
	IsEmptyUA     *bool
	AutoBanned    *bool
	StartTime     int64
	EndTime       int64
	StatusCodeMin int
	StatusCodeMax int
}

// UABlockLogItem is the list DTO for a UA interception record.
type UABlockLogItem struct {
	Id                int    `json:"id"`
	UserId            int    `json:"user_id"`
	Username          string `json:"username"`
	DisplayName       string `json:"display_name"`
	Remark            string `json:"remark"`
	Ip                string `json:"ip"`
	UserAgent         string `json:"user_agent"`
	RulePattern       string `json:"rule_pattern"`
	RuleMessage       string `json:"rule_message"`
	ErrorCode         string `json:"error_code"`
	HTTPStatusCode    int    `json:"http_status_code"`
	RequestPath       string `json:"request_path"`
	IsEmptyUA         bool   `json:"is_empty_ua"`
	AutoBanConfigured bool   `json:"auto_ban_configured"`
	AutoBanned        bool   `json:"auto_banned"`
	BanReason         string `json:"ban_reason"`
	MatchedAt         int64  `json:"matched_at"`
	CreatedAt         int64  `json:"created_at"`
	UpdatedAt         int64  `json:"updated_at"`
}

// UABlockLogDetail is the detail DTO; unlike the list item it includes the raw
// request headers / params for admin inspection.
type UABlockLogDetail struct {
	Id                int    `json:"id"`
	UserId            int    `json:"user_id"`
	Username          string `json:"username"`
	DisplayName       string `json:"display_name"`
	Remark            string `json:"remark"`
	Ip                string `json:"ip"`
	UserAgent         string `json:"user_agent"`
	RequestHeadersRaw string `json:"request_headers_raw"`
	RequestParamsRaw  string `json:"request_params_raw"`
	RulePattern       string `json:"rule_pattern"`
	RuleMessage       string `json:"rule_message"`
	ErrorCode         string `json:"error_code"`
	HTTPStatusCode    int    `json:"http_status_code"`
	RequestPath       string `json:"request_path"`
	IsEmptyUA         bool   `json:"is_empty_ua"`
	AutoBanConfigured bool   `json:"auto_ban_configured"`
	AutoBanned        bool   `json:"auto_banned"`
	BanReason         string `json:"ban_reason"`
	MatchedAt         int64  `json:"matched_at"`
	CreatedAt         int64  `json:"created_at"`
	UpdatedAt         int64  `json:"updated_at"`
}

// GetUABlockLogDetail returns a single UA interception record with raw request
// headers/params, enriched with the target user's display_name/remark.
func GetUABlockLogDetail(ctx context.Context, logId int) (*UABlockLogDetail, error) {
	if logId <= 0 {
		return nil, errors.New("invalid log id")
	}
	var record model.UABlockLog
	if err := model.DB.WithContext(ctx).First(&record, logId).Error; err != nil {
		return nil, err
	}
	detail := &UABlockLogDetail{
		Id:                record.Id,
		UserId:            record.UserId,
		Username:          record.Username,
		Ip:                record.Ip,
		UserAgent:         record.UserAgent,
		RequestHeadersRaw: record.RequestHeadersRaw,
		RequestParamsRaw:  record.RequestParamsRaw,
		RulePattern:       record.RulePattern,
		RuleMessage:       record.RuleMessage,
		ErrorCode:         record.ErrorCode,
		HTTPStatusCode:    record.HTTPStatusCode,
		RequestPath:       record.RequestPath,
		IsEmptyUA:         record.IsEmptyUA,
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

// ListUABlockLogs returns a paginated, filtered list of UA interception records
// enriched with the target user's display_name/remark.
func ListUABlockLogs(ctx context.Context, filter UABlockLogListFilter, startIdx int, pageSize int) (items []UABlockLogItem, total int64, err error) {
	if pageSize <= 0 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}
	if startIdx < 0 {
		startIdx = 0
	}

	query := model.DB.WithContext(ctx).Model(&model.UABlockLog{})
	if filter.UserId > 0 {
		query = query.Where("user_id = ?", filter.UserId)
	}
	if strings.TrimSpace(filter.Username) != "" {
		query = query.Where("username = ?", strings.TrimSpace(filter.Username))
	}
	if strings.TrimSpace(filter.IP) != "" {
		query = query.Where("ip = ?", strings.TrimSpace(filter.IP))
	}
	if strings.TrimSpace(filter.UserAgent) != "" {
		query = query.Where("user_agent LIKE ?", "%"+strings.TrimSpace(filter.UserAgent)+"%")
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
	if filter.IsEmptyUA != nil {
		query = query.Where("is_empty_ua = ?", *filter.IsEmptyUA)
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

	var records []model.UABlockLog
	if err = query.Order("matched_at desc, id desc").
		Limit(pageSize).
		Offset(startIdx).
		Find(&records).Error; err != nil {
		return nil, 0, err
	}
	if len(records) == 0 {
		return []UABlockLogItem{}, total, nil
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

	items = make([]UABlockLogItem, 0, len(records))
	for _, record := range records {
		meta := userMap[record.UserId]
		items = append(items, UABlockLogItem{
			Id:                record.Id,
			UserId:            record.UserId,
			Username:          record.Username,
			DisplayName:       meta.DisplayName,
			Remark:            meta.Remark,
			Ip:                record.Ip,
			UserAgent:         record.UserAgent,
			RulePattern:       record.RulePattern,
			RuleMessage:       record.RuleMessage,
			ErrorCode:         record.ErrorCode,
			HTTPStatusCode:    record.HTTPStatusCode,
			RequestPath:       record.RequestPath,
			IsEmptyUA:         record.IsEmptyUA,
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

// AppendUABlockLogRemark persists an admin remark for a UA block log by
// appending it to the target user's remark field. No banning, no ban_sync.
func AppendUABlockLogRemark(ctx context.Context, logId int, operatorUserId int, operatorName string, remark string) error {
	remark = strings.TrimSpace(remark)
	if logId <= 0 {
		return errors.New("invalid log id")
	}
	if remark == "" {
		return errors.New("remark is empty")
	}
	var record model.UABlockLog
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

type UAAutoBanEvidence struct {
	RuleName    string
	RulePattern string
	UserAgent   string
	ClientIP    string
	RequestPath string
}

func truncateUAAutoBanValue(value string, maxRunes int) string {
	value = strings.TrimSpace(strings.ToValidUTF8(value, ""))
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "…"
}

// buildUAAutoBanReason keeps the durable user remark concise while preserving
// the most useful evidence: the rule identity and the exact incoming UA. The
// full rule pattern remains available in the UA audit and admin-only log data.
func buildUAAutoBanReason(evidence UAAutoBanEvidence) string {
	rule := strings.TrimSpace(evidence.RuleName)
	if rule == "" {
		rule = strings.TrimSpace(evidence.RulePattern)
	}
	if rule == "" {
		rule = "未命名规则"
	}
	rule = truncateUAAutoBanValue(rule, 64)

	userAgent := truncateUAAutoBanValue(evidence.UserAgent, 140)
	if userAgent == "" {
		userAgent = "<空>"
	}
	return fmt.Sprintf("UA 拦截自动封禁：%s；原始 UA：%s", rule, userAgent)
}

// AutoBanUserForUABlockWithIP performs the LOCAL auto-ban for a UA regex /
// empty-UA hit: bans user + disables tokens and marks the client IP suspicious.
// Admin/root protected. The caller records the admin audit after the UA log
// persistence attempt so it can include the resulting UA log ID/status.
func AutoBanUserForUABlockWithIP(ctx context.Context, userId int, evidence UAAutoBanEvidence) (bool, string, error) {
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
	reason := buildUAAutoBanReason(evidence)
	markCtx := context.Background()
	if ctx != nil {
		markCtx = context.WithoutCancel(ctx)
	}
	trimmedIP := strings.TrimSpace(evidence.ClientIP)
	sourceLabel := "ua_auto_ban"
	banContext := fmt.Sprintf(
		"source=%s; rule=%s; pattern=%s; ua=%s; path=%s",
		sourceLabel,
		strings.TrimSpace(evidence.RuleName),
		strings.TrimSpace(evidence.RulePattern),
		strings.TrimSpace(evidence.UserAgent),
		strings.TrimSpace(evidence.RequestPath),
	)
	markContext := reason
	if user.Status == common.UserStatusDisabled {
		reason = reason + "（已处于封禁状态）"
		if _, appendErr := AppendUserRemarkLine(userId, reason); appendErr != nil {
			common.SysLog("append ua autoban remark failed: " + appendErr.Error())
		}
		if err := model.DisableAllUserTokens(userId); err != nil {
			return false, reason, err
		}
		// Already-disabled users still get a suspicious-IP mark.
		markSuspiciousIP(markCtx, user.Id, user.Username, trimmedIP, sourceLabel, markContext, banContext, reason)
		return false, reason, nil
	}
	if err := BanUserAndDisableTokens(user, reason); err != nil {
		return false, "", err
	}
	markSuspiciousIP(markCtx, user.Id, user.Username, trimmedIP, sourceLabel, markContext, banContext, reason)
	return true, reason, nil
}

func recordUAAutoBanManageLog(userId int, reason string, evidence UAAutoBanEvidence, persistence uaBlockLogPersistence) {
	adminInfo := map[string]interface{}{
		"ua_auto_ban": map[string]interface{}{
			"rule_name":        strings.TrimSpace(evidence.RuleName),
			"rule_pattern":     strings.TrimSpace(evidence.RulePattern),
			"user_agent":       strings.TrimSpace(strings.ToValidUTF8(evidence.UserAgent, "")),
			"client_ip":        strings.TrimSpace(evidence.ClientIP),
			"request_path":     strings.TrimSpace(evidence.RequestPath),
			"is_empty_ua":      strings.TrimSpace(evidence.UserAgent) == "",
			"ua_block_log_id":  persistence.LogId,
			"ua_log_persisted": persistence.LogId > 0,
			"ua_log_degraded":  persistence.Degraded,
		},
	}
	model.RecordLogWithAdminInfo(userId, model.LogTypeManage, reason, adminInfo)
}

// UABlockRecordContext is the minimal interface the relay controller exposes so
// the service can record a UA block log + run local auto-ban without taking a
// *gin.Context. Raw-body reads go through BuildRawRequestParamsForInterceptLog.
type UABlockRecordContext interface {
	RequestContext() context.Context
	UserID() int
	Username() string
	ClientIP() string
	RequestPath() string
	UserAgent() string
	RequestHeadersRaw() string
	RequestParamsRaw() string
}

// BuildUABlockedErrorAndRecord derives the (status, code, message) triple for
// a blocked UA, runs local auto-ban when configured, and persists a UABlockLog
// (with masked raw headers/params + the normalized incoming UA). No ban_sync /
// AutoBanSync.
func BuildUABlockedErrorAndRecord(c UABlockRecordContext, hit *SensitiveRuleHit) (status int, code types.ErrorCode, errMsg error) {
	fallbackMessage := setting.SensitiveUABlockedMessage
	statusCode, errorCode, messageErr := BuildSensitiveBlockedError(hit, fallbackMessage)
	if c == nil {
		return statusCode, errorCode, messageErr
	}

	pattern := ""
	ruleMessage := fallbackMessage
	ruleName := ""
	if hit != nil {
		pattern = strings.TrimSpace(hit.Pattern)
		ruleName = strings.TrimSpace(hit.RuleName)
		if strings.TrimSpace(hit.Message) != "" {
			ruleMessage = strings.TrimSpace(hit.Message)
		}
	}
	userAgent := strings.TrimSpace(strings.ToValidUTF8(c.UserAgent(), ""))
	evidence := UAAutoBanEvidence{
		RuleName:    ruleName,
		RulePattern: pattern,
		UserAgent:   userAgent,
		ClientIP:    c.ClientIP(),
		RequestPath: c.RequestPath(),
	}
	requestHeadersRaw := c.RequestHeadersRaw()
	requestParamsRaw := c.RequestParamsRaw()
	matchedAt := common.GetTimestamp()

	// Local auto-ban: for UA regex-rule hits, AutoBan on the hit; for empty-UA
	// synthetic hits, the global CheckSensitiveOnEmptyUAAutoBanEnabled flag.
	autoBanConfigured := false
	if hit != nil {
		if strings.EqualFold(strings.TrimSpace(hit.MatchMode), "empty_ua") {
			autoBanConfigured = setting.CheckSensitiveOnEmptyUAAutoBanEnabled
		} else {
			autoBanConfigured = hit.AutoBan
		}
	}
	autoBanned := false
	banReason := ""
	if autoBanConfigured {
		banned, reason, err := AutoBanUserForUABlockWithIP(c.RequestContext(), c.UserID(), evidence)
		if err != nil {
			common.SysLog("ua auto ban failed: " + err.Error())
		} else {
			autoBanned = banned
			banReason = reason
		}
	}

	persistence := persistUABlockLogForIntercept(c.RequestContext(), UABlockLogCreateInput{
		UserId:            c.UserID(),
		Username:          c.Username(),
		IP:                evidence.ClientIP,
		UserAgent:         userAgent,
		RequestHeadersRaw: requestHeadersRaw,
		RequestParamsRaw:  requestParamsRaw,
		RulePattern:       pattern,
		RuleMessage:       ruleMessage,
		ErrorCode:         string(errorCode),
		HTTPStatusCode:    statusCode,
		RequestPath:       evidence.RequestPath,
		IsEmptyUA:         strings.TrimSpace(userAgent) == "",
		AutoBanConfigured: autoBanConfigured,
		AutoBanned:        autoBanned,
		BanReason:         banReason,
		MatchedAt:         matchedAt,
	})
	if persistence.Err != nil {
		if persistence.LogId > 0 {
			common.SysLog("create full ua block log failed; stored core evidence only: " + persistence.Err.Error())
		} else {
			common.SysLog("create ua block log failed after fallback: " + persistence.Err.Error())
		}
	}
	if autoBanned {
		recordUAAutoBanManageLog(c.UserID(), banReason, evidence, persistence)
	}

	return statusCode, errorCode, messageErr
}

// keep setting + types imports referenced.
var _ = setting.CheckSensitiveOnEmptyUAAutoBanEnabled
var _ = types.ErrorCodeSensitiveWordsDetected
