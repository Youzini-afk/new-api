package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

const riskDetailBatchUsers = 20

type RiskEvidenceSample struct {
	RequestId     string `json:"request_id"`
	CreatedAt     int64  `json:"created_at"`
	IP            string `json:"ip"`
	UserAgent     string `json:"user_agent"`
	Model         string `json:"model"`
	RequestPath   string `json:"request_path"`
	RequestParams string `json:"request_params,omitempty"`
}

type RiskSignalSummary struct {
	RequestCount          int                  `json:"request_count"`
	TotalTokens           int64                `json:"total_tokens"`
	TotalQuota            int64                `json:"total_quota"`
	DistinctTokens        int                  `json:"distinct_tokens"`
	ErrorCount            int                  `json:"error_count"`
	ErrorRate             float64              `json:"error_rate"`
	MaxRPM                int                  `json:"max_rpm"`
	AverageRPM            float64              `json:"average_rpm"`
	MaxConcurrency        int                  `json:"max_concurrency"`
	DistinctIPs           int                  `json:"distinct_ips"`
	DistinctUAs           int                  `json:"distinct_uas"`
	DistinctModels        int                  `json:"distinct_models"`
	DistinctPaths         int                  `json:"distinct_paths"`
	ActiveHours           int                  `json:"active_hours"`
	DistinctSemantics     int                  `json:"distinct_semantics"`
	DominantSemanticRatio float64              `json:"dominant_semantic_ratio"`
	GatewayUAHits         int                  `json:"gateway_ua_hits"`
	ForbiddenClientUAHits int                  `json:"forbidden_client_ua_hits"`
	TopIP                 string               `json:"top_ip"`
	TopUA                 string               `json:"top_ua"`
	DetailRows            int                  `json:"detail_rows"`
	DetailSampled         bool                 `json:"detail_sampled"`
	Samples               []RiskEvidenceSample `json:"samples"`
}

type RiskScreeningRunSummary struct {
	Status             string  `json:"status"`
	Enabled            bool    `json:"enabled"`
	Manual             bool    `json:"manual"`
	WindowsChecked     int     `json:"windows_checked"`
	CandidatesSeen     int     `json:"candidates_seen"`
	CasesCreated       int     `json:"cases_created"`
	CasesUpdated       int     `json:"cases_updated"`
	CasesResolved      int64   `json:"cases_resolved"`
	CandidateCapped    bool    `json:"candidate_capped"`
	DetailCapped       bool    `json:"detail_capped"`
	StartedAt          int64   `json:"started_at"`
	FinishedAt         int64   `json:"finished_at"`
	ElapsedMs          int64   `json:"elapsed_ms"`
	CaseIds            []int64 `json:"case_ids"`
	AutoActionsApplied int     `json:"auto_actions_applied"`
	AgentAnalyzed      int     `json:"agent_analyzed"`
	AgentAttempts      int     `json:"agent_attempts"`
	AgentErrors        int     `json:"agent_errors"`
	ExpiredActions     int     `json:"expired_actions"`
}

