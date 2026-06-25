package model

import (
	"fmt"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withGameSetting 复用 model/lottery_test.go 中已定义的 helper（暂存并替换全局 GameSetting，
// cleanup 恢复整个 struct，含 RouletteWheel json.RawMessage）。

// withRouletteOutcomePicker 暂存并替换 outcome picker seam，用于注入确定性 outcome。
func withRouletteOutcomePicker(t *testing.T, fn func(items []RouletteWheelItem) (RouletteWheelItem, error)) {
	t.Helper()
	orig := rouletteOutcomePickerFn
	rouletteOutcomePickerFn = fn
	t.Cleanup(func() { rouletteOutcomePickerFn = orig })
}

func insertRouletteTestUser(t *testing.T, id int, quota int) *User {
	t.Helper()
	u := &User{
		Id:       id,
		Username: "roulette_user_" + common.GetRandomString(6),
		Password: "password1234",
		Quota:    quota,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  common.GetRandomString(8),
	}
	require.NoError(t, DB.Create(u).Error)
	return u
}

// countRouletteSpins 统计指定用户的 spin 行数。
func countRouletteSpins(t *testing.T, userID int) int64 {
	t.Helper()
	var n int64
	require.NoError(t, DB.Model(&GameRouletteSpin{}).Where("user_id = ?", userID).Count(&n).Error)
	return n
}

// rouletteDaily 读取用户当日 daily 聚合。
func rouletteDaily(t *testing.T, userID int, day int) GameRouletteDailyUser {
	t.Helper()
	var d GameRouletteDailyUser
	err := DB.Where("user_id = ? AND day = ?", userID, day).First(&d).Error
	if err != nil {
		return GameRouletteDailyUser{}
	}
	return d
}

func userQuota(t *testing.T, userID int) int {
	t.Helper()
	var u User
	require.NoError(t, DB.Select("quota").Where("id = ?", userID).First(&u).Error)
	return u.Quota
}

// wheelWin 是测试用确定性 win wheel：RTP = (1*30000 + 9*0)/10 = 3000 bps <= 9000 cap。
// 3x 单 outcome 的 RTP 远超 9500 硬上限，不能作为单 outcome wheel；故用 1:9 加权 + seam 强制命中 win。
const wheelWin = `[{"key":"win","multiplier_bps":30000,"weight":1},{"key":"lose","multiplier_bps":0,"weight":9}]`

// forcedWinPicker 忽略随机性，始终返回 win outcome（3x）。
func forcedWinPicker(items []RouletteWheelItem) (RouletteWheelItem, error) {
	return RouletteWheelItem{Key: "win", MultiplierBps: 30000, Weight: 1}, nil
}

// --- Test 1: disabled ---

func TestRoulette_Disabled_StatusAndSpin(t *testing.T) {
	truncateTables(t)
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.RouletteEnabled = false
	})

	const userID = 2200
	insertRouletteTestUser(t, userID, 100000)

	status, err := GetRouletteStatus(userID)
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.False(t, status.Enabled)

	_, err = SpinRoulette(userID, &RouletteSpinRequest{StakeQuota: 1000, IdempotencyKey: "k1"})
	require.Error(t, err)

	// 不应创建任何 spin/daily 行，额度不变。
	assert.Equal(t, int64(0), countRouletteSpins(t, userID))
	assert.Equal(t, 100000, userQuota(t, userID))
}

// --- Test 2: invalid wheel / RTP > cap ---

