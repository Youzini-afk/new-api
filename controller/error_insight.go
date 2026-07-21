package controller

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// ============================================================================
// Error Insight — admin-only endpoints for governance-classified error
// analytics. Provides summary, signature aggregation, log list, and
// signature deletion. Regular users never access these.
// ============================================================================

func GetErrorInsightSummary(c *gin.Context) {
	var params model.ErrorLogsListParams
	if err := c.ShouldBindQuery(&params); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "invalid query parameters",
		})
		return
	}
	summary, err := model.GetErrorLogSummary(&params)
	if err != nil {
		common.SysError("failed to get error insight summary: " + err.Error())
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "failed to get summary",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    summary,
	})
}

func GetErrorInsightSignatures(c *gin.Context) {
	var params model.ErrorLogsListParams
	if err := c.ShouldBindQuery(&params); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "invalid query parameters",
		})
		return
	}
	signatures, err := model.GetErrorLogSignatures(&params)
	if err != nil {
		common.SysError("failed to get error insight signatures: " + err.Error())
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "failed to get signatures",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    signatures,
	})
}

func GetErrorInsightLogs(c *gin.Context) {
	var params model.ErrorLogsListParams
	if err := c.ShouldBindQuery(&params); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "invalid query parameters",
		})
		return
	}
	logs, total, err := model.GetErrorLogList(&params)
	if err != nil {
		common.SysError("failed to get error insight logs: " + err.Error())
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "failed to get logs",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"logs":  logs,
			"total": total,
		},
	})
}

func DeleteErrorInsightSignature(c *gin.Context) {
	signature := c.Param("signature")
	if !model.ValidateNormalizedSignature(signature) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "invalid signature",
		})
		return
	}
	deleted, err := model.DeleteErrorLogsBySignature(signature)
	if err != nil {
		common.SysError("failed to delete error insight signature: " + err.Error())
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "failed to delete signature",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"deleted": deleted,
		},
	})
}

func GetErrorInsightAISetting(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    system_setting.GetErrorInsightAISetting(),
	})
}

func SaveErrorInsightAISetting(c *gin.Context) {
	var req system_setting.ErrorInsightAISetting
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "invalid request body"})
		return
	}
	req = system_setting.NormalizeErrorInsightAISetting(req)
	if req.Enabled && req.ChannelID <= 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "channel is required when AI generation is enabled"})
		return
	}
	if req.Enabled && strings.TrimSpace(req.Model) == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "model is required when AI generation is enabled"})
		return
	}
	if !json.Valid(req.JSONOutputParams) {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "json output params must be valid JSON"})
		return
	}
	values, err := errorInsightAISettingToOptions(req)
	if err != nil {
		common.SysError("failed to encode error insight ai setting: " + err.Error())
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "failed to save setting"})
		return
	}
	if err := model.UpdateOptionsBulk(values); err != nil {
		common.SysError("failed to save error insight ai setting: " + err.Error())
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": system_setting.GetErrorInsightAISetting()})
}

func GetErrorGovernanceAISetting(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    system_setting.GetErrorGovernanceAISetting(),
	})
}

func SaveErrorGovernanceAISetting(c *gin.Context) {
	var req system_setting.ErrorGovernanceAISetting
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "invalid request body"})
		return
	}
	req = system_setting.NormalizeErrorGovernanceAISetting(req)
	if req.Enabled && req.ChannelID <= 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "channel is required when AI governance is enabled"})
		return
	}
	if req.Enabled && strings.TrimSpace(req.Model) == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "model is required when AI governance is enabled"})
		return
	}
	if !json.Valid(req.JSONOutputParams) {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "json output params must be valid JSON"})
		return
	}
	values, err := errorGovernanceAISettingToOptions(req)
	if err != nil {
		common.SysError("failed to encode error governance ai setting: " + err.Error())
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "failed to save setting"})
		return
	}
	if err := model.UpdateOptionsBulk(values); err != nil {
		common.SysError("failed to save error governance ai setting: " + err.Error())
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": system_setting.GetErrorGovernanceAISetting()})
}

func GenerateErrorGovernanceAIOrganization(c *gin.Context) {
	var req ErrorGovernanceAIOrganizeRequest
	if err := c.ShouldBindJSON(&req); err != nil && err != io.EOF {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "invalid request body"})
		return
	}
	cfg := system_setting.GetErrorGovernanceAISetting()
	if !cfg.Enabled {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "AI governance is disabled"})
		return
	}
	if cfg.ChannelID <= 0 || strings.TrimSpace(cfg.Model) == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "AI channel and model are required"})
		return
	}
	governanceCfg := system_setting.GetRelayErrorGovernanceSetting()
	if req.GovernanceConfig != nil {
		governanceCfg = req.GovernanceConfig
	}
	if governanceCfg == nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "failed to load governance setting"})
		return
	}
	content, err := generateErrorGovernanceOrganizationWithAI(c, cfg, governanceCfg)
	if err != nil {
		common.SysError("failed to generate error governance ai organization: " + err.Error())
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	result, err := parseErrorGovernanceAIOrganization(content)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error(), "data": gin.H{"raw": content}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": result})
}

