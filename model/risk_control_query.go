package model

import (
	"context"
	"sort"
	"strconv"
)

type RiskCandidateAgg struct {
	UserId         int   `gorm:"column:user_id"`
	TokenId        int   `gorm:"column:token_id"`
	RequestCount   int   `gorm:"column:request_count"`
	TotalTokens    int64 `gorm:"column:total_tokens"`
	TotalQuota     int64 `gorm:"column:total_quota"`
	DistinctTokens int   `gorm:"column:distinct_tokens"`
	LastSeen       int64 `gorm:"column:last_seen"`
}

type RiskLogDetail struct {
	UserId           int    `gorm:"column:user_id"`
	TokenId          int    `gorm:"column:token_id"`
	Type             int    `gorm:"column:type"`
	PromptTokens     int    `gorm:"column:prompt_tokens"`
	CompletionTokens int    `gorm:"column:completion_tokens"`
	Quota            int    `gorm:"column:quota"`
	UseTime          int    `gorm:"column:use_time"`
	Other            string `gorm:"column:other"`
	UserAgent        string `gorm:"column:user_agent"`
	Ip               string `gorm:"column:ip"`
	ModelName        string `gorm:"column:model_name"`
	RequestPath      string `gorm:"column:request_path"`
	RequestId        string `gorm:"column:request_id"`
	CreatedAt        int64  `gorm:"column:created_at"`
}

type RiskSubjectMeta struct {
	UserId      int
	Username    string
	DisplayName string
	Role        int
	Status      int
	TokenId     int
	TokenName   string
	TokenStatus int
}

func RiskListCandidates(ctx context.Context, startTimestamp, endTimestamp int64, minRequests, limit int) ([]RiskCandidateAgg, error) {
	var tokenRows []RiskCandidateAgg
	tokenQuery := LOG_DB.WithContext(ctx).
		Table("logs").
		Where("type IN ?", []int{LogTypeConsume, LogTypeError}).
		Where("user_id > 0 AND token_id > 0").
		Where("created_at >= ? AND created_at < ?", startTimestamp, endTimestamp).
		Select("user_id, token_id, count(*) as request_count, sum(prompt_tokens) + sum(completion_tokens) as total_tokens, sum(quota) as total_quota, 1 as distinct_tokens, max(created_at) as last_seen").
		Group("user_id, token_id")
	if minRequests > 0 {
		tokenQuery = tokenQuery.Having("count(*) >= ?", minRequests)
	}
	if limit > 0 {
		tokenQuery = tokenQuery.Limit(limit)
	}
	if err := tokenQuery.Order("request_count desc").Scan(&tokenRows).Error; err != nil {
		return nil, err
	}

	// A reseller can spread traffic over several keys owned by the same user.
	// Add one user-level candidate only when at least two distinct tokens are
	// active, keeping ordinary single-token users represented exactly once.
	var userRows []RiskCandidateAgg
	userQuery := LOG_DB.WithContext(ctx).
		Table("logs").
		Where("type IN ?", []int{LogTypeConsume, LogTypeError}).
		Where("user_id > 0 AND token_id > 0").
		Where("created_at >= ? AND created_at < ?", startTimestamp, endTimestamp).
		Select("user_id, 0 as token_id, count(*) as request_count, sum(prompt_tokens) + sum(completion_tokens) as total_tokens, sum(quota) as total_quota, count(DISTINCT token_id) as distinct_tokens, max(created_at) as last_seen").
		Group("user_id").
		Having("count(DISTINCT token_id) >= ?", 2)
	if minRequests > 0 {
		userQuery = userQuery.Having("count(*) >= ?", minRequests)
	}
	if limit > 0 {
		userQuery = userQuery.Limit(limit)
	}
	if err := userQuery.Order("request_count desc").Scan(&userRows).Error; err != nil {
		return nil, err
	}

	rows := append(tokenRows, userRows...)
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].RequestCount != rows[j].RequestCount {
			return rows[i].RequestCount > rows[j].RequestCount
		}
		if rows[i].TotalQuota != rows[j].TotalQuota {
			return rows[i].TotalQuota > rows[j].TotalQuota
		}
		if rows[i].UserId != rows[j].UserId {
			return rows[i].UserId < rows[j].UserId
		}
		return rows[i].TokenId < rows[j].TokenId
	})
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

