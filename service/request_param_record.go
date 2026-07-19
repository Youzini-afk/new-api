package service

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// logScreeningRiskLevelHigh is the risk level stored for high-risk screening
// matches. The observed-user semantic-capture path uses it to decide whether to
// record extra prompt/UA semantic fields for a user currently flagged by an
// active (non-expired) local screening record. It does NOT depend on ban_sync.
const logScreeningRiskLevelHigh = "high"

// rawInterceptLogMaxBytes bounds the size of the raw headers / params strings
// returned by BuildRawRequestHeadersForInterceptLog /
// BuildRawRequestParamsForInterceptLog, so a large body or many headers do not
// blow up the TEXT column of an interception log.
const rawInterceptLogMaxBytes = 8192

type observedScreeningCacheEntry struct {
	observed  bool
	expiresAt int64
}

var observedScreeningUserCache sync.Map

// BuildRequestParamsForLog extracts configured fields from the request body and
// returns a sanitized map. It is admin-visible only (merged into log "other").
//
// Body reading adapts to the upstream io.Seeker body storage: it seeks to 0,
// decodes via common.DecodeJson (no full materialization for disk-backed
// bodies), and ALWAYS seeks back to 0 + restores c.Request.Body so subsequent
// relay handlers can re-read the body. The storage is NOT closed here — the
// middleware owns its cleanup.
func BuildRequestParamsForLog(ctx *gin.Context, request dto.Request) map[string]interface{} {
	if ctx == nil {
		return nil
	}
	setting := system_setting.GetRelayParamRecordSetting()
	if setting == nil {
		setting = &system_setting.RelayParamRecordSetting{}
	}
	observedUser := false
	if setting != nil && setting.ObservedSemanticCaptureEnabled {
		observedUser = isObservedLogScreeningUser(ctx.GetInt("id"))
	}
	group := detectRequestParamGroup(ctx, request)
	fields := system_setting.ResolveRelayParamRecordFields()
	fields = applyObservedSemanticFieldsIfNeeded(fields, observedUser, group)
	if fields == nil || len(fields) == 0 {
		return nil
	}

	fieldList := fields[group]
	if len(fieldList) == 0 {
		return nil
	}
	requestPayload := requestToMap(request)

	// Parse request body into map via the io.Seeker body storage.
	var payload map[string]interface{}
	payloadFromBody := false
	payload, payloadFromBody = readRequestBodyAsMap(ctx)
	if payload == nil {
		payload = requestPayload
	}
	if payload == nil {
		return nil
	}

	maxBytes := setting.MaxValueBytes
	if maxBytes <= 0 {
		maxBytes = 4096
	}
	maxSystemDeveloperBytes := setting.SystemDeveloperMaxBytes
	if maxSystemDeveloperBytes <= 0 {
		maxSystemDeveloperBytes = 100
	}
	observedSemanticMaxBytes := resolveObservedSemanticMaxBytes()

	result := make(map[string]interface{})
	for _, field := range fieldList {
		if field == "" {
			continue
		}
		value, ok := getNestedValue(payload, field)
		if payloadFromBody && shouldPreferRequestPayloadField(group, field) && (!ok || value == nil) && requestPayload != nil {
			if fallbackValue, fallbackOK := getNestedValue(requestPayload, field); fallbackOK {
				value = fallbackValue
				ok = true
			}
		}
		if payloadFromBody && ok && shouldPreferRequestPayloadField(group, field) && requestPayload != nil {
			if fallbackValue, fallbackOK := getNestedValue(requestPayload, field); fallbackOK {
				value = choosePreferredRequestPayloadFieldValue(group, field, value, fallbackValue)
			}
		}
		if !ok {
			continue
		}
		result[field] = sanitizeParamValueForGroup(group, field, value, maxBytes, maxSystemDeveloperBytes, observedUser, observedSemanticMaxBytes)
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// readRequestBodyAsMap reads the request body as a JSON object map via the
// upstream body storage. It ALWAYS restores the body to position 0 and
// reassigns c.Request.Body so the relay chain can re-read it. The storage is
// not closed here. Returns (nil, false) when the body is absent, not JSON, or
// unreadable — never panics.
//
// The storage (common.BodyStorage) implements io.ReadSeeker; the upstream
// GetRequestBody returns it as a bare io.Seeker, so we type-assert to the
// storage (or io.Reader) here for decoding.
func readRequestBodyAsMap(ctx *gin.Context) (map[string]interface{}, bool) {
	if ctx == nil || ctx.Request == nil {
		return nil, false
	}
	seeker, err := common.GetRequestBody(ctx)
	if err != nil || seeker == nil {
		return nil, false
	}
	reader, ok := seeker.(io.Reader)
	if !ok {
		// No Read method — cannot decode; restore position defensively.
		if _, seekErr := seeker.Seek(0, io.SeekStart); seekErr == nil {
			if bs, bsOK := seeker.(common.BodyStorage); bsOK {
				ctx.Request.Body = io.NopCloser(bs)
			}
		}
		return nil, false
	}
	// Ensure we read from the start.
	if _, err := seeker.Seek(0, io.SeekStart); err != nil {
		return nil, false
	}
	var payload map[string]interface{}
	decodeErr := common.DecodeJson(reader, &payload)
	// Always restore the body pointer + seek to 0 for downstream readers.
	if _, seekErr := seeker.Seek(0, io.SeekStart); seekErr == nil {
		if bs, bsOK := seeker.(common.BodyStorage); bsOK {
			ctx.Request.Body = io.NopCloser(bs)
		}
	}
	if decodeErr != nil {
		return nil, false
	}
	if payload == nil {
		return nil, false
	}
	return payload, true
}

// readRequestBodyBytes reads the request body as a byte slice via the body
// storage, restoring the seek position to 0 and reassigning c.Request.Body
// afterwards. Used by the raw intercept-log builders.
func readRequestBodyBytes(ctx *gin.Context) ([]byte, bool) {
	if ctx == nil || ctx.Request == nil {
		return nil, false
	}
	seeker, err := common.GetRequestBody(ctx)
	if err != nil || seeker == nil {
		return nil, false
	}
	reader, ok := seeker.(io.Reader)
	if !ok {
		if _, seekErr := seeker.Seek(0, io.SeekStart); seekErr == nil {
			if bs, bsOK := seeker.(common.BodyStorage); bsOK {
				ctx.Request.Body = io.NopCloser(bs)
			}
		}
		return nil, false
	}
	if _, err := seeker.Seek(0, io.SeekStart); err != nil {
		return nil, false
	}
	data, readErr := io.ReadAll(reader)
	// Always restore position + body pointer regardless of read outcome.
	if _, seekErr := seeker.Seek(0, io.SeekStart); seekErr == nil {
		if bs, bsOK := seeker.(common.BodyStorage); bsOK {
			ctx.Request.Body = io.NopCloser(bs)
		}
	}
	if readErr != nil {
		return nil, false
	}
	return data, true
}

func isMessageFieldPath(field string) bool {
	lowerField := strings.ToLower(strings.TrimSpace(field))
	if lowerField == "" {
		return false
	}
	return lowerField == "messages" || strings.HasSuffix(lowerField, ".messages")
}

func shouldPreferRequestPayloadField(group string, field string) bool {
	if isMessageFieldPath(field) {
		return true
	}
	lowerGroup := strings.ToLower(strings.TrimSpace(group))
	lowerField := strings.ToLower(strings.TrimSpace(field))
	return isResponsesInputField(lowerGroup, lowerField)
}

func choosePreferredRequestPayloadFieldValue(group string, field string, primary interface{}, fallback interface{}) interface{} {
	if isMessageFieldPath(field) {
		return choosePreferredMessageFieldValue(primary, fallback)
	}
	lowerGroup := strings.ToLower(strings.TrimSpace(group))
	lowerField := strings.ToLower(strings.TrimSpace(field))
	if isResponsesInputField(lowerGroup, lowerField) {
		return choosePreferredResponsesInputFieldValue(primary, fallback)
	}
	return primary
}

func choosePreferredMessageFieldValue(primary interface{}, fallback interface{}) interface{} {
	primaryMessages, primaryOK := normalizeMessageArray(primary)
	fallbackMessages, fallbackOK := normalizeMessageArray(fallback)

	if primaryOK && len(primaryMessages) > 0 {
		return primary
	}
	if fallbackOK && len(fallbackMessages) > 0 {
		return fallback
	}
	if primary != nil {
		return primary
	}
	return fallback
}

func choosePreferredResponsesInputFieldValue(primary interface{}, fallback interface{}) interface{} {
	primaryMessages, primaryOK := normalizeResponsesInputArray(primary)
	fallbackMessages, fallbackOK := normalizeResponsesInputArray(fallback)

	if primaryOK && len(primaryMessages) > 0 {
		return primary
	}
	if fallbackOK && len(fallbackMessages) > 0 {
		return fallback
	}
	if hasMeaningfulRequestParamValue(primary) {
		return primary
	}
	return fallback
}

func hasMeaningfulRequestParamValue(value interface{}) bool {
	switch v := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(v) != ""
	case []byte:
		return len(v) > 0
	case []interface{}:
		return len(v) > 0
	case []map[string]interface{}:
		return len(v) > 0
	case map[string]interface{}:
		return len(v) > 0
	default:
		return true
	}
}

func applyObservedSemanticFieldsIfNeeded(fields map[string][]string, observedUser bool, group string) map[string][]string {
	if fields == nil || len(fields) == 0 {
		return fields
	}
	setting := system_setting.GetRelayParamRecordSetting()
	if setting == nil || !setting.ObservedSemanticCaptureEnabled {
		return fields
	}
	if !observedUser {
		return fields
	}
	if strings.TrimSpace(group) == "" {
		group = "openai"
	}
	observedFields := setting.ObservedSemanticFields
	if len(observedFields) == 0 {
		observedFields = system_setting.DefaultRelayParamRecordObservedSemanticFields()
	}
	if len(observedFields) == 0 {
		return fields
	}
	merged := cloneParamRecordFields(fields)
	base := merged[group]
	seen := make(map[string]struct{}, len(base)+len(observedFields))
	result := make([]string, 0, len(base)+len(observedFields))
	for _, field := range base {
		normalized := strings.TrimSpace(field)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	for _, field := range observedFields {
		normalized := strings.TrimSpace(field)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	merged[group] = result
	return merged
}

func cloneParamRecordFields(source map[string][]string) map[string][]string {
	if source == nil {
		return nil
	}
	result := make(map[string][]string, len(source))
	for key, list := range source {
		if list == nil {
			result[key] = nil
			continue
		}
		cloned := make([]string, len(list))
		copy(cloned, list)
		result[key] = cloned
	}
	return result
}

// isObservedLogScreeningUser reports whether the user currently has an active
// high-risk LogScreeningRecord (observed_until / expires_at not passed). This
// drives the observed-semantic-capture path so admin-configured extra prompt/UA
// semantic fields are recorded only for users already flagged by Phase 5
// screening — it does NOT depend on ban_sync and performs no banning.
func isObservedLogScreeningUser(userId int) bool {
	if userId <= 0 {
		return false
	}
	if model.DB == nil {
		return false
	}
	now := common.GetTimestamp()
	if cached, ok := observedScreeningUserCache.Load(userId); ok {
		entry := cached.(observedScreeningCacheEntry)
		if entry.expiresAt > now {
			return entry.observed
		}
		observedScreeningUserCache.Delete(userId)
	}
	var record model.LogScreeningRecord
	err := model.DB.Select("id", "observed_until", "expires_at").
		Where("user_id = ?", userId).
		Where("risk_level = ?", logScreeningRiskLevelHigh).
		Where("(observed_until = 0 OR observed_until >= ?)", now).
		Where("(expires_at = 0 OR expires_at >= ?)", now).
		Order("matched_at desc, id desc").
		First(&record).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		common.SysLog("observed log-screening cache lookup failed: " + err.Error())
		return false
	}
	observed := err == nil
	cacheUntil := now + int64((30 * time.Second).Seconds())
	if observed {
		if record.ObservedUntil > 0 && record.ObservedUntil < cacheUntil {
			cacheUntil = record.ObservedUntil
		}
		if record.ExpiresAt > 0 && record.ExpiresAt < cacheUntil {
			cacheUntil = record.ExpiresAt
		}
	} else {
		cacheUntil = now + int64((10 * time.Second).Seconds())
	}
	if cacheUntil <= now {
		return observed
	}
	observedScreeningUserCache.Store(userId, observedScreeningCacheEntry{
		observed:  observed,
		expiresAt: cacheUntil,
	})
	return observed
}

func invalidateObservedLogScreeningUser(userId int) {
	if userId > 0 {
		observedScreeningUserCache.Delete(userId)
	}
}

// MergeRequestParamsToOther appends request params to the log "other" map under
// the shared common.RequestParamsOtherKey. If params is empty it returns other
// unchanged (allocating if nil).
func MergeRequestParamsToOther(other map[string]interface{}, params map[string]interface{}) map[string]interface{} {
	if params == nil || len(params) == 0 {
		return other
	}
	if other == nil {
		other = make(map[string]interface{})
	}
	other[common.RequestParamsOtherKey] = params
	return other
}

func detectRequestParamGroup(ctx *gin.Context, request dto.Request) string {
	if _, ok := request.(*dto.OpenAIResponsesRequest); ok {
		return "openai_responses"
	}
	if _, ok := request.(*dto.EmbeddingRequest); ok {
		return "embeddings"
	}
	if _, ok := request.(*dto.ImageRequest); ok {
		return "images"
	}
	if _, ok := request.(*dto.AudioRequest); ok {
		return "audio"
	}
	if _, ok := request.(*dto.ClaudeRequest); ok {
		return "claude"
	}
	if gemini, ok := request.(*dto.GeminiChatRequest); ok {
		if len(gemini.Requests) > 0 {
			return "gemini_batch_embedding"
		}
		return "gemini_chat"
	}
	if _, ok := request.(*dto.GeminiEmbeddingRequest); ok {
		return "gemini_embedding"
	}
	if _, ok := request.(*dto.GeminiBatchEmbeddingRequest); ok {
		return "gemini_batch_embedding"
	}
	if _, ok := request.(*dto.RerankRequest); ok {
		return "rerank"
	}
	if ctx != nil && ctx.Request != nil && ctx.Request.URL != nil {
		path := ctx.Request.URL.Path
		switch {
		case strings.HasPrefix(path, "/v1/responses"):
			return "openai_responses"
		case strings.HasPrefix(path, "/v1/embeddings"):
			return "embeddings"
		case strings.HasPrefix(path, "/v1/images"):
			return "images"
		case strings.HasPrefix(path, "/v1/audio"):
			return "audio"
		case strings.HasPrefix(path, "/v1/messages"):
			return "claude"
		case strings.HasPrefix(path, "/v1/rerank"):
			return "rerank"
		case strings.HasPrefix(path, "/v1beta/models"):
			return "gemini_chat"
		case strings.HasPrefix(path, "/v1/moderations"):
			return "openai"
		}
	}
	return "openai"
}

func getNestedValue(payload map[string]interface{}, fieldPath string) (interface{}, bool) {
	parts := strings.Split(fieldPath, ".")
	var current interface{} = payload
	for _, part := range parts {
		if part == "" {
			return nil, false
		}
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil, false
		}
		value, exists := m[part]
		if !exists {
			return nil, false
		}
		current = value
	}
	return current, true
}

func sanitizeParamValueForGroup(group string, field string, value interface{}, maxBytes int, systemDeveloperMaxBytes int, observedUser bool, observedSemanticMaxBytes int) interface{} {
	if value == nil {
		return nil
	}
	if maxBytes <= 0 {
		maxBytes = 4096
	}
	if systemDeveloperMaxBytes <= 0 {
		systemDeveloperMaxBytes = 100
	}

	lowerField := strings.ToLower(field)
	if isSensitiveField(lowerField) {
		return "***masked***"
	}
	if lowerField == "messages" || strings.HasSuffix(lowerField, ".messages") {
		return sanitizeMessageFieldValue(value, maxBytes, systemDeveloperMaxBytes)
	}
	if isResponsesInputField(strings.ToLower(strings.TrimSpace(group)), lowerField) {
		return sanitizeResponsesInputValue(value, maxBytes, systemDeveloperMaxBytes, observedUser, observedSemanticMaxBytes)
	}
	if shouldKeepStructuredFieldForLog(lowerField) {
		return sanitizeObservedUserTextValue(value, maxBytes)
	}
	if shouldClearUserTextField(lowerField) {
		if shouldKeepUserTextField(field) {
			if observedUser {
				return sanitizeObservedUserTextValue(value, observedSemanticMaxBytes)
			}
			return sanitizeObservedUserTextValue(value, maxBytes)
		}
		return sanitizeUserTextValue(value, systemDeveloperMaxBytes)
	}
	if shouldTrimSystemDeveloperField(lowerField) {
		return sanitizeSystemDeveloperValue(value, systemDeveloperMaxBytes)
	}

	switch v := value.(type) {
	case string:
		return truncateAndMaskString(v, maxBytes)
	case []byte:
		return truncateAndMaskString(string(v), maxBytes)
	default:
		data, err := common.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return truncateAndMaskString(string(data), maxBytes)
	}
}

func sanitizeParamValueForLog(field string, value interface{}, maxBytes int, systemDeveloperMaxBytes int, observedUser bool, observedSemanticMaxBytes int) interface{} {
	return sanitizeParamValueForGroup("", field, value, maxBytes, systemDeveloperMaxBytes, observedUser, observedSemanticMaxBytes)
}

func sanitizeParamValue(field string, value interface{}, maxBytes int, systemDeveloperMaxBytes int) interface{} {
	return sanitizeParamValueForGroup("", field, value, maxBytes, systemDeveloperMaxBytes, false, resolveObservedSemanticMaxBytes())
}

func sanitizeMessageFieldValue(value interface{}, maxBytes int, systemDeveloperMaxBytes int) interface{} {
	if maxBytes <= 0 {
		maxBytes = 4096
	}
	if systemDeveloperMaxBytes <= 0 {
		systemDeveloperMaxBytes = 100
	}

	messages, ok := normalizeMessageArray(value)
	if !ok {
		return sanitizeUserTextValue(value, systemDeveloperMaxBytes)
	}
	if len(messages) == 0 {
		return messages
	}

	keepIndices := buildMessageRoleKeepIndex(messages)
	result := make([]interface{}, 0, len(keepIndices))
	for i, item := range messages {
		if _, keep := keepIndices[i]; !keep {
			continue
		}
		typed, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		result = append(result, sanitizeMessageItemByRole(typed, maxBytes, systemDeveloperMaxBytes))
	}
	return result
}

func isResponsesInputField(group string, field string) bool {
	if group != "openai_responses" {
		return false
	}
	return field == "input" || strings.HasSuffix(field, ".input")
}

func sanitizeResponsesInputValue(value interface{}, maxBytes int, systemDeveloperMaxBytes int, observedUser bool, observedSemanticMaxBytes int) interface{} {
	if maxBytes <= 0 {
		maxBytes = 4096
	}
	if systemDeveloperMaxBytes <= 0 {
		systemDeveloperMaxBytes = 100
	}
	if observedSemanticMaxBytes <= 0 {
		observedSemanticMaxBytes = resolveObservedSemanticMaxBytes()
	}

	if messages, ok := normalizeResponsesInputArray(value); ok {
		if len(messages) == 0 {
			return messages
		}
		return sanitizeMessageFieldValue(messages, maxBytes, systemDeveloperMaxBytes)
	}
	if observedUser {
		return sanitizeObservedUserTextValue(value, observedSemanticMaxBytes)
	}
	return sanitizeRetainedMessageValue(value, maxBytes)
}

func normalizeMessageArray(value interface{}) ([]interface{}, bool) {
	if value == nil {
		return nil, false
	}
	if messages, ok := value.([]interface{}); ok {
		return messages, true
	}
	if messages, ok := value.([]map[string]interface{}); ok {
		result := make([]interface{}, len(messages))
		for i := range messages {
			result[i] = messages[i]
		}
		return result, true
	}
	data, err := common.Marshal(value)
	if err != nil {
		return nil, false
	}
	var messages []interface{}
	if err := common.Unmarshal(data, &messages); err != nil {
		return nil, false
	}
	return messages, true
}

func normalizeResponsesInputArray(value interface{}) ([]interface{}, bool) {
	messages, ok := normalizeMessageArray(value)
	if !ok {
		return nil, false
	}
	for _, item := range messages {
		typed, ok := item.(map[string]interface{})
		if !ok {
			return nil, false
		}
		if _, hasRole := typed["role"]; !hasRole {
			return nil, false
		}
	}
	return messages, true
}

func sanitizeMessageItemByRole(value map[string]interface{}, maxBytes int, systemDeveloperMaxBytes int) map[string]interface{} {
	if value == nil {
		return nil
	}

	role := ""
	if rawRole, ok := value["role"]; ok {
		if roleValue, ok := rawRole.(string); ok {
			role = normalizeMessageRole(roleValue)
		}
	}

	result := make(map[string]interface{}, len(value))
	for key, item := range value {
		lowerKey := strings.ToLower(key)
		if lowerKey == "role" {
			result[key] = item
			continue
		}
		switch role {
		case "system":
			result[key] = sanitizeSystemDeveloperValue(item, systemDeveloperMaxBytes)
		case "assistant":
			result[key] = sanitizeRetainedMessageValue(item, maxBytes)
		default:
			result[key] = sanitizeRetainedMessageValue(item, maxBytes)
		}
	}
	return result
}

func sanitizeRetainedMessageValue(value interface{}, maxBytes int) interface{} {
	if maxBytes <= 0 {
		maxBytes = 4096
	}
	switch v := value.(type) {
	case string:
		return truncateAndMaskString(v, maxBytes)
	case []byte:
		return truncateAndMaskString(string(v), maxBytes)
	case []interface{}:
		return sanitizeObservedUserTextArray(v, maxBytes)
	case map[string]interface{}:
		return sanitizeObservedUserTextMap(v, maxBytes)
	default:
		data, err := common.Marshal(v)
		if err != nil {
			return truncateAndMaskString(fmt.Sprintf("%v", v), maxBytes)
		}
		return truncateAndMaskString(string(data), maxBytes)
	}
}

func buildMessageRoleKeepIndex(messages []interface{}) map[int]struct{} {
	keep := make(map[int]struct{})
	if len(messages) == 0 {
		return keep
	}

	setting := system_setting.GetRelayParamRecordSetting()
	userLimit := 3
	assistantLimit := 3
	systemLimit := 2
	if setting != nil {
		if setting.MessageKeepUserCount >= 0 {
			userLimit = setting.MessageKeepUserCount
		}
		if setting.MessageKeepAssistantCount >= 0 {
			assistantLimit = setting.MessageKeepAssistantCount
		}
		if setting.MessageKeepSystemCount >= 0 {
			systemLimit = setting.MessageKeepSystemCount
		}
	}

	userKept := 0
	assistantKept := 0
	systemKept := 0

	for i := len(messages) - 1; i >= 0; i-- {
		typed, ok := messages[i].(map[string]interface{})
		if !ok {
			continue
		}
		role := normalizeMessageRole(extractMessageRole(typed))
		switch role {
		case "system":
			if systemLimit <= 0 || systemKept >= systemLimit {
				continue
			}
			systemKept++
			keep[i] = struct{}{}
		case "assistant":
			if assistantLimit <= 0 || assistantKept >= assistantLimit {
				continue
			}
			assistantKept++
			keep[i] = struct{}{}
		default:
			if userLimit <= 0 || userKept >= userLimit {
				continue
			}
			userKept++
			keep[i] = struct{}{}
		}
	}

	return keep
}

func extractMessageRole(message map[string]interface{}) string {
	if message == nil {
		return ""
	}
	rawRole, ok := message["role"]
	if !ok {
		return ""
	}
	role, ok := rawRole.(string)
	if !ok {
		return ""
	}
	return role
}

func normalizeMessageRole(role string) string {
	normalized := strings.ToLower(strings.TrimSpace(role))
	switch normalized {
	case "system", "developer":
		return "system"
	case "assistant", "model":
		return "assistant"
	case "user":
		return "user"
	default:
		return "user"
	}
}

func shouldKeepUserTextField(field string) bool {
	field = strings.TrimSpace(strings.ToLower(field))
	if field == "" {
		return false
	}
	setting := system_setting.GetRelayParamRecordSetting()
	if setting == nil || !setting.ObservedSemanticCaptureEnabled {
		return false
	}
	allowed := setting.ObservedSemanticFields
	if len(allowed) == 0 {
		allowed = system_setting.DefaultRelayParamRecordObservedSemanticFields()
	}
	if len(allowed) == 0 {
		return false
	}
	if strings.Contains(field, ".") {
		parts := strings.Split(field, ".")
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed == "" {
				continue
			}
			if shouldKeepUserTextField(trimmed) {
				return true
			}
		}
	}
	for _, item := range allowed {
		normalized := strings.TrimSpace(strings.ToLower(item))
		if normalized != "" && normalized == field {
			return true
		}
	}
	return false
}