func RunRiskScreening(ctx context.Context, manual bool) (*RiskScreeningRunSummary, error) {
	started := time.Now()
	cfg := *system_setting.GetRiskControlSetting()
	summary := &RiskScreeningRunSummary{
		Enabled:   cfg.Enabled,
		Manual:    manual,
		StartedAt: started.Unix(),
		CaseIds:   []int64{},
	}
	if !cfg.Enabled {
		summary.Status = "disabled"
		summary.FinishedAt = time.Now().Unix()
		summary.ElapsedMs = time.Since(started).Milliseconds()
		return summary, nil
	}

	windowEnd := common.GetTimestamp()
	for _, hours := range cfg.WindowHours {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		windowStart := windowEnd - int64(hours*3600)
		candidates, err := model.RiskListCandidates(ctx, windowStart, windowEnd, cfg.MinRequests, cfg.CandidateLimit+1)
		if err != nil {
			return nil, err
		}
		summary.WindowsChecked++
		summary.CandidatesSeen += len(candidates)
		if len(candidates) > cfg.CandidateLimit {
			summary.CandidateCapped = true
			candidates = candidates[:cfg.CandidateLimit]
		}
		if len(candidates) == 0 {
			continue
		}
		meta, err := model.RiskFillSubjectMeta(ctx, candidates)
		if err != nil {
			return nil, err
		}
		details, capped, err := loadRiskDetails(ctx, candidates, windowStart, windowEnd, cfg.DetailLimit)
		if err != nil {
			return nil, err
		}
		if capped {
			summary.DetailCapped = true
		}

		for _, candidate := range candidates {
			subject, ok := meta[model.RiskSubjectKey(candidate.UserId, candidate.TokenId)]
			if !ok || subject.Role >= common.RoleAdminUser || subject.Status != common.UserStatusEnabled {
				continue
			}
			pairKey := model.RiskSubjectKey(candidate.UserId, candidate.TokenId)
			signals := buildRiskSignals(candidate, details[pairKey], hours, cfg)
			score, verdict, reasons := scoreRiskSignals(signals, hours, cfg)
			if score < cfg.CaseThreshold {
				continue
			}
			riskLevel := riskLevelForScore(score)
			action, duration := deterministicRiskRecommendation(verdict, score, candidate.TokenId, cfg)
			signalJSON, err := common.Marshal(signals)
			if err != nil {
				return nil, err
			}
			requestIds := make([]string, 0, len(signals.Samples))
			for _, sample := range signals.Samples {
				if sample.RequestId != "" {
					requestIds = append(requestIds, sample.RequestId)
				}
			}
			requestIDJSON, err := common.Marshal(requestIds)
			if err != nil {
				return nil, err
			}
			fingerprint := buildRiskCaseFingerprint(candidate.UserId, candidate.TokenId, hours, verdict, candidate.LastSeen, cfg.CaseCooldownMinutes)
			riskCase, created, err := model.UpsertRiskCase(ctx, &model.RiskCase{
				Fingerprint:                fingerprint,
				UserId:                     candidate.UserId,
				Username:                   subject.Username,
				TokenId:                    candidate.TokenId,
				TokenName:                  subject.TokenName,
				Status:                     model.RiskCaseStatusOpen,
				Verdict:                    verdict,
				RuleVerdict:                verdict,
				RiskLevel:                  riskLevel,
				RuleScore:                  score,
				FinalScore:                 score,
				Confidence:                 math.Min(0.85, 0.45+float64(score)/250),
				Signals:                    string(signalJSON),
				SampleRequestIds:           string(requestIDJSON),
				RuleReason:                 strings.Join(reasons, "；"),
				RuleRecommendedAction:      action,
				RuleRecommendedDuration:    duration,
				RecommendedAction:          action,
				RecommendedDurationMinutes: duration,
				RecommendedReason:          strings.Join(reasons, "；"),
				WindowHours:                hours,
				WindowStart:                windowStart,
				WindowEnd:                  windowEnd,
				LastSeenAt:                 candidate.LastSeen,
			})
			if err != nil {
				return nil, err
			}
			if created {
				summary.CasesCreated++
			} else {
				summary.CasesUpdated++
			}
			summary.CaseIds = append(summary.CaseIds, riskCase.Id)
		}
	}
	resolved, err := model.ResolveStaleOpenRiskCases(ctx, windowEnd)
	if err != nil {
		return nil, err
	}
	summary.CasesResolved = resolved

	summary.Status = "completed"
	summary.FinishedAt = time.Now().Unix()
	summary.ElapsedMs = time.Since(started).Milliseconds()
	return summary, nil
}

func loadRiskDetails(ctx context.Context, candidates []model.RiskCandidateAgg, start, end int64, limit int) (map[string][]model.RiskLogDetail, bool, error) {
	result := make(map[string][]model.RiskLogDetail)
	if len(candidates) == 0 || limit <= 0 {
		return result, false, nil
	}
	userIds := make([]int, 0, len(candidates))
	seenUsers := map[int]struct{}{}
	allowedPairs := map[string]struct{}{}
	allowedUserAggregates := map[int]struct{}{}
	for _, candidate := range candidates {
		allowedPairs[model.RiskSubjectKey(candidate.UserId, candidate.TokenId)] = struct{}{}
		if candidate.TokenId == 0 {
			allowedUserAggregates[candidate.UserId] = struct{}{}
		}
		if _, ok := seenUsers[candidate.UserId]; !ok {
			seenUsers[candidate.UserId] = struct{}{}
			userIds = append(userIds, candidate.UserId)
		}
	}
	remaining := limit
	capped := false
	for i := 0; i < len(userIds); i += riskDetailBatchUsers {
		if remaining <= 0 {
			capped = true
			break
		}
		batchEnd := i + riskDetailBatchUsers
		if batchEnd > len(userIds) {
			batchEnd = len(userIds)
		}
		batchesRemaining := (len(userIds) - i + riskDetailBatchUsers - 1) / riskDetailBatchUsers
		batchLimit := remaining / batchesRemaining
		if batchLimit <= 0 {
			batchLimit = 1
		}
		rows, err := model.RiskListLogDetails(ctx, start, end, userIds[i:batchEnd], batchLimit+1)
		if err != nil {
			return nil, false, err
		}
		if len(rows) > batchLimit {
			rows = rows[:batchLimit]
			capped = true
		}
		remaining -= len(rows)
		for _, row := range rows {
			key := model.RiskSubjectKey(row.UserId, row.TokenId)
			if _, ok := allowedPairs[key]; ok {
				result[key] = append(result[key], row)
			}
			if _, ok := allowedUserAggregates[row.UserId]; ok {
				userKey := model.RiskSubjectKey(row.UserId, 0)
				result[userKey] = append(result[userKey], row)
			}
		}
	}
	return result, capped, nil
}