type ErrorInsightAIGenerateRequest struct {
	Signature string `json:"signature"`
}

type SaveErrorInsightCustomAIRuleRequest struct {
	Signature string                       `json:"signature"`
	Rule      ErrorInsightAIRuleSuggestion `json:"rule"`
}

type ErrorGovernanceAIOrganizeRequest struct {
	GovernanceConfig *system_setting.RelayErrorGovernanceSetting `json:"governance_config"`
}

type ErrorInsightAIResultResponse struct {
	Rules []ErrorInsightAIRuleSuggestion `json:"rules"`
	Raw   json.RawMessage                `json:"raw"`
}

type ErrorGovernanceAIOrganizeResult struct {
	Summary string                                                `json:"summary"`
	Rules   []system_setting.RelayErrorGovernanceCustomRuleConfig `json:"rules"`
	Raw     json.RawMessage                                       `json:"raw"`
}

func GenerateErrorInsightAIRules(c *gin.Context) {
	var req ErrorInsightAIGenerateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "invalid request body"})
		return
	}
	req.Signature = strings.TrimSpace(req.Signature)
	if !model.ValidateNormalizedSignature(req.Signature) {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "invalid signature"})
		return
	}
	cfg := system_setting.GetErrorInsightAISetting()
	if !cfg.Enabled {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "AI generation is disabled"})
		return
	}
	if cfg.ChannelID <= 0 || strings.TrimSpace(cfg.Model) == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "AI channel and model are required"})
		return
	}
	params := model.ErrorLogsListParams{NormalizedSignature: req.Signature, Page: 1, PageSize: cfg.SampleSize}
	logs, _, err := model.GetErrorLogList(&params)
	if err != nil {
		common.SysError("failed to load error insight samples: " + err.Error())
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "failed to load sample logs"})
		return
	}
	if len(logs) == 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "no sample logs found"})
		return
	}
	content, err := generateErrorInsightRulesWithAI(c, cfg, req.Signature, logs)
	if err != nil {
		common.SysError("failed to generate error insight ai rules: " + err.Error())
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	rules, raw, err := parseErrorInsightAISuggestions(content)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error(), "data": gin.H{"raw": content}})
		return
	}
	rulesValue, err := json.Marshal(rules)
	if err != nil {
		common.SysError("failed to encode error insight ai rules: " + err.Error())
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "failed to save generated rules"})
		return
	}
	if err := model.UpsertErrorInsightAIResult(c.Request.Context(), req.Signature, c.GetInt("id"), string(rulesValue), string(raw)); err != nil {
		common.SysError("failed to persist error insight ai result: " + err.Error())
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "failed to save generated rules"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"rules": rules, "raw": raw}})
}

func GetErrorInsightAIResult(c *gin.Context) {
	signature := strings.TrimSpace(c.Param("signature"))
	if !model.ValidateNormalizedSignature(signature) {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "invalid signature"})
		return
	}
	result, err := model.GetErrorInsightAIResult(c.Request.Context(), signature)
	if err != nil {
		common.SysError("failed to get error insight ai result: " + err.Error())
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "failed to load generated rules"})
		return
	}
	if result == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": nil})
		return
	}
	var rules []ErrorInsightAIRuleSuggestion
	if strings.TrimSpace(result.Rules) != "" {
		if err := json.Unmarshal([]byte(result.Rules), &rules); err != nil {
			common.SysError("failed to decode error insight ai rules: " + err.Error())
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "failed to load generated rules"})
			return
		}
	}
	raw := json.RawMessage([]byte("null"))
	if strings.TrimSpace(result.Raw) != "" && json.Valid([]byte(result.Raw)) {
		raw = json.RawMessage(result.Raw)
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": ErrorInsightAIResultResponse{Rules: rules, Raw: raw}})
}