func sanitizeObservedUserTextValue(value interface{}, maxBytes int) interface{} {
	if maxBytes <= 0 {
		maxBytes = 1024
	}
	switch v := value.(type) {
	case string:
		return truncateAndMaskString(v, maxBytes)
	case []byte:
		return truncateAndMaskString(string(v), maxBytes)
	case []interface{}:
		return sanitizeObservedUserTextArray(v, maxBytes)
	case map[string]interface{}:
		return sanitizeObservedUserTextMap(v, maxBytes)
	default:
		data, err := common.Marshal(v)
		if err != nil {
			return truncateAndMaskString(fmt.Sprintf("%v", v), maxBytes)
		}
		return truncateAndMaskString(string(data), maxBytes)
	}
}

func sanitizeObservedUserTextArray(values []interface{}, maxBytes int) []interface{} {
	if len(values) == 0 {
		return values
	}
	result := make([]interface{}, len(values))
	for i, item := range values {
		switch typed := item.(type) {
		case map[string]interface{}:
			result[i] = sanitizeObservedUserTextMap(typed, maxBytes)
		case string:
			result[i] = truncateAndMaskString(typed, maxBytes)
		case []byte:
			result[i] = truncateAndMaskString(string(typed), maxBytes)
		case []interface{}:
			result[i] = sanitizeObservedUserTextArray(typed, maxBytes)
		default:
			data, err := common.Marshal(typed)
			if err != nil {
				result[i] = truncateAndMaskString(fmt.Sprintf("%v", typed), maxBytes)
				continue
			}
			result[i] = truncateAndMaskString(string(data), maxBytes)
		}
	}
	return result
}

