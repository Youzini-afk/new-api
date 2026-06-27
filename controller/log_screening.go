package controller

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// logScreeningRunRequest is the optional JSON body accepted by RunLogScreening.
// `kind` selects the screening scope; absent body falls back to the query param
// or the default scope.
type logScreeningRunRequest struct {
	Kind string `json:"kind"`
}

func firstNonEmptyQuery(c *gin.Context, keys ...string) string {
	for _, key := range keys {
		value := strings.TrimSpace(c.Query(key))
		if value != "" {
			return value
		}
	}
	return ""
}

// RunLogScreening triggers a real log-screening pass: it reads configured
// rules, aggregates LOG_DB logs, writes screening records, and returns a
// summary. It does not perform banning and does not call ban_sync.
func RunLogScreening(c *gin.Context) {
	var req logScreeningRunRequest
	_ = common.UnmarshalBodyReusable(c, &req)
	kind := strings.TrimSpace(req.Kind)
	if kind == "" {
		kind = strings.TrimSpace(c.Query("kind"))
	}
	if kind == "" {
		kind = service.LogScreeningKindChatCompletions
	}

	summary, err := service.RunLogScreening(c.Request.Context(), c.GetInt("id"), c.GetString("username"), kind, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, summary)
}

// ListLogScreeningRecords returns a paginated list of screening records.
func ListLogScreeningRecords(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	userId, _ := strconv.Atoi(strings.TrimSpace(c.Query("user_id")))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	expired := parseLogScreeningBoolQuery(c.Query("expired"))

	filter := service.LogScreeningListFilter{
		UserId:      userId,
		Username:    strings.TrimSpace(c.Query("username")),
		Ip:          strings.TrimSpace(c.Query("ip")),
		RuleName:    strings.TrimSpace(c.Query("rule")),
		Window:      strings.TrimSpace(c.Query("window")),
		ParamKey:    strings.TrimSpace(c.Query("param_key")),
		UA:          strings.TrimSpace(c.Query("ua")),
		RequestPath: strings.TrimSpace(c.Query("request_path")),
		StartTime:   startTimestamp,
		EndTime:     endTimestamp,
		Expired:     expired,
	}
	if raw := strings.TrimSpace(c.Query("window_start")); raw != "" {
		if value, err := strconv.ParseInt(raw, 10, 64); err == nil {
			filter.WindowStart = value
		}
	}
	if raw := strings.TrimSpace(c.Query("window_end")); raw != "" {
		if value, err := strconv.ParseInt(raw, 10, 64); err == nil {
			filter.WindowEnd = value
		}
	}
	items, total, err := service.ListLogScreeningRecords(c.Request.Context(), filter, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

// ListUABlockLogs returns a paginated list of UA interception logs.
func ListUABlockLogs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	userId, _ := strconv.Atoi(strings.TrimSpace(c.Query("user_id")))
	startTimestamp, _ := strconv.ParseInt(strings.TrimSpace(c.Query("start_timestamp")), 10, 64)
	endTimestamp, _ := strconv.ParseInt(strings.TrimSpace(c.Query("end_timestamp")), 10, 64)
	statusCodeMin, _ := strconv.Atoi(strings.TrimSpace(c.Query("status_code_min")))
	statusCodeMax, _ := strconv.Atoi(strings.TrimSpace(c.Query("status_code_max")))

	filter := service.UABlockLogListFilter{
		UserId:        userId,
		Username:      strings.TrimSpace(c.Query("username")),
		IP:            strings.TrimSpace(c.Query("ip")),
		UserAgent:     strings.TrimSpace(firstNonEmptyQuery(c, "user_agent", "ua")),
		RulePattern:   strings.TrimSpace(c.Query("rule_pattern")),
		RequestPath:   strings.TrimSpace(c.Query("request_path")),
		ErrorCode:     strings.TrimSpace(c.Query("error_code")),
		IsEmptyUA:     parseLogScreeningBoolQuery(c.Query("is_empty_ua")),
		AutoBanned:    parseLogScreeningBoolQuery(c.Query("auto_banned")),
		StartTime:     startTimestamp,
		EndTime:       endTimestamp,
		StatusCodeMin: statusCodeMin,
		StatusCodeMax: statusCodeMax,
	}

	items, total, err := service.ListUABlockLogs(c.Request.Context(), filter, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

// ListPromptBlockLogs returns a paginated list of prompt interception logs.
func ListPromptBlockLogs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	userId, _ := strconv.Atoi(strings.TrimSpace(c.Query("user_id")))
	startTimestamp, _ := strconv.ParseInt(strings.TrimSpace(c.Query("start_timestamp")), 10, 64)
	endTimestamp, _ := strconv.ParseInt(strings.TrimSpace(c.Query("end_timestamp")), 10, 64)
	statusCodeMin, _ := strconv.Atoi(strings.TrimSpace(c.Query("status_code_min")))
	statusCodeMax, _ := strconv.Atoi(strings.TrimSpace(c.Query("status_code_max")))

	filter := service.PromptBlockLogListFilter{
		UserId:        userId,
		Username:      strings.TrimSpace(c.Query("username")),
		IP:            strings.TrimSpace(c.Query("ip")),
		RulePattern:   strings.TrimSpace(c.Query("rule_pattern")),
		RequestPath:   strings.TrimSpace(c.Query("request_path")),
		ErrorCode:     strings.TrimSpace(c.Query("error_code")),
		MatchMode:     strings.TrimSpace(c.Query("match_mode")),
		AutoBanned:    parseLogScreeningBoolQuery(c.Query("auto_banned")),
		StartTime:     startTimestamp,
		EndTime:       endTimestamp,
		StatusCodeMin: statusCodeMin,
		StatusCodeMax: statusCodeMax,
	}

	items, total, err := service.ListPromptBlockLogs(c.Request.Context(), filter, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

// logScreeningRemarkRequest is the JSON body accepted by the remark endpoints.
type logScreeningRemarkRequest struct {
	Remark string `json:"remark"`
}

// AppendLogScreeningRemark appends an admin remark to the user behind a
// screening record.
func AppendLogScreeningRemark(c *gin.Context) {
	recordId, _ := strconv.Atoi(strings.TrimSpace(c.Param("id")))
	if recordId <= 0 {
		common.ApiErrorMsg(c, "invalid record id")
		return
	}
	var req logScreeningRemarkRequest
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := service.AppendLogScreeningRemark(c.Request.Context(), recordId, c.GetInt("id"), c.GetString("username"), req.Remark); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"id": recordId})
}

// AppendUABlockLogRemark appends an admin remark to the user behind a UA block log.
func AppendUABlockLogRemark(c *gin.Context) {
	recordId, _ := strconv.Atoi(strings.TrimSpace(c.Param("id")))
	if recordId <= 0 {
		common.ApiErrorMsg(c, "invalid record id")
		return
	}
	var req logScreeningRemarkRequest
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := service.AppendUABlockLogRemark(c.Request.Context(), recordId, c.GetInt("id"), c.GetString("username"), req.Remark); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"id": recordId})
}