func buildRiskSignals(candidate model.RiskCandidateAgg, details []model.RiskLogDetail, windowHours int, cfg system_setting.RiskControlSetting) RiskSignalSummary {
	ipCounts := map[string]int{}
	uaCounts := map[string]int{}
	modelSet := map[string]struct{}{}
	pathSet := map[string]struct{}{}
	hourSet := map[int64]struct{}{}
	minuteCounts := map[int64]int{}
	semanticCounts := map[string]int{}
	events := make([]riskConcurrencyEvent, 0, len(details)*2)
	sampleCandidates := make([]riskEvidenceCandidate, 0, len(details))
	errorCount := 0
	gatewayUAHits := 0
	forbiddenClientUAHits := 0
	for _, detail := range details {
		if detail.Type == model.LogTypeError {
			errorCount++
		}
		ip := strings.TrimSpace(detail.Ip)
		ua := strings.TrimSpace(detail.UserAgent)
		if ip != "" {
			ipCounts[ip]++
		}
		if ua != "" {
			uaCounts[ua]++
			gateway, forbiddenClient := classifyRiskUserAgent(ua, cfg)
			if gateway {
				gatewayUAHits++
			}
			if forbiddenClient {
				forbiddenClientUAHits++
			}
		}
		if detail.ModelName != "" {
			modelSet[detail.ModelName] = struct{}{}
		}
		if detail.RequestPath != "" {
			pathSet[detail.RequestPath] = struct{}{}
		}
		hourSet[detail.CreatedAt/3600] = struct{}{}
		minuteCounts[detail.CreatedAt/60]++
		duration := int64(detail.UseTime)
		if duration <= 0 {
			duration = 1
		}
		events = append(events,
			riskConcurrencyEvent{at: detail.CreatedAt - duration, delta: 1},
			riskConcurrencyEvent{at: detail.CreatedAt, delta: -1},
		)
		params := extractRiskRequestParams(detail.Other, cfg.RedactSensitive)
		semanticKey := ""
		if params != "" {
			hash := sha256.Sum256([]byte(params))
			semanticKey = hex.EncodeToString(hash[:])
			semanticCounts[semanticKey]++
		}
		sample := RiskEvidenceSample{
			RequestId:   detail.RequestId,
			CreatedAt:   detail.CreatedAt,
			IP:          ip,
			UserAgent:   ua,
			Model:       detail.ModelName,
			RequestPath: detail.RequestPath,
		}
		if cfg.IncludeRequestContent {
			sample.RequestParams = params
		}
		sampleCandidates = append(sampleCandidates, riskEvidenceCandidate{
			sample:      sample,
			semanticKey: semanticKey,
			hourBucket:  detail.CreatedAt / 3600,
		})
	}
	maxRPM := 0
	for _, count := range minuteCounts {
		if count > maxRPM {
			maxRPM = count
		}
	}
	windowMinutes := windowHours * 60
	if windowMinutes <= 0 {
		windowMinutes = 1
	}
	averageRPM := float64(candidate.RequestCount) / float64(windowMinutes)
	if int(math.Ceil(averageRPM)) > maxRPM {
		maxRPM = int(math.Ceil(averageRPM))
	}
	dominantSemantic := 0
	for _, count := range semanticCounts {
		if count > dominantSemantic {
			dominantSemantic = count
		}
	}
	dominantRatio := 0.0
	if len(details) > 0 {
		dominantRatio = float64(dominantSemantic) / float64(len(details))
	}
	errorRate := 0.0
	if len(details) > 0 {
		errorRate = float64(errorCount) / float64(len(details))
	}
	return RiskSignalSummary{
		RequestCount:          candidate.RequestCount,
		TotalTokens:           candidate.TotalTokens,
		TotalQuota:            candidate.TotalQuota,
		DistinctTokens:        candidate.DistinctTokens,
		ErrorCount:            errorCount,
		ErrorRate:             errorRate,
		MaxRPM:                maxRPM,
		AverageRPM:            averageRPM,
		MaxConcurrency:        maxRiskConcurrency(events),
		DistinctIPs:           len(ipCounts),
		DistinctUAs:           len(uaCounts),
		DistinctModels:        len(modelSet),
		DistinctPaths:         len(pathSet),
		ActiveHours:           len(hourSet),
		DistinctSemantics:     len(semanticCounts),
		DominantSemanticRatio: dominantRatio,
		GatewayUAHits:         gatewayUAHits,
		ForbiddenClientUAHits: forbiddenClientUAHits,
		TopIP:                 pickRiskTopKey(ipCounts),
		TopUA:                 pickRiskTopKey(uaCounts),
		DetailRows:            len(details),
		DetailSampled:         len(details) < candidate.RequestCount,
		Samples:               selectDiverseRiskEvidenceSamples(sampleCandidates, cfg.MaxSamples),
	}
}

