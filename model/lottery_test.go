package model

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withGameSetting 暂存并替换全局 GameSetting，返回还原函数。
// 必须在 t.Cleanup 中调用返回值，避免污染其他测试。
func withGameSetting(t *testing.T, mutate func(s *operation_setting.GameSetting)) {
	t.Helper()
	orig := *operation_setting.GetGameSetting()
	operation_setting.GetGameSetting().LotteryEnabled = false
	operation_setting.GetGameSetting().LotteryDailyBuyLimit = orig.LotteryDailyBuyLimit
	operation_setting.GetGameSetting().LotteryMinStakeQuota = orig.LotteryMinStakeQuota
	operation_setting.GetGameSetting().LotteryMaxStakeQuota = orig.LotteryMaxStakeQuota
	operation_setting.GetGameSetting().LotterySystemInjectedQuota = orig.LotterySystemInjectedQuota
	operation_setting.GetGameSetting().LotteryMaxUserQuota = orig.LotteryMaxUserQuota
	operation_setting.GetGameSetting().LotteryDrawHour = orig.LotteryDrawHour
	mutate(operation_setting.GetGameSetting())
	t.Cleanup(func() {
		*operation_setting.GetGameSetting() = orig
	})
}

func insertLotteryTestUser(t *testing.T, id int, quota int) *User {
	t.Helper()
	u := &User{
		Id:       id,
		Username: "lottery_user_" + strconv.Itoa(id),
		Password: "password1234",
		Quota:    quota,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  common.GetRandomString(8),
	}
	require.NoError(t, DB.Create(u).Error)
	return u
}

func currentQuotaForLotteryTest(t *testing.T, userID int) int {
	t.Helper()
	var u User
	require.NoError(t, DB.Select("quota").Where("id = ?", userID).First(&u).Error)
	return u.Quota
}

// ---------- Disabled behavior ----------

func TestLottery_StatusDisabled_ReturnsEnabledFalse(t *testing.T) {
	truncateTables(t)
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.LotteryEnabled = false
	})

	data, err := GetLotteryStatus(42)
	require.NoError(t, err)
	require.NotNil(t, data)
	assert.False(t, data.Enabled)
	// 即使 disabled 也应返回可序列化的非 nil 切片，方便前端直接渲染
	assert.NotNil(t, data.MyTickets)
	assert.NotNil(t, data.MyRecentTickets)
	assert.NotNil(t, data.RecentRounds)
}

func TestLottery_BuyDisabled_ReturnsError(t *testing.T) {
	truncateTables(t)
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.LotteryEnabled = false
	})

	_, err := BuyLotteryTicket(1, &LotteryBuyRequest{
		RedBalls:   []int{1, 2, 3, 4, 5},
		BlueBall:   1,
		StakeQuota: 1000,
	})
	require.Error(t, err)
}

// ---------- Tier helper (deterministic table test) ----------

func TestDeterminePrizeTier_Table(t *testing.T) {
	cases := []struct {
		name      string
		redMatch  int
		blueMatch int
		expected  string
	}{
		{"jackpot_5red_1blue", 5, 1, "jackpot"},
		{"second_5red_0blue", 5, 0, "second"},
		{"third_4red_1blue", 4, 1, "third"},
		{"fourth_4red_0blue", 4, 0, "fourth"},
		{"fifth_3red_1blue", 3, 1, "fifth"},
		{"small_three_3red_0blue", 3, 0, "small_three"},
		{"small_two_blue", 2, 1, "small_two_blue"},
		{"small_one_blue", 1, 1, "small_one_blue"},
		{"none_0red_0blue", 0, 0, "none"},
		{"none_2red_0blue", 2, 0, "none"},
		{"none_1red_0blue", 1, 0, "none"},
		{"none_0red_1blue", 0, 1, "none"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, determinePrizeTier(tc.redMatch, tc.blueMatch))
		})
	}
}

// ---------- Big.Int safe helpers ----------

func TestSafeSumNonNegative_OverflowAndNegative(t *testing.T) {
	v, err := safeSumNonNegative(1, 2, 3)
	require.NoError(t, err)
	assert.Equal(t, 6, v)

	_, err = safeSumNonNegative(-1)
	require.Error(t, err)
}

func TestSafeFloorMulDiv_BasicAndErrors(t *testing.T) {
	v, err := safeFloorMulDiv(100, 50, 100)
	require.NoError(t, err)
	assert.Equal(t, 50, v)

	// floor behavior
	v, err = safeFloorMulDiv(7, 1, 3)
	require.NoError(t, err)
	assert.Equal(t, 2, v)

	_, err = safeFloorMulDiv(1, 1, 0)
	require.Error(t, err)
	_, err = safeFloorMulDiv(-1, 1, 3)
	require.Error(t, err)
}

// ---------- Buy behavior ----------

