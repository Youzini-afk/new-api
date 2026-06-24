package model

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 彩球规则常量（与旧 gy 一致，前端契约依赖）。
const (
	LotteryStatusOpen   = "open"
	LotteryStatusDrawn  = "drawn"
	LotteryRedBallMin   = 1
	LotteryRedBallMax   = 12
	LotteryBlueBallMin  = 1
	LotteryBlueBallMax  = 6
	LotteryRedBallCount = 5
)

// lotteryRoundMutex 序列化同进程内的 round 创建与结算，避免并发场景下重复创建
// 今日 round / 重复结算。跨进程安全依赖 DB 唯一约束 + 条件原子更新。
var lotteryRoundMutex sync.Mutex

var bigMaxInt = big.NewInt(int64(^uint(0) >> 1))

// safeSumNonNegative sums one or more non-negative ints using big.Int.
// Returns an error if any input is negative or if the sum overflows max int.
func safeSumNonNegative(values ...int) (int, error) {
	sum := big.NewInt(0)
	for _, v := range values {
		if v < 0 {
			return 0, errors.New("negative value in safe sum")
		}
		sum.Add(sum, big.NewInt(int64(v)))
	}
	if sum.Cmp(bigMaxInt) > 0 {
		return 0, errors.New("integer overflow: sum exceeds max int")
	}
	return int(sum.Int64()), nil
}

// safeFloorMulDiv computes floor(amount * numerator / denominator) safely.
// Returns an error if denominator <= 0, if any input is negative,
// or if the result overflows max int.
func safeFloorMulDiv(amount, numerator, denominator int) (int, error) {
	if denominator <= 0 {
		return 0, errors.New("denominator must be positive")
	}
	if amount < 0 || numerator < 0 {
		return 0, errors.New("negative input in safe floor mul div")
	}
	res := big.NewInt(int64(amount))
	res.Mul(res, big.NewInt(int64(numerator)))
	res.Div(res, big.NewInt(int64(denominator)))
	if res.Cmp(bigMaxInt) > 0 {
		return 0, errors.New("integer overflow: result exceeds max int")
	}
	return int(res.Int64()), nil
}

// GameLotteryRound 一期彩票。Day 为 YYYYMMDD 整数，唯一索引保证每期一行。
type GameLotteryRound struct {
	Id                int    `json:"id" gorm:"primaryKey;autoIncrement"`
	Day               int    `json:"day" gorm:"uniqueIndex;not null"`
	Status            string `json:"status" gorm:"type:varchar(20);index;default:'open'"`
	RedBalls          string `json:"red_balls" gorm:"type:varchar(32);default:''"`
	BlueBall          int    `json:"blue_ball" gorm:"default:0"`
	DrawnAt           int64  `json:"drawn_at" gorm:"type:bigint;default:0"`
	TotalStakeQuota   int    `json:"total_stake_quota" gorm:"default:0"`
	PoolCarryInQuota  int    `json:"pool_carry_in_quota" gorm:"default:0"`
	PoolInjectedQuota int    `json:"pool_injected_quota" gorm:"default:0"`
	PoolPrizeQuota    int    `json:"pool_prize_quota" gorm:"default:0"`
	PoolCarryOutQuota int    `json:"pool_carry_out_quota" gorm:"default:0"`
	WinnerJackpot     int    `json:"winner_jackpot" gorm:"default:0"`
	WinnerSecond      int    `json:"winner_second" gorm:"default:0"`
	WinnerThird       int    `json:"winner_third" gorm:"default:0"`
	WinnerFourth      int    `json:"winner_fourth" gorm:"default:0"`
	WinnerFifth       int    `json:"winner_fifth" gorm:"default:0"`
	WinnerSmall       int    `json:"winner_small" gorm:"default:0"`
}

// GameLotteryTicket 一注彩票。(round_id, user_id, red_balls, blue_ball) 唯一组合
// 防止同一用户在同一期购买完全相同的号码。
type GameLotteryTicket struct {
	Id         int    `json:"id" gorm:"primaryKey;autoIncrement"`
	RoundID    int    `json:"round_id" gorm:"index;uniqueIndex:idx_lottery_combo"`
	UserID     int    `json:"user_id" gorm:"index;uniqueIndex:idx_lottery_combo"`
	Username   string `json:"username" gorm:"type:varchar(255);default:''"`
	RedBalls   string `json:"red_balls" gorm:"type:varchar(32);not null;uniqueIndex:idx_lottery_combo"`
	BlueBall   int    `json:"blue_ball" gorm:"not null;uniqueIndex:idx_lottery_combo"`
	StakeQuota int    `json:"stake_quota" gorm:"not null"`
	PrizeQuota int    `json:"prize_quota" gorm:"default:0"`
	Result     string `json:"result" gorm:"type:varchar(20);default:'pending';index"`
	CreatedAt  int64  `json:"created_at" gorm:"type:bigint"`
	DrawnAt    int64  `json:"drawn_at" gorm:"type:bigint;default:0"`
}

// ---------- Views / DTOs ----------

type LotteryTicketView struct {
	Id         int    `json:"id"`
	RoundID    int    `json:"round_id"`
	RedBalls   []int  `json:"red_balls"`
	BlueBall   int    `json:"blue_ball"`
	StakeQuota int    `json:"stake_quota"`
	PrizeQuota int    `json:"prize_quota"`
	Result     string `json:"result"`
	CreatedAt  int64  `json:"created_at"`
	DrawnAt    int64  `json:"drawn_at"`
}