type riskEvidenceCandidate struct {
	sample      RiskEvidenceSample
	semanticKey string
	hourBucket  int64
}

// selectDiverseRiskEvidenceSamples greedily preserves different clients,
// models, paths, time buckets, and request shapes before filling remaining
// slots with the newest rows. Input order is already newest-first.
func selectDiverseRiskEvidenceSamples(candidates []riskEvidenceCandidate, limit int) []RiskEvidenceSample {
	if limit <= 0 || len(candidates) == 0 {
		return []RiskEvidenceSample{}
	}
	if limit > len(candidates) {
		limit = len(candidates)
	}
	selected := make([]bool, len(candidates))
	result := make([]RiskEvidenceSample, 0, limit)
	seenUA := map[string]struct{}{}
	seenModel := map[string]struct{}{}
	seenPath := map[string]struct{}{}
	seenHour := map[int64]struct{}{}
	seenSemantic := map[string]struct{}{}
	for index, candidate := range candidates {
		novel := false
		if candidate.sample.UserAgent != "" {
			_, known := seenUA[candidate.sample.UserAgent]
			novel = novel || !known
		}
		if candidate.sample.Model != "" {
			_, known := seenModel[candidate.sample.Model]
			novel = novel || !known
		}
		if candidate.sample.RequestPath != "" {
			_, known := seenPath[candidate.sample.RequestPath]
			novel = novel || !known
		}
		if _, known := seenHour[candidate.hourBucket]; !known {
			novel = true
		}
		if candidate.semanticKey != "" {
			_, known := seenSemantic[candidate.semanticKey]
			novel = novel || !known
		}
		if !novel {
			continue
		}
		selected[index] = true
		result = append(result, candidate.sample)
		seenUA[candidate.sample.UserAgent] = struct{}{}
		seenModel[candidate.sample.Model] = struct{}{}
		seenPath[candidate.sample.RequestPath] = struct{}{}
		seenHour[candidate.hourBucket] = struct{}{}
		if candidate.semanticKey != "" {
			seenSemantic[candidate.semanticKey] = struct{}{}
		}
		if len(result) == limit {
			return result
		}
	}
	for index, candidate := range candidates {
		if selected[index] {
			continue
		}
		result = append(result, candidate.sample)
		if len(result) == limit {
			break
		}
	}
	return result
}

type riskConcurrencyEvent struct {
	at    int64
	delta int
}

func maxRiskConcurrency(events []riskConcurrencyEvent) int {
	sort.Slice(events, func(i, j int) bool {
		if events[i].at == events[j].at {
			// Treat request intervals as [start, end): a request ending at the
			// same second another starts is not concurrent with it. This avoids
			// systematically inflating concurrency on second-resolution logs.
			return events[i].delta < events[j].delta
		}
		return events[i].at < events[j].at
	})
	current := 0
	maximum := 0
	for _, event := range events {
		current += event.delta
		if current > maximum {
			maximum = current
		}
	}
	return maximum
}