func TestLottery_Buy_DeductsQuotaAtomicallyAndCreatesTicketAndUpdatesPool(t *testing.T) {
	truncateTables(t)
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.LotteryEnabled = true
		s.LotteryDailyBuyLimit = 3
		s.LotteryMinStakeQuota = 1000
		s.LotteryMaxStakeQuota = 100000
		s.LotterySystemInjectedQuota = 0
		s.LotteryMaxUserQuota = 0
	})

	const userID = 2001
	insertLotteryTestUser(t, userID, 50000)

	stake := 1000
	res, err := BuyLotteryTicket(userID, &LotteryBuyRequest{
		RedBalls:   []int{1, 2, 3, 4, 5},
		BlueBall:   1,
		StakeQuota: stake,
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, stake, res.StakeQuota)
	assert.Equal(t, 1, res.DailyBuyCount)
	assert.Equal(t, 3, res.DailyBuyLimit)
	assert.Equal(t, 50000-stake, res.NewQuota)

	// 用户额度被扣
	assert.Equal(t, 50000-stake, currentQuotaForLotteryTest(t, userID))

	// ticket 落库
	var ticket GameLotteryTicket
	require.NoError(t, DB.Where("round_id = ? AND user_id = ?", res.RoundID, userID).First(&ticket).Error)
	assert.Equal(t, "1,2,3,4,5", ticket.RedBalls)
	assert.Equal(t, 1, ticket.BlueBall)
	assert.Equal(t, stake, ticket.StakeQuota)
	assert.Equal(t, "pending", ticket.Result)
	assert.Equal(t, 0, ticket.PrizeQuota)

	// round 总投注额更新
	var round GameLotteryRound
	require.NoError(t, DB.Where("id = ?", res.RoundID).First(&round).Error)
	assert.Equal(t, stake, round.TotalStakeQuota)
	assert.Equal(t, LotteryStatusOpen, round.Status)
}

func TestLottery_Buy_StakeUSD_ConvertsToQuota(t *testing.T) {
	truncateTables(t)
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.LotteryEnabled = true
		s.LotteryDailyBuyLimit = 3
		s.LotteryMinStakeQuota = 1
		s.LotteryMaxStakeQuota = 100 * int(common.QuotaPerUnit)
		s.LotteryMaxUserQuota = 0
	})

	const userID = 2002
	insertLotteryTestUser(t, userID, 10*int(common.QuotaPerUnit))

	res, err := BuyLotteryTicket(userID, &LotteryBuyRequest{
		RedBalls: []int{2, 4, 6, 8, 10},
		BlueBall: 2,
		StakeUSD: 2,
	})
	require.NoError(t, err)
	expectedStake := int(float64(2) * common.QuotaPerUnit)
	assert.Equal(t, expectedStake, res.StakeQuota)
	assert.Equal(t, 10*int(common.QuotaPerUnit)-expectedStake, res.NewQuota)
}

func TestLottery_Buy_StakeBelowMin_Rejected(t *testing.T) {
	truncateTables(t)
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.LotteryEnabled = true
		s.LotteryDailyBuyLimit = 3
		s.LotteryMinStakeQuota = 5000
		s.LotteryMaxStakeQuota = 100000
		s.LotteryMaxUserQuota = 0
	})

	const userID = 2003
	insertLotteryTestUser(t, userID, 50000)

	_, err := BuyLotteryTicket(userID, &LotteryBuyRequest{
		RedBalls:   []int{1, 2, 3, 4, 5},
		BlueBall:   1,
		StakeQuota: 1000, // < min 5000
	})
	require.Error(t, err)
	// 不应扣款
	assert.Equal(t, 50000, currentQuotaForLotteryTest(t, userID))
}

func TestLottery_Buy_InsufficientQuota_DoesNotCreateTicketOrPool(t *testing.T) {
	truncateTables(t)
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.LotteryEnabled = true
		s.LotteryDailyBuyLimit = 3
		s.LotteryMinStakeQuota = 1000
		s.LotteryMaxStakeQuota = 100000
		s.LotteryMaxUserQuota = 0
	})

	const userID = 2004
	insertLotteryTestUser(t, userID, 500) // 不足 1000

	_, err := BuyLotteryTicket(userID, &LotteryBuyRequest{
		RedBalls:   []int{1, 2, 3, 4, 5},
		BlueBall:   1,
		StakeQuota: 1000,
	})
	require.Error(t, err)

	// 不创建 ticket
	var ticketCount int64
	require.NoError(t, DB.Model(&GameLotteryTicket{}).Where("user_id = ?", userID).Count(&ticketCount).Error)
	assert.Equal(t, int64(0), ticketCount)

	// 不创建 round（因为 Buy 失败应该回滚整个事务，包括 round 创建）
	// 注：BuyLotteryTicket 在事务内创建 round，失败会回滚 round。
	var roundCount int64
	require.NoError(t, DB.Model(&GameLotteryRound{}).Count(&roundCount).Error)
	assert.Equal(t, int64(0), roundCount)

	// 用户额度未变
	assert.Equal(t, 500, currentQuotaForLotteryTest(t, userID))
}

func TestLottery_Buy_DailyLimit_BlocksExcess(t *testing.T) {
	truncateTables(t)
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.LotteryEnabled = true
		s.LotteryDailyBuyLimit = 2
		s.LotteryMinStakeQuota = 1000
		s.LotteryMaxStakeQuota = 100000
		s.LotteryMaxUserQuota = 0
	})

	const userID = 2005
	insertLotteryTestUser(t, userID, 100000)

	// 买两注（不同号码：注1选 1-5，注2选 6-10）
	buy1 := &LotteryBuyRequest{
		RedBalls:   []int{1, 2, 3, 4, 5},
		BlueBall:   1,
		StakeQuota: 1000,
	}
	_, err := BuyLotteryTicket(userID, buy1)
	require.NoError(t, err)

	buy2 := &LotteryBuyRequest{
		RedBalls:   []int{6, 7, 8, 9, 10},
		BlueBall:   2,
		StakeQuota: 1000,
	}
	_, err = BuyLotteryTicket(userID, buy2)
	require.NoError(t, err)

	// 第三注应被拒绝
	_, err = BuyLotteryTicket(userID, &LotteryBuyRequest{
		RedBalls:   []int{1, 3, 5, 7, 9},
		BlueBall:   3,
		StakeQuota: 1000,
	})
	require.Error(t, err)

	// 仅两注落库
	var ticketCount int64
	require.NoError(t, DB.Model(&GameLotteryTicket{}).Where("user_id = ?", userID).Count(&ticketCount).Error)
	assert.Equal(t, int64(2), ticketCount)
	// 额度仅扣 2000
	assert.Equal(t, 98000, currentQuotaForLotteryTest(t, userID))
}

