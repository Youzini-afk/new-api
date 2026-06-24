package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

// logScreeningRequestPaths returns the request_path filter for the
// chat_completions screening scope.
func logScreeningRequestPaths() []string {
	return model.LogScreeningDefaultRequestPaths
}

// logScreeningMatchedTarget captures the computed match for a single user
// during a screening rule pass.
type logScreeningMatchedTarget struct {
	UserId           int
	RequestCount     int
	TPM              int
	RPM              int
	RPH              int
	LastSeen         int64
	ParamHits        []string
	UAHits           []string
	PromptDeltaCount int
	PromptDeltaMax   int
	IP               string
	TokenName        string
}

// logScreeningWindowDuration maps a window identifier to its duration.
func logScreeningWindowDuration(window string) time.Duration {
	switch window {
	case system_setting.LogScreeningWindow24h:
		return 24 * time.Hour
	case system_setting.LogScreeningWindow1h:
		return time.Hour
	default:
		return time.Hour
	}
}

// logScreeningAllowedParamFields flattens the resolved per-group field allow
// list into a set, used to filter which param-rule fields are inspected.
func logScreeningAllowedParamFields() map[string]struct{} {
	fields := system_setting.ResolveRelayParamRecordFields()
	result := make(map[string]struct{})
	for _, list := range fields {
		for _, field := range list {
			field = strings.TrimSpace(field)
			if field == "" {
				continue
			}
			result[field] = struct{}{}
		}
	}
	return result
}

// computeLogScreeningMatches applies a single screening rule to the per-user
// aggregate rows + detail rows. It returns the matched targets keyed by user.
//
// Match strategy:
//   - primary (rough) thresholds: request_count / rpm / rph / tpm OR ua_direct;
//     if no secondary conditions are configured, a primary hit is a match.
//   - secondary conditions: ParamRules / PromptDelta / UABlacklist. When any
//     secondary condition is configured, the match also requires the secondary
//     gate per secondary_mode (or/all). ua_direct always matches directly.
func computeLogScreeningMatches(
	rule system_setting.LogScreeningRule,
	rows []model.LogScreeningAggRow,
	detailMap map[int][]model.LogScreeningLogDetail,
	windowMinutes int,
	windowHours int,
	allowedFields map[string]struct{},
) map[int]*logScreeningMatchedTarget {
	result := make(map[int]*logScreeningMatchedTarget)
	if windowMinutes <= 0 {
		windowMinutes = 1
	}
	if windowHours <= 0 {
		windowHours = 1
	}
	secondaryMode := strings.ToLower(strings.TrimSpace(rule.SecondaryMode))
	if secondaryMode == "" {
		secondaryMode = system_setting.LogScreeningSecondaryModeOr
	}
	hasSecondaryConfig := len(rule.ParamRules) > 0 || rule.PromptDeltaCount > 0 || len(rule.UABlacklist) > 0

	for _, row := range rows {
		rpm := int(float64(row.RequestCount) / float64(windowMinutes))
		tpm := int(float64(row.TotalTokens) / float64(windowMinutes))
		rph := int(float64(row.RequestCount) / float64(windowHours))
		details := detailMap[row.UserId]

		uaDirectHits := logScreeningExtractUAHitsByDetails(details, rule.UADirect)
		roughMatch := false
		if rule.RequestCount > 0 && row.RequestCount >= rule.RequestCount {
			roughMatch = true
		}
		if rule.TPM > 0 && tpm >= rule.TPM {
			roughMatch = true
		}
		if rule.RPM > 0 && rpm >= rule.RPM {
			roughMatch = true
		}
		if rule.RPH > 0 && rph >= rule.RPH {
			roughMatch = true
		}
		if !roughMatch && len(uaDirectHits) == 0 {
			continue
		}

		var paramHits []string
		var uaHits []string
		var deltaCount, deltaMax int
		if hasSecondaryConfig {
			paramHits = logScreeningExtractParamHitsByDetails(details, rule.ParamRules, allowedFields)
			uaHits = logScreeningExtractUAHitsByDetails(details, rule.UABlacklist)
			deltaCount, deltaMax = logScreeningComputePromptDeltas(details, rule.PromptDelta)
		}

		secondaryMatched := false
		if hasSecondaryConfig {
			if secondaryMode == system_setting.LogScreeningSecondaryModeAll {
				secondaryMatched = true
				if len(rule.ParamRules) > 0 && len(paramHits) == 0 {
					secondaryMatched = false
				}
				if rule.PromptDelta > 0 && rule.PromptDeltaCount > 0 && deltaCount < rule.PromptDeltaCount {
					secondaryMatched = false
				}
				if len(rule.UABlacklist) > 0 && len(uaHits) == 0 {
					secondaryMatched = false
				}
			} else {
				if (rule.PromptDelta > 0 && rule.PromptDeltaCount > 0 && deltaCount >= rule.PromptDeltaCount) || len(paramHits) > 0 || len(uaHits) > 0 {
					secondaryMatched = true
				}
			}
		}

		// If there are no secondary conditions, a primary hit is sufficient.
		// ua_direct always matches directly (bypassing the secondary gate).
		if len(uaDirectHits) == 0 && hasSecondaryConfig && !secondaryMatched {
			continue
		}
		combinedUAHits := append([]string{}, uaHits...)
		if len(uaDirectHits) > 0 {
			combinedUAHits = append(combinedUAHits, uaDirectHits...)
		}
		sort.Strings(combinedUAHits)

		ip, tokenName := model.LogScreeningPickTopIPAndToken(details)
		selected := &logScreeningMatchedTarget{
			UserId:           row.UserId,
			RequestCount:     row.RequestCount,
			TPM:              tpm,
			RPM:              rpm,
			RPH:              rph,
			LastSeen:         row.LastSeen,
			ParamHits:        paramHits,
			UAHits:           combinedUAHits,
			PromptDeltaCount: deltaCount,
			PromptDeltaMax:   deltaMax,
			IP:               ip,
			TokenName:       tokenName,
		}
		result[row.UserId] = selected
	}
	return result
}