func RiskListLogDetails(ctx context.Context, startTimestamp, endTimestamp int64, userIds []int, limit int) ([]RiskLogDetail, error) {
	if len(userIds) == 0 {
		return []RiskLogDetail{}, nil
	}
	var rows []RiskLogDetail
	query := LOG_DB.WithContext(ctx).
		Table("logs").
		Select("user_id, token_id, type, prompt_tokens, completion_tokens, quota, use_time, other, user_agent, ip, model_name, request_path, request_id, created_at").
		Where("type IN ?", []int{LogTypeConsume, LogTypeError}).
		Where("user_id IN ?", userIds).
		Where("created_at >= ? AND created_at < ?", startTimestamp, endTimestamp).
		Order("created_at desc, user_id asc")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func RiskFillSubjectMeta(ctx context.Context, candidates []RiskCandidateAgg) (map[string]RiskSubjectMeta, error) {
	if len(candidates) == 0 {
		return map[string]RiskSubjectMeta{}, nil
	}
	userIds := make([]int, 0, len(candidates))
	tokenIds := make([]int, 0, len(candidates))
	userSeen := map[int]struct{}{}
	tokenSeen := map[int]struct{}{}
	for _, candidate := range candidates {
		if _, ok := userSeen[candidate.UserId]; !ok {
			userSeen[candidate.UserId] = struct{}{}
			userIds = append(userIds, candidate.UserId)
		}
		if candidate.TokenId > 0 {
			if _, ok := tokenSeen[candidate.TokenId]; ok {
				continue
			}
			tokenSeen[candidate.TokenId] = struct{}{}
			tokenIds = append(tokenIds, candidate.TokenId)
		}
	}
	type userRow struct {
		Id          int
		Username    string
		DisplayName string
		Role        int
		Status      int
	}
	type tokenRow struct {
		Id     int
		UserId int
		Name   string
		Status int
	}
	var users []userRow
	if err := DB.WithContext(ctx).Model(&User{}).
		Select("id, username, display_name, role, status").
		Where("id IN ?", userIds).Find(&users).Error; err != nil {
		return nil, err
	}
	var tokens []tokenRow
	if len(tokenIds) > 0 {
		if err := DB.WithContext(ctx).Model(&Token{}).
			Select("id, user_id, name, status").
			Where("id IN ?", tokenIds).Find(&tokens).Error; err != nil {
			return nil, err
		}
	}
	userMap := make(map[int]userRow, len(users))
	for _, user := range users {
		userMap[user.Id] = user
	}
	tokenMap := make(map[int]tokenRow, len(tokens))
	for _, token := range tokens {
		tokenMap[token.Id] = token
	}
	result := make(map[string]RiskSubjectMeta, len(candidates))
	for _, candidate := range candidates {
		user, userOK := userMap[candidate.UserId]
		if candidate.TokenId == 0 {
			if !userOK {
				continue
			}
			result[riskSubjectKey(candidate.UserId, 0)] = RiskSubjectMeta{
				UserId:      user.Id,
				Username:    user.Username,
				DisplayName: user.DisplayName,
				Role:        user.Role,
				Status:      user.Status,
			}
			continue
		}
		token, tokenOK := tokenMap[candidate.TokenId]
		if !userOK || !tokenOK || token.UserId != candidate.UserId {
			continue
		}
		result[riskSubjectKey(candidate.UserId, candidate.TokenId)] = RiskSubjectMeta{
			UserId:      user.Id,
			Username:    user.Username,
			DisplayName: user.DisplayName,
			Role:        user.Role,
			Status:      user.Status,
			TokenId:     token.Id,
			TokenName:   token.Name,
			TokenStatus: token.Status,
		}
	}
	return result, nil
}

func riskSubjectKey(userId, tokenId int) string {
	return strconv.Itoa(userId) + ":" + strconv.Itoa(tokenId)
}

func RiskSubjectKey(userId, tokenId int) string {
	return riskSubjectKey(userId, tokenId)
}