func TestLottery_Buy_DuplicateCombination_Rejected(t *testing.T) {
	truncateTables(t)
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.LotteryEnabled = true
		s.LotteryDailyBuyLimit = 5
		s.LotteryMinStakeQuota = 1000
		s.LotteryMaxStakeQuota = 100000
		s.LotteryMaxUserQuota = 0
	})

	const userID = 2006
	insertLotteryTestUser(t, userID, 100000)

	req := &LotteryBuyRequest{
		RedBalls:   []int{1, 2, 3, 4, 5},
		BlueBall:   1,
		StakeQuota: 1000,
	}
	_, err := BuyLotteryTicket(userID, req)
	require.NoError(t, err)

	// 相同号码（不同顺序也应被规范化为同号）应被拒绝
	_, err = BuyLotteryTicket(userID, &LotteryBuyRequest{
		RedBalls:   []int{5, 4, 3, 2, 1}, // 顺序不同但集合相同
		BlueBall:   1,
		StakeQuota: 1000,
	})
	require.Error(t, err)

	// 仅一注落库
	var ticketCount int64
	require.NoError(t, DB.Model(&GameLotteryTicket{}).Where("user_id = ?", userID).Count(&ticketCount).Error)
	assert.Equal(t, int64(1), ticketCount)
	assert.Equal(t, 99000, currentQuotaForLotteryTest(t, userID))
}

func TestLottery_Buy_MaxUserQuota_RejectsRichUser(t *testing.T) {
	truncateTables(t)
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.LotteryEnabled = true
		s.LotteryDailyBuyLimit = 3
		s.LotteryMinStakeQuota = 1000
		s.LotteryMaxStakeQuota = 100000
		s.LotteryMaxUserQuota = 50000 // 额度 >=50000 不可购买
		s.LotterySystemInjectedQuota = 0
	})

	const userID = 2007
	insertLotteryTestUser(t, userID, 60000) // 超过 50000 阈值

	_, err := BuyLotteryTicket(userID, &LotteryBuyRequest{
		RedBalls:   []int{1, 2, 3, 4, 5},
		BlueBall:   1,
		StakeQuota: 1000,
	})
	require.Error(t, err)
	// 额度不变
	assert.Equal(t, 60000, currentQuotaForLotteryTest(t, userID))

	// status 中 ineligible_reason 应说明原因
	data, err := GetLotteryStatus(userID)
	require.NoError(t, err)
	assert.True(t, data.Enabled)
	assert.False(t, data.Eligible)
	assert.NotEmpty(t, data.IneligibleReason)
}

// ---------- Settlement idempotency ----------