// logScreeningExtractParamHits parses logs.other (JSON) for each detail row,
// extracts the nested param field value, and reports which ParamRules hit.
// Fields not in the allowedFields set are skipped (defense-in-depth).
func logScreeningExtractParamHits(other string, paramRules []system_setting.LogScreeningParamRule, allowedFields map[string]struct{}) []string {
	if len(paramRules) == 0 || other == "" {
		return nil
	}
	otherMap, err := common.StrToMap(other)
	if err != nil {
		return nil
	}
	rawParams, ok := otherMap[common.RequestParamsOtherKey]
	if !ok || rawParams == nil {
		return nil
	}
	params, ok := rawParams.(map[string]interface{})
	if !ok {
		return nil
	}
	var hits []string
	for _, rule := range paramRules {
		field := strings.TrimSpace(rule.Field)
		if field == "" {
			continue
		}
		if len(allowedFields) > 0 {
			if _, ok := allowedFields[field]; !ok {
				continue
			}
		}
		value, ok := params[field]
		if !ok {
			continue
		}
		numeric, ok := logScreeningToFloat(value)
		if !ok {
			continue
		}
		if logScreeningCompareValue(numeric, rule.Op, rule.Value) {
			hits = append(hits, field)
		}
	}
	return hits
}

// logScreeningExtractParamHitsByDetails aggregates param hits across all detail
// rows for a user (union of hit fields, sorted).
func logScreeningExtractParamHitsByDetails(details []model.LogScreeningLogDetail, paramRules []system_setting.LogScreeningParamRule, allowedFields map[string]struct{}) []string {
	if len(details) == 0 || len(paramRules) == 0 {
		return nil
	}
	hitSet := make(map[string]struct{})
	for _, detail := range details {
		hits := logScreeningExtractParamHits(detail.Other, paramRules, allowedFields)
		for _, hit := range hits {
			hitSet[hit] = struct{}{}
		}
	}
	if len(hitSet) == 0 {
		return nil
	}
	result := make([]string, 0, len(hitSet))
	for hit := range hitSet {
		result = append(result, hit)
	}
	sort.Strings(result)
	return result
}

// logScreeningMatchUA checks whether userAgent contains any blacklist entry
// (case-insensitive substring match).
func logScreeningMatchUA(userAgent string, blacklist []string) []string {
	if userAgent == "" || len(blacklist) == 0 {
		return nil
	}
	check := strings.ToLower(userAgent)
	var hits []string
	for _, item := range blacklist {
		value := strings.TrimSpace(item)
		if value == "" {
			continue
		}
		if strings.Contains(check, strings.ToLower(value)) {
			hits = append(hits, value)
		}
	}
	return hits
}

// logScreeningExtractUAHitsByDetails aggregates UA hits across all detail rows
// for a user (union of hit entries, sorted).
func logScreeningExtractUAHitsByDetails(details []model.LogScreeningLogDetail, blacklist []string) []string {
	if len(details) == 0 || len(blacklist) == 0 {
		return nil
	}
	hitSet := make(map[string]struct{})
	for _, detail := range details {
		hits := logScreeningMatchUA(detail.UserAgent, blacklist)
		for _, hit := range hits {
			hitSet[hit] = struct{}{}
		}
	}
	if len(hitSet) == 0 {
		return nil
	}
	result := make([]string, 0, len(hitSet))
	for hit := range hitSet {
		result = append(result, hit)
	}
	sort.Strings(result)
	return result
}

// logScreeningComputePromptDeltas computes the count of consecutive prompt
// token deltas that meet or exceed `threshold`, plus the max delta.
// Details must be ordered by created_at asc (caller responsibility).
func logScreeningComputePromptDeltas(details []model.LogScreeningLogDetail, threshold int) (count int, maxDelta int) {
	if len(details) == 0 || threshold <= 0 {
		return 0, 0
	}
	prev := details[0].PromptTokens
	for i := 1; i < len(details); i++ {
		delta := details[i].PromptTokens - prev
		if delta < 0 {
			delta = -delta
		}
		if delta > maxDelta {
			maxDelta = delta
		}
		if delta >= threshold {
			count++
		}
		prev = details[i].PromptTokens
	}
	return count, maxDelta
}

// logScreeningToFloat coerces a JSON-decoded value (float64/json.Number/string)
// to float64.
func logScreeningToFloat(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		if err == nil {
			return f, true
		}
		return 0, false
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err == nil {
			return f, true
		}
	}
	return 0, false
}

// logScreeningCompareValue applies the op (default ">=") to value vs threshold.
func logScreeningCompareValue(value float64, op string, threshold float64) bool {
	op = strings.TrimSpace(op)
	if op == "<=" {
		return value <= threshold
	}
	return value >= threshold
}

// appendLogScreeningUserRemark appends a de-duplicated "[日志筛查]rule yyyy-mm-dd
// hh:mm" line to the user's remark, for new or manual matches. No ban_sync.
func appendLogScreeningUserRemark(ctx context.Context, userId int, ruleName string, matchedAt int64) error {
	if userId == 0 || ruleName == "" {
		return nil
	}
	when := time.Unix(matchedAt, 0).Format("2006-01-02 15:04")
	line := fmt.Sprintf("[日志筛查]%s %s", ruleName, when)
	_, err := AppendUserRemarkLine(userId, line)
	return err
}