func sanitizeObservedUserTextMap(value map[string]interface{}, maxBytes int) map[string]interface{} {
	if value == nil {
		return nil
	}
	role := ""
	if rawRole, ok := value["role"]; ok {
		if roleValue, ok := rawRole.(string); ok {
			role = strings.ToLower(roleValue)
		}
	}
	isSystemDeveloper := role == "system" || role == "developer"
	result := make(map[string]interface{}, len(value))
	for key, item := range value {
		lowerKey := strings.ToLower(key)
		if lowerKey == "role" {
			result[key] = item
			continue
		}
		if isSystemDeveloper {
			result[key] = sanitizeSystemDeveloperValue(item, maxBytes)
			continue
		}
		switch typed := item.(type) {
		case map[string]interface{}:
			result[key] = sanitizeObservedUserTextMap(typed, maxBytes)
		case []interface{}:
			result[key] = sanitizeObservedUserTextArray(typed, maxBytes)
		case string:
			result[key] = truncateAndMaskString(typed, maxBytes)
		case []byte:
			result[key] = truncateAndMaskString(string(typed), maxBytes)
		case float64:
			result[key] = typed
		case float32:
			result[key] = typed
		case int, int8, int16, int32, int64:
			result[key] = typed
		case uint, uint8, uint16, uint32, uint64:
			result[key] = typed
		case bool:
			result[key] = typed
		default:
			result[key] = sanitizeObservedUserTextValue(item, maxBytes)
		}
	}
	return result
}