func SaveErrorInsightCustomAIRule(c *gin.Context) {
	var req SaveErrorInsightCustomAIRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "invalid request body"})
		return
	}
	rule, err := normalizeErrorInsightCustomRule(req.Rule)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	cfg := system_setting.GetRelayErrorGovernanceSetting()
	if cfg == nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "failed to load governance setting"})
		return
	}
	customRules := make([]system_setting.RelayErrorGovernanceCustomRuleConfig, 0, len(cfg.CustomRules)+1)
	updated := false
	for _, existing := range cfg.CustomRules {
		if existing.RuleCode == rule.RuleCode {
			if !updated {
				customRules = append(customRules, rule)
				updated = true
			}
			continue
		}
		customRules = append(customRules, existing)
	}
	if !updated {
		customRules = append(customRules, rule)
	}
	merged := system_setting.RelayErrorGovernanceSetting{
		Enabled:     cfg.Enabled,
		Rules:       cfg.Rules,
		CustomRules: customRules,
	}
	normalized, err := model.UpdateRelayErrorGovernanceSetting(merged)
	if err != nil {
		common.SysError("failed to save error insight custom rule: " + err.Error())
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	for _, savedRule := range normalized.CustomRules {
		if savedRule.RuleCode == rule.RuleCode {
			rule = savedRule
			break
		}
	}
	if signature := strings.TrimSpace(req.Signature); model.ValidateNormalizedSignature(signature) {
		if err := model.MarkErrorInsightAIResultApproved(c.Request.Context(), signature); err != nil {
			common.SysError("failed to mark error insight ai result approved: " + err.Error())
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": rule})
}

// parseErrorInsightPageParam is a small helper for backward-compatible page
// parsing from query strings. Returns page (default 1) and pageSize (default 20, max 100).
func parseErrorInsightPageParam(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page <= 0 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	return page, pageSize
}

func errorInsightAISettingToOptions(setting system_setting.ErrorInsightAISetting) (map[string]string, error) {
	jsonParams := json.RawMessage([]byte("{}"))
	if len(setting.JSONOutputParams) > 0 {
		jsonParams = setting.JSONOutputParams
	}
	if !json.Valid(jsonParams) {
		return nil, errors.New("invalid json output params")
	}
	return map[string]string{
		"error_insight_ai.enabled":                strconv.FormatBool(setting.Enabled),
		"error_insight_ai.channel_id":             strconv.Itoa(setting.ChannelID),
		"error_insight_ai.model":                  setting.Model,
		"error_insight_ai.sample_size":            strconv.Itoa(setting.SampleSize),
		"error_insight_ai.batch_limit":            strconv.Itoa(setting.BatchLimit),
		"error_insight_ai.include_original_error": strconv.FormatBool(setting.IncludeOriginalError),
		"error_insight_ai.redact_sensitive":       strconv.FormatBool(setting.RedactSensitive),
		"error_insight_ai.prompt_template":        setting.PromptTemplate,
		"error_insight_ai.json_output_params":     string(jsonParams),
	}, nil
}

func errorGovernanceAISettingToOptions(setting system_setting.ErrorGovernanceAISetting) (map[string]string, error) {
	jsonParams := json.RawMessage([]byte("{}"))
	if len(setting.JSONOutputParams) > 0 {
		jsonParams = setting.JSONOutputParams
	}
	if !json.Valid(jsonParams) {
		return nil, errors.New("invalid json output params")
	}
	return map[string]string{
		"error_governance_ai.enabled":            strconv.FormatBool(setting.Enabled),
		"error_governance_ai.channel_id":         strconv.Itoa(setting.ChannelID),
		"error_governance_ai.model":              setting.Model,
		"error_governance_ai.redact_sensitive":   strconv.FormatBool(setting.RedactSensitive),
		"error_governance_ai.prompt_template":    setting.PromptTemplate,
		"error_governance_ai.json_output_params": string(jsonParams),
	}, nil
}

func generateErrorGovernanceOrganizationWithAI(c *gin.Context, cfg *system_setting.ErrorGovernanceAISetting, governanceCfg *system_setting.RelayErrorGovernanceSetting) (string, error) {
	prompt, err := buildErrorGovernanceAIOrganizationPrompt(cfg, governanceCfg)
	if err != nil {
		return "", err
	}
	return invokeErrorInsightAI(c, cfg.ChannelID, cfg.Model, cfg.JSONOutputParams, prompt)
}

func buildErrorGovernanceAIOrganizationPrompt(cfg *system_setting.ErrorGovernanceAISetting, governanceCfg *system_setting.RelayErrorGovernanceSetting) (string, error) {
	governanceData, err := json.MarshalIndent(governanceCfg, "", "  ")
	if err != nil {
		return "", err
	}
	if cfg.RedactSensitive {
		governanceData = []byte(redactErrorInsightAIText(string(governanceData), true))
	}
	conflicts := buildErrorGovernanceAIConflictSummary(governanceCfg.CustomRules)
	conflictData, err := json.MarshalIndent(conflicts, "", "  ")
	if err != nil {
		return "", err
	}
	prompt := cfg.PromptTemplate
	prompt = strings.ReplaceAll(prompt, "{{governance_config}}", string(governanceData))
	prompt = strings.ReplaceAll(prompt, "{{conflicts}}", string(conflictData))
	return prompt, nil
}

func buildErrorGovernanceAIConflictSummary(rules []system_setting.RelayErrorGovernanceCustomRuleConfig) []map[string]any {
	conflicts := make([]map[string]any, 0)
	for i, rule := range rules {
		pattern := strings.TrimSpace(strings.ToLower(rule.MatchPattern))
		if pattern == "" {
			continue
		}
		for j := i + 1; j < len(rules); j++ {
			other := rules[j]
			otherPattern := strings.TrimSpace(strings.ToLower(other.MatchPattern))
			if otherPattern == "" {
				continue
			}
			reason := ""
			if rule.RuleCode != "" && rule.RuleCode == other.RuleCode {
				reason = "duplicate rule_code"
			} else if rule.MatchType == other.MatchType && pattern == otherPattern {
				reason = "same match pattern"
			} else if rule.MatchType == "contains" && other.MatchType == "contains" && (strings.Contains(pattern, otherPattern) || strings.Contains(otherPattern, pattern)) {
				reason = "contains pattern overlap"
			} else if rule.MatchType == "regex" {
				if re, err := regexp.Compile(rule.MatchPattern); err != nil {
					reason = "invalid regex"
				} else if re.MatchString(otherPattern) {
					reason = "regex may cover another pattern"
				}
			} else if other.MatchType == "regex" {
				if re, err := regexp.Compile(other.MatchPattern); err != nil {
					reason = "other invalid regex"
				} else if re.MatchString(pattern) {
					reason = "covered by another regex"
				}
			}
			if reason != "" {
				conflicts = append(conflicts, map[string]any{
					"rule_code":       rule.RuleCode,
					"other_rule_code": other.RuleCode,
					"reason":          reason,
				})
			}
		}
	}
	return conflicts
}

func parseErrorGovernanceAIOrganization(content string) (ErrorGovernanceAIOrganizeResult, error) {
	trimmed := normalizeErrorInsightAIJSONContent(content)
	type aiOrganizationRule struct {
		Enabled          *bool  `json:"enabled"`
		RuleCode         string `json:"rule_code"`
		Category         string `json:"category,omitempty"`
		MatchType        string `json:"match_type"`
		MatchPattern     string `json:"match_pattern"`
		SafeErrorCode    string `json:"safe_error_code"`
		SafeErrorType    string `json:"safe_error_type"`
		SafeErrorMessage string `json:"safe_error_message"`
		StatusCode       int    `json:"status_code,omitempty"`
	}
	var payload struct {
		Summary string               `json:"summary"`
		Rules   []aiOrganizationRule `json:"rules"`
	}
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return ErrorGovernanceAIOrganizeResult{}, errors.New("AI response is not valid JSON")
	}
	rules := make([]system_setting.RelayErrorGovernanceCustomRuleConfig, 0, len(payload.Rules))
	for _, input := range payload.Rules {
		enabled := true
		if input.Enabled != nil {
			enabled = *input.Enabled
		}
		rules = append(rules, system_setting.RelayErrorGovernanceCustomRuleConfig{
			Enabled:          enabled,
			RuleCode:         input.RuleCode,
			Category:         input.Category,
			MatchType:        input.MatchType,
			MatchPattern:     input.MatchPattern,
			SafeErrorCode:    input.SafeErrorCode,
			SafeErrorType:    input.SafeErrorType,
			SafeErrorMessage: input.SafeErrorMessage,
			StatusCode:       input.StatusCode,
		})
	}
	normalizedRules, err := system_setting.NormalizeRelayErrorGovernanceCustomRules(rules)
	if err != nil {
		return ErrorGovernanceAIOrganizeResult{}, err
	}
	if normalizedRules == nil {
		normalizedRules = []system_setting.RelayErrorGovernanceCustomRuleConfig{}
	}
	return ErrorGovernanceAIOrganizeResult{Summary: strings.TrimSpace(payload.Summary), Rules: normalizedRules, Raw: json.RawMessage(trimmed)}, nil
}

func generateErrorInsightRulesWithAI(c *gin.Context, cfg *system_setting.ErrorInsightAISetting, signature string, logs []*model.ErrorLog) (string, error) {
	prompt, err := buildErrorInsightAIPrompt(cfg, signature, logs)
	if err != nil {
		return "", err
	}
	return invokeErrorInsightAI(c, cfg.ChannelID, cfg.Model, cfg.JSONOutputParams, prompt)
}

func invokeErrorInsightAI(c *gin.Context, channelID int, modelName string, jsonOutputParams json.RawMessage, prompt string) (string, error) {
	return invokeErrorInsightAIWithPrompts(c, channelID, modelName, jsonOutputParams, "", prompt)
}

func invokeErrorInsightAIWithPrompts(c *gin.Context, channelID int, modelName string, jsonOutputParams json.RawMessage, systemPrompt string, userPrompt string) (string, error) {
	messages := make([]dto.Message, 0, 2)
	if strings.TrimSpace(systemPrompt) != "" {
		messages = append(messages, dto.Message{Role: "system", Content: systemPrompt})
	}
	messages = append(messages, dto.Message{Role: "user", Content: userPrompt})
	return invokeErrorInsightAIWithMessages(c, channelID, modelName, jsonOutputParams, messages)
}

func invokeErrorInsightAIWithMessages(c *gin.Context, channelID int, modelName string, jsonOutputParams json.RawMessage, messages []dto.Message) (string, error) {
	const maxAIResponseBytes = 2 << 20

	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return "", errors.New("AI model is not configured")
	}
	channel, err := model.GetChannelById(channelID, true)
	if err != nil {
		return "", errors.New("failed to load AI channel")
	}
	if channel.Status != common.ChannelStatusEnabled {
		return "", errors.New("AI channel is not enabled")
	}
	if !channel.IsAvailableAt(time.Now()) {
		return "", errors.New("AI channel is outside its configured availability schedule")
	}
	request := &dto.GeneralOpenAIRequest{
		Model:    modelName,
		Messages: messages,
	}
	passthroughParams, err := applyErrorInsightAIJSONParams(request, jsonOutputParams)
	if err != nil {
		return "", err
	}
	relayCtx, _ := gin.CreateTestContext(c.Writer)
	relayCtx.Request = c.Request.Clone(c.Request.Context())
	if err := prepareErrorInsightAIRelayRequest(relayCtx.Request); err != nil {
		return "", err
	}
	if newAPIError := middleware.SetupContextForSelectedChannel(relayCtx, channel, modelName); newAPIError != nil {
		return "", newAPIError
	}
	apiType, ok := common.ChannelType2APIType(channel.Type)
	if !ok {
		return "", errors.New("unsupported AI channel type")
	}
	info, err := relaycommon.GenRelayInfo(relayCtx, types.RelayFormatOpenAI, request, nil)
	if err != nil {
		return "", err
	}
	info.RelayMode = relayconstant.RelayModeChatCompletions
	info.IsChannelTest = true
	info.BypassChannelTrafficControl = true
	info.InitChannelMeta(relayCtx)
	if err := helper.ModelMappedHelper(relayCtx, info, request); err != nil {
		return "", err
	}
	outboundModel, err := ensureErrorInsightAIModel(request, info, modelName)
	if err != nil {
		return "", err
	}
	adaptor := relay.GetAdaptor(apiType)
	if adaptor == nil {
		return "", errors.New("invalid AI channel adaptor")
	}
	adaptor.Init(info)
	convertedRequest, err := adaptor.ConvertOpenAIRequest(relayCtx, info, request)
	if err != nil {
		return "", err
	}
	jsonData, err := common.Marshal(convertedRequest)
	if err != nil {
		return "", err
	}
	bodyModel, bodyCarriesModel, err := getErrorInsightAIRequestModel(jsonData)
	if err != nil {
		return "", err
	}
	if bodyModel == "" {
		bodyModel = outboundModel
	}
	if len(info.ParamOverride) > 0 {
		jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
		if err != nil {
			return "", err
		}
	}
	if errorInsightAIRequestSupportsPassthrough(convertedRequest) && len(passthroughParams) > 0 {
		jsonData, err = mergeErrorInsightAIJSONParams(jsonData, passthroughParams)
		if err != nil {
			return "", err
		}
	}
	if bodyCarriesModel {
		var repaired bool
		jsonData, repaired, err = ensureErrorInsightAIRequestModel(jsonData, bodyModel)
		if err != nil {
			return "", err
		}
		if repaired {
			common.SysLog(fmt.Sprintf("restored missing model in internal AI request: channel_id=%d", channelID))
		}
	}
	reqBody := bytes.NewBuffer(jsonData)
	relayCtx.Request.Body = io.NopCloser(bytes.NewBuffer(jsonData))
	resp, err := adaptor.DoRequest(relayCtx, info, reqBody)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", errors.New("AI channel returned empty response")
	}
	httpResp := resp.(*http.Response)
	defer httpResp.Body.Close()
	if httpResp.StatusCode < http.StatusOK || httpResp.StatusCode >= http.StatusMultipleChoices {
		err := service.RelayErrorHandler(relayCtx.Request.Context(), httpResp, true)
		return "", fmt.Errorf("AI channel request failed: %w", err)
	}
	body, err := io.ReadAll(io.LimitReader(httpResp.Body, maxAIResponseBytes+1))
	if err != nil {
		return "", err
	}
	if len(body) > maxAIResponseBytes {
		return "", errors.New("AI channel response is too large")
	}
	return extractErrorInsightAIContent(body)
}