func extractRiskRequestParams(other string, redact bool) string {
	if strings.TrimSpace(other) == "" {
		return ""
	}
	var payload map[string]interface{}
	if err := common.UnmarshalJsonStr(other, &payload); err != nil {
		return ""
	}
	params, ok := payload[common.RequestParamsOtherKey]
	if !ok || params == nil {
		return ""
	}
	data, err := common.Marshal(params)
	if err != nil {
		return ""
	}
	text := string(data)
	if redact {
		text = common.MaskSensitiveInfo(text)
	}
	if len(text) > 4096 {
		text = text[:4096] + "..."
	}
	return text
}

func scoreRiskSignals(signals RiskSignalSummary, windowHours int, cfg system_setting.RiskControlSetting) (int, string, []string) {
	score := 0
	reasons := make([]string, 0, 8)
	if signals.MaxRPM >= cfg.CriticalRPM {
		score += 25
		reasons = append(reasons, fmt.Sprintf("峰值 RPM %d 达到严重阈值", signals.MaxRPM))
	} else if signals.MaxRPM >= cfg.HighRPM {
		score += 15
		reasons = append(reasons, fmt.Sprintf("峰值 RPM %d 达到高频阈值", signals.MaxRPM))
	} else if signals.MaxRPM >= maxRiskInt(2, cfg.HighRPM/2) {
		score += 7
	}
	if signals.MaxConcurrency >= cfg.ConcurrencyThreshold*2 {
		score += 20
		reasons = append(reasons, fmt.Sprintf("估算并发 %d 显著超过阈值", signals.MaxConcurrency))
	} else if signals.MaxConcurrency >= cfg.ConcurrencyThreshold {
		score += 12
		reasons = append(reasons, fmt.Sprintf("估算并发 %d 达到阈值", signals.MaxConcurrency))
	}
	if signals.DistinctIPs >= cfg.IPFanoutThreshold*2 {
		score += 18
		reasons = append(reasons, fmt.Sprintf("同一令牌覆盖 %d 个 IP", signals.DistinctIPs))
	} else if signals.DistinctIPs >= cfg.IPFanoutThreshold {
		score += 10
		reasons = append(reasons, fmt.Sprintf("同一令牌覆盖 %d 个 IP", signals.DistinctIPs))
	} else if signals.DistinctIPs >= 3 {
		score += 4
	}
	if signals.DistinctUAs >= cfg.UAFanoutThreshold*2 {
		score += 10
		reasons = append(reasons, fmt.Sprintf("出现 %d 种 User-Agent", signals.DistinctUAs))
	} else if signals.DistinctUAs >= cfg.UAFanoutThreshold {
		score += 6
	}
	if signals.DistinctTokens >= 5 {
		score += 12
		reasons = append(reasons, fmt.Sprintf("同一用户在窗口内同时使用 %d 个令牌", signals.DistinctTokens))
	} else if signals.DistinctTokens >= 2 {
		score += 5
		reasons = append(reasons, fmt.Sprintf("同一用户在窗口内使用 %d 个令牌", signals.DistinctTokens))
	}
	if windowHours >= 24 && signals.ActiveHours >= cfg.ActiveHoursThreshold {
		score += 10
		reasons = append(reasons, fmt.Sprintf("24 小时窗口内活跃跨越 %d 个小时", signals.ActiveHours))
	} else if windowHours >= 24 && signals.ActiveHours >= cfg.ActiveHoursThreshold/2 {
		score += 5
	}
	if signals.GatewayUAHits > 0 {
		score += 40
		reasons = append(reasons, fmt.Sprintf("发现 %d 条已知中转网关 UA 特征", signals.GatewayUAHits))
	}
	if signals.ForbiddenClientUAHits > 0 {
		score += 45
		reasons = append(reasons, fmt.Sprintf("发现 %d 条禁止客户端 UA 特征", signals.ForbiddenClientUAHits))
	}
	if signals.ErrorRate >= 0.6 && signals.ErrorCount >= 10 {
		score += 5
		reasons = append(reasons, fmt.Sprintf("错误率 %.0f%%", signals.ErrorRate*100))
	}
	if signals.RequestCount >= cfg.MinRequests*10 {
		score += 10
		reasons = append(reasons, fmt.Sprintf("窗口请求量 %d 显著偏高", signals.RequestCount))
	}
	if signals.DistinctIPs >= cfg.IPFanoutThreshold && signals.DominantSemanticRatio >= 0.5 {
		score += 8
		reasons = append(reasons, "多个 IP 重复使用高度相似的请求结构")
	}
	if signals.DistinctIPs >= cfg.IPFanoutThreshold && signals.MaxConcurrency >= cfg.ConcurrencyThreshold {
		score += 10
		reasons = append(reasons, "IP 扇出与高并发同时出现")
	}
	if score > 100 {
		score = 100
	}

	verdict := "uncertain"
	switch {
	case signals.GatewayUAHits > 0:
		verdict = "gateway_distribution"
	case signals.ForbiddenClientUAHits > 0:
		verdict = "forbidden_paid_client"
	case signals.DistinctIPs >= cfg.IPFanoutThreshold*2 && signals.DistinctUAs >= cfg.UAFanoutThreshold*2 && windowHours <= 1:
		verdict = "key_leak"
	case windowHours >= 24 && signals.DistinctIPs >= cfg.IPFanoutThreshold*2 && signals.ActiveHours >= cfg.ActiveHoursThreshold:
		verdict = "multi_node_gateway"
	case signals.DistinctTokens >= 3 && signals.DistinctIPs >= cfg.IPFanoutThreshold:
		verdict = "gateway_distribution"
	case signals.DistinctIPs >= cfg.IPFanoutThreshold && signals.MaxConcurrency >= cfg.ConcurrencyThreshold:
		verdict = "gateway_distribution"
	case signals.DistinctIPs <= 3 && signals.MaxConcurrency < cfg.ConcurrencyThreshold:
		verdict = "small_share"
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "多个弱信号组合达到案件阈值")
	}
	return score, verdict, reasons
}