type LotteryRoundView struct {
	Day               int   `json:"day"`
	RedBalls          []int `json:"red_balls"`
	BlueBall          int   `json:"blue_ball"`
	DrawnAt           int64 `json:"drawn_at"`
	TotalStakeQuota   int   `json:"total_stake_quota"`
	PoolCarryInQuota  int   `json:"pool_carry_in_quota"`
	PoolInjectedQuota int   `json:"pool_injected_quota"`
	PoolPrizeQuota    int   `json:"pool_prize_quota"`
	PoolCarryOutQuota int   `json:"pool_carry_out_quota"`
	WinnerJackpot     int   `json:"winner_jackpot"`
	WinnerSecond      int   `json:"winner_second"`
	WinnerThird       int   `json:"winner_third"`
	WinnerFourth      int   `json:"winner_fourth"`
	WinnerFifth       int   `json:"winner_fifth"`
	WinnerSmall       int   `json:"winner_small"`
}

// LotteryStatusData 彩票状态响应。Enabled/StakeMin/StakeMax/Eligible 字段供前端 gating。
type LotteryStatusData struct {
	Enabled             bool                `json:"enabled"`
	Eligible            bool                `json:"eligible"`
	IneligibleReason    string              `json:"ineligible_reason"`
	RoundID             int                 `json:"round_id"`
	Day                 int                 `json:"day"`
	Status              string              `json:"status"`
	DrawAt              int64               `json:"draw_at"`
	DailyBuyCount       int                 `json:"daily_buy_count"`
	DailyBuyLimit       int                 `json:"daily_buy_limit"`
	CurrentQuota        int                 `json:"current_quota"`
	StakeMinQuota       int                 `json:"stake_min_quota"`
	StakeMaxQuota       int                 `json:"stake_max_quota"`
	RedBallMax          int                 `json:"red_ball_max"`
	BlueBallMax         int                 `json:"blue_ball_max"`
	SystemInjectedQuota int                 `json:"system_injected_quota"`
	PoolCarryInQuota    int                 `json:"pool_carry_in_quota"`
	PoolInjectedQuota   int                 `json:"pool_injected_quota"`
	TotalStakeQuota     int                 `json:"total_stake_quota"`
	PoolPrizeQuota      int                 `json:"pool_prize_quota"`
	PoolCarryOutQuota   int                 `json:"pool_carry_out_quota"`
	WinnerJackpot       int                 `json:"winner_jackpot"`
	WinnerSecond        int                 `json:"winner_second"`
	WinnerThird         int                 `json:"winner_third"`
	WinnerFourth        int                 `json:"winner_fourth"`
	WinnerFifth         int                 `json:"winner_fifth"`
	WinnerSmall         int                 `json:"winner_small"`
	MyTickets           []LotteryTicketView `json:"my_tickets"`
	MyRecentTickets     []LotteryTicketView `json:"my_recent_tickets"`
	RecentRounds        []LotteryRoundView  `json:"recent_rounds"`
}

// LotteryBuyRequest 兼容旧 gy API：保留 StakeUSD（美元整数），同时支持 StakeQuota（quota 单位）。
// 二者同时提供时以 StakeQuota 为准；StakeQuota<=0 时按 StakeUSD 换算。
type LotteryBuyRequest struct {
	RedBalls   []int `json:"red_balls"`
	BlueBall   int   `json:"blue_ball"`
	StakeUSD   int   `json:"stake_usd"`
	StakeQuota int   `json:"stake_quota"`
}

// LotteryBuyResult 购买成功响应。
type LotteryBuyResult struct {
	RoundID       int    `json:"round_id"`
	Day           int    `json:"day"`
	Status        string `json:"status"`
	DailyBuyCount int    `json:"daily_buy_count"`
	DailyBuyLimit int    `json:"daily_buy_limit"`
	NewQuota      int    `json:"new_quota"`
	TicketID      int    `json:"ticket_id"`
	StakeQuota    int    `json:"stake_quota"`
}

// ---------- Helpers ----------

func weightedLotteryShare(pool, stake, totalStake int) int {
	if pool <= 0 || stake <= 0 || totalStake <= 0 {
		return 0
	}
	poolBig := big.NewInt(int64(pool))
	stakeBig := big.NewInt(int64(stake))
	totalStakeBig := big.NewInt(int64(totalStake))
	poolBig.Mul(poolBig, stakeBig)
	poolBig.Div(poolBig, totalStakeBig)
	if !poolBig.IsInt64() {
		return 0
	}
	result := poolBig.Int64()
	maxInt := int64(^uint(0) >> 1)
	if result > maxInt {
		return int(maxInt)
	}
	return int(result)
}

func lotteryCryptoInt(max int) (int, error) {
	if max <= 0 {
		return 0, errors.New("max must be positive")
	}
	nBig, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0, err
	}
	return int(nBig.Int64()), nil
}

// lotteryRoundDay 返回给定时间所属的开奖日（YYYYMMDD 整数）。
// 在 drawHour 之前属于当日，之后属于次日。
func lotteryRoundDay(t time.Time, drawHour int) int {
	hour := drawHour
	if hour < 0 || hour > 23 {
		hour = 22
	}
	local := t.In(time.Local)
	if local.Hour() < hour {
		day, _ := strconv.Atoi(local.Format("20060102"))
		return day
	}
	next := local.Add(24 * time.Hour)
	day, _ := strconv.Atoi(next.Format("20060102"))
	return day
}