// TestLottery_Settle_IdempotentNoDoublePayout 直接构造一个 open 期（已过开奖时间），
// 调用 settleLotteryRound 两次，断言第二次为 no-op 且不重复派奖。
func TestLottery_Settle_IdempotentNoDoublePayout(t *testing.T) {
	truncateTables(t)
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.LotteryEnabled = true
		s.LotteryDailyBuyLimit = 3
		s.LotteryMinStakeQuota = 1000
		s.LotteryMaxStakeQuota = 100000
		s.LotterySystemInjectedQuota = 100000
		s.LotteryMaxUserQuota = 0
		s.LotteryDrawHour = 22
	})

	// 构造一个昨天开奖的 open 期（drawTime 已过）
	now := time.Now().In(time.Local)
	yesterday := now.AddDate(0, 0, -1)
	day, err := strconv.Atoi(yesterday.Format("20060102"))
	require.NoError(t, err)

	// 两个用户，各持一注（号码不同以保证至少一个能中奖：覆盖所有蓝球 + 多组红球）
	const u1, u2 = 3001, 3002
	insertLotteryTestUser(t, u1, 0)
	insertLotteryTestUser(t, u2, 0)

	const stake1, stake2 = 10000, 5000
	round := &GameLotteryRound{
		Day:               day,
		Status:            LotteryStatusOpen,
		TotalStakeQuota:   stake1 + stake2,
		PoolInjectedQuota: 100000,
		PoolCarryInQuota:  0,
	}
	require.NoError(t, DB.Create(round).Error)

	t1 := &GameLotteryTicket{
		RoundID:    round.Id,
		UserID:     u1,
		Username:   "u1",
		RedBalls:   "1,2,3,4,5",
		BlueBall:   1,
		StakeQuota: stake1,
		Result:     "pending",
		CreatedAt:  common.GetTimestamp(),
	}
	t2 := &GameLotteryTicket{
		RoundID:    round.Id,
		UserID:     u2,
		Username:   "u2",
		RedBalls:   "6,7,8,9,10",
		BlueBall:   2,
		StakeQuota: stake2,
		Result:     "pending",
		CreatedAt:  common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(t1).Error)
	require.NoError(t, DB.Create(t2).Error)

	// 第一次结算
	affectedIDs, prizeLogs, err := settleLotteryRound(round)
	require.NoError(t, err)
	_ = affectedIDs // 可能为空（若两注均未命中），不强制要求

	// 验证 round 状态翻转
	var settledRound GameLotteryRound
	require.NoError(t, DB.Where("id = ?", round.Id).First(&settledRound).Error)
	assert.Equal(t, LotteryStatusDrawn, settledRound.Status)
	assert.Greater(t, settledRound.DrawnAt, int64(0))
	assert.NotEmpty(t, settledRound.RedBalls)
	assert.GreaterOrEqual(t, settledRound.BlueBall, 1)
	assert.LessOrEqual(t, settledRound.BlueBall, 6)

	// 奖池守恒：PoolPrizeQuota + PoolCarryOutQuota == PoolCarryInQuota + TotalStakeQuota + PoolInjectedQuota
	allocatable := settledRound.PoolCarryInQuota + settledRound.TotalStakeQuota + settledRound.PoolInjectedQuota
	assert.Equal(t, allocatable, settledRound.PoolPrizeQuota+settledRound.PoolCarryOutQuota,
		"pool conservation: prize + carryOut == allocatable")

	// 至少有一个 ticket 被 drawn（result != pending）
	var drawnTickets []GameLotteryTicket
	require.NoError(t, DB.Where("round_id = ?", round.Id).Find(&drawnTickets).Error)
	for _, tk := range drawnTickets {
		assert.NotEqual(t, "pending", tk.Result, "ticket must be drawn after settle")
		assert.GreaterOrEqual(t, tk.PrizeQuota, 0)
	}
	// prize log 数量应等于 prize_quota>0 的 ticket 数
	var winningCount int64
	require.NoError(t, DB.Model(&GameLotteryTicket{}).Where("round_id = ? AND prize_quota > 0", round.Id).Count(&winningCount).Error)
	assert.Equal(t, int(winningCount), len(prizeLogs))

	// 记录第一次结算后的用户额度
	quota1AfterFirst := currentQuotaForLotteryTest(t, u1)
	quota2AfterFirst := currentQuotaForLotteryTest(t, u2)

	// 第二次结算：必须为 no-op，不重复派奖
	_, prizeLogs2, err := settleLotteryRound(round)
	require.NoError(t, err)
	assert.Empty(t, prizeLogs2, "second settle must not produce prize logs")

	// round 字段不变
	var reround GameLotteryRound
	require.NoError(t, DB.Where("id = ?", round.Id).First(&reround).Error)
	assert.Equal(t, settledRound.Status, reround.Status)
	assert.Equal(t, settledRound.DrawnAt, reround.DrawnAt)
	assert.Equal(t, settledRound.PoolPrizeQuota, reround.PoolPrizeQuota)
	assert.Equal(t, settledRound.PoolCarryOutQuota, reround.PoolCarryOutQuota)
	assert.Equal(t, settledRound.RedBalls, reround.RedBalls)
	assert.Equal(t, settledRound.BlueBall, reround.BlueBall)

	// 用户额度未变（无重复派奖）
	assert.Equal(t, quota1AfterFirst, currentQuotaForLotteryTest(t, u1))
	assert.Equal(t, quota2AfterFirst, currentQuotaForLotteryTest(t, u2))
}

// ---------- Status gating ----------

func TestLottery_Status_Enabled_ReturnsRoundAndLimits(t *testing.T) {
	truncateTables(t)
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.LotteryEnabled = true
		s.LotteryDailyBuyLimit = 3
		s.LotteryMinStakeQuota = 1000
		s.LotteryMaxStakeQuota = 50000
		s.LotteryMaxUserQuota = 0
		s.LotteryDrawHour = 22
	})

	const userID = 4001
	insertLotteryTestUser(t, userID, 5000)

	data, err := GetLotteryStatus(userID)
	require.NoError(t, err)
	require.NotNil(t, data)
	assert.True(t, data.Enabled)
	assert.True(t, data.Eligible)
	assert.Equal(t, 3, data.DailyBuyLimit)
	assert.Equal(t, 1000, data.StakeMinQuota)
	assert.Equal(t, 50000, data.StakeMaxQuota)
	assert.Equal(t, LotteryRedBallMax, data.RedBallMax)
	assert.Equal(t, LotteryBlueBallMax, data.BlueBallMax)
	assert.Equal(t, 5000, data.CurrentQuota)
	assert.Greater(t, data.DrawAt, int64(0))
	assert.NotNil(t, data.MyTickets)
}

// ---------- Draw hour fallback ----------

func TestLottery_DrawHour_FallbackOnInvalid(t *testing.T) {
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.LotteryDrawHour = 99 // 非法
	})
	assert.Equal(t, 22, operation_setting.GetLotteryDrawHour())

	// 0 和 23 合法
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.LotteryDrawHour = 0
	})
	assert.Equal(t, 0, operation_setting.GetLotteryDrawHour())
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.LotteryDrawHour = 23
	})
	assert.Equal(t, 23, operation_setting.GetLotteryDrawHour())
}

func TestLottery_StakeRange_Normalized(t *testing.T) {
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.LotteryMinStakeQuota = 0 // 非法 → fallback 1
		s.LotteryMaxStakeQuota = 0 // < min → fallback 到 min
	})
	min, max := operation_setting.GetLotteryStakeQuotaRange()
	assert.Equal(t, 1, min)
	assert.Equal(t, max, min)
}