func resolveObservedSemanticMaxBytes() int {
	setting := system_setting.GetRelayParamRecordSetting()
	if setting == nil || setting.ObservedSemanticMaxBytes <= 0 {
		return 1024
	}
	return setting.ObservedSemanticMaxBytes
}

func shouldKeepStructuredFieldForLog(field string) bool {
	if field == "" {
		return false
	}
	keepFields := map[string]struct{}{
		"contents":         {},
		"generationconfig": {},
		"safetysettings":   {},
	}
	if _, ok := keepFields[field]; ok {
		return true
	}
	for key := range keepFields {
		if strings.HasSuffix(field, "."+key) {
			return true
		}
	}
	return false
}

func shouldClearUserTextField(field string) bool {
	if field == "" {
		return false
	}
	userTextFields := map[string]struct{}{
		"messages":     {},
		"input":        {},
		"prompt":       {},
		"prefix":       {},
		"suffix":       {},
		"instruction":  {},
		"instructions": {},
		"text":         {},
		"query":        {},
		"documents":    {},
		"content":      {},
		"contents":     {},
		"title":        {},
		"requests":     {},
	}
	_, ok := userTextFields[field]
	return ok
}

func shouldTrimSystemDeveloperField(field string) bool {
	if field == "" {
		return false
	}
	systemDeveloperFields := map[string]struct{}{
		"system":            {},
		"systeminstruction": {},
	}
	_, ok := systemDeveloperFields[field]
	return ok
}

