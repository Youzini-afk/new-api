package controller

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/external_app_setting"
	"github.com/gin-gonic/gin"
)

type externalTokenRequest struct {
	Code string `json:"code"`
}

type externalIdentity struct {
	UserId       int    `json:"user_id"`
	Username     string `json:"username"`
	DisplayName  string `json:"display_name"`
	AvatarUrl    string `json:"avatar_url"`
	Quota        int    `json:"quota"`
	QuotaPerUnit int    `json:"quota_per_unit"`
}

type externalQuotaRequest struct {
	OperationId string `json:"operation_id"`
	UserId      int    `json:"user_id"`
	Amount      int    `json:"amount"`
}

type externalQuotaStatusRequest struct {
	OperationId string `json:"operation_id"`
}

type externalQuotaResponse struct {
	OperationId string `json:"operation_id"`
	UserId      int    `json:"user_id"`
	Kind        string `json:"kind"`
	Amount      int    `json:"amount"`
	Status      string `json:"status"`
	ErrorCode   string `json:"error_code,omitempty"`
	QuotaAfter  int    `json:"quota_after"`
	Applied     bool   `json:"applied"`
}

// ExternalAppAuthorize is a top-level browser redirect protected by
// BrowserSessionAuth. The redirect URI is operator-configured rather than
// supplied by the request, preventing authorization-code exfiltration.
func ExternalAppAuthorize(c *gin.Context) {
	settings := external_app_setting.GetSettings()
	if err := settings.Validate(); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "external game integration is unavailable"})
		return
	}
	if strings.TrimSpace(c.Query("app_id")) != settings.AppId {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid external application"})
		return
	}
	state := strings.TrimSpace(c.Query("state"))
	if len(state) < 8 || len(state) > 256 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid authorization state"})
		return
	}
	code, _, err := model.CreateExternalAppAuthCode(settings.AppId, c.GetInt("id"), time.Duration(settings.CodeTTLSeconds)*time.Second)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to create authorization code"})
		return
	}
	redirect, _ := url.Parse(settings.RedirectUri)
	query := redirect.Query()
	query.Set("provider", "new-api")
	query.Set("code", code)
	query.Set("state", state)
	redirect.RawQuery = query.Encode()
	c.Redirect(http.StatusFound, redirect.String())
}

func ExternalAppExchangeToken(c *gin.Context) {
	var request externalTokenRequest
	if err := c.ShouldBindJSON(&request); err != nil || strings.TrimSpace(request.Code) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "authorization code is required"})
		return
	}
	user, err := model.ConsumeExternalAppAuthCode(c.GetString(middleware.ExternalAppContextKey), request.Code)
	if err != nil {
		status := http.StatusInternalServerError
		message := "failed to exchange authorization code"
		if errors.Is(err, model.ErrExternalAuthCodeInvalid) {
			status = http.StatusUnauthorized
			message = "authorization code is invalid, expired, or already used"
		}
		c.JSON(status, gin.H{"success": false, "message": message})
		return
	}
	displayName := strings.TrimSpace(user.DisplayName)
	if displayName == "" {
		displayName = user.Username
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": externalIdentity{
		UserId:       user.Id,
		Username:     user.Username,
		DisplayName:  displayName,
		AvatarUrl:    user.AvatarURL,
		Quota:        user.Quota,
		QuotaPerUnit: int(common.QuotaPerUnit),
	}})
}

func ExternalAppDebitQuota(c *gin.Context) {
	handleExternalQuotaMutation(c, model.ExternalQuotaKindDebit)
}

func ExternalAppCreditQuota(c *gin.Context) {
	handleExternalQuotaMutation(c, model.ExternalQuotaKindCredit)
}

func ExternalAppQuotaStatus(c *gin.Context) {
	var request externalQuotaStatusRequest
	if err := c.ShouldBindJSON(&request); err != nil || strings.TrimSpace(request.OperationId) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "operation_id is required"})
		return
	}
	operation, err := model.GetExternalQuotaOperation(c.GetString(middleware.ExternalAppContextKey), request.OperationId)
	if err != nil {
		if errors.Is(err, model.ErrExternalOperationNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "quota operation was not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to query quota operation"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": quotaOperationResponse(operation, false)})
}

func handleExternalQuotaMutation(c *gin.Context, kind string) {
	var request externalQuotaRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid quota operation request"})
		return
	}
	operation, applied, err := model.ApplyExternalQuotaOperation(
		c.GetString(middleware.ExternalAppContextKey), request.OperationId, request.UserId, kind, request.Amount,
	)
	if err != nil {
		status := http.StatusInternalServerError
		message := "failed to apply quota operation"
		switch {
		case errors.Is(err, model.ErrExternalOperationInvalid):
			status = http.StatusBadRequest
			message = "invalid quota operation"
		case errors.Is(err, model.ErrExternalOperationConflict):
			status = http.StatusConflict
			message = "operation_id was already used for a different request"
		}
		c.JSON(status, gin.H{"success": false, "message": message})
		return
	}
	response := quotaOperationResponse(operation, applied)
	if operation.Status == model.ExternalQuotaStatusFailed {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"message": externalQuotaFailureMessage(operation.ErrorCode),
			"data":    response,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": response})
}

func quotaOperationResponse(operation *model.ExternalQuotaOperation, applied bool) externalQuotaResponse {
	return externalQuotaResponse{
		OperationId: operation.OperationId,
		UserId:      operation.UserId,
		Kind:        operation.Kind,
		Amount:      operation.Amount,
		Status:      operation.Status,
		ErrorCode:   operation.ErrorCode,
		QuotaAfter:  operation.QuotaAfter,
		Applied:     applied,
	}
}

func externalQuotaFailureMessage(code string) string {
	switch code {
	case model.ExternalQuotaErrorInsufficient:
		return "insufficient New API quota"
	case model.ExternalQuotaErrorUserDisabled:
		return "the New API user is disabled"
	case model.ExternalQuotaErrorUserNotFound:
		return "the New API user does not exist"
	default:
		return "quota operation failed"
	}
}
