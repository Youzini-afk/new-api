package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// GetRouletteStatus GET /api/user/game/roulette
// roulette 默认关闭：disabled 时返回 success + enabled=false，前端按 data.enabled 做 gating。
func GetRouletteStatus(c *gin.Context) {
	id := c.GetInt("id")
	status, err := model.GetRouletteStatus(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, status)
}

// SpinRoulette POST /api/user/game/roulette/spin
// paid instant spin。Turnstile/CriticalRateLimit 在 router middleware 层。
// disabled 或配置无效时直接报错（fail closed），不允许 spin。
func SpinRoulette(c *gin.Context) {
	id := c.GetInt("id")
	var req model.RouletteSpinRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := model.SpinRoulette(id, &req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}