func sanitizeUserTextValue(value interface{}, systemDeveloperMaxBytes int) interface{} {
	switch v := value.(type) {
	case string:
		return ""
	case []byte:
		return ""
	case []interface{}:
		return sanitizeUserTextArray(v, systemDeveloperMaxBytes)
	case map[string]interface{}:
		return sanitizeUserTextMap(v, systemDeveloperMaxBytes)
	default:
		return ""
	}
}

func sanitizeSystemDeveloperValue(value interface{}, maxBytes int) interface{} {
	if maxBytes <= 0 {
		maxBytes = 100
	}
	switch v := value.(type) {
	case string:
		return truncateAndMaskString(v, maxBytes)
	case []byte:
		return truncateAndMaskString(string(v), maxBytes)
	case []interface{}:
		return sanitizeSystemDeveloperArray(v, maxBytes)
	case map[string]interface{}:
		return sanitizeSystemDeveloperMap(v, maxBytes)
	default:
		data, err := common.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return truncateAndMaskString(string(data), maxBytes)
	}
}

func sanitizeUserTextArray(values []interface{}, systemDeveloperMaxBytes int) []interface{} {
	if len(values) == 0 {
		return values
	}
	result := make([]interface{}, len(values))
	for i, item := range values {
		switch typed := item.(type) {
		case map[string]interface{}:
			result[i] = sanitizeUserTextMap(typed, systemDeveloperMaxBytes)
		case string:
			result[i] = ""
		case []byte:
			result[i] = ""
		case []interface{}:
			result[i] = sanitizeUserTextArray(typed, systemDeveloperMaxBytes)
		default:
			result[i] = item
		}
	}
	return result
}