func prepareErrorInsightAIRelayRequest(request *http.Request) error {
	if request == nil || request.URL == nil {
		return errors.New("failed to prepare AI relay request")
	}
	request.Method = http.MethodPost
	request.URL.Path = "/v1/chat/completions"
	request.URL.RawPath = ""
	request.URL.RawQuery = ""
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	// The admin analyze endpoint and scheduled Agent context may not carry a
	// Content-Type because their original requests have no body. This relay
	// request does carry a generated JSON body, so always describe it as JSON;
	// otherwise a new-api upstream skips body parsing and sees an empty model.
	request.Header.Set("Content-Type", gin.MIMEJSON)
	request.Header.Set("Accept", gin.MIMEJSON)
	return nil
}

func ensureErrorInsightAIModel(request *dto.GeneralOpenAIRequest, info *relaycommon.RelayInfo, configuredModel string) (string, error) {
	configuredModel = strings.TrimSpace(configuredModel)
	if request == nil || info == nil {
		return "", errors.New("failed to prepare AI request model")
	}

	resolvedModel := ""
	if info.ChannelMeta != nil {
		resolvedModel = strings.TrimSpace(info.UpstreamModelName)
	}
	if resolvedModel == "" {
		resolvedModel = strings.TrimSpace(request.Model)
	}
	if resolvedModel == "" {
		resolvedModel = configuredModel
	}
	if resolvedModel == "" {
		return "", errors.New("AI model is not configured")
	}

	if info.ChannelMeta == nil {
		info.ChannelMeta = &relaycommon.ChannelMeta{}
	}
	info.UpstreamModelName = resolvedModel
	if strings.TrimSpace(info.OriginModelName) == "" {
		info.OriginModelName = configuredModel
	}
	request.Model = resolvedModel
	return resolvedModel, nil
}

