package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

var riskAgentJSONPunctuationReplacer = strings.NewReplacer(
	"\ufeff", "",
	"“", "\"",
	"”", "\"",
	"：", ":",
	"，", ",",
)

func parseRiskAgentDecisionTolerant(content string) (riskAgentDecision, string, bool, error) {
	var empty riskAgentDecision
	candidates := collectRiskAgentJSONCandidates(content)
	if len(candidates) == 0 {
		return empty, "", false, nil
	}
	var lastErr error
	for _, candidate := range candidates {
		object, err := decodeRiskAgentJSONObject(candidate)
		if err != nil {
			lastErr = err
			continue
		}
		decision, recognized, warnings := normalizeRiskAgentDecisionObject(object)
		if recognized < 2 {
			lastErr = errors.New("JSON object does not contain enough recognized risk Agent fields")
			continue
		}
		decision.ValidationWarnings = appendRiskAgentWarnings(decision.ValidationWarnings, warnings...)
		normalized, err := common.Marshal(decision)
		if err != nil {
			return empty, "", true, err
		}
		return decision, string(normalized), true, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no usable JSON object found")
	}
	return empty, "", true, fmt.Errorf("risk Agent response does not contain a usable decision object: %w", lastErr)
}

func collectRiskAgentJSONCandidates(content string) []string {
	content = strings.TrimSpace(strings.TrimPrefix(content, "\ufeff"))
	if content == "" {
		return nil
	}
	candidates := make([]string, 0, 24)
	seen := make(map[string]struct{}, 24)
	add := func(candidate string) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || len(candidate) > 256*1024 {
			return
		}
		if _, exists := seen[candidate]; exists {
			return
		}
		seen[candidate] = struct{}{}
		candidates = append(candidates, candidate)
		repaired := repairRiskAgentJSONCandidate(candidate)
		if repaired != candidate {
			if _, exists := seen[repaired]; !exists {
				seen[repaired] = struct{}{}
				candidates = append(candidates, repaired)
			}
		}
	}

	withoutThinking := stripRiskAgentThinking(content)
	add(withoutThinking)
	for _, fenced := range extractRiskAgentCodeFences(content) {
		add(fenced)
	}
	balancedSource := riskAgentJSONPunctuationReplacer.Replace(withoutThinking)
	balanced := extractBalancedRiskAgentJSONObjects(balancedSource)
	for index := len(balanced) - 1; index >= 0; index-- {
		add(balanced[index])
	}
	if withoutThinking != content {
		balanced = extractBalancedRiskAgentJSONObjects(riskAgentJSONPunctuationReplacer.Replace(content))
		for index := len(balanced) - 1; index >= 0; index-- {
			add(balanced[index])
		}
	}
	add(normalizeErrorInsightAIJSONContent(content))
	add(content)
	return candidates
}

func stripRiskAgentThinking(content string) string {
	lower := strings.ToLower(content)
	for _, closingTag := range []string{"</think>", "</analysis>", "</reasoning>"} {
		if index := strings.LastIndex(lower, closingTag); index >= 0 {
			return strings.TrimSpace(content[index+len(closingTag):])
		}
	}
	return content
}

func extractRiskAgentCodeFences(content string) []string {
	results := make([]string, 0, 4)
	remaining := content
	for len(results) < 8 {
		start := strings.Index(remaining, "```")
		if start < 0 {
			break
		}
		remaining = remaining[start+3:]
		if newline := strings.IndexByte(remaining, '\n'); newline >= 0 {
			language := strings.TrimSpace(remaining[:newline])
			if language == "" || strings.EqualFold(language, "json") || strings.EqualFold(language, "javascript") {
				remaining = remaining[newline+1:]
			}
		}
		end := strings.Index(remaining, "```")
		if end < 0 {
			break
		}
		results = append(results, remaining[:end])
		remaining = remaining[end+3:]
	}
	return results
}