// lotteryDrawTime 返回该开奖日对应的开奖时刻（本地时区）。
func lotteryDrawTime(day int, drawHour int) time.Time {
	hour := drawHour
	if hour < 0 || hour > 23 {
		hour = 22
	}
	s := strconv.Itoa(day)
	if len(s) != 8 {
		return time.Time{}
	}
	year, _ := strconv.Atoi(s[:4])
	month, _ := strconv.Atoi(s[4:6])
	d, _ := strconv.Atoi(s[6:8])
	return time.Date(year, time.Month(month), d, hour, 0, 0, 0, time.Local)
}

func lotteryDrawTimeUnix(day int, drawHour int) int64 {
	return lotteryDrawTime(day, drawHour).Unix()
}

func parseLotteryBalls(s string) []int {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	res := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := strconv.Atoi(p)
		if err != nil {
			continue
		}
		res = append(res, v)
	}
	return res
}

func formatLotteryBalls(balls []int) string {
	parts := make([]string, len(balls))
	for i, v := range balls {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ",")
}

// determinePrizeTier 根据红球命中数与蓝球命中数确定奖级。
// 纯函数，便于直接表测。
func determinePrizeTier(redMatch, blueMatch int) string {
	switch {
	case redMatch == 5 && blueMatch == 1:
		return "jackpot"
	case redMatch == 5 && blueMatch == 0:
		return "second"
	case redMatch == 4 && blueMatch == 1:
		return "third"
	case redMatch == 4 && blueMatch == 0:
		return "fourth"
	case redMatch == 3 && blueMatch == 1:
		return "fifth"
	case redMatch == 3 && blueMatch == 0:
		return "small_three"
	case redMatch == 2 && blueMatch == 1:
		return "small_two_blue"
	case redMatch == 1 && blueMatch == 1:
		return "small_one_blue"
	default:
		return "none"
	}
}

func findLastDrawnRound(tx *gorm.DB) (GameLotteryRound, error) {
	var r GameLotteryRound
	if err := tx.Where("status = ?", LotteryStatusDrawn).Order("day desc, id desc").First(&r).Error; err != nil {
		return r, err
	}
	return r, nil
}

// ---------- Settlement ----------

// settleOpenLotteryRounds 结算所有已到开奖时间的 open 期。幂等：已 drawn 的期跳过。
// 用户额度更新在 transaction 内；RecordLog/InvalidateUserCache 在 commit 后执行。
func settleOpenLotteryRounds() error {
	var rounds []GameLotteryRound
	if err := DB.Where("status = ?", LotteryStatusOpen).Find(&rounds).Error; err != nil {
		return err
	}
	drawHour := operation_setting.GetLotteryDrawHour()
	now := time.Now().In(time.Local)
	for i := range rounds {
		round := rounds[i]
		drawTime := lotteryDrawTime(round.Day, drawHour)
		if !drawTime.IsZero() && (now.After(drawTime) || now.Equal(drawTime)) {
			affectedUserIDs, prizeLogs, err := settleLotteryRound(&round)
			if err != nil {
				common.SysLog(fmt.Sprintf("lottery settle round %d failed: %v", round.Id, err))
				continue
			}
			// commit 后写系统日志与失效缓存
			for _, entry := range prizeLogs {
				RecordLog(entry.userID, LogTypeSystem, entry.content)
			}
			for _, userID := range affectedUserIDs {
				if invErr := InvalidateUserCache(userID); invErr != nil {
					common.SysLog(fmt.Sprintf("lottery cache invalidate failed for user %d: %v", userID, invErr))
				}
			}
		}
	}
	return nil
}

// prizeLogEntry 用于在 commit 后再写 RecordLog。
type prizeLogEntry struct {
	userID  int
	content string
}