// AppendPromptBlockLogRemark appends an admin remark to the user behind a prompt block log.
func AppendPromptBlockLogRemark(c *gin.Context) {
	recordId, _ := strconv.Atoi(strings.TrimSpace(c.Param("id")))
	if recordId <= 0 {
		common.ApiErrorMsg(c, "invalid record id")
		return
	}
	var req logScreeningRemarkRequest
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := service.AppendPromptBlockLogRemark(c.Request.Context(), recordId, c.GetInt("id"), c.GetString("username"), req.Remark); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"id": recordId})
}

// GetUABlockLogDetail returns a single UA interception record including raw
// request headers/params.
func GetUABlockLogDetail(c *gin.Context) {
	recordId, _ := strconv.Atoi(strings.TrimSpace(c.Param("id")))
	if recordId <= 0 {
		common.ApiErrorMsg(c, "invalid record id")
		return
	}
	detail, err := service.GetUABlockLogDetail(c.Request.Context(), recordId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, detail)
}

// GetPromptBlockLogDetail returns a single prompt interception record including
// raw request headers/params.
func GetPromptBlockLogDetail(c *gin.Context) {
	recordId, _ := strconv.Atoi(strings.TrimSpace(c.Param("id")))
	if recordId <= 0 {
		common.ApiErrorMsg(c, "invalid record id")
		return
	}
	detail, err := service.GetPromptBlockLogDetail(c.Request.Context(), recordId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, detail)
}

// CleanupLogScreeningRecords deletes expired screening records in bounded
// batches. `now` and `limit` are optional query overrides.
func CleanupLogScreeningRecords(c *gin.Context) {
	now, _ := strconv.ParseInt(c.Query("now"), 10, 64)
	limit, _ := strconv.Atoi(c.Query("limit"))
	count, err := service.DeleteExpiredLogScreeningRecords(c.Request.Context(), now, limit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"deleted": count})
}

// parseLogScreeningBoolQuery interprets the common tristate query values
// ("", "1"/"true", "0"/"false") as a *bool. An empty value yields nil (no
// filter). Used by the screening list endpoints.
func parseLogScreeningBoolQuery(raw string) *bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	v := raw == "1" || strings.EqualFold(raw, "true")
	return &v
}