func extractBalancedRiskAgentJSONObjects(content string) []string {
	results := make([]string, 0, 8)
	for start := 0; start < len(content) && len(results) < 32; start++ {
		if content[start] != '{' {
			continue
		}
		stack := make([]byte, 0, 8)
		inString := false
		escaped := false
		for index := start; index < len(content); index++ {
			character := content[index]
			if inString {
				if escaped {
					escaped = false
					continue
				}
				if character == '\\' {
					escaped = true
					continue
				}
				if character == '"' {
					inString = false
				}
				continue
			}
			switch character {
			case '"':
				inString = true
			case '{', '[':
				stack = append(stack, character)
			case '}', ']':
				if len(stack) == 0 || !riskAgentJSONBracketsMatch(stack[len(stack)-1], character) {
					index = len(content)
					continue
				}
				stack = stack[:len(stack)-1]
				if len(stack) == 0 {
					results = append(results, content[start:index+1])
					start = index
					index = len(content)
				}
			}
		}
	}
	return results
}

func riskAgentJSONBracketsMatch(opening byte, closing byte) bool {
	return (opening == '{' && closing == '}') || (opening == '[' && closing == ']')
}

func repairRiskAgentJSONCandidate(candidate string) string {
	candidate = riskAgentJSONPunctuationReplacer.Replace(strings.TrimSpace(candidate))
	var builder strings.Builder
	builder.Grow(len(candidate))
	inString := false
	escaped := false
	for index := 0; index < len(candidate); index++ {
		character := candidate[index]
		if inString {
			builder.WriteByte(character)
			if escaped {
				escaped = false
			} else if character == '\\' {
				escaped = true
			} else if character == '"' {
				inString = false
			}
			continue
		}
		if character == '"' {
			inString = true
			builder.WriteByte(character)
			continue
		}
		if character == ',' {
			next := index + 1
			for next < len(candidate) && (candidate[next] == ' ' || candidate[next] == '\n' || candidate[next] == '\r' || candidate[next] == '\t') {
				next++
			}
			if next < len(candidate) && (candidate[next] == '}' || candidate[next] == ']') {
				continue
			}
		}
		builder.WriteByte(character)
	}
	return strings.TrimSpace(builder.String())
}

func decodeRiskAgentJSONObject(candidate string) (map[string]any, error) {
	current := candidate
	for depth := 0; depth < 3; depth++ {
		decoder := json.NewDecoder(strings.NewReader(current))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		if encoded, ok := value.(string); ok {
			current = strings.TrimSpace(encoded)
			continue
		}
		return unwrapRiskAgentDecisionObject(value, 0)
	}
	return nil, errors.New("risk Agent JSON is wrapped too deeply")
}

func unwrapRiskAgentDecisionObject(value any, depth int) (map[string]any, error) {
	if depth > 4 {
		return nil, errors.New("risk Agent decision wrapper is too deep")
	}
	switch typed := value.(type) {
	case map[string]any:
		normalized := normalizeRiskAgentObjectKeys(typed)
		if riskAgentObjectHasDecisionFields(normalized) {
			return normalized, nil
		}
		for _, key := range []string{"result", "decision", "analysis", "data", "output", "response"} {
			if nested, exists := normalized[key]; exists {
				if object, err := unwrapRiskAgentDecisionObject(nested, depth+1); err == nil {
					return object, nil
				}
			}
		}
		return normalized, nil
	case []any:
		for index := len(typed) - 1; index >= 0; index-- {
			if object, err := unwrapRiskAgentDecisionObject(typed[index], depth+1); err == nil {
				return object, nil
			}
		}
		return nil, errors.New("risk Agent JSON array does not contain an object")
	default:
		return nil, errors.New("risk Agent JSON root is not an object")
	}
}

func normalizeRiskAgentObjectKeys(object map[string]any) map[string]any {
	normalized := make(map[string]any, len(object))
	for key, value := range object {
		key = normalizeRiskAgentKey(key)
		if key != "" {
			normalized[key] = value
		}
	}
	return normalized
}

func normalizeRiskAgentKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.NewReplacer("-", "_", " ", "_", ".", "_").Replace(key)
	for strings.Contains(key, "__") {
		key = strings.ReplaceAll(key, "__", "_")
	}
	return strings.Trim(key, "_")
}

func riskAgentObjectHasDecisionFields(object map[string]any) bool {
	for _, key := range []string{"verdict", "risk_type", "risk_score", "recommended_action", "action", "policy_violation", "evidence"} {
		if _, exists := object[key]; exists {
			return true
		}
	}
	return false
}

func normalizeRiskAgentDecisionObject(object map[string]any) (riskAgentDecision, int, []string) {
	var decision riskAgentDecision
	warnings := make([]string, 0, 8)
	recognized := 0
	read := func(keys ...string) (any, bool) {
		for _, key := range keys {
			if value, exists := object[key]; exists {
				recognized++
				return value, true
			}
		}
		return nil, false
	}

	if value, ok := read("verdict", "risk_type", "category", "conclusion"); ok {
		decision.Verdict = normalizeRiskAgentVerdict(riskAgentString(value))
	}
	if decision.Verdict == "" {
		decision.Verdict = "uncertain"
		warnings = append(warnings, "模型未返回可识别的 verdict，已设为 uncertain")
	}
	if value, ok := read("risk_score", "score", "risk"); ok {
		decision.RiskScore = riskAgentInt(value)
	}
	if decision.RiskScore < 0 || decision.RiskScore > 100 {
		warnings = append(warnings, "risk_score 超出 0-100，已截断")
		decision.RiskScore = clampRiskScore(decision.RiskScore)
	}
	if value, ok := read("confidence", "confidence_score", "probability"); ok {
		decision.Confidence = riskAgentFloat(value)
	}
	if decision.Confidence > 1 && decision.Confidence <= 100 {
		decision.Confidence /= 100
		warnings = append(warnings, "confidence 使用百分数返回，已转换为 0-1")
	}
	if decision.Confidence < 0 || decision.Confidence > 1 {
		warnings = append(warnings, "confidence 超出 0-1，已截断")
		decision.Confidence = max(0, min(1, decision.Confidence))
	}
	if value, ok := read("agrees_with_triage", "agree_with_triage", "triage_agreement"); ok {
		decision.AgreesWithTriage = riskAgentBool(value)
	}
	if value, ok := read("policy_violation", "is_policy_violation", "violation"); ok {
		decision.PolicyViolation = riskAgentBool(value)
	}
	if value, ok := read("evidence", "evidences", "supporting_evidence"); ok {
		decision.Evidence, warnings = normalizeRiskAgentEvidenceItems(value, warnings)
	}
	if decision.Evidence == nil {
		decision.Evidence = []riskAgentEvidence{}
	}
	if value, ok := read("counter_evidence", "counterevidence", "mitigating_factors"); ok {
		decision.CounterEvidence = riskAgentStringSlice(value)
	}
	if decision.CounterEvidence == nil {
		decision.CounterEvidence = []string{}
	}
	if value, ok := read("recommended_action", "action", "recommendation"); ok {
		decision.RecommendedAction = normalizeRiskAgentAction(riskAgentString(value))
	}
	if decision.RecommendedAction == "" {
		decision.RecommendedAction = model.RiskActionManualReview
		warnings = append(warnings, "模型未返回可识别的 recommended_action，已降级为 manual_review")
	}
	if value, ok := read("recommended_duration_minutes", "duration_minutes", "duration"); ok {
		decision.RecommendedDurationMinutes = riskAgentInt(value)
	}
	if decision.RecommendedDurationMinutes < 0 || decision.RecommendedDurationMinutes > 43200 {
		warnings = append(warnings, "recommended_duration_minutes 超出范围，已截断")
		decision.RecommendedDurationMinutes = max(0, min(43200, decision.RecommendedDurationMinutes))
	}
	if value, ok := read("admin_reason", "reason", "rationale", "analysis_summary"); ok {
		decision.AdminReason = truncateRiskAgentText(riskAgentString(value), 4000)
	}
	if value, ok := read("user_reason", "user_message", "public_reason"); ok {
		decision.UserReason = truncateRiskAgentText(riskAgentString(value), 2000)
	}
	if value, ok := read("suggested_fingerprint", "fingerprint", "suggested_rule"); ok {
		decision.SuggestedFingerprint = normalizeRiskSuggestedFingerprint(value)
	}
	if !validRiskSuggestedFingerprintKind(decision.SuggestedFingerprint.Kind) {
		warnings = append(warnings, "suggested_fingerprint.kind 无效，已忽略")
		decision.SuggestedFingerprint = riskSuggestedFingerprint{Kind: "none"}
	}
	return decision, recognized, warnings
}