func TestRoulette_InvalidWheel_RejectsSpin(t *testing.T) {
	truncateTables(t)
	const userID = 2201
	insertRouletteTestUser(t, userID, 1000000)

	cases := []struct {
		name  string
		wheel string
		rtp   int
	}{
		{"malformed_json", `not json`, 9000},
		{"empty_array", `[]`, 9000},
		{"duplicate_key", `[{"key":"a","multiplier_bps":0,"weight":1},{"key":"a","multiplier_bps":100,"weight":1}]`, 9000},
		{"zero_weight", `[{"key":"a","multiplier_bps":0,"weight":0}]`, 9000},
		{"rtp_exceeds_cap", `[{"key":"a","multiplier_bps":9600,"weight":1}]`, 9000},
		{"rtp_exceeds_hard_cap_9500", `[{"key":"a","multiplier_bps":9600,"weight":1}]`, 9600},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withGameSetting(t, func(s *operation_setting.GameSetting) {
				s.RouletteEnabled = true
				s.RouletteMinStakeQuota = 1000
				s.RouletteMaxStakeQuota = 100000
				s.RouletteMaxUserQuota = 0
				s.RouletteRTPBps = tc.rtp
				s.RouletteWheel = []byte(tc.wheel)
			})
			// RTPBps=9600 应被 clamp 到 9500 硬上限。
			if tc.rtp > 9500 {
				assert.Equal(t, 9500, operation_setting.GetRouletteRTPBps())
			}

			_, err := SpinRoulette(userID, &RouletteSpinRequest{StakeQuota: 1000, IdempotencyKey: "k_" + tc.name})
			require.Error(t, err, "case %s should reject", tc.name)
		})
	}

	// 全部失败，不应创建任何 spin 行。
	assert.Equal(t, int64(0), countRouletteSpins(t, userID))
}

// --- Test 2b: strict rational RTP comparison (must-fix 1) ---

// TestRoulette_RTP_RationalComparison: RTP 校验必须用有理数比较 sumProd <= cap*totalWeight，
// 而非 floor(avgRTP) > cap。旧 floor 实现会放过真实 RTP 9500.5（floor 9500）通过 cap=9500。
// 覆盖：真实 RTP 略超 9500 但 floor=9500 → 必须拒绝；真实 RTP 恰好 9500 → 通过；
// cap=0 且正 RTP wheel → 拒绝（fail closed）；cap=0 且全 0 wheel → 通过（RTP 0）。
func TestRoulette_RTP_RationalComparison(t *testing.T) {
	cases := []struct {
		name      string
		wheel     string
		rtpCap    int
		wantPass  bool
		wantFloor int // 期望的 floor RTP（仅 wantPass 时校验）
	}{
		{
			// 真实 RTP = (9501+9500)/2 = 9500.5 bps；floor=9500。cap=9500。
			// 旧 floor 实现会放过（9500 > 9500 false）；有理数比较 19001 > 19000 → 拒绝。
			name:      "real_rtp_9500.5_floored_9500_rejected",
			wheel:     `[{"key":"a","multiplier_bps":9501,"weight":1},{"key":"b","multiplier_bps":9500,"weight":1}]`,
			rtpCap:    9500,
			wantPass:  false,
			wantFloor: 9500,
		},
		{
			// 真实 RTP 恰好 9500；cap=9500 → 通过。
			name:      "real_rtp_exact_9500_passes",
			wheel:     `[{"key":"a","multiplier_bps":9500,"weight":1}]`,
			rtpCap:    9500,
			wantPass:  true,
			wantFloor: 9500,
		},
		{
			// 真实 RTP = (9001+9000)/2 = 9000.5；floor=9000；cap=9000。
			// 旧 floor 实现放过（9000>9000 false）；有理数比较 18001 > 18000 → 拒绝。
			name:      "real_rtp_9000.5_floored_9000_rejected_at_cap_9000",
			wheel:     `[{"key":"a","multiplier_bps":9001,"weight":1},{"key":"b","multiplier_bps":9000,"weight":1}]`,
			rtpCap:    9000,
			wantPass:  false,
			wantFloor: 9000,
		},
		{
			// cap=0 且正 RTP wheel → 拒绝（fail closed：禁止任何正 payout）。
			name:     "cap_zero_positive_rtp_rejected",
			wheel:    `[{"key":"a","multiplier_bps":1,"weight":1}]`,
			rtpCap:   0,
			wantPass: false,
		},
		{
			// cap=0 且全 0 multiplier wheel → 通过（sumProd=0, 0 <= 0）。RTP 0，玩家必输，安全。
			name:      "cap_zero_zero_wheel_passes",
			wheel:     `[{"key":"a","multiplier_bps":0,"weight":1}]`,
			rtpCap:    0,
			wantPass:  true,
			wantFloor: 0,
		},
		{
			// cap 负值 clamp 到 0；正 RTP wheel → 拒绝。
			name:     "cap_negative_clamps_zero_positive_rtp_rejected",
			wheel:    `[{"key":"a","multiplier_bps":100,"weight":1}]`,
			rtpCap:   -5,
			wantPass: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withGameSetting(t, func(s *operation_setting.GameSetting) {
				s.RouletteEnabled = true
				s.RouletteRTPBps = tc.rtpCap
				s.RouletteWheel = []byte(tc.wheel)
			})
			items, floor, err := validatedRouletteWheel()
			if tc.wantPass {
				require.NoError(t, err, "case %s should pass rational RTP check", tc.name)
				assert.NotEmpty(t, items)
				assert.Equal(t, tc.wantFloor, floor, "floor RTP for display")
			} else {
				require.Error(t, err, "case %s must be rejected by rational RTP check", tc.name)
				assert.Nil(t, items)
			}
		})
	}
}

