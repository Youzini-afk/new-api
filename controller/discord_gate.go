package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const discordGateBatchLimit = 100

type discordGateBatchRequest struct {
	IDs []int `json:"ids"`
}

type discordGateExemptRequest struct {
	Exempt bool `json:"exempt"`
}

func RecheckSelfDiscordGate(c *gin.Context) {
	userID := c.GetInt("id")
	if userID == 0 {
		common.ApiErrorMsg(c, "未登录")
		return
	}
	user, err := model.GetUserById(userID, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	outcome, err := oauth.RecheckDiscordGate(c.Request.Context(), user)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, outcome)
}

func AdminRecheckDiscordGate(c *gin.Context) {
	user, ok := getManageableDiscordGateUser(c)
	if !ok {
		return
	}
	outcome, err := oauth.RecheckDiscordGate(c.Request.Context(), user)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, outcome)
}

func AdminRecheckDiscordGateBatch(c *gin.Context) {
	req := discordGateBatchRequest{}
	if err := c.ShouldBindJSON(&req); err != nil {
		discordGateBadRequest(c, "invalid request body")
		return
	}
	if len(req.IDs) == 0 {
		discordGateBadRequest(c, "ids must not be empty")
		return
	}
	if len(req.IDs) > discordGateBatchLimit {
		discordGateBadRequest(c, "ids exceeds limit")
		return
	}

	results := make([]gin.H, 0, len(req.IDs))
	successCount := 0
	errorCount := 0
	myRole := c.GetInt("role")
	for _, id := range req.IDs {
		entry := gin.H{"user_id": id}
		user, err := model.GetUserById(id, true)
		if err != nil {
			entry["success"] = false
			entry["message"] = err.Error()
			errorCount++
			results = append(results, entry)
			continue
		}
		if !canManageTargetRole(myRole, user.Role) {
			entry["success"] = false
			entry["message"] = "no permission"
			errorCount++
			results = append(results, entry)
			continue
		}
		outcome, err := oauth.RecheckDiscordGate(c.Request.Context(), user)
		if err != nil {
			entry["success"] = false
			entry["message"] = err.Error()
			errorCount++
			results = append(results, entry)
			continue
		}
		entry["success"] = true
		entry["data"] = outcome
		successCount++
		results = append(results, entry)
	}

	common.ApiSuccess(c, gin.H{
		"total":   len(req.IDs),
		"success": successCount,
		"failed":  errorCount,
		"results": results,
	})
}

func AdminForceDiscordGateReauth(c *gin.Context) {
	user, ok := getManageableDiscordGateUser(c)
	if !ok {
		return
	}
	outcome, err := oauth.ForceDiscordGateReauth(user)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, outcome)
}

func AdminSetDiscordGateExempt(c *gin.Context) {
	user, ok := getManageableDiscordGateUser(c)
	if !ok {
		return
	}
	req := discordGateExemptRequest{}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	outcome, err := oauth.SetDiscordGateExempt(user, req.Exempt)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, outcome)
}

func discordGateBadRequest(c *gin.Context, message string) {
	c.JSON(http.StatusBadRequest, gin.H{
		"success": false,
		"message": message,
	})
}

func getManageableDiscordGateUser(c *gin.Context) (*model.User, bool) {
	userID, err := strconv.Atoi(c.Param("id"))
	if err != nil || userID == 0 {
		common.ApiErrorMsg(c, "invalid user id")
		return nil, false
	}
	user, err := model.GetUserById(userID, true)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiErrorMsg(c, "user not found")
			return nil, false
		}
		common.ApiError(c, err)
		return nil, false
	}
	if !canManageTargetRole(c.GetInt("role"), user.Role) {
		common.ApiErrorMsg(c, "no permission")
		return nil, false
	}
	return user, true
}