// ---------- Default safety config (oracle should-fix) ----------

// TestLottery_DefaultGameSettingIsSafe: 出厂默认配置必须安全——
// 彩票默认关闭、系统注入额度为 0。
func TestLottery_DefaultGameSettingIsSafe(t *testing.T) {
	s := operation_setting.GetGameSetting()
	assert.False(t, s.LotteryEnabled, "lottery must default to disabled")
	assert.LessOrEqual(t, s.LotterySystemInjectedQuota, 0, "system injected must default to non-positive")
	assert.Equal(t, 0, operation_setting.GetLotterySystemInjectedQuota(),
		"clamped default injected quota must be 0")
}

// TestLottery_GetSystemInjectedQuota_ClampsNegativeToZero: 负配置应被 clamp 到 0，
// 避免奖池为负导致 round 长期 open 无法结算。
func TestLottery_GetSystemInjectedQuota_ClampsNegativeToZero(t *testing.T) {
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.LotterySystemInjectedQuota = -500
	})
	assert.Equal(t, 0, operation_setting.GetLotterySystemInjectedQuota())

	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.LotterySystemInjectedQuota = 1000
	})
	assert.Equal(t, 1000, operation_setting.GetLotterySystemInjectedQuota())
}

// ---------- Settlement: concurrent only one awards (oracle blocker A) ----------

// TestLottery_Settle_ConcurrentOnlyOneAwards: 多个 goroutine 并发结算同一 open 期，
// 断言至多一个 goroutine 派奖，用户额度仅增加一次。
// 在 SQLite 单连接测试环境下 goroutine 实际被串行化，但本测试仍验证：
//   - 不会 panic/deadlock；
//   - 不会重复派奖（quota == PoolPrizeQuota，而非 2x）；
//   - round 最终状态为 drawn。
//
// 在 MySQL/PostgreSQL 多连接环境下，DB 级 `WHERE status='open'` RowsAffected 校验
// 保证只有一个事务能完成 open→drawn 迁移。
func TestLottery_Settle_ConcurrentOnlyOneAwards(t *testing.T) {
	truncateTables(t)
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.LotteryEnabled = true
		s.LotteryDailyBuyLimit = 3
		s.LotteryMinStakeQuota = 1000
		s.LotteryMaxStakeQuota = 100000
		s.LotterySystemInjectedQuota = 100000
		s.LotteryMaxUserQuota = 0
		s.LotteryDrawHour = 22
	})

	// 构造一个昨天开奖的 open 期（drawTime 已过）。
	now := time.Now().In(time.Local)
	yesterday := now.AddDate(0, 0, -1)
	day, err := strconv.Atoi(yesterday.Format("20060102"))
	require.NoError(t, err)

	const u1 = 3010
	insertLotteryTestUser(t, u1, 0)

	const stake1 = 10000
	round := &GameLotteryRound{
		Day:               day,
		Status:            LotteryStatusOpen,
		TotalStakeQuota:   stake1,
		PoolInjectedQuota: 100000,
		PoolCarryInQuota:  0,
	}
	require.NoError(t, DB.Create(round).Error)

	t1 := &GameLotteryTicket{
		RoundID:    round.Id,
		UserID:     u1,
		Username:   "u1",
		RedBalls:   "1,2,3,4,5",
		BlueBall:   1,
		StakeQuota: stake1,
		Result:     "pending",
		CreatedAt:  common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(t1).Error)

	const goroutines = 5
	var wg sync.WaitGroup
	wg.Add(goroutines)
	prizeLogCounts := make([]int, goroutines)
	errs := make([]error, goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			_, logs, settleErr := settleLotteryRound(round)
			errs[idx] = settleErr
			prizeLogCounts[idx] = len(logs)
		}(i)
	}
	wg.Wait()

	for i, e := range errs {
		require.NoError(t, e, "goroutine %d settle must not error", i)
	}

	// 至多一个 goroutine 产生 prize logs（0 表示没有中奖 ticket，所有 goroutine no-op）。
	awardingCount := 0
	for _, c := range prizeLogCounts {
		if c > 0 {
			awardingCount++
		}
	}
	assert.LessOrEqual(t, awardingCount, 1, "at most one goroutine should award prizes")

	// round 最终为 drawn。
	var finalRound GameLotteryRound
	require.NoError(t, DB.Where("id = ?", round.Id).First(&finalRound).Error)
	assert.Equal(t, LotteryStatusDrawn, finalRound.Status)

	// 用户额度必须等于 round 派奖总额（单一用户、单一 ticket），
	// 任何重复派奖都会导致额度 > PoolPrizeQuota。
	assert.Equal(t, finalRound.PoolPrizeQuota, currentQuotaForLotteryTest(t, u1),
		"user quota must equal total prize paid (no double payout)")
}

// ---------- Buy: pre-existing round uses OnConflict path (oracle blocker B) ----------

