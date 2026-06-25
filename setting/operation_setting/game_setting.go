package operation_setting

import (
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

// rouletteMaxRTPBps 是 RTP（return-to-player）硬上限：配置值与 wheel 实算 RTP 都不得超过该值，
// 避免运维误配成正期望水龙头。9500 bps = 95%。
const rouletteMaxRTPBps = 9500

// defaultRouletteWheelJSON 是默认 wheel：加权 outcomes，实算 RTP = 9000 bps（90%）。
// 旧 gy roulette 是免费 6 人满员正期望开奖（quota 水龙头），禁止原样迁移；本 wheel 为付费即时 spin 的安全默认。
var defaultRouletteWheelJSON = json.RawMessage(`[` +
	`{"key":"lose","multiplier_bps":0,"weight":50},` +
	`{"key":"x1","multiplier_bps":10000,"weight":20},` +
	`{"key":"x2","multiplier_bps":20000,"weight":20},` +
	`{"key":"x3","multiplier_bps":30000,"weight":10}` +
	`]`)

// GameSetting 游戏类功能（lottery / roulette 等）的全局开关与参数。
// Phase 7A 落地 lottery MVP；Phase 7B 落地付费即时 spin roulette。默认均关闭。
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

	// --- Phase 7B: 付费即时 spin roulette（默认全安全关闭） ---

	// RouletteEnabled 是否启用付费即时 spin roulette。默认关闭。
	RouletteEnabled bool `json:"roulette_enabled"`
	// RouletteDailySpinLimit 每用户每日最多 spin 次数。
	RouletteDailySpinLimit int `json:"roulette_daily_spin_limit"`
	// RouletteMinStakeQuota 单次 spin 最小投入额度（quota 单位），读取时 clamp >= 1。
	RouletteMinStakeQuota int `json:"roulette_min_stake_quota"`
	// RouletteMaxStakeQuota 单次 spin 最大投入额度（quota 单位），读取时 clamp >= min。
	RouletteMaxStakeQuota int `json:"roulette_max_stake_quota"`
	// RouletteMaxDailyStakeQuota 每用户每日累计 stake 上限；0 表示无额外每日 stake cap（仍受 daily spin limit 约束）。
	RouletteMaxDailyStakeQuota int `json:"roulette_max_daily_stake_quota"`
	// RouletteMaxUserQuota 用户当前额度上限：>0 时，quota>=max 的用户不可 spin（阻止富用户刷），且中奖后 payout capped 不超过 max；0 表示不限制。
	RouletteMaxUserQuota int `json:"roulette_max_user_quota"`
	// RouletteRTPBps 配置的 RTP 上限（basis points，9500 = 95%）。读取时 clamp <= rouletteMaxRTPBps。
	// wheel 实算 RTP 不得超过该值且不得超过 rouletteMaxRTPBps，否则 spin fail closed。
	RouletteRTPBps int `json:"roulette_rtp_bps"`
	// RouletteWheel 加权 outcomes JSON（json.RawMessage 原样持久化）。model 层解析校验，失败 fail closed。
	RouletteWheel json.RawMessage `json:"roulette_wheel"`
}

