package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// GetLotteryStatus GET /api/user/game/lottery
// 彩票默认关闭：disabled 时返回 success + enabled=false，前端按 data.enabled 做 gating。
func GetLotteryStatus(c *gin.Context) {
	id := c.GetInt("id")
	status, err := model.GetLotteryStatus(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, status)
}

// BuyLotteryTicket POST /api/user/game/lottery/buy
// disabled 时直接报错（不允许购买），保持与 controller 风格一致。
func BuyLotteryTicket(c *gin.Context) {
	id := c.GetInt("id")
	var req model.LotteryBuyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := model.BuyLotteryTicket(id, &req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}
