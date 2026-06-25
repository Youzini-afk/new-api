package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
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