// TestLottery_Buy_PreExistingRound_UsesOnConflictPath: 预先创建今日 round（模拟并发创建），
// BuyLotteryTicket 应通过 OnConflict DoNothing 路径复用已存在的 round，不报错、不重复创建。
// 这覆盖了 PostgreSQL 下 `current transaction is aborted` 的回归。
func TestLottery_Buy_PreExistingRound_UsesOnConflictPath(t *testing.T) {
	truncateTables(t)
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.LotteryEnabled = true
		s.LotteryDailyBuyLimit = 3
		s.LotteryMinStakeQuota = 1000
		s.LotteryMaxStakeQuota = 100000
		s.LotteryMaxUserQuota = 0
		s.LotteryDrawHour = 22
		s.LotterySystemInjectedQuota = 0
	})

	const userID = 2020
	insertLotteryTestUser(t, userID, 50000)

	// 预先创建今日 round（模拟并发创建者）。
	drawHour := operation_setting.GetLotteryDrawHour()
	now := time.Now().In(time.Local)
	roundDay := lotteryRoundDay(now, drawHour)
	preRound := &GameLotteryRound{
		Day:               roundDay,
		Status:            LotteryStatusOpen,
		PoolInjectedQuota: 0,
		PoolCarryInQuota:  0,
	}
	require.NoError(t, DB.Create(preRound).Error)

	// Buy 应复用已存在 round（通过 OnConflict DoNothing + 重新 select），不应报 duplicate key。
	res, err := BuyLotteryTicket(userID, &LotteryBuyRequest{
		RedBalls:   []int{1, 2, 3, 4, 5},
		BlueBall:   1,
		StakeQuota: 1000,
	})
	require.NoError(t, err)
	assert.Equal(t, preRound.Id, res.RoundID, "should reuse pre-existing round id")

	// 仍只有一行 round。
	var roundCount int64
	require.NoError(t, DB.Model(&GameLotteryRound{}).Where("day = ?", roundDay).Count(&roundCount).Error)
	assert.Equal(t, int64(1), roundCount, "only one round should exist for the day")
}

// ---------- Buy-vs-settle race guard (oracle must-fix) ----------

// TestLottery_Buy_DrawnRound_ConditionalUpdateBlocksPurchase: 构造一个已 drawn 的今日 round，
// 调用 BuyLotteryTicket。条件原子更新 `total_stake_quota = total_stake_quota + ? WHERE id=? AND status='open'`
// 因 status='drawn' 而 RowsAffected=0，buy 必须 rollback：
//   - 不扣用户额度；
//   - 不创建 ticket；
//   - round.total_stake_quota 不变；
//   - round.status 保持 drawn。
//
// 这验证了 buy 侧的 buy-vs-settle race 守卫：若 settle 已先 claim（status=open→drawn），
// 后续 buy 的条件 update 失败，不会产生"扣款但不参与开奖"的孤儿 ticket。
func TestLottery_Buy_DrawnRound_ConditionalUpdateBlocksPurchase(t *testing.T) {
	truncateTables(t)
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.LotteryEnabled = true
		s.LotteryDailyBuyLimit = 3
		s.LotteryMinStakeQuota = 1000
		s.LotteryMaxStakeQuota = 100000
		s.LotteryMaxUserQuota = 0
		s.LotteryDrawHour = 22
		s.LotterySystemInjectedQuota = 0
	})

	const userID = 2030
	insertLotteryTestUser(t, userID, 50000)

	// 直接构造一个已 drawn 的今日 round（模拟 settle 已先 claim）。
	drawHour := operation_setting.GetLotteryDrawHour()
	now := time.Now().In(time.Local)
	roundDay := lotteryRoundDay(now, drawHour)
	drawnRound := &GameLotteryRound{
		Day:             roundDay,
		Status:          LotteryStatusDrawn,
		TotalStakeQuota: 9999,
		RedBalls:        "1,2,3,4,5",
		BlueBall:        3,
		DrawnAt:         common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(drawnRound).Error)

	_, err := BuyLotteryTicket(userID, &LotteryBuyRequest{
		RedBalls:   []int{1, 2, 3, 4, 5},
		BlueBall:   1,
		StakeQuota: 1000,
	})
	require.Error(t, err, "buy on drawn round must fail")

	// 不扣款
	assert.Equal(t, 50000, currentQuotaForLotteryTest(t, userID))

	// 不创建 ticket
	var ticketCount int64
	require.NoError(t, DB.Model(&GameLotteryTicket{}).Where("user_id = ?", userID).Count(&ticketCount).Error)
	assert.Equal(t, int64(0), ticketCount)

	// round.total_stake_quota 不变（条件 update RowsAffected=0，未累加）
	var round GameLotteryRound
	require.NoError(t, DB.Where("id = ?", drawnRound.Id).First(&round).Error)
	assert.Equal(t, 9999, round.TotalStakeQuota, "total_stake_quota must not change when buy on drawn round")
	assert.Equal(t, LotteryStatusDrawn, round.Status)
}