// --- Test 3: stake below min / above max ---

func TestRoulette_StakeOutOfRange_RejectsNoQuotaChange(t *testing.T) {
	truncateTables(t)
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.RouletteEnabled = true
		s.RouletteMinStakeQuota = 1000
		s.RouletteMaxStakeQuota = 5000
		s.RouletteMaxUserQuota = 0
		s.RouletteRTPBps = 9000
		s.RouletteWheel = []byte(wheelWin)
	})

	const userID = 2202
	insertRouletteTestUser(t, userID, 100000)

	// below min
	_, err := SpinRoulette(userID, &RouletteSpinRequest{StakeQuota: 999, IdempotencyKey: "below"})
	require.Error(t, err)
	// above max
	_, err = SpinRoulette(userID, &RouletteSpinRequest{StakeQuota: 5001, IdempotencyKey: "above"})
	require.Error(t, err)

	assert.Equal(t, int64(0), countRouletteSpins(t, userID))
	assert.Equal(t, 100000, userQuota(t, userID))
}

// --- Test 4: insufficient quota ---

func TestRoulette_InsufficientQuota_RejectsNoSideEffects(t *testing.T) {
	truncateTables(t)
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.RouletteEnabled = true
		s.RouletteMinStakeQuota = 1000
		s.RouletteMaxStakeQuota = 100000
		s.RouletteMaxUserQuota = 0
		s.RouletteRTPBps = 9000
		s.RouletteWheel = []byte(wheelWin)
	})

	const userID = 2203
	insertRouletteTestUser(t, userID, 100) // 远低于 stake

	_, err := SpinRoulette(userID, &RouletteSpinRequest{StakeQuota: 1000, IdempotencyKey: "k"})
	require.Error(t, err)

	// 不应创建 spin 行、daily 行，额度不变。
	assert.Equal(t, int64(0), countRouletteSpins(t, userID))
	day := rouletteCurrentDay()
	assert.Equal(t, 0, rouletteDaily(t, userID, day).SpinCount)
	assert.Equal(t, 100, userQuota(t, userID))
}

// --- Test 5: successful spin accounting ---