// settleLotteryRound 结算单期。跨进程/开奖边界并发安全（buy-vs-settle race 已封死）：
//  1. 事务内首先以 `UPDATE ... SET status='drawn', red_balls=?, blue_ball=?, drawn_at=?
//     WHERE id=? AND status='open'` 原子抢占 open→drawn 迁移。RowsAffected!=1 视为已被
//     并发结算，直接 no-op 返回（不读取 tickets、不派奖、不改 ticket/user）。
//  2. 抢占成功后，在同一事务内重新读取 round（此时 total_stake_quota 已包含所有在抢占前
//     成功 commit 的 buy），再读取 tickets，计算奖级/奖额，更新 round 终态字段、tickets
//     与 user quota。
//  3. 任何后续失败 rollback：status 回到 open，red_balls/blue_ball/drawn_at 还原。
//     commit 后日志/cache 不变。
//
// 与 BuyLotteryTicket 的协同（封死 buy-vs-settle race）：
//   - buy 在扣款/创建 ticket 前以 `UPDATE total_stake_quota = total_stake_quota + ?
//     WHERE id=? AND status='open'` 占用 open round；RowsAffected!=1 直接 rollback。
//   - 若 buy 先持有 round row lock，settle 的 claim 等待 buy commit 后再 claim 并读取
//     tickets/round，故 buy 的 ticket 一定在 settle 快照内。
//   - 若 settle 先 claim，buy 的条件 update 因 status='drawn' 而 RowsAffected=0，失败
//     回滚（不扣款、不创建 ticket）。
func settleLotteryRound(round *GameLotteryRound) ([]int, []prizeLogEntry, error) {
	var affectedUserIDs []int
	var prizeLogs []prizeLogEntry
	err := DB.Transaction(func(tx *gorm.DB) error {
		// 1. 生成中奖号码（crypto/rand，无 DB 读）。不读取 tickets / round.total_stake。
		redSet := map[int]bool{}
		reds := make([]int, 0, LotteryRedBallCount)
		for len(reds) < LotteryRedBallCount {
			v, err := lotteryCryptoInt(LotteryRedBallMax)
			if err != nil {
				return err
			}
			n := v + 1 // 1-based
			if !redSet[n] {
				redSet[n] = true
				reds = append(reds, n)
			}
		}
		sort.Ints(reds)
		blue, err := lotteryCryptoInt(LotteryBlueBallMax)
		if err != nil {
			return err
		}
		blueBall := blue + 1
		drawnAt := common.GetTimestamp()

		// 2. 原子抢占：open -> drawn。仅写入 status 与开奖号码/时间戳，不读 tickets、
		//    不使用 round.total_stake 做计算。WHERE status='open' 保证只有一个事务能完成迁移。
		claimRes := tx.Model(&GameLotteryRound{}).
			Where("id = ? AND status = ?", round.Id, LotteryStatusOpen).
			Updates(map[string]interface{}{
				"status":    LotteryStatusDrawn,
				"red_balls": formatLotteryBalls(reds),
				"blue_ball": blueBall,
				"drawn_at":  drawnAt,
			})
		if claimRes.Error != nil {
			return claimRes.Error
		}
		if claimRes.RowsAffected != 1 {
			// 已被并发结算，幂等 no-op：不读取 tickets、不派奖。
			return nil
		}

		// 3. 抢占成功后重新读取 round：此时 total_stake_quota 包含所有在抢占前成功 commit
		//    的 buy（buy 的条件 update 持有 round row lock，claim 会等待其提交）。
		var lockedRound GameLotteryRound
		if err := tx.Where("id = ?", round.Id).First(&lockedRound).Error; err != nil {
			return err
		}

		// 4. 读取 tickets（所有在抢占前 commit 的 buy 的 ticket 均可见）。
		var tickets []GameLotteryTicket
		if err := tx.Where("round_id = ?", lockedRound.Id).Find(&tickets).Error; err != nil {
			return err
		}

		tierCounts := map[string]int{}
		tierStakeSums := map[string]int{}
		for i := range tickets {
			t := &tickets[i]
			ticketReds := parseLotteryBalls(t.RedBalls)
			redMatch := 0
			for _, r := range ticketReds {
				for _, wr := range reds {
					if r == wr {
						redMatch++
						break
					}
				}
			}
			blueMatch := 0
			if t.BlueBall == blueBall {
				blueMatch = 1
			}
			tier := determinePrizeTier(redMatch, blueMatch)
			t.Result = tier
			tierCounts[tier]++
			if tier != "none" {
				tierStakeSums[tier] += t.StakeQuota
			}
		}

		allocatable, err := safeSumNonNegative(lockedRound.PoolCarryInQuota, lockedRound.TotalStakeQuota, lockedRound.PoolInjectedQuota)
		if err != nil {
			return errors.New("彩票奖池过大，无法安全结算")
		}
		carryOut := 0
		prizeTotal := 0
		totalPoolAllocated := 0

		// Big tiers
		bigTiers := []struct {
			tier    string
			percent int
		}{
			{"jackpot", 50},
			{"second", 20},
			{"third", 12},
			{"fourth", 8},
			{"fifth", 5},
		}

		ticketPrizes := map[int]int{}

		for _, bt := range bigTiers {
			pool, err := safeFloorMulDiv(allocatable, bt.percent, 100)
			if err != nil {
				return errors.New("彩票奖池过大，无法安全结算")
			}
			totalPoolAllocated += pool
			cnt := tierCounts[bt.tier]
			totalStake := tierStakeSums[bt.tier]
			if cnt > 0 && totalStake > 0 {
				tierPrizeTotal := 0
				for i := range tickets {
					t := &tickets[i]
					if t.Result == bt.tier {
						prize := weightedLotteryShare(pool, t.StakeQuota, totalStake)
						ticketPrizes[t.Id] = prize
						tierPrizeTotal += prize
					}
				}
				carryOut += pool - tierPrizeTotal
				prizeTotal += tierPrizeTotal
			} else {
				carryOut += pool
			}
		}

		// Small pool (5%)
		smallPool, err := safeFloorMulDiv(allocatable, 5, 100)
		if err != nil {
			return errors.New("彩票奖池过大，无法安全结算")
		}
		totalPoolAllocated += smallPool

		smallSubTiers := []struct {
			tier   string
			weight int
		}{
			{"small_three", 3},
			{"small_two_blue", 2},
			{"small_one_blue", 1},
		}
		totalWeight := 6
		smallAllocated := 0
		for _, st := range smallSubTiers {
			subPool, err := safeFloorMulDiv(smallPool, st.weight, totalWeight)
			if err != nil {
				return errors.New("彩票奖池过大，无法安全结算")
			}
			smallAllocated += subPool
			cnt := tierCounts[st.tier]
			totalStake := tierStakeSums[st.tier]
			if cnt > 0 && totalStake > 0 {
				tierPrizeTotal := 0
				for i := range tickets {
					t := &tickets[i]
					if t.Result == st.tier {
						prize := weightedLotteryShare(subPool, t.StakeQuota, totalStake)
						ticketPrizes[t.Id] = prize
						tierPrizeTotal += prize
					}
				}
				carryOut += subPool - tierPrizeTotal
				prizeTotal += tierPrizeTotal
			} else {
				carryOut += subPool
			}
		}
		carryOut += smallPool - smallAllocated

		// Percent allocation remainder
		carryOut += allocatable - totalPoolAllocated

		// 5. 更新 round 终态字段（status/red_balls/blue_ball/drawn_at 已由步骤 2 抢占写入）。
		if err := tx.Model(&GameLotteryRound{}).Where("id = ?", lockedRound.Id).Updates(map[string]interface{}{
			"pool_prize_quota":     prizeTotal,
			"pool_carry_out_quota": carryOut,
			"winner_jackpot":       tierCounts["jackpot"],
			"winner_second":        tierCounts["second"],
			"winner_third":         tierCounts["third"],
			"winner_fourth":        tierCounts["fourth"],
			"winner_fifth":         tierCounts["fifth"],
			"winner_small":         tierCounts["small_three"] + tierCounts["small_two_blue"] + tierCounts["small_one_blue"],
		}).Error; err != nil {
			return err
		}

		// 6. 派奖：更新 tickets 与 user quota（仍在 tx 内；
		//    logs 收集到 prizeLogs 供 commit 后由调用方写入）。
		for i := range tickets {
			t := &tickets[i]
			prize := ticketPrizes[t.Id]
			t.PrizeQuota = prize

			if prize > 0 {
				if err := tx.Model(&User{}).Where("id = ?", t.UserID).Update("quota", gorm.Expr("quota + ?", prize)).Error; err != nil {
					return err
				}
				prizeLogs = append(prizeLogs, prizeLogEntry{
					userID:  t.UserID,
					content: fmt.Sprintf("每日幸运彩票 #%d 开奖：获得 %s（%s）", lockedRound.Id, logger.LogQuota(prize), t.Result),
				})
				affectedUserIDs = append(affectedUserIDs, t.UserID)
			}

			if err := tx.Model(&GameLotteryTicket{}).Where("id = ?", t.Id).Updates(map[string]interface{}{
				"prize_quota": prize,
				"result":      t.Result,
				"drawn_at":    drawnAt,
			}).Error; err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return nil, nil, err
	}

	// Deduplicate affected user IDs
	seen := make(map[int]struct{}, len(affectedUserIDs))
	deduped := make([]int, 0, len(affectedUserIDs))
	for _, id := range affectedUserIDs {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			deduped = append(deduped, id)
		}
	}
	return deduped, prizeLogs, err
}

// ---------- Public APIs ----------

// GetLotteryStatus 返回当前彩票状态。若 disabled，返回 enabled=false 的最小响应（不报错）。
// 若 enabled，懒结算 open 期，创建/读取今日 round，并返回额度/限制/历史等。
func GetLotteryStatus(userID int) (*LotteryStatusData, error) {
	setting := operation_setting.GetGameSetting()
	if !setting.LotteryEnabled {
		return &LotteryStatusData{
			Enabled:         false,
			DailyBuyLimit:   setting.LotteryDailyBuyLimit,
			StakeMinQuota:   setting.LotteryMinStakeQuota,
			StakeMaxQuota:   setting.LotteryMaxStakeQuota,
			RedBallMax:      LotteryRedBallMax,
			BlueBallMax:     LotteryBlueBallMax,
			CurrentQuota:    0,
			MyTickets:       []LotteryTicketView{},
			MyRecentTickets: []LotteryTicketView{},
			RecentRounds:    []LotteryRoundView{},
		}, nil
	}

	lotteryRoundMutex.Lock()
	defer lotteryRoundMutex.Unlock()

	if err := settleOpenLotteryRounds(); err != nil {
		return nil, err
	}

	drawHour := operation_setting.GetLotteryDrawHour()
	now := time.Now().In(time.Local)
	roundDay := lotteryRoundDay(now, drawHour)

	var round GameLotteryRound
	err := DB.Where("day = ?", roundDay).First(&round).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		var lastDrawn GameLotteryRound
		carryIn := 0
		if findErr := DB.Where("status = ?", LotteryStatusDrawn).Order("day desc, id desc").First(&lastDrawn).Error; findErr == nil {
			carryIn = lastDrawn.PoolCarryOutQuota
		}
		round = GameLotteryRound{
			Day:               roundDay,
			Status:            LotteryStatusOpen,
			PoolInjectedQuota: operation_setting.GetLotterySystemInjectedQuota(),
			PoolCarryInQuota:  carryIn,
		}
		// 使用 OnConflict DoNothing 避免 PostgreSQL 下 unique violation 后无法继续。
		// 无论是否新插入，都通过 day=? 重新读取规范行。
		if err := DB.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "day"}},
			DoNothing: true,
		}).Create(&round).Error; err != nil {
			return nil, err
		}
		if err := DB.Where("day = ?", roundDay).First(&round).Error; err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	stakeMin, stakeMax := operation_setting.GetLotteryStakeQuotaRange()

	var data LotteryStatusData
	data.Enabled = true
	data.RoundID = round.Id
	data.Day = round.Day
	data.Status = round.Status
	data.DrawAt = lotteryDrawTimeUnix(roundDay, drawHour)
	data.DailyBuyLimit = setting.LotteryDailyBuyLimit
	data.StakeMinQuota = stakeMin
	data.StakeMaxQuota = stakeMax
	data.RedBallMax = LotteryRedBallMax
	data.BlueBallMax = LotteryBlueBallMax
	data.SystemInjectedQuota = round.PoolInjectedQuota
	data.PoolCarryInQuota = round.PoolCarryInQuota
	data.PoolInjectedQuota = round.PoolInjectedQuota
	data.TotalStakeQuota = round.TotalStakeQuota
	data.PoolPrizeQuota = round.PoolPrizeQuota
	data.PoolCarryOutQuota = round.PoolCarryOutQuota
	data.WinnerJackpot = round.WinnerJackpot
	data.WinnerSecond = round.WinnerSecond
	data.WinnerThird = round.WinnerThird
	data.WinnerFourth = round.WinnerFourth
	data.WinnerFifth = round.WinnerFifth
	data.WinnerSmall = round.WinnerSmall

	var user User
	if err := DB.Select("quota").Where("id = ?", userID).First(&user).Error; err != nil {
		return nil, err
	}
	data.CurrentQuota = user.Quota

	// 资格判定：max_user_quota 阈值与今日已签到/购买上限无关，仅供前端 gating；
	// 实际购买时会在事务内原子二次校验。
	if setting.LotteryMaxUserQuota > 0 && user.Quota >= setting.LotteryMaxUserQuota {
		data.Eligible = false
		data.IneligibleReason = fmt.Sprintf("当前额度已达彩票上限 %d", setting.LotteryMaxUserQuota)
	} else {
		data.Eligible = true
	}

	var myTickets []GameLotteryTicket
	if err := DB.Where("round_id = ? AND user_id = ?", round.Id, userID).Order("id asc").Find(&myTickets).Error; err != nil {
		return nil, err
	}
	data.DailyBuyCount = len(myTickets)
	if data.DailyBuyCount >= data.DailyBuyLimit && data.Eligible {
		data.Eligible = false
		if data.IneligibleReason == "" {
			data.IneligibleReason = "今日已达到彩票购买次数上限"
		}
	}

	data.MyTickets = []LotteryTicketView{}
	for _, t := range myTickets {
		data.MyTickets = append(data.MyTickets, LotteryTicketView{
			Id:         t.Id,
			RoundID:    t.RoundID,
			RedBalls:   parseLotteryBalls(t.RedBalls),
			BlueBall:   t.BlueBall,
			StakeQuota: t.StakeQuota,
			PrizeQuota: t.PrizeQuota,
			Result:     t.Result,
			CreatedAt:  t.CreatedAt,
			DrawnAt:    t.DrawnAt,
		})
	}

	var myRecent []GameLotteryTicket
	if err := DB.Where("user_id = ? AND result != ?", userID, "pending").Order("drawn_at desc, id desc").Limit(5).Find(&myRecent).Error; err != nil {
		return nil, err
	}
	data.MyRecentTickets = []LotteryTicketView{}
	for _, t := range myRecent {
		data.MyRecentTickets = append(data.MyRecentTickets, LotteryTicketView{
			Id:         t.Id,
			RoundID:    t.RoundID,
			RedBalls:   parseLotteryBalls(t.RedBalls),
			BlueBall:   t.BlueBall,
			StakeQuota: t.StakeQuota,
			PrizeQuota: t.PrizeQuota,
			Result:     t.Result,
			CreatedAt:  t.CreatedAt,
			DrawnAt:    t.DrawnAt,
		})
	}

	var recentRounds []GameLotteryRound
	if err := DB.Where("status = ?", LotteryStatusDrawn).Order("drawn_at desc, id desc").Limit(5).Find(&recentRounds).Error; err != nil {
		return nil, err
	}
	data.RecentRounds = []LotteryRoundView{}
	for _, r := range recentRounds {
		data.RecentRounds = append(data.RecentRounds, LotteryRoundView{
			Day:               r.Day,
			RedBalls:          parseLotteryBalls(r.RedBalls),
			BlueBall:          r.BlueBall,
			DrawnAt:           r.DrawnAt,
			TotalStakeQuota:   r.TotalStakeQuota,
			PoolCarryInQuota:  r.PoolCarryInQuota,
			PoolInjectedQuota: r.PoolInjectedQuota,
			PoolPrizeQuota:    r.PoolPrizeQuota,
			PoolCarryOutQuota: r.PoolCarryOutQuota,
			WinnerJackpot:     r.WinnerJackpot,
			WinnerSecond:      r.WinnerSecond,
			WinnerThird:       r.WinnerThird,
			WinnerFourth:      r.WinnerFourth,
			WinnerFifth:       r.WinnerFifth,
			WinnerSmall:       r.WinnerSmall,
		})
	}

	return &data, nil
}