func sanitizeUserTextMap(value map[string]interface{}, systemDeveloperMaxBytes int) map[string]interface{} {
	if value == nil {
		return nil
	}
	role := ""
	if rawRole, ok := value["role"]; ok {
		if roleValue, ok := rawRole.(string); ok {
			role = strings.ToLower(roleValue)
		}
	}
	isSystemDeveloper := role == "system" || role == "developer"
	result := make(map[string]interface{}, len(value))
	for key, item := range value {
		lowerKey := strings.ToLower(key)
		if lowerKey == "role" {
			result[key] = item
			continue
		}
		if isSystemDeveloper {
			result[key] = sanitizeSystemDeveloperValue(item, systemDeveloperMaxBytes)
			continue
		}
		result[key] = sanitizeUserTextValue(item, systemDeveloperMaxBytes)
	}
	return result
}

func sanitizeSystemDeveloperArray(values []interface{}, maxBytes int) []interface{} {
	if len(values) == 0 {
		return values
	}
	result := make([]interface{}, len(values))
	for i, item := range values {
		switch typed := item.(type) {
		case map[string]interface{}:
			result[i] = sanitizeSystemDeveloperMap(typed, maxBytes)
		case string:
			result[i] = truncateAndMaskString(typed, maxBytes)
		case []interface{}:
			result[i] = sanitizeSystemDeveloperArray(typed, maxBytes)
		default:
			data, err := common.Marshal(typed)
			if err != nil {
				result[i] = fmt.Sprintf("%v", typed)
				continue
			}
			result[i] = truncateAndMaskString(string(data), maxBytes)
		}
	}
	return result
}