func TestRoulette_SuccessfulSpin_Accounting(t *testing.T) {
	truncateTables(t)
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.RouletteEnabled = true
		s.RouletteMinStakeQuota = 1000
		s.RouletteMaxStakeQuota = 100000
		s.RouletteMaxUserQuota = 0 // 不限制
		s.RouletteRTPBps = 9000
		s.RouletteWheel = []byte(wheelWin)
	})
	withRouletteOutcomePicker(t, forcedWinPicker) // 强制 3x win

	const userID = 2204
	insertRouletteTestUser(t, userID, 100000)

	const stake = 10000
	res, err := SpinRoulette(userID, &RouletteSpinRequest{StakeQuota: stake, IdempotencyKey: "win1"})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.False(t, res.Idempotent)

	// prize = stake * 30000 / 10000 = 30000; net = 30000 - 10000 = +20000
	assert.Equal(t, 30000, res.MultiplierBps)
	assert.Equal(t, 30000, res.RawPrizeQuota)
	assert.Equal(t, 30000, res.PrizeQuota)
	assert.Equal(t, 20000, res.NetQuota)
	assert.False(t, res.Capped)
	// 100000 - 10000 (stake) + 30000 (prize) = 120000
	assert.Equal(t, 120000, res.NewQuota)
	assert.Equal(t, 1, res.DailySpinCount)

	// DB 一致性
	assert.Equal(t, 120000, userQuota(t, userID))
	assert.Equal(t, int64(1), countRouletteSpins(t, userID))
	day := rouletteCurrentDay()
	d := rouletteDaily(t, userID, day)
	assert.Equal(t, 1, d.SpinCount)
	assert.Equal(t, stake, d.StakeQuota)
	assert.Equal(t, 30000, d.PrizeQuota)
	assert.Equal(t, 20000, d.NetQuota)

	// spin 行审计字段
	var spin GameRouletteSpin
	require.NoError(t, DB.Where("user_id = ?", userID).First(&spin).Error)
	assert.Equal(t, "win", spin.OutcomeKey)
	assert.Equal(t, 30000, spin.RawPrizeQuota)
	assert.Equal(t, 30000, spin.PrizeQuota)
	assert.Equal(t, 20000, spin.NetQuota)
	assert.False(t, spin.Capped)
}

// --- Test 6: max_user_quota blocks rich user + caps payout ---

func TestRoulette_MaxUserQuota_BlocksAndCaps(t *testing.T) {
	truncateTables(t)
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.RouletteEnabled = true
		s.RouletteMinStakeQuota = 1000
		s.RouletteMaxStakeQuota = 100000
		s.RouletteMaxUserQuota = 110000
		s.RouletteRTPBps = 9000
		s.RouletteWheel = []byte(wheelWin)
	})
	withRouletteOutcomePicker(t, forcedWinPicker)

	t.Run("blocks_rich_user", func(t *testing.T) {
		const userID = 2205
		insertRouletteTestUser(t, userID, 110000) // 等于上限，debit `quota < max` 失败

		_, err := SpinRoulette(userID, &RouletteSpinRequest{StakeQuota: 10000, IdempotencyKey: "rich"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "上限")

		assert.Equal(t, int64(0), countRouletteSpins(t, userID))
		assert.Equal(t, 110000, userQuota(t, userID))
	})

	t.Run("caps_payout_to_max", func(t *testing.T) {
		const userID = 2206
		insertRouletteTestUser(t, userID, 100000) // < 110000，通过 debit

		// stake=10000 → 扣后 90000；room = 110000-90000 = 20000；rawPrize=30000 > 20000 → capped=20000
		res, err := SpinRoulette(userID, &RouletteSpinRequest{StakeQuota: 10000, IdempotencyKey: "cap"})
		require.NoError(t, err)
		require.NotNil(t, res)

		assert.Equal(t, 30000, res.RawPrizeQuota)
		assert.Equal(t, 20000, res.PrizeQuota) // 被 cap 到 room
		assert.True(t, res.Capped)
		assert.Equal(t, 10000, res.NetQuota) // 20000 - 10000
		// 最终额度 = 90000 + 20000 = 110000，恰好等于上限，未超过
		assert.Equal(t, 110000, res.NewQuota)
		assert.Equal(t, 110000, userQuota(t, userID))
	})
}

// --- Test 7: daily spin limit + daily stake cap ---