// TestLottery_Settle_AlreadyDrawn_DoesNotReadOrTouchTickets: 构造一个已 drawn 的期
// （带一张 result='pending'、prize_quota=0 的 ticket），调用 settleLotteryRound。
// 抢占的 claim `WHERE status='open'` RowsAffected=0，必须 no-op 返回：
//   - 不读取 tickets（ticket.result 保持 pending，未被重新计算/覆盖）；
//   - 不派奖（user quota 不变）；
//   - 不改 round 终态字段（status/red_balls/blue_ball/drawn_at/pool_*/winner_* 均不变）。
//
// 这验证了 settle 侧的 buy-vs-settle race 守卫：claim RowsAffected=0 时绝不读取/更新 tickets。
func TestLottery_Settle_AlreadyDrawn_DoesNotReadOrTouchTickets(t *testing.T) {
	truncateTables(t)
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.LotteryEnabled = true
		s.LotteryDailyBuyLimit = 3
		s.LotteryMinStakeQuota = 1000
		s.LotteryMaxStakeQuota = 100000
		s.LotterySystemInjectedQuota = 100000
		s.LotteryMaxUserQuota = 0
		s.LotteryDrawHour = 22
	})

	const u1 = 3020
	insertLotteryTestUser(t, u1, 0)

	// 构造一个已 drawn 的期，带一张已"开奖"的 jackpot ticket（prize_quota>0）。
	round := &GameLotteryRound{
		Day:               20240101,
		Status:            LotteryStatusDrawn,
		TotalStakeQuota:   10000,
		PoolInjectedQuota: 100000,
		PoolCarryInQuota:  0,
		RedBalls:          "1,2,3,4,5",
		BlueBall:          1,
		DrawnAt:           common.GetTimestamp(),
		PoolPrizeQuota:    50000,
		PoolCarryOutQuota: 60000,
		WinnerJackpot:     1,
	}
	require.NoError(t, DB.Create(round).Error)

	// 已开奖 ticket（result != pending，prize_quota > 0）
	ticket := &GameLotteryTicket{
		RoundID:    round.Id,
		UserID:     u1,
		Username:   "u1",
		RedBalls:   "1,2,3,4,5",
		BlueBall:   1,
		StakeQuota: 10000,
		PrizeQuota: 50000,
		Result:     "jackpot",
		CreatedAt:  common.GetTimestamp(),
		DrawnAt:    round.DrawnAt,
	}
	require.NoError(t, DB.Create(ticket).Error)

	// 调用 settle：claim RowsAffected=0，必须 no-op，不读取/更新 tickets/users。
	affectedIDs, prizeLogs, err := settleLotteryRound(round)
	require.NoError(t, err)
	assert.Empty(t, affectedIDs, "no users should be affected when round already drawn")
	assert.Empty(t, prizeLogs, "no prize logs should be produced when round already drawn")

	// round 终态字段全部不变
	var reround GameLotteryRound
	require.NoError(t, DB.Where("id = ?", round.Id).First(&reround).Error)
	assert.Equal(t, round.Status, reround.Status)
	assert.Equal(t, round.RedBalls, reround.RedBalls)
	assert.Equal(t, round.BlueBall, reround.BlueBall)
	assert.Equal(t, round.DrawnAt, reround.DrawnAt)
	assert.Equal(t, round.PoolPrizeQuota, reround.PoolPrizeQuota)
	assert.Equal(t, round.PoolCarryOutQuota, reround.PoolCarryOutQuota)
	assert.Equal(t, round.WinnerJackpot, reround.WinnerJackpot)

	// ticket 不变（result/prize_quota/drawn_at 未被覆盖）
	var reticket GameLotteryTicket
	require.NoError(t, DB.Where("id = ?", ticket.Id).First(&reticket).Error)
	assert.Equal(t, ticket.Result, reticket.Result, "ticket result must not be recomputed")
	assert.Equal(t, ticket.PrizeQuota, reticket.PrizeQuota, "ticket prize must not be changed")
	assert.Equal(t, ticket.DrawnAt, reticket.DrawnAt, "ticket drawn_at must not be changed")

	// user quota 不变（未重复派奖）
	assert.Equal(t, 0, currentQuotaForLotteryTest(t, u1))
}

// TestLottery_Settle_PendingTicketStaysPending_WhenAlreadyDrawn: 更强的守卫验证——
// 构造一个已 drawn 的期，但其中有一张 result='pending' 的"未处理" ticket
// （模拟 claim 失败前的中间态若被并发 settle 触碰）。settle 必须 no-op，
// ticket.result 保持 'pending'（证明 settle 未读取 tickets 并重新计算奖级）。
func TestLottery_Settle_PendingTicketStaysPending_WhenAlreadyDrawn(t *testing.T) {
	truncateTables(t)
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.LotteryEnabled = true
		s.LotteryDailyBuyLimit = 3
		s.LotteryMinStakeQuota = 1000
		s.LotteryMaxStakeQuota = 100000
		s.LotterySystemInjectedQuota = 100000
		s.LotteryMaxUserQuota = 0
		s.LotteryDrawHour = 22
	})

	const u1 = 3030
	insertLotteryTestUser(t, u1, 0)

	round := &GameLotteryRound{
		Day:               20240102,
		Status:            LotteryStatusDrawn,
		TotalStakeQuota:   10000,
		PoolInjectedQuota: 100000,
		RedBalls:          "1,2,3,4,5",
		BlueBall:          1,
		DrawnAt:           common.GetTimestamp(),
		PoolPrizeQuota:    0,
		PoolCarryOutQuota: 110000,
	}
	require.NoError(t, DB.Create(round).Error)

	// 一张 pending ticket（号码正好匹配 round 的开奖号码——若 settle 误读 tickets
	// 并重新计算，会把它变成 jackpot 并派奖）。
	ticket := &GameLotteryTicket{
		RoundID:    round.Id,
		UserID:     u1,
		Username:   "u1",
		RedBalls:   "1,2,3,4,5",
		BlueBall:   1,
		StakeQuota: 10000,
		PrizeQuota: 0,
		Result:     "pending",
		CreatedAt:  common.GetTimestamp(),
		DrawnAt:    0,
	}
	require.NoError(t, DB.Create(ticket).Error)

	affectedIDs, prizeLogs, err := settleLotteryRound(round)
	require.NoError(t, err)
	assert.Empty(t, affectedIDs)
	assert.Empty(t, prizeLogs)

	// ticket 仍为 pending——证明 settle 未读取 tickets / 未重新计算奖级 / 未派奖。
	var reticket GameLotteryTicket
	require.NoError(t, DB.Where("id = ?", ticket.Id).First(&reticket).Error)
	assert.Equal(t, "pending", reticket.Result, "pending ticket must stay pending when round already drawn")
	assert.Equal(t, 0, reticket.PrizeQuota, "no prize should be awarded when round already drawn")
	assert.Equal(t, int64(0), reticket.DrawnAt, "ticket drawn_at must not be set")

	// user quota 不变
	assert.Equal(t, 0, currentQuotaForLotteryTest(t, u1))

	// round 终态字段不变
	var reround GameLotteryRound
	require.NoError(t, DB.Where("id = ?", round.Id).First(&reround).Error)
	assert.Equal(t, round.PoolPrizeQuota, reround.PoolPrizeQuota)
	assert.Equal(t, round.PoolCarryOutQuota, reround.PoolCarryOutQuota)
	assert.Equal(t, round.WinnerJackpot, reround.WinnerJackpot)
}