func sanitizeSystemDeveloperMap(value map[string]interface{}, maxBytes int) map[string]interface{} {
	if value == nil {
		return nil
	}
	result := make(map[string]interface{}, len(value))
	for key, item := range value {
		lowerKey := strings.ToLower(key)
		if lowerKey == "content" || lowerKey == "text" || lowerKey == "input" || lowerKey == "prompt" {
			result[key] = truncateAndMaskString(fmt.Sprintf("%v", item), maxBytes)
			continue
		}
		switch typed := item.(type) {
		case map[string]interface{}:
			result[key] = sanitizeSystemDeveloperMap(typed, maxBytes)
		case []interface{}:
			result[key] = sanitizeSystemDeveloperArray(typed, maxBytes)
		case string:
			result[key] = truncateAndMaskString(typed, maxBytes)
		default:
			data, err := common.Marshal(typed)
			if err != nil {
				result[key] = fmt.Sprintf("%v", typed)
				continue
			}
			result[key] = truncateAndMaskString(string(data), maxBytes)
		}
	}
	return result
}

func truncateAndMaskString(input string, maxBytes int) string {
	if input == "" {
		return input
	}
	masked := common.MaskSensitiveInfo(input)
	if maxBytes <= 0 {
		return masked
	}
	if len(masked) <= maxBytes {
		return masked
	}
	return masked[:maxBytes] + "..."
}

var relayParamRecordSensitiveFieldSafeExact = map[string]struct{}{
	"max_tokens":            {},
	"max_output_tokens":     {},
	"max_completion_tokens": {},
	"token_name":            {},
	"metadata":              {},
	"thinking":              {},
}

var relayParamRecordSensitiveFieldSafeSuffixes = []string{
	".max_tokens",
	".max_output_tokens",
	".max_completion_tokens",
	".token_name",
	".metadata",
	"_tokens",
}

var relayParamRecordSensitiveFieldExact = map[string]struct{}{
	"authorization": {},
	"api_key":       {},
	"apikey":        {},
	"secret":        {},
	"password":      {},
	"private_key":   {},
	"access_token":  {},
	"refresh_token": {},
	"id_token":      {},
	"token":         {},
	"file":          {},
	"file_data":     {},
	"base64":        {},
	"image":         {},
	"audio":         {},
	"data":          {},
}

var relayParamRecordSensitiveFieldSuffixes = []string{
	".authorization",
	".api_key",
	".apikey",
	".secret",
	".password",
	".private_key",
	".access_token",
	".refresh_token",
	".token",
	"_authorization",
	"_api_key",
	"_apikey",
	"_secret",
	"_password",
	"_private_key",
	"_access_token",
	"_refresh_token",
	"_token",
	"_file",
	"_file_data",
	"_base64",
	"_image",
	"_audio",
	"_data",
	"-api-key",
	"-authorization",
}

var relayParamRecordSensitiveFieldSegments = map[string]struct{}{
	"authorization": {},
	"api":           {},
	"key":           {},
	"apikey":        {},
	"secret":        {},
	"password":      {},
	"token":         {},
	"private":       {},
	"file":          {},
	"base64":        {},
	"image":         {},
	"audio":         {},
	"data":          {},
}

func hasAnySuffix(field string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(field, suffix) {
			return true
		}
	}
	return false
}