func TestRoulette_DailyLimits_Block(t *testing.T) {
	truncateTables(t)
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.RouletteEnabled = true
		s.RouletteMinStakeQuota = 1000
		s.RouletteMaxStakeQuota = 100000
		s.RouletteMaxUserQuota = 0
		s.RouletteRTPBps = 9000
		s.RouletteWheel = []byte(wheelWin)
	})
	withRouletteOutcomePicker(t, forcedWinPicker)

	t.Run("daily_spin_limit", func(t *testing.T) {
		withGameSetting(t, func(s *operation_setting.GameSetting) {
			s.RouletteDailySpinLimit = 1
		})
		const userID = 2207
		insertRouletteTestUser(t, userID, 1000000)

		_, err := SpinRoulette(userID, &RouletteSpinRequest{StakeQuota: 10000, IdempotencyKey: "s1"})
		require.NoError(t, err)

		_, err = SpinRoulette(userID, &RouletteSpinRequest{StakeQuota: 10000, IdempotencyKey: "s2"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "次数上限")

		assert.Equal(t, int64(1), countRouletteSpins(t, userID))
		day := rouletteCurrentDay()
		assert.Equal(t, 1, rouletteDaily(t, userID, day).SpinCount)
	})

	t.Run("daily_stake_cap", func(t *testing.T) {
		withGameSetting(t, func(s *operation_setting.GameSetting) {
			s.RouletteDailySpinLimit = 100
			s.RouletteMaxDailyStakeQuota = 10000
		})
		const userID = 2208
		insertRouletteTestUser(t, userID, 1000000)

		// 第一次 stake=10000 累计达 cap
		_, err := SpinRoulette(userID, &RouletteSpinRequest{StakeQuota: 10000, IdempotencyKey: "c1"})
		require.NoError(t, err)

		// 第二次再 10000 → 累计 20000 > 10000 cap
		_, err = SpinRoulette(userID, &RouletteSpinRequest{StakeQuota: 10000, IdempotencyKey: "c2"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "stake 上限")

		assert.Equal(t, int64(1), countRouletteSpins(t, userID))
		day := rouletteCurrentDay()
		assert.Equal(t, 1, rouletteDaily(t, userID, day).SpinCount)
	})
}

// --- Test 7b: daily limit <= 0 is fail-closed (should-fix 4) ---

// TestRoulette_DailyLimitZeroOrNegative_FailClosed: DailySpinLimit<=0 必须禁止任何 spin
// （不当作 unlimited），且 status 应 eligible=false。负值经 helper clamp 到 0 同样禁止。
func TestRoulette_DailyLimitZeroOrNegative_FailClosed(t *testing.T) {
	cases := []struct {
		name  string
		limit int
	}{
		{"zero_blocks", 0},
		{"negative_clamps_to_zero_blocks", -3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			truncateTables(t)
			withGameSetting(t, func(s *operation_setting.GameSetting) {
				s.RouletteEnabled = true
				s.RouletteMinStakeQuota = 1000
				s.RouletteMaxStakeQuota = 100000
				s.RouletteMaxUserQuota = 0
				s.RouletteDailySpinLimit = tc.limit
				s.RouletteRTPBps = 9000
				s.RouletteWheel = []byte(wheelWin)
			})
			withRouletteOutcomePicker(t, forcedWinPicker)

			// helper clamp 负值到 0
			assert.LessOrEqual(t, operation_setting.GetRouletteDailySpinLimit(), 0)

			const userID = 2215
			insertRouletteTestUser(t, userID, 1000000)

			// spin 必须被拒绝（fail-closed），不是 unlimited。
			_, err := SpinRoulette(userID, &RouletteSpinRequest{StakeQuota: 10000, IdempotencyKey: "k"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "次数上限")

			// 不创建任何 spin/daily 行，额度不变。
			assert.Equal(t, int64(0), countRouletteSpins(t, userID))
			assert.Equal(t, 1000000, userQuota(t, userID))

			// status eligible=false（即使额度充足、wheel 有效）。
			status, err := GetRouletteStatus(userID)
			require.NoError(t, err)
			assert.False(t, status.Eligible, "daily limit <= 0 must make status ineligible")
			assert.Contains(t, status.IneligibleReason, "次数上限")
		})
	}
}

// --- Test 8: idempotency retry ---

func TestRoulette_Idempotency_NoDoubleDebit(t *testing.T) {
	truncateTables(t)
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.RouletteEnabled = true
		s.RouletteMinStakeQuota = 1000
		s.RouletteMaxStakeQuota = 100000
		s.RouletteMaxUserQuota = 0
		s.RouletteRTPBps = 9000
		s.RouletteWheel = []byte(wheelWin)
	})
	withRouletteOutcomePicker(t, forcedWinPicker)

	const userID = 2209
	insertRouletteTestUser(t, userID, 100000)

	res1, err := SpinRoulette(userID, &RouletteSpinRequest{StakeQuota: 10000, IdempotencyKey: "idem"})
	require.NoError(t, err)
	require.False(t, res1.Idempotent)

	// 同 key 重试：返回同一结果，Idempotent=true，不重复扣/派奖。
	res2, err := SpinRoulette(userID, &RouletteSpinRequest{StakeQuota: 10000, IdempotencyKey: "idem"})
	require.NoError(t, err)
	require.True(t, res2.Idempotent)
	assert.Equal(t, res1.SpinID, res2.SpinID)
	assert.Equal(t, res1.PrizeQuota, res2.PrizeQuota)
	assert.Equal(t, res1.NetQuota, res2.NetQuota)

	// 仅一条 spin 行；daily 仅 1 次；额度仅变化一次（100000 → 120000）。
	assert.Equal(t, int64(1), countRouletteSpins(t, userID))
	day := rouletteCurrentDay()
	assert.Equal(t, 1, rouletteDaily(t, userID, day).SpinCount)
	assert.Equal(t, 120000, userQuota(t, userID))
}