func normalizeRiskAgentEvidenceItems(value any, warnings []string) ([]riskAgentEvidence, []string) {
	items := make([]any, 0)
	switch typed := value.(type) {
	case []any:
		items = typed
	case map[string]any:
		items = append(items, typed)
	case string:
		var decoded any
		decoder := json.NewDecoder(strings.NewReader(typed))
		decoder.UseNumber()
		if err := decoder.Decode(&decoded); err == nil {
			return normalizeRiskAgentEvidenceItems(decoded, warnings)
		}
		warnings = append(warnings, "evidence 不是数组或对象，已忽略")
		return []riskAgentEvidence{}, warnings
	default:
		return []riskAgentEvidence{}, warnings
	}
	if len(items) > 50 {
		items = items[:50]
		warnings = append(warnings, "evidence 超过 50 条，已截断")
	}
	result := make([]riskAgentEvidence, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			warnings = append(warnings, "存在非对象 evidence，已忽略")
			continue
		}
		object = normalizeRiskAgentObjectKeys(object)
		evidence := riskAgentEvidence{
			SignalID:   strings.TrimSpace(riskAgentString(firstRiskAgentValue(object, "signal_id", "signal", "id"))),
			Strength:   riskAgentInt(firstRiskAgentValue(object, "strength", "score", "weight")),
			Summary:    truncateRiskAgentText(strings.TrimSpace(riskAgentString(firstRiskAgentValue(object, "summary", "reason", "description"))), 2000),
			RequestIDs: riskAgentStringSlice(firstRiskAgentValue(object, "request_ids", "requests", "request_id")),
		}
		if evidence.Strength < 0 || evidence.Strength > 100 {
			evidence.Strength = clampRiskScore(evidence.Strength)
			warnings = append(warnings, "evidence.strength 超出范围，已截断")
		}
		if evidence.Summary == "" && evidence.SignalID != "" {
			evidence.Summary = "Agent 引用了该信号，但未提供摘要"
			warnings = append(warnings, "evidence 缺少 summary，已补充占位说明")
		}
		result = append(result, evidence)
	}
	return result, warnings
}