func getErrorInsightAIRequestModel(jsonData []byte) (string, bool, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(jsonData, &payload); err != nil {
		return "", false, err
	}
	rawModel, exists := payload["model"]
	if !exists {
		return "", false, nil
	}
	var modelName string
	if err := json.Unmarshal(rawModel, &modelName); err != nil {
		return "", true, nil
	}
	return strings.TrimSpace(modelName), true, nil
}

func ensureErrorInsightAIRequestModel(jsonData []byte, fallbackModel string) ([]byte, bool, error) {
	fallbackModel = strings.TrimSpace(fallbackModel)
	if fallbackModel == "" {
		return nil, false, errors.New("AI request model is empty")
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(jsonData, &payload); err != nil {
		return nil, false, err
	}
	if rawModel, exists := payload["model"]; exists {
		var modelName string
		if err := json.Unmarshal(rawModel, &modelName); err == nil && strings.TrimSpace(modelName) != "" {
			return jsonData, false, nil
		}
	}

	rawModel, err := json.Marshal(fallbackModel)
	if err != nil {
		return nil, false, err
	}
	payload["model"] = rawModel
	repaired, err := json.Marshal(payload)
	return repaired, true, err
}

var errorInsightAIRequestFieldNames = collectJSONFieldNames(reflect.TypeOf(dto.GeneralOpenAIRequest{}))

var errorInsightAIReservedParams = map[string]struct{}{
	"model":             {},
	"messages":          {},
	"input":             {},
	"prompt":            {},
	"stream":            {},
	"tools":             {},
	"tool_choice":       {},
	"functions":         {},
	"function_call":     {},
	"user":              {},
	"safety_identifier": {},
}

func collectJSONFieldNames(valueType reflect.Type) map[string]struct{} {
	fields := make(map[string]struct{}, valueType.NumField())
	for index := 0; index < valueType.NumField(); index++ {
		name := strings.Split(valueType.Field(index).Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			fields[name] = struct{}{}
		}
	}
	return fields
}

func applyErrorInsightAIJSONParams(request *dto.GeneralOpenAIRequest, params json.RawMessage) (map[string]json.RawMessage, error) {
	if len(params) == 0 || string(params) == "null" {
		return nil, nil
	}
	var extra map[string]json.RawMessage
	if err := json.Unmarshal(params, &extra); err != nil {
		return nil, errors.New("json output params must be a JSON object")
	}
	requestData, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	var requestPayload map[string]json.RawMessage
	if err := json.Unmarshal(requestData, &requestPayload); err != nil {
		return nil, err
	}
	passthrough := make(map[string]json.RawMessage)
	for key, value := range extra {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if _, reserved := errorInsightAIReservedParams[normalizedKey]; reserved {
			return nil, fmt.Errorf("json output params cannot override reserved field: %s", key)
		}
		if _, known := errorInsightAIRequestFieldNames[key]; known {
			requestPayload[key] = value
		} else {
			passthrough[key] = value
		}
	}
	normalized, err := json.Marshal(requestPayload)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(normalized, request); err != nil {
		return nil, fmt.Errorf("json output params contain an invalid request field: %w", err)
	}
	return passthrough, nil
}

func errorInsightAIRequestSupportsPassthrough(convertedRequest any) bool {
	switch convertedRequest.(type) {
	case *dto.GeneralOpenAIRequest, dto.GeneralOpenAIRequest:
		return true
	default:
		return false
	}
}

func mergeErrorInsightAIJSONParams(jsonData []byte, extra map[string]json.RawMessage) ([]byte, error) {
	if len(extra) == 0 {
		return jsonData, nil
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(jsonData, &payload); err != nil {
		return nil, err
	}
	for key, value := range extra {
		payload[key] = value
	}
	return json.Marshal(payload)
}

func buildErrorInsightAIPrompt(cfg *system_setting.ErrorInsightAISetting, signature string, logs []*model.ErrorLog) (string, error) {
	samples := make([]map[string]any, 0, len(logs))
	for _, log := range logs {
		item := map[string]any{
			"created_at":           log.CreatedAt,
			"user_id":              log.UserId,
			"channel_id":           log.ChannelId,
			"model_name":           log.ModelName,
			"request_path":         log.RequestPath,
			"error_source":         log.ErrorSource,
			"error_stage":          log.ErrorStage,
			"client_status_code":   log.ClientStatusCode,
			"upstream_status_code": log.UpstreamStatusCode,
			"safe_error_code":      log.SafeErrorCode,
			"safe_error_type":      log.SafeErrorType,
			"safe_error_message":   log.SafeErrorMessage,
			"original_error_code":  redactErrorInsightAIText(log.OriginalErrorCode, cfg.RedactSensitive),
			"original_error_type":  redactErrorInsightAIText(log.OriginalErrorType, cfg.RedactSensitive),
			"normalized_signature": log.NormalizedSignature,
			"request_time":         log.RequestTime,
			"retry_count":          log.RetryCount,
			"unmatched_reason":     log.UnmatchedReason,
			"current_rule_matched": log.RuleMatched,
			"current_rule_code":    log.RuleCode,
		}
		if cfg.IncludeOriginalError {
			item["original_error_message"] = redactErrorInsightAIText(log.OriginalErrorMessage, cfg.RedactSensitive)
		}
		samples = append(samples, item)
	}
	sampleBytes, err := json.MarshalIndent(samples, "", "  ")
	if err != nil {
		return "", err
	}
	prompt := cfg.PromptTemplate
	prompt = strings.ReplaceAll(prompt, "{{signature}}", signature)
	prompt = strings.ReplaceAll(prompt, "{{sample_logs}}", string(sampleBytes))
	return prompt, nil
}

var errorInsightAIRedactors = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization\s*[:=]\s*bearer\s+)[^\s,;]+`),
	regexp.MustCompile(`(?i)((api[_-]?key|token|secret|cookie|key)\s*[:=]\s*)[^\s,;]+`),
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{12,}`),
}