// --- Test 9: parallel determinism (SQLite serialized) ---

func TestRoulette_Parallel_SameKey_OneSpin(t *testing.T) {
	truncateTables(t)
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.RouletteEnabled = true
		s.RouletteMinStakeQuota = 1000
		s.RouletteMaxStakeQuota = 100000
		s.RouletteMaxUserQuota = 0
		s.RouletteDailySpinLimit = 100
		s.RouletteRTPBps = 9000
		s.RouletteWheel = []byte(wheelWin)
	})
	withRouletteOutcomePicker(t, forcedWinPicker)

	const userID = 2210
	insertRouletteTestUser(t, userID, 1000000)

	const goroutines = 5
	results := make([]*RouletteSpinResult, goroutines)
	errs := make([]error, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			// 同一 idempotency_key，并发重试。首请求 win，其余应返回幂等结果（非错误）。
			res, err := SpinRoulette(userID, &RouletteSpinRequest{StakeQuota: 10000, IdempotencyKey: "parallel_same"})
			results[idx] = res
			errs[idx] = err
		}(i)
	}
	wg.Wait()

	// 无任何错误：1 个真实 spin + 4 个幂等返回。绝不能因 daily limit/quota 拦截 retry。
	for i, e := range errs {
		require.NoError(t, e, "goroutine %d must not error (idempotent retry or win)", i)
		require.NotNil(t, results[i], "goroutine %d must return a result", i)
	}
	// 全部返回同一 SpinID。
	firstID := results[0].SpinID
	require.NotZero(t, firstID)
	for i, r := range results {
		assert.Equal(t, firstID, r.SpinID, "goroutine %d must return same spin id", i)
	}
	// 恰好 1 个 Idempotent=false（真实 win），其余 Idempotent=true。
	idempotentCount := 0
	for _, r := range results {
		if r.Idempotent {
			idempotentCount++
		}
	}
	assert.Equal(t, goroutines-1, idempotentCount, "exactly one non-idempotent win, rest idempotent")

	// 恰好一条 spin 行；额度仅变化一次（10000 stake, 30000 prize → net +20000）。
	assert.Equal(t, int64(1), countRouletteSpins(t, userID), "exactly one spin for same idempotency key")
	day := rouletteCurrentDay()
	assert.Equal(t, 1, rouletteDaily(t, userID, day).SpinCount)
	// 1000000 + 20000 (net win) = 1020000
	assert.Equal(t, 1020000, userQuota(t, userID))
}