func sanitizeRiskAgentDecision(decision riskAgentDecision, input riskAgentInput) riskAgentDecision {
	warnings := append([]string(nil), decision.ValidationWarnings...)
	evidence := make([]riskAgentEvidence, 0, len(decision.Evidence))
	for _, item := range decision.Evidence {
		item.SignalID = strings.TrimSpace(item.SignalID)
		item.Summary = strings.TrimSpace(item.Summary)
		if item.SignalID == "" || item.Summary == "" {
			warnings = append(warnings, "缺少 signal_id 或 summary 的 evidence 已忽略")
			continue
		}
		if _, exists := input.AllowedSignalIDs[item.SignalID]; !exists {
			warnings = append(warnings, "模型引用了不存在的 signal_id "+item.SignalID+"，该证据已忽略")
			continue
		}
		requestIDs := make([]string, 0, len(item.RequestIDs))
		seen := make(map[string]struct{}, len(item.RequestIDs))
		for _, requestID := range item.RequestIDs {
			requestID = strings.TrimSpace(requestID)
			if requestID == "" {
				continue
			}
			if _, duplicate := seen[requestID]; duplicate {
				continue
			}
			seen[requestID] = struct{}{}
			if _, exists := input.AllowedRequestIDs[requestID]; !exists {
				warnings = append(warnings, "模型引用了不存在的 request_id "+requestID+"，该引用已移除")
				continue
			}
			requestIDs = append(requestIDs, requestID)
		}
		item.RequestIDs = requestIDs
		evidence = append(evidence, item)
	}
	decision.Evidence = evidence

	if riskAgentActionIsEnforcement(decision.RecommendedAction) {
		reason := ""
		switch {
		case !decision.PolicyViolation:
			reason = "模型建议执行限制，但 policy_violation=false"
		case len(decision.Evidence) == 0:
			reason = "模型建议执行限制，但没有通过本地校验的证据"
		case strings.TrimSpace(decision.AdminReason) == "":
			reason = "模型建议执行限制，但没有提供管理员理由"
		}
		if reason != "" {
			decision.RecommendedAction = model.RiskActionManualReview
			decision.RecommendedDurationMinutes = 0
			warnings = append(warnings, reason+"，已降级为 manual_review")
			if decision.AdminReason == "" {
				decision.AdminReason = reason
			} else {
				decision.AdminReason = truncateRiskAgentText(decision.AdminReason+"\n\n本地安全降级："+reason, 4000)
			}
		}
	}
	decision.ValidationWarnings = appendRiskAgentWarnings(nil, warnings...)
	return decision
}

func marshalRiskAgentDecision(decision riskAgentDecision) (string, error) {
	normalized, err := common.Marshal(decision)
	if err != nil {
		return "", err
	}
	return string(normalized), nil
}

func riskAgentActionIsEnforcement(action string) bool {
	return action == model.RiskActionRateLimit ||
		action == model.RiskActionFreezeToken ||
		action == model.RiskActionTemporaryBlock ||
		action == model.RiskActionPermanentBan
}

func appendRiskAgentWarnings(existing []string, warnings ...string) []string {
	result := append([]string(nil), existing...)
	seen := make(map[string]struct{}, len(result)+len(warnings))
	for _, warning := range result {
		seen[warning] = struct{}{}
	}
	for _, warning := range warnings {
		warning = truncateRiskAgentText(strings.TrimSpace(warning), 500)
		if warning == "" {
			continue
		}
		if _, duplicate := seen[warning]; duplicate {
			continue
		}
		seen[warning] = struct{}{}
		result = append(result, warning)
		if len(result) >= 30 {
			break
		}
	}
	return result
}

func normalizeRiskAgentVerdict(value string) string {
	value = normalizeRiskAgentKey(value)
	switch value {
	case "normal", "正常", "safe", "benign":
		return "normal"
	case "small_share", "small_sharing", "family_share", "亲友共享", "少量共享":
		return "small_share"
	case "key_leak", "key_leakage", "api_key_leak", "密钥泄露":
		return "key_leak"
	case "gateway_distribution", "gateway", "redistribution_gateway", "网关分发", "中转分发":
		return "gateway_distribution"
	case "multi_node_gateway", "multi_gateway", "multi_node", "多节点中转":
		return "multi_node_gateway"
	case "commercial_resale", "resale", "commercial", "商业倒卖", "倒卖":
		return "commercial_resale"
	case "forbidden_paid_client", "paid_client", "closed_source_paid_client", "付费闭源客户端":
		return "forbidden_paid_client"
	case "uncertain", "unknown", "不确定", "需复核":
		return "uncertain"
	default:
		return ""
	}
}