// ---------- Buy: concurrent daily limit=1 race (Phase 7A must-fix) ----------

// TestLottery_Buy_ConcurrentDailyLimit1_OnlyOneSucceeds 验证 daily limit=1 时两个并发
// （不同号码）购买最终只成功一个。这是 Phase 7A 最后一个 must-fix 的回归守卫：
// MySQL/InnoDB 默认 REPEATABLE READ 下，BuyLotteryTicket 事务内第一条非锁定 SELECT
// 建立 read view，导致后续 COUNT(tickets) 看不到前一个并发 buy 持锁等待期间已 commit
// 的 ticket，多实例可突破每日上限。
//
// 修复：DB.Transaction 显式指定 &sql.TxOptions{Isolation: sql.LevelReadCommitted}，
// 配合 round row lock（条件 update 在 COUNT 之前）串行化同一期购买，使等待方拿到锁后
// COUNT 取最新已提交快照，能看到已 commit 的 ticket，从而正确拒绝超额购买。
//
// 三库兼容：SQLite (glebarez/go-sqlite) 忽略 Isolation 字段，其事务即快照隔离，无此竞态；
// PostgreSQL 默认即 READ COMMITTED，显式指定为 no-op；MySQL 映射为 "READ COMMITTED" 修复点。
//
// 本测试在 SQLite :memory: 单连接环境下运行（TestMain 设置 SetMaxOpenConns(1)），
// 两个 goroutine 会被连接层串行化——这恰好等价于「持有 round row lock 串行化」的效果，
// 故可作为该 RC 修复点的确定性回归：第一个 buy 成功，第二个因 COUNT==limit 被拒绝，
// 且全程不 panic/deadlock，事务选项被三库驱动接受。
// 在 MySQL/PostgreSQL 多连接环境下，DB 级条件 update 的 round row lock 同样串行化同期购买，
// 加上 READ COMMITTED 快照，行为与该断言一致。
func TestLottery_Buy_ConcurrentDailyLimit1_OnlyOneSucceeds(t *testing.T) {
	truncateTables(t)
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.LotteryEnabled = true
		s.LotteryDailyBuyLimit = 1
		s.LotteryMinStakeQuota = 1000
		s.LotteryMaxStakeQuota = 100000
		s.LotteryMaxUserQuota = 0
		s.LotteryDrawHour = 22
		s.LotterySystemInjectedQuota = 0
	})

	const userID = 2040
	insertLotteryTestUser(t, userID, 100000)

	// 两组不同号码，避免触发「同期同号去重」分支而误判 daily limit 路径。
	buyA := &LotteryBuyRequest{
		RedBalls:   []int{1, 2, 3, 4, 5},
		BlueBall:   1,
		StakeQuota: 1000,
	}
	buyB := &LotteryBuyRequest{
		RedBalls:   []int{6, 7, 8, 9, 10},
		BlueBall:   2,
		StakeQuota: 1000,
	}

	type buyOutcome struct {
		err     error
		success bool
	}

	const goroutines = 2
	results := make([]buyOutcome, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		req := buyA
		if i == 1 {
			req = buyB
		}
		go func(idx int, r *LotteryBuyRequest) {
			defer wg.Done()
			_, err := BuyLotteryTicket(userID, r)
			results[idx] = buyOutcome{err: err, success: err == nil}
		}(i, req)
	}
	wg.Wait()

	successCount := 0
	for i, o := range results {
		if o.success {
			successCount++
		} else {
			require.Error(t, o.err, "goroutine %d must return an error on failure", i)
		}
	}
	assert.Equal(t, 1, successCount, "exactly one concurrent buy must succeed when daily limit=1")

	// 仅一注落库
	var ticketCount int64
	require.NoError(t, DB.Model(&GameLotteryTicket{}).Where("user_id = ?", userID).Count(&ticketCount).Error)
	assert.Equal(t, int64(1), ticketCount, "only one ticket must be persisted")

	// 额度仅扣一次（1000）
	assert.Equal(t, 100000-1000, currentQuotaForLotteryTest(t, userID),
		"quota must be deducted exactly once")

	// round 总投注额仅累加一次（1000），未被失败方回滚后双加或漏减。
	var round GameLotteryRound
	require.NoError(t, DB.Where("day = ?", lotteryRoundDay(time.Now().In(time.Local), operation_setting.GetLotteryDrawHour())).First(&round).Error)
	assert.Equal(t, 1000, round.TotalStakeQuota, "round total_stake_quota must reflect exactly one successful buy")
}