// 默认配置：彩票默认关闭；下注额度以 common.QuotaPerUnit 为单位换算（1 USD ~ QuotaPerUnit）。
// 注意 common.QuotaPerUnit 是 float64 运行期变量，因此默认值在 init() 中显式计算以保证编译期不依赖其具体取值。
// roulette 默认安全关闭；RTP 9000 bps，wheel 默认 RTP 9000 bps（<= 配置且 <= 9500）。
var gameSetting = GameSetting{
	LotteryEnabled:             false,
	LotteryDailyBuyLimit:       3,
	LotteryMinStakeQuota:       int(common.QuotaPerUnit),
	LotteryMaxStakeQuota:       100 * int(common.QuotaPerUnit),
	LotterySystemInjectedQuota: 0,
	LotteryMaxUserQuota:        0,
	LotteryDrawHour:            22,

	RouletteEnabled:            false,
	RouletteDailySpinLimit:     3,
	RouletteMinStakeQuota:      500000,
	RouletteMaxStakeQuota:      5000000,
	RouletteMaxDailyStakeQuota: 0,
	RouletteMaxUserQuota:       0,
	RouletteRTPBps:             9000,
	RouletteWheel:              defaultRouletteWheelJSON,
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

// IsRouletteEnabled 是否启用 roulette。
func IsRouletteEnabled() bool {
	return gameSetting.RouletteEnabled
}

// GetRouletteDailySpinLimit 返回每日 spin 上限，clamp >= 0。
// 语义：返回值 <= 0（含负配置 clamp 后）视为 fail-closed —— 禁止任何 spin，不被当作 unlimited。
// model 层 SpinRoulette 与 GetRouletteStatus 均按此语义处理，避免误配 0 放开无限刷量。
func GetRouletteDailySpinLimit() int {
	if gameSetting.RouletteDailySpinLimit < 0 {
		return 0
	}
	return gameSetting.RouletteDailySpinLimit
}

// GetRouletteStakeQuotaRange 返回 (min, max) spin 额度，保证 min >= 1 且 max >= min。
func GetRouletteStakeQuotaRange() (int, int) {
	min := gameSetting.RouletteMinStakeQuota
	max := gameSetting.RouletteMaxStakeQuota
	if min < 1 {
		min = 1
	}
	if max < min {
		max = min
	}
	return min, max
}

// GetRouletteMaxDailyStakeQuota 返回每日累计 stake 上限，clamp >= 0（0 表示无额外 cap）。
func GetRouletteMaxDailyStakeQuota() int {
	if gameSetting.RouletteMaxDailyStakeQuota < 0 {
		return 0
	}
	return gameSetting.RouletteMaxDailyStakeQuota
}

// GetRouletteMaxUserQuota 返回用户额度风控上限，clamp >= 0（0 表示不限制）。
func GetRouletteMaxUserQuota() int {
	if gameSetting.RouletteMaxUserQuota < 0 {
		return 0
	}
	return gameSetting.RouletteMaxUserQuota
}

// GetRouletteRTPBps 返回配置的 RTP 上限，clamp 到 [0, rouletteMaxRTPBps]。
// wheel 实算 RTP 不得超过该值且不得超过 rouletteMaxRTPBps。
func GetRouletteRTPBps() int {
	rtp := gameSetting.RouletteRTPBps
	if rtp < 0 {
		return 0
	}
	if rtp > rouletteMaxRTPBps {
		return rouletteMaxRTPBps
	}
	return rtp
}

// GetRouletteWheelJSON 返回 wheel 原始 JSON。若为空则回落到安全默认 wheel。
func GetRouletteWheelJSON() json.RawMessage {
	if len(gameSetting.RouletteWheel) == 0 {
		return defaultRouletteWheelJSON
	}
	return gameSetting.RouletteWheel
}

// DebugGameSettingSummary 返回配置摘要（仅供日志/调试使用）。
func DebugGameSettingSummary() string {
	rlMin, rlMax := GetRouletteStakeQuotaRange()
	return fmt.Sprintf("lottery_enabled=%v daily_buy_limit=%d stake_quota_range=[%d,%d] system_injected=%d max_user_quota=%d draw_hour=%d | roulette_enabled=%v daily_spin_limit=%d stake_quota_range=[%d,%d] max_daily_stake=%d max_user_quota=%d rtp_bps=%d",
		gameSetting.LotteryEnabled,
		gameSetting.LotteryDailyBuyLimit,
		gameSetting.LotteryMinStakeQuota,
		gameSetting.LotteryMaxStakeQuota,
		gameSetting.LotterySystemInjectedQuota,
		gameSetting.LotteryMaxUserQuota,
		GetLotteryDrawHour(),
		gameSetting.RouletteEnabled,
		GetRouletteDailySpinLimit(),
		rlMin, rlMax,
		gameSetting.RouletteMaxDailyStakeQuota,
		gameSetting.RouletteMaxUserQuota,
		GetRouletteRTPBps(),
	)
}