func TestRoulette_Parallel_DailyLimit_NotExceeded(t *testing.T) {
	truncateTables(t)
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.RouletteEnabled = true
		s.RouletteMinStakeQuota = 1000
		s.RouletteMaxStakeQuota = 100000
		s.RouletteMaxUserQuota = 0
		s.RouletteDailySpinLimit = 3
		s.RouletteMaxDailyStakeQuota = 0
		s.RouletteRTPBps = 9000
		s.RouletteWheel = []byte(wheelWin)
	})
	withRouletteOutcomePicker(t, forcedWinPicker)

	const userID = 2211
	insertRouletteTestUser(t, userID, 1000000)

	const goroutines = 5
	errs := make([]error, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			// 不同 idempotency_key，并发 spin；daily limit=3 应阻止超额。
			_, err := SpinRoulette(userID, &RouletteSpinRequest{
				StakeQuota:     10000,
				IdempotencyKey: fmt.Sprintf("parallel_diff_%d", idx),
			})
			errs[idx] = err
		}(i)
	}
	wg.Wait()

	// 恰好 3 个成功（daily limit=3），2 个因上限报错。
	successCount := 0
	for i, e := range errs {
		if e == nil {
			successCount++
		} else {
			require.Error(t, e, "goroutine %d must error when over limit", i)
		}
	}
	assert.Equal(t, 3, successCount, "exactly 3 concurrent spins must succeed (daily limit=3)")

	// 恰好 3 次成功（daily limit=3），不超过。
	day := rouletteCurrentDay()
	assert.Equal(t, 3, rouletteDaily(t, userID, day).SpinCount, "daily spin count must not exceed limit")
	assert.Equal(t, int64(3), countRouletteSpins(t, userID))
}

// --- Test 10: daily limit=1, retry with same key returns same spin (must-fix 2) ---

// TestRoulette_DailyLimit1_RetrySameKey_ReturnsExistingNotLimitError:
// daily limit=1 时，首请求用尽上限；同 key retry 必须返回已有结果（Idempotent=true），
// 而不是返回"次数上限"错误。这是 must-fix 2 的契约守卫：post-lock 二次幂等检查确保
// retry 不会被首请求用尽的 daily limit/stake cap/quota/max_user_quota 拦截。
func TestRoulette_DailyLimit1_RetrySameKey_ReturnsExistingNotLimitError(t *testing.T) {
	truncateTables(t)
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.RouletteEnabled = true
		s.RouletteMinStakeQuota = 1000
		s.RouletteMaxStakeQuota = 100000
		s.RouletteMaxUserQuota = 0
		s.RouletteDailySpinLimit = 1 // 用尽即上限
		s.RouletteRTPBps = 9000
		s.RouletteWheel = []byte(wheelWin)
	})
	withRouletteOutcomePicker(t, forcedWinPicker)

	const userID = 2212
	insertRouletteTestUser(t, userID, 1000000)

	// 首请求成功，用尽 daily limit=1。
	res1, err := SpinRoulette(userID, &RouletteSpinRequest{StakeQuota: 10000, IdempotencyKey: "limit1"})
	require.NoError(t, err)
	require.False(t, res1.Idempotent)

	// 同 key retry：必须返回已有结果，不是"次数上限"错误。
	res2, err := SpinRoulette(userID, &RouletteSpinRequest{StakeQuota: 10000, IdempotencyKey: "limit1"})
	require.NoError(t, err)
	require.True(t, res2.Idempotent)
	assert.Equal(t, res1.SpinID, res2.SpinID)
	assert.Equal(t, res1.PrizeQuota, res2.PrizeQuota)

	// 仅一条 spin 行；daily 仍为 1；额度仅变化一次。
	assert.Equal(t, int64(1), countRouletteSpins(t, userID))
	day := rouletteCurrentDay()
	assert.Equal(t, 1, rouletteDaily(t, userID, day).SpinCount)
	// 1000000 + 20000 (net win) = 1020000
	assert.Equal(t, 1020000, userQuota(t, userID))
}