func normalizeRiskAgentAction(value string) string {
	value = normalizeRiskAgentKey(value)
	switch value {
	case "none", "no_action", "无":
		return model.RiskActionNone
	case "observe", "watch", "monitor", "观察":
		return model.RiskActionObserve
	case "rate_limit", "limit", "限速":
		return model.RiskActionRateLimit
	case "freeze_token", "freeze_key", "冻结令牌":
		return model.RiskActionFreezeToken
	case "temporary_block", "temp_block", "temporary_ban", "临时封禁":
		return model.RiskActionTemporaryBlock
	case "permanent_ban", "ban", "永久封禁":
		return model.RiskActionPermanentBan
	case "manual_review", "review", "human_review", "人工复核":
		return model.RiskActionManualReview
	default:
		return ""
	}
}

func normalizeRiskSuggestedFingerprint(value any) riskSuggestedFingerprint {
	object, ok := value.(map[string]any)
	if !ok {
		return riskSuggestedFingerprint{Kind: "none"}
	}
	object = normalizeRiskAgentObjectKeys(object)
	kind := normalizeRiskAgentKey(riskAgentString(firstRiskAgentValue(object, "kind", "type")))
	if kind == "" {
		kind = "none"
	}
	return riskSuggestedFingerprint{
		Kind:    kind,
		Pattern: truncateRiskAgentText(strings.TrimSpace(riskAgentString(firstRiskAgentValue(object, "pattern", "value"))), 512),
		Reason:  truncateRiskAgentText(strings.TrimSpace(riskAgentString(firstRiskAgentValue(object, "reason", "description"))), 2000),
	}
}

func firstRiskAgentValue(object map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, exists := object[key]; exists {
			return value
		}
	}
	return nil
}

func riskAgentString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := riskAgentString(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "; ")
	default:
		encoded, err := common.Marshal(typed)
		if err == nil {
			return string(encoded)
		}
		return ""
	}
}

func riskAgentInt(value any) int {
	switch typed := value.(type) {
	case json.Number:
		if integer, err := typed.Int64(); err == nil {
			return int(integer)
		}
		if number, err := typed.Float64(); err == nil {
			return int(number)
		}
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	case int:
		return typed
	case int64:
		return int(typed)
	case string:
		value := strings.TrimSpace(strings.TrimSuffix(typed, "%"))
		if number, err := strconv.ParseFloat(value, 64); err == nil {
			return int(number)
		}
	}
	return 0
}

func riskAgentFloat(value any) float64 {
	switch typed := value.(type) {
	case json.Number:
		number, _ := typed.Float64()
		return number
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case string:
		value := strings.TrimSpace(typed)
		percent := strings.HasSuffix(value, "%")
		value = strings.TrimSuffix(value, "%")
		number, _ := strconv.ParseFloat(value, 64)
		if percent {
			return number / 100
		}
		return number
	default:
		return 0
	}
}

func riskAgentBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case json.Number:
		return typed.String() != "0"
	case float64:
		return typed != 0
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "1", "yes", "y", "是", "同意":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func riskAgentStringSlice(value any) []string {
	result := make([]string, 0)
	appendValue := func(item any) {
		text := strings.TrimSpace(riskAgentString(item))
		if text != "" {
			result = append(result, text)
		}
	}
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			appendValue(item)
		}
	case []string:
		for _, item := range typed {
			appendValue(item)
		}
	case string:
		trimmed := strings.TrimSpace(typed)
		if strings.HasPrefix(trimmed, "[") {
			var decoded []any
			if err := common.Unmarshal([]byte(trimmed), &decoded); err == nil {
				return riskAgentStringSlice(decoded)
			}
		}
		if strings.Contains(trimmed, ",") {
			for _, item := range strings.Split(trimmed, ",") {
				appendValue(item)
			}
		} else {
			appendValue(trimmed)
		}
	default:
		appendValue(typed)
	}
	if len(result) > 50 {
		result = result[:50]
	}
	return result
}