func deterministicRiskRecommendation(verdict string, score, tokenId int, cfg system_setting.RiskControlSetting) (string, int) {
	switch {
	case verdict == "key_leak" && score >= 90 && tokenId > 0:
		return model.RiskActionFreezeToken, 0
	case (verdict == "gateway_distribution" || verdict == "multi_node_gateway" || verdict == "forbidden_paid_client") && score >= 85:
		return model.RiskActionTemporaryBlock, cfg.TemporaryBlockMinutes
	case score >= 70:
		return model.RiskActionRateLimit, 60
	default:
		return model.RiskActionManualReview, 0
	}
}

func riskLevelForScore(score int) string {
	switch {
	case score >= 90:
		return "critical"
	case score >= 75:
		return "high"
	case score >= 55:
		return "medium"
	default:
		return "low"
	}
}

func RiskLevelForScore(score int) string {
	return riskLevelForScore(score)
}

func buildRiskCaseFingerprint(userId, tokenId, windowHours int, verdict string, evidenceTimestamp int64, cooldownMinutes int) string {
	cooldownSeconds := int64(cooldownMinutes * 60)
	if cooldownSeconds <= 0 {
		cooldownSeconds = 3600
	}
	if evidenceTimestamp <= 0 {
		evidenceTimestamp = common.GetTimestamp()
	}
	// Bucket by the newest evidence, not by scanner wall-clock time. Re-running
	// the task over unchanged logs therefore updates the same case instead of
	// manufacturing a fresh incident at every cooldown boundary.
	bucket := evidenceTimestamp / cooldownSeconds
	sum := sha256.Sum256([]byte(fmt.Sprintf("v2|%d|%d|%d|%s|%d", userId, tokenId, windowHours, verdict, bucket)))
	return hex.EncodeToString(sum[:])
}

func classifyRiskUserAgent(userAgent string, cfg system_setting.RiskControlSetting) (bool, bool) {
	lower := strings.ToLower(userAgent)
	gateway := false
	for _, marker := range cfg.GatewayUAMarkers {
		if strings.Contains(lower, marker) {
			gateway = true
			break
		}
	}
	forbidden := false
	for _, marker := range cfg.ForbiddenClientMarkers {
		if strings.Contains(lower, marker) {
			forbidden = true
			break
		}
	}
	return gateway, forbidden
}

func pickRiskTopKey(counts map[string]int) string {
	best := ""
	bestCount := 0
	for key, count := range counts {
		if count > bestCount || (count == bestCount && (best == "" || key < best)) {
			best = key
			bestCount = count
		}
	}
	return best
}

func maxRiskInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