// TestRoulette_DailyLimit1_ConcurrentSameKey_AllReturnSameSpin:
// daily limit=1 + 同 key 并发：首请求 win，其余并发 retry 必须全部返回同一 spin（幂等），
// 不能有任何一个因"次数上限"报错。验证 post-lock 二次幂等检查封死并发 retry 被拦截的竞态。
func TestRoulette_DailyLimit1_ConcurrentSameKey_AllReturnSameSpin(t *testing.T) {
	truncateTables(t)
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.RouletteEnabled = true
		s.RouletteMinStakeQuota = 1000
		s.RouletteMaxStakeQuota = 100000
		s.RouletteMaxUserQuota = 0
		s.RouletteDailySpinLimit = 1
		s.RouletteRTPBps = 9000
		s.RouletteWheel = []byte(wheelWin)
	})
	withRouletteOutcomePicker(t, forcedWinPicker)

	const userID = 2213
	insertRouletteTestUser(t, userID, 1000000)

	const goroutines = 4
	results := make([]*RouletteSpinResult, goroutines)
	errs := make([]error, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			res, err := SpinRoulette(userID, &RouletteSpinRequest{StakeQuota: 10000, IdempotencyKey: "concurrent_limit1"})
			results[idx] = res
			errs[idx] = err
		}(i)
	}
	wg.Wait()

	// 全部无错误（1 win + 3 idempotent），绝不返回"次数上限"。
	for i, e := range errs {
		require.NoError(t, e, "goroutine %d must not error (same-key retry cannot be blocked by daily limit)", i)
		require.NotNil(t, results[i])
	}
	firstID := results[0].SpinID
	require.NotZero(t, firstID)
	for i, r := range results {
		assert.Equal(t, firstID, r.SpinID, "goroutine %d must return same spin", i)
	}
	assert.Equal(t, int64(1), countRouletteSpins(t, userID))
	assert.Equal(t, 1020000, userQuota(t, userID))
}

// --- Test 11: stake cap used up, retry same key returns same spin (must-fix 2) ---

// TestRoulette_StakeCapUsedUp_RetrySameKey_ReturnsExistingNotCapError:
// 首请求用尽每日 stake cap；同 key retry 必须返回已有结果，不是"stake 上限"错误。
func TestRoulette_StakeCapUsedUp_RetrySameKey_ReturnsExistingNotCapError(t *testing.T) {
	truncateTables(t)
	withGameSetting(t, func(s *operation_setting.GameSetting) {
		s.RouletteEnabled = true
		s.RouletteMinStakeQuota = 1000
		s.RouletteMaxStakeQuota = 100000
		s.RouletteMaxUserQuota = 0
		s.RouletteDailySpinLimit = 100 // 不用 daily spin limit，专注 stake cap
		s.RouletteMaxDailyStakeQuota = 10000
		s.RouletteRTPBps = 9000
		s.RouletteWheel = []byte(wheelWin)
	})
	withRouletteOutcomePicker(t, forcedWinPicker)

	const userID = 2214
	insertRouletteTestUser(t, userID, 1000000)

	// 首请求 stake=10000 用尽每日 stake cap。
	res1, err := SpinRoulette(userID, &RouletteSpinRequest{StakeQuota: 10000, IdempotencyKey: "capkey"})
	require.NoError(t, err)
	require.False(t, res1.Idempotent)

	// 同 key retry：必须返回已有结果，不是"stake 上限"错误。
	res2, err := SpinRoulette(userID, &RouletteSpinRequest{StakeQuota: 10000, IdempotencyKey: "capkey"})
	require.NoError(t, err)
	require.True(t, res2.Idempotent)
	assert.Equal(t, res1.SpinID, res2.SpinID)

	// 仅一条 spin 行；daily stake 累计仅 10000；额度仅变化一次。
	assert.Equal(t, int64(1), countRouletteSpins(t, userID))
	day := rouletteCurrentDay()
	assert.Equal(t, 10000, rouletteDaily(t, userID, day).StakeQuota)
	assert.Equal(t, 1020000, userQuota(t, userID))
}