// BuyLotteryTicket 购买一注彩票。所有写操作在同一事务内；RecordLog/InvalidateUserCache 在 commit 后执行。
// 扣款采用条件原子更新 `WHERE id=? AND quota >= stake` RowsAffected==1，避免先查后扣的竞态。
// 占用 open round 投注额采用条件原子更新 `WHERE id=? AND status='open'` RowsAffected==1，
// 与 settleLotteryRound 的 claim 互斥，封死 buy-vs-settle 跨开奖边界竞态：
// 若 settle 已先 claim 该期，buy 的条件 update RowsAffected=0，rollback（不扣款、不创建 ticket）。
//
// 事务隔离级别显式指定为 READ COMMITTED：MySQL/InnoDB 默认 REPEATABLE READ 会在第一条
// 非锁定 SELECT 处建立 read view，导致同一事务内后续 COUNT(tickets) 看不到前一个并发 buy
// 在持锁等待期间已 commit 的 ticket（多实例突破每日上限），以及 round 不存在时
// `SELECT miss -> OnConflict DoNothing -> reselect` 看不到并发插入的 round（reselect 仍 miss）。
// READ COMMITTED 让事务内每条非锁定 SELECT 取最新已提交快照：
//   - round row lock（条件 update）串行化同一期购买，等待方拿到锁后 COUNT 能看到已 commit 的 ticket；
//   - OnConflict DoNothing 后 reselect 能看到并发插入的 round。
//
// 三库兼容性：SQLite (glebarez/go-sqlite) 忽略 Isolation 字段（其事务即快照隔离，更强且无此竞态）；
// PostgreSQL 默认即 READ COMMITTED，显式指定为 no-op；MySQL 将其映射为 "READ COMMITTED" 修复点。
func BuyLotteryTicket(userID int, req *LotteryBuyRequest) (*LotteryBuyResult, error) {
	setting := operation_setting.GetGameSetting()
	if !setting.LotteryEnabled {
		return nil, errors.New("彩票功能未启用")
	}
	if userID <= 0 {
		return nil, errors.New("invalid user id")
	}

	if len(req.RedBalls) != LotteryRedBallCount {
		return nil, errors.New("红球数量必须为 5 个")
	}
	redSet := map[int]bool{}
	for _, r := range req.RedBalls {
		if r < LotteryRedBallMin || r > LotteryRedBallMax {
			return nil, errors.New("红球号码必须在 1-12 之间")
		}
		if redSet[r] {
			return nil, errors.New("红球号码不可重复")
		}
		redSet[r] = true
	}
	if req.BlueBall < LotteryBlueBallMin || req.BlueBall > LotteryBlueBallMax {
		return nil, errors.New("蓝球号码必须在 1-6 之间")
	}

	// 投注额度解析：StakeQuota 优先，否则按 StakeUSD 换算（兼容旧 gy API）。
	stakeMin, stakeMax := operation_setting.GetLotteryStakeQuotaRange()
	stakeQuota := req.StakeQuota
	if stakeQuota <= 0 && req.StakeUSD > 0 {
		stakeQuota = int(float64(req.StakeUSD) * common.QuotaPerUnit)
	}
	if stakeQuota <= 0 {
		return nil, errors.New("投注额度必须为正数")
	}
	if stakeQuota < stakeMin {
		return nil, fmt.Errorf("投注额度不能低于 %d", stakeMin)
	}
	if stakeQuota > stakeMax {
		return nil, fmt.Errorf("投注额度不能高于 %d", stakeMax)
	}

	sortedReds := make([]int, LotteryRedBallCount)
	copy(sortedReds, req.RedBalls)
	sort.Ints(sortedReds)
	redStr := formatLotteryBalls(sortedReds)

	lotteryRoundMutex.Lock()
	defer lotteryRoundMutex.Unlock()

	if err := settleOpenLotteryRounds(); err != nil {
		return nil, err
	}

	drawHour := operation_setting.GetLotteryDrawHour()
	now := time.Now().In(time.Local)
	roundDay := lotteryRoundDay(now, drawHour)

	var (
		result        LotteryBuyResult
		username      string
		newQuota      int
		ticketID      int
		dailyBuyCount int
		roundID       int
		roundStatus   string
		roundDayValue int
		postCommitLog string
		shouldLog     bool
	)

	err := DB.Transaction(func(tx *gorm.DB) error {
		var round GameLotteryRound
		err := tx.Where("day = ?", roundDay).First(&round).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			lastDrawn, lastErr := findLastDrawnRound(tx)
			carryIn := 0
			if lastErr == nil {
				carryIn = lastDrawn.PoolCarryOutQuota
			}
			newRound := GameLotteryRound{
				Day:               roundDay,
				Status:            LotteryStatusOpen,
				PoolInjectedQuota: operation_setting.GetLotterySystemInjectedQuota(),
				PoolCarryInQuota:  carryIn,
			}
			// 使用 OnConflict DoNothing 避免在 PostgreSQL 下 unique violation
			// 导致事务进入 aborted 状态（无法在同事务内继续 select）。
			// 无论是否新插入，都通过 day=? 重新读取规范行。
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "day"}},
				DoNothing: true,
			}).Create(&newRound).Error; err != nil {
				return err
			}
			if err := tx.Where("day = ?", roundDay).First(&round).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}

		if round.Status != LotteryStatusOpen {
			return errors.New("当前期次已开奖，请刷新后重试")
		}

		// 条件原子占用 open round 的投注额：`UPDATE total_stake_quota = total_stake_quota + ?
		// WHERE id=? AND status='open'`。RowsAffected!=1 说明该期已被并发开奖，
		// 直接 rollback（不扣款、不创建 ticket）。此 update 放在 count/duplicate 检查前，
		// 用 round row lock 串行化同一期购买：后续 daily limit 查询能看到前一个 buy
		// commit 后的 ticket，避免多实例并发突破每日上限。若后续校验/扣款/建票失败，
		// transaction rollback 会撤销这次 total_stake_quota 累加。
		// 同时它与 settleLotteryRound 的 claim 互斥，封死 buy-vs-settle 跨开奖边界竞态：
		//   - buy 先持锁 → settle claim 等待 buy commit 后再读 tickets，buy 的 ticket 一定在快照内；
		//   - settle 先 claim → buy 的 WHERE status='open' 失败，buy rollback。
		stakeRes := tx.Model(&GameLotteryRound{}).
			Where("id = ? AND status = ?", round.Id, LotteryStatusOpen).
			Update("total_stake_quota", gorm.Expr("total_stake_quota + ?", stakeQuota))
		if stakeRes.Error != nil {
			return stakeRes.Error
		}
		if stakeRes.RowsAffected != 1 {
			return errors.New("当前期次已开奖，请刷新后重试")
		}

		// Daily buy limit：round row lock 已由上方条件 update 持有，跨进程同一期购买被串行化。
		var ticketCount int64
		if err := tx.Model(&GameLotteryTicket{}).Where("round_id = ? AND user_id = ?", round.Id, userID).Count(&ticketCount).Error; err != nil {
			return err
		}
		if int(ticketCount) >= setting.LotteryDailyBuyLimit {
			return errors.New("今日已达到彩票购买次数上限")
		}

		// 同期同用户同号组合去重（DB 唯一约束兜底并发）
		var existing GameLotteryTicket
		findErr := tx.Where("round_id = ? AND user_id = ? AND red_balls = ? AND blue_ball = ?", round.Id, userID, redStr, req.BlueBall).First(&existing).Error
		if findErr == nil {
			return errors.New("同一期不可购买完全相同的号码组合")
		}
		if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}

		// 条件原子扣款：余额不足或超过 max_user_quota 阈值时 RowsAffected==0。
		// 不先查 quota 后普通扣，避免 TOCTOU 竞态。
		cond := "id = ? AND quota >= ?"
		args := []interface{}{userID, stakeQuota}
		if setting.LotteryMaxUserQuota > 0 {
			cond += " AND quota < ?"
			args = append(args, setting.LotteryMaxUserQuota)
		}
		res := tx.Model(&User{}).Where(cond, args...).Update("quota", gorm.Expr("quota - ?", stakeQuota))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			// 扣款失败：诊断具体原因以便给出清晰错误
			var u User
			if qErr := tx.Select("quota").Where("id = ?", userID).First(&u).Error; qErr != nil {
				return errors.New("额度不足或用户不存在")
			}
			if setting.LotteryMaxUserQuota > 0 && u.Quota >= setting.LotteryMaxUserQuota {
				return fmt.Errorf("当前额度已达彩票上限 %d，不可购买", setting.LotteryMaxUserQuota)
			}
			if u.Quota < stakeQuota {
				return errors.New("额度不足")
			}
			return errors.New("额度不足或不可购买")
		}

		// 读取用户名（用于 ticket 快照，仅展示用途）
		var u User
		if qErr := tx.Select("username, quota").Where("id = ?", userID).First(&u).Error; qErr != nil {
			return qErr
		}
		username = u.Username
		newQuota = u.Quota

		ticket := GameLotteryTicket{
			RoundID:    round.Id,
			UserID:     userID,
			Username:   username,
			RedBalls:   redStr,
			BlueBall:   req.BlueBall,
			StakeQuota: stakeQuota,
			PrizeQuota: 0,
			Result:     "pending",
			CreatedAt:  common.GetTimestamp(),
			DrawnAt:    0,
		}
		if err := tx.Create(&ticket).Error; err != nil {
			errStr := strings.ToLower(err.Error())
			if strings.Contains(errStr, "duplicate") || strings.Contains(errStr, "unique") {
				return errors.New("同一期不能重复购买相同号码")
			}
			return err
		}

		// round.total_stake_quota 已在上方条件 update 中原子累加（带 status='open' 守卫），
		// 不再在此无条件重复更新，避免与 settle claim 竞态后双加。

		ticketID = ticket.Id
		dailyBuyCount = int(ticketCount) + 1
		roundID = round.Id
		roundStatus = round.Status
		roundDayValue = round.Day
		postCommitLog = fmt.Sprintf("每日幸运彩票 #%d 投注：扣除 %s", round.Id, logger.LogQuota(stakeQuota))
		shouldLog = true

		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})

	if err != nil {
		return nil, err
	}

	// commit 后写系统日志与失效缓存（不在事务内写日志）
	if shouldLog {
		RecordLog(userID, LogTypeSystem, postCommitLog)
	}
	if invErr := InvalidateUserCache(userID); invErr != nil {
		common.SysLog(fmt.Sprintf("lottery cache invalidate failed for user %d: %v", userID, invErr))
	}

	result = LotteryBuyResult{
		RoundID:       roundID,
		Day:           roundDayValue,
		Status:        roundStatus,
		DailyBuyCount: dailyBuyCount,
		DailyBuyLimit: setting.LotteryDailyBuyLimit,
		NewQuota:      newQuota,
		TicketID:      ticketID,
		StakeQuota:    stakeQuota,
	}
	return &result, nil
}
