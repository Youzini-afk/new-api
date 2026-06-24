package operation_setting

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

// GameSetting 游戏类功能（lottery / roulette 等）的全局开关与参数。
// Phase 7A 仅落地 lottery MVP，默认关闭；roulette 不迁移。
type GameSetting struct {
	// LotteryEnabled 是否启用每日彩票。默认关闭，运维显式开启后用户才可购买。
	LotteryEnabled bool `json:"lottery_enabled"`
	// LotteryDailyBuyLimit 每个用户每期最多购买的注数（含同号去重校验）。
	LotteryDailyBuyLimit int `json:"lottery_daily_buy_limit"`
	// LotteryMinStakeQuota 单注最小投入额度（quota 单位）。
	LotteryMinStakeQuota int `json:"lottery_min_stake_quota"`
	// LotteryMaxStakeQuota 单注最大投入额度（quota 单位）。
	LotteryMaxStakeQuota int `json:"lottery_max_stake_quota"`
	// LotterySystemInjectedQuota 每期开奖池由系统注入的额度（默认 0，区别于旧 gy 的 500 USD 默认）。
	LotterySystemInjectedQuota int `json:"lottery_system_injected_quota"`
	// LotteryMaxUserQuota 用户当前额度上限：>0 时，达到/超过该上限的用户不可购买（防止富账户刷奖池）；0 表示不限制。
	LotteryMaxUserQuota int `json:"lottery_max_user_quota"`
	// LotteryDrawHour 每日开奖小时（0-23，本地时区）。非法值在读取时 fallback 为 22。
	LotteryDrawHour int `json:"lottery_draw_hour"`
}

// 默认配置：彩票默认关闭；下注额度以 common.QuotaPerUnit 为单位换算（1 USD ~ QuotaPerUnit）。
// 注意 common.QuotaPerUnit 是 float64 运行期变量，因此默认值在 init() 中显式计算以保证编译期不依赖其具体取值。
var gameSetting = GameSetting{
	LotteryEnabled:             false,
	LotteryDailyBuyLimit:       3,
	LotteryMinStakeQuota:       int(common.QuotaPerUnit),
	LotteryMaxStakeQuota:       100 * int(common.QuotaPerUnit),
	LotterySystemInjectedQuota: 0,
	LotteryMaxUserQuota:        0,
	LotteryDrawHour:            22,
}

func init() {
	config.GlobalConfig.Register("game_setting", &gameSetting)
}

// GetGameSetting 获取游戏配置（返回指针便于上层读取最新字段）。
func GetGameSetting() *GameSetting {
	return &gameSetting
}

// IsLotteryEnabled 是否启用彩票。
func IsLotteryEnabled() bool {
	return gameSetting.LotteryEnabled
}

// GetLotteryDrawHour 返回合法的开奖小时（0-23），非法时 fallback 为 22。
func GetLotteryDrawHour() int {
	h := gameSetting.LotteryDrawHour
	if h < 0 || h > 23 {
		return 22
	}
	return h
}

// GetLotteryStakeQuotaRange 返回 (min, max) 投注额度，并保证 min <= max；min 至少为 1。
// 同时保证 min/max 都不会低于 1，避免 0 投注导致免费参与。
func GetLotteryStakeQuotaRange() (int, int) {
	min := gameSetting.LotteryMinStakeQuota
	max := gameSetting.LotteryMaxStakeQuota
	if min < 1 {
		min = 1
	}
	if max < min {
		max = min
	}
	return min, max
}

// GetLotterySystemInjectedQuota 返回每期系统注入额度，clamp 到 >= 0。
// 负配置会导致奖池为负、round 长期无法结算，此处统一兜底为 0。
func GetLotterySystemInjectedQuota() int {
	if gameSetting.LotterySystemInjectedQuota < 0 {
		return 0
	}
	return gameSetting.LotterySystemInjectedQuota
}

// DebugGameSettingSummary 返回配置摘要（仅供日志/调试使用）。
func DebugGameSettingSummary() string {
	return fmt.Sprintf("lottery_enabled=%v daily_buy_limit=%d stake_quota_range=[%d,%d] system_injected=%d max_user_quota=%d draw_hour=%d",
		gameSetting.LotteryEnabled,
		gameSetting.LotteryDailyBuyLimit,
		gameSetting.LotteryMinStakeQuota,
		gameSetting.LotteryMaxStakeQuota,
		gameSetting.LotterySystemInjectedQuota,
		gameSetting.LotteryMaxUserQuota,
		GetLotteryDrawHour(),
	)
}