func redactErrorInsightAIText(text string, enabled bool) string {
	if !enabled || text == "" {
		return text
	}
	for _, re := range errorInsightAIRedactors {
		text = re.ReplaceAllString(text, `${1}[REDACTED]`)
	}
	if len(text) > 2000 {
		text = text[:2000] + "...[truncated]"
	}
	return text
}

func extractErrorInsightAIContent(body []byte) (string, error) {
	var payload struct {
		Choices []struct {
			Message struct {
				Content          any `json:"content"`
				ReasoningContent any `json:"reasoning_content"`
				Reasoning        any `json:"reasoning"`
			} `json:"message"`
			Text string `json:"text"`
		} `json:"choices"`
		OutputText any `json:"output_text"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	if len(payload.Choices) > 0 {
		if content := stringifyAIContent(payload.Choices[0].Message.Content); content != "" {
			return content, nil
		}
		if strings.TrimSpace(payload.Choices[0].Text) != "" {
			return strings.TrimSpace(payload.Choices[0].Text), nil
		}
		if content := stringifyAIContent(payload.Choices[0].Message.ReasoningContent); content != "" {
			return content, nil
		}
		if content := stringifyAIContent(payload.Choices[0].Message.Reasoning); content != "" {
			return content, nil
		}
	}
	if content := stringifyAIContent(payload.OutputText); content != "" {
		return content, nil
	}
	return "", errors.New("AI response does not contain content")
}

func stringifyAIContent(content any) string {
	switch value := content.(type) {
	case string:
		return strings.TrimSpace(value)
	case []any:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			if text := stringifyAIContent(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, ""))
	case map[string]any:
		for _, key := range []string{"json", "output_text", "content", "text"} {
			if text := stringifyAIContent(value[key]); text != "" {
				return text
			}
		}
		encoded, err := json.Marshal(value)
		if err == nil {
			return string(encoded)
		}
		return ""
	default:
		return ""
	}
}

type ErrorInsightAIRuleSuggestion struct {
	RuleCode         string  `json:"rule_code"`
	Category         string  `json:"category"`
	MatchType        string  `json:"match_type"`
	MatchPattern     string  `json:"match_pattern"`
	SafeErrorCode    string  `json:"safe_error_code"`
	SafeErrorType    string  `json:"safe_error_type"`
	SafeErrorMessage string  `json:"safe_error_message"`
	StatusCode       int     `json:"status_code,omitempty"`
	Confidence       float64 `json:"confidence"`
	Reason           string  `json:"reason"`
}

func parseErrorInsightAISuggestions(content string) ([]ErrorInsightAIRuleSuggestion, json.RawMessage, error) {
	trimmed := normalizeErrorInsightAIJSONContent(content)
	rules, err := decodeErrorInsightAIRules([]byte(trimmed))
	if err != nil {
		return nil, nil, errors.New("AI response is not valid JSON")
	}
	seenRuleCodes := make(map[string]struct{}, len(rules))
	for i := range rules {
		normalized, err := normalizeErrorInsightCustomRule(rules[i])
		if err != nil {
			return nil, nil, fmt.Errorf("AI generated invalid rule %q: %w", rules[i].RuleCode, err)
		}
		rules[i].RuleCode = normalized.RuleCode
		rules[i].Category = normalized.Category
		rules[i].MatchType = normalized.MatchType
		rules[i].MatchPattern = normalized.MatchPattern
		rules[i].SafeErrorCode = normalized.SafeErrorCode
		rules[i].SafeErrorType = normalized.SafeErrorType
		rules[i].SafeErrorMessage = normalized.SafeErrorMessage
		rules[i].StatusCode = normalized.StatusCode
		rules[i].Reason = strings.TrimSpace(rules[i].Reason)
		if _, ok := seenRuleCodes[normalized.RuleCode]; ok {
			return nil, nil, fmt.Errorf("AI generated duplicate rule_code %q", normalized.RuleCode)
		}
		seenRuleCodes[normalized.RuleCode] = struct{}{}
	}
	if rules == nil {
		rules = []ErrorInsightAIRuleSuggestion{}
	}
	return rules, json.RawMessage(trimmed), nil
}

func normalizeErrorInsightAIJSONContent(content string) string {
	trimmed := strings.TrimSpace(content)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	trimmed = strings.TrimSpace(trimmed)
	start := strings.IndexAny(trimmed, "{[")
	if start > 0 {
		trimmed = trimmed[start:]
	}
	return strings.TrimSpace(trimmed)
}

func decodeErrorInsightAIRules(data []byte) ([]ErrorInsightAIRuleSuggestion, error) {
	var arrayPayload []ErrorInsightAIRuleSuggestion
	if err := json.Unmarshal(data, &arrayPayload); err == nil {
		return arrayPayload, nil
	}
	var payload struct {
		Rules          []ErrorInsightAIRuleSuggestion `json:"rules"`
		Rule           *ErrorInsightAIRuleSuggestion  `json:"rule"`
		CandidateRules []ErrorInsightAIRuleSuggestion `json:"candidate_rules"`
		Candidates     []ErrorInsightAIRuleSuggestion `json:"candidates"`
		Suggestions    []ErrorInsightAIRuleSuggestion `json:"suggestions"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	if len(payload.Rules) > 0 {
		return payload.Rules, nil
	}
	if payload.Rule != nil {
		return []ErrorInsightAIRuleSuggestion{*payload.Rule}, nil
	}
	if len(payload.CandidateRules) > 0 {
		return payload.CandidateRules, nil
	}
	if len(payload.Candidates) > 0 {
		return payload.Candidates, nil
	}
	if len(payload.Suggestions) > 0 {
		return payload.Suggestions, nil
	}
	return []ErrorInsightAIRuleSuggestion{}, nil
}

func normalizeErrorInsightCustomRule(input ErrorInsightAIRuleSuggestion) (system_setting.RelayErrorGovernanceCustomRuleConfig, error) {
	ruleCode := strings.TrimSpace(input.RuleCode)
	if ruleCode == "" {
		ruleCode = "ai_" + strings.TrimSpace(input.Category)
	}
	ruleCode = strings.ReplaceAll(ruleCode, " ", "_")
	return system_setting.NormalizeRelayErrorGovernanceCustomRule(system_setting.RelayErrorGovernanceCustomRuleConfig{
		Enabled:          true,
		RuleCode:         ruleCode,
		Category:         input.Category,
		MatchType:        input.MatchType,
		MatchPattern:     input.MatchPattern,
		SafeErrorCode:    input.SafeErrorCode,
		SafeErrorType:    input.SafeErrorType,
		SafeErrorMessage: input.SafeErrorMessage,
		StatusCode:       input.StatusCode,
	})
}