func isSensitiveField(field string) bool {
	field = strings.ToLower(strings.TrimSpace(field))
	if field == "" {
		return false
	}

	if _, ok := relayParamRecordSensitiveFieldSafeExact[field]; ok {
		return false
	}
	if hasAnySuffix(field, relayParamRecordSensitiveFieldSafeSuffixes) {
		return false
	}

	if _, ok := relayParamRecordSensitiveFieldExact[field]; ok {
		return true
	}
	if hasAnySuffix(field, relayParamRecordSensitiveFieldSuffixes) {
		return true
	}

	tokens := strings.FieldsFunc(field, func(r rune) bool {
		return r == '.' || r == '_' || r == '-' || r == '[' || r == ']'
	})
	for _, token := range tokens {
		if token == "" {
			continue
		}
		if _, ok := relayParamRecordSensitiveFieldSafeExact[token]; ok {
			continue
		}
		if _, ok := relayParamRecordSensitiveFieldSegments[token]; ok {
			return true
		}
	}
	return false
}

func requestToMap(request dto.Request) map[string]interface{} {
	if request == nil {
		return nil
	}
	data, err := common.Marshal(request)
	if err != nil {
		return nil
	}
	var payload map[string]interface{}
	if err := common.Unmarshal(data, &payload); err != nil {
		return nil
	}
	return payload
}

// rawInterceptHeaderDenylist is the set of request header keys whose value must
// be masked in BuildRawRequestHeadersForInterceptLog. Lookup is case-insensitive
// (header keys are canonicalized by gin/http before reaching here, but we
// normalize defensively).
var rawInterceptHeaderDenylist = map[string]struct{}{
	"Authorization":             {},
	"Proxy-Authorization":       {},
	"Cookie":                    {},
	"Set-Cookie":                {},
	"X-Api-Key":                 {},
	"Api-Key":                   {},
	"Ocp-Apim-Subscription-Key": {},
	"Anthropic-Api-Key":         {},
	"X-Goog-Api-Key":            {},
}

// isRawInterceptHeaderSensitive reports whether a header key is sensitive and
// must be masked in the raw intercept log.
func isRawInterceptHeaderSensitive(key string) bool {
	if key == "" {
		return false
	}
	// http.Header canonicalizes keys (e.g. "authorization" -> "Authorization"),
	// but we still normalize to lower for a defensive lookup against the
	// canonical and lower forms.
	if _, ok := rawInterceptHeaderDenylist[key]; ok {
		return true
	}
	lower := strings.ToLower(key)
	switch lower {
	case "authorization", "proxy-authorization", "cookie", "set-cookie",
		"x-api-key", "api-key", "ocp-apim-subscription-key",
		"anthropic-api-key", "x-goog-api-key":
		return true
	}
	// Also catch any header whose name mentions an api key / secret / token.
	if strings.Contains(lower, "api-key") || strings.Contains(lower, "apikey") ||
		strings.Contains(lower, "authorization") || strings.Contains(lower, "secret") ||
		strings.Contains(lower, "token") {
		return true
	}
	return false
}

// BuildRawRequestHeadersForInterceptLog returns a JSON string of the request
// headers for an interception log, masking sensitive headers (Authorization,
// cookies, API keys). The output is bounded to rawInterceptLogMaxBytes so a
// large header set does not blow up the TEXT column. It does NOT read or modify
// the request body.
func BuildRawRequestHeadersForInterceptLog(ctx *gin.Context) string {
	if ctx == nil || ctx.Request == nil || len(ctx.Request.Header) == 0 {
		return ""
	}
	headers := make(map[string][]string, len(ctx.Request.Header))
	for key, values := range ctx.Request.Header {
		if isRawInterceptHeaderSensitive(key) {
			masked := make([]string, len(values))
			for i := range values {
				masked[i] = "***masked***"
			}
			headers[key] = masked
			continue
		}
		copied := make([]string, len(values))
		copy(copied, values)
		headers[key] = copied
	}
	data, err := common.Marshal(headers)
	if err != nil {
		return ""
	}
	return boundRawInterceptOutput(string(data))
}

// BuildRawRequestParamsForInterceptLog returns a JSON string of the request
// body for an interception log. It reads the body via the io.Seeker body
// storage (seeking back to 0 + restoring c.Request.Body afterwards so the relay
// chain can still re-read it), falling back to the request DTO when the body is
// unavailable. Output is bounded to rawInterceptLogMaxBytes.
func BuildRawRequestParamsForInterceptLog(ctx *gin.Context, request dto.Request) string {
	if ctx != nil {
		if body, ok := readRequestBodyBytes(ctx); ok && len(body) > 0 {
			return boundRawInterceptOutput(string(body))
		}
	}
	payload := requestToMap(request)
	if payload == nil {
		return ""
	}
	data, err := common.Marshal(payload)
	if err != nil {
		return ""
	}
	return boundRawInterceptOutput(string(data))
}

// boundRawInterceptOutput truncates the raw intercept log output to
// rawInterceptLogMaxBytes, appending an ellipsis when truncated.
func boundRawInterceptOutput(s string) string {
	if len(s) <= rawInterceptLogMaxBytes {
		return s
	}
	return s[:rawInterceptLogMaxBytes] + "..."
}

// keep sanitizeParamValueForLog / sanitizeParamValue referenced: they are
// library-grade helpers exported indirectly via the group variants above.
// Defining a no-op reference avoids unused-symbol warnings when only the group
// variant is called from tests.
var _ = sanitizeParamValueForLog
var _ = sanitizeParamValue
