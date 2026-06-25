package model

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// rouletteMaxRecentSpins 控制状态接口返回的最近 spin 数量上限。
const rouletteMaxRecentSpins = 10

// errRouletteIdempotencyConflict 标记事务内 Create 因 (user_id, idempotency_key)
// 唯一约束冲突而失败：调用方需 rollback 本事务的 debit/prize，并重新读取已存在 spin 返回。
var errRouletteIdempotencyConflict = errors.New("roulette idempotency conflict")

// ---------- Models ----------

// GameRouletteSpin 单次付费即时 spin 的审计记录。
// (user_id, idempotency_key) 唯一约束保证幂等：同 key 重试返回已存在结果，不重复扣/派奖。
type GameRouletteSpin struct {
	Id             int    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserID         int    `json:"user_id" gorm:"index;uniqueIndex:idx_roulette_spin_user_key"`
	Username       string `json:"username" gorm:"type:varchar(255);default:''"`
	Day            int    `json:"day" gorm:"index"`
	IdempotencyKey string `json:"idempotency_key" gorm:"type:varchar(128);not null;uniqueIndex:idx_roulette_spin_user_key"`
	StakeQuota     int    `json:"stake_quota" gorm:"not null"`
	MultiplierBps  int    `json:"multiplier_bps" gorm:"not null"`
	RawPrizeQuota  int    `json:"raw_prize_quota" gorm:"default:0"`
	PrizeQuota     int    `json:"prize_quota" gorm:"default:0"`
	NetQuota       int    `json:"net_quota" gorm:"default:0"`
	OutcomeKey     string `json:"outcome_key" gorm:"type:varchar(64);default:''"`
	Capped         bool   `json:"capped" gorm:"default:false"`
	CreatedAt      int64  `json:"created_at" gorm:"type:bigint"`
}

// GameRouletteDailyUser 每用户每日聚合：spin 次数 / 累计 stake / 累计 prize / 累计 net。
// (user_id, day) 唯一约束保证每用户每日一行；事务内以 SELECT FOR UPDATE 加锁串行化。
type GameRouletteDailyUser struct {
	Id         int   `json:"id" gorm:"primaryKey;autoIncrement"`
	UserID     int   `json:"user_id" gorm:"index;uniqueIndex:idx_roulette_daily_user_day"`
	Day        int   `json:"day" gorm:"index;uniqueIndex:idx_roulette_daily_user_day"`
	SpinCount  int   `json:"spin_count" gorm:"default:0"`
	StakeQuota int   `json:"stake_quota" gorm:"default:0"`
	PrizeQuota int   `json:"prize_quota" gorm:"default:0"`
	NetQuota   int   `json:"net_quota" gorm:"default:0"`
	CreatedAt  int64 `json:"created_at" gorm:"type:bigint"`
	UpdatedAt  int64 `json:"updated_at" gorm:"type:bigint"`
}

// ---------- DTOs ----------

// RouletteWheelItem 加权 wheel 的单个 outcome。MultiplierBps 为 prize/stake 的基点（20000 = 2x）。
type RouletteWheelItem struct {
	Key           string `json:"key"`
	MultiplierBps int    `json:"multiplier_bps"`
	Weight        int    `json:"weight"`
}

// RouletteSpinRequest POST /spin 请求体。
type RouletteSpinRequest struct {
	StakeQuota     int    `json:"stake_quota"`
	IdempotencyKey string `json:"idempotency_key"`
}

// RouletteSpinResult POST /spin 响应。Idempotent=true 表示本次为幂等重试，未改变 quota/daily。
type RouletteSpinResult struct {
	SpinID          int    `json:"spin_id"`
	Day             int    `json:"day"`
	StakeQuota      int    `json:"stake_quota"`
	MultiplierBps   int    `json:"multiplier_bps"`
	RawPrizeQuota   int    `json:"raw_prize_quota"`
	PrizeQuota      int    `json:"prize_quota"`
	NetQuota        int    `json:"net_quota"`
	OutcomeKey      string `json:"outcome_key"`
	Capped          bool   `json:"capped"`
	NewQuota        int    `json:"new_quota"`
	DailySpinCount  int    `json:"daily_spin_count"`
	DailySpinLimit  int    `json:"daily_spin_limit"`
	DailyStakeQuota int    `json:"daily_stake_quota"`
	DailyStakeLimit int    `json:"daily_stake_limit"`
	Idempotent      bool   `json:"idempotent"`
}

// RouletteSpinView 历史记录展示用视图。
type RouletteSpinView struct {
	Id            int    `json:"id"`
	Day           int    `json:"day"`
	StakeQuota    int    `json:"stake_quota"`
	MultiplierBps int    `json:"multiplier_bps"`
	RawPrizeQuota int    `json:"raw_prize_quota"`
	PrizeQuota    int    `json:"prize_quota"`
	NetQuota      int    `json:"net_quota"`
	OutcomeKey    string `json:"outcome_key"`
	Capped        bool   `json:"capped"`
	CreatedAt     int64  `json:"created_at"`
}

// RouletteStatusData GET /roulette 状态响应。disabled 时仅返回 enabled=false 的最小数据。
type RouletteStatusData struct {
	Enabled          bool                `json:"enabled"`
	Eligible         bool                `json:"eligible"`
	IneligibleReason string              `json:"ineligible_reason"`
	CurrentQuota     int                 `json:"current_quota"`
	DailySpinCount   int                 `json:"daily_spin_count"`
	DailySpinLimit   int                 `json:"daily_spin_limit"`
	DailyStakeQuota  int                 `json:"daily_stake_quota"`
	DailyStakeLimit  int                 `json:"daily_stake_limit"`
	StakeMinQuota    int                 `json:"stake_min_quota"`
	StakeMaxQuota    int                 `json:"stake_max_quota"`
	MaxUserQuota     int                 `json:"max_user_quota"`
	RTPBps           int                 `json:"rtp_bps"`
	Wheel            []RouletteWheelItem `json:"wheel"`
	MyRecentSpins    []RouletteSpinView  `json:"my_recent_spins"`
}

// ---------- Helpers ----------

// rouletteCryptoInt 返回 [0, max) 的密码学随机整数。仅使用 crypto/rand，禁止 math/rand。
func rouletteCryptoInt(max int) (int, error) {
	if max <= 0 {
		return 0, errors.New("max must be positive")
	}
	nBig, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0, err
	}
	return int(nBig.Int64()), nil
}

// rouletteCurrentDay 返回当前本地日期的 YYYYMMDD 整数。
func rouletteCurrentDay() int {
	day, _ := strconv.Atoi(time.Now().In(time.Local).Format("20060102"))
	return day
}

// parseRouletteWheel 解析 wheel JSON 并校验每一项，返回 items、sumProd=sum(weight_i*mult_i)
// 与 totalWeight。调用方据此做有理数 RTP 上限比较（见 validatedRouletteWheel），避免 floor(avgRTP)
// 放过真实 RTP 略超 cap 的情况（如 9500.5 bps floor 为 9500 通过 9500 cap）。
// 校验：至少一项；key 非空且唯一；weight > 0；multiplier_bps >= 0；各项与总和不超安全上限。
func parseRouletteWheel(raw []byte) ([]RouletteWheelItem, *big.Int, int, error) {
	if len(raw) == 0 {
		return nil, nil, 0, errors.New("roulette wheel is empty")
	}
	var items []RouletteWheelItem
	if err := common.Unmarshal(raw, &items); err != nil {
		return nil, nil, 0, fmt.Errorf("roulette wheel JSON invalid: %w", err)
	}
	if len(items) == 0 {
		return nil, nil, 0, errors.New("roulette wheel has no outcomes")
	}
	seenKeys := make(map[string]struct{}, len(items))
	totalWeight := 0
	sumProd := big.NewInt(0)
	for _, it := range items {
		if strings.TrimSpace(it.Key) == "" {
			return nil, nil, 0, errors.New("roulette wheel item key is empty")
		}
		if _, dup := seenKeys[it.Key]; dup {
			return nil, nil, 0, fmt.Errorf("roulette wheel item key duplicated: %s", it.Key)
		}
		seenKeys[it.Key] = struct{}{}
		if it.Weight <= 0 {
			return nil, nil, 0, fmt.Errorf("roulette wheel item %s weight must be positive", it.Key)
		}
		if it.MultiplierBps < 0 {
			return nil, nil, 0, fmt.Errorf("roulette wheel item %s multiplier must be non-negative", it.Key)
		}
		if it.Weight > 1<<30 {
			return nil, nil, 0, fmt.Errorf("roulette wheel item %s weight too large", it.Key)
		}
		if it.MultiplierBps > 1<<30 {
			return nil, nil, 0, fmt.Errorf("roulette wheel item %s multiplier too large", it.Key)
		}
		totalWeight += it.Weight
		if totalWeight > 1<<31 {
			return nil, nil, 0, errors.New("roulette wheel total weight too large")
		}
		prod := big.NewInt(int64(it.Weight))
		prod.Mul(prod, big.NewInt(int64(it.MultiplierBps)))
		sumProd.Add(sumProd, prod)
	}
	return items, sumProd, totalWeight, nil
}

// pickRouletteOutcome 按 weight 加权随机选择一个 outcome（crypto/rand）。
func pickRouletteOutcome(items []RouletteWheelItem) (RouletteWheelItem, error) {
	if len(items) == 0 {
		return RouletteWheelItem{}, errors.New("roulette wheel empty")
	}
	totalWeight := 0
	for _, it := range items {
		totalWeight += it.Weight
	}
	if totalWeight <= 0 {
		return RouletteWheelItem{}, errors.New("roulette wheel total weight non-positive")
	}
	n, err := rouletteCryptoInt(totalWeight)
	if err != nil {
		return RouletteWheelItem{}, err
	}
	cum := 0
	for _, it := range items {
		cum += it.Weight
		if n < cum {
			return it, nil
		}
	}
	// 理论不可达；兜底返回最后一项。
	return items[len(items)-1], nil
}

// rouletteOutcomePickerFn 是 outcome 选择的可注入 seam，默认走 crypto/rand 加权实现。
// 仅用于测试中注入确定性 outcome 以校验账务不变量（如 payout capping）；
// 生产路径不得替换，默认即密码学随机。
var rouletteOutcomePickerFn = pickRouletteOutcome

// rouletteRTPFloor 返回 floor(sumProd / totalWeight)，仅供状态展示用途。
// 通过/拒绝决策必须用 validatedRouletteWheel 的有理数比较，不得用此 floor 值。
func rouletteRTPFloor(sumProd *big.Int, totalWeight int) int {
	if totalWeight <= 0 || sumProd == nil {
		return 0
	}
	floor := new(big.Int).Set(sumProd)
	floor.Div(floor, big.NewInt(int64(totalWeight)))
	if !floor.IsInt64() {
		return 0
	}
	return int(floor.Int64())
}

// validatedRouletteWheel 解析并校验 wheel + RTP 上限。失败时调用方应 fail closed。
//
// RTP 比较采用有理数形式，杜绝 floor(avgRTP) 放过真实 RTP 略超 cap 的漏洞：
//
//	真实 RTP = sumProd / totalWeight（有理数）。通过条件为 sumProd <= capBps * totalWeight。
//	例如真实 RTP 9500.5 bps → sumProd=95005, totalWeight=10, cap=9500 → cap*totalWeight=95000；
//	95005 > 95000 → 拒绝（而旧 floor 实现会算出 9500 并放过）。
//
// capBps 由 GetRouletteRTPBps() clamp 到 [0, 9500] 硬上限，故 wheel RTP <= cap 同时保证
// wheel RTP <= 9500 硬上限。capBps=0 表示"禁止任何正 payout"：只有全 0 multiplier wheel
// （sumProd=0）才能通过，任何正 RTP wheel 都被拒绝（fail closed）。
func validatedRouletteWheel() ([]RouletteWheelItem, int, error) {
	items, sumProd, totalWeight, err := parseRouletteWheel(operation_setting.GetRouletteWheelJSON())
	if err != nil {
		return nil, 0, err
	}
	capBps := operation_setting.GetRouletteRTPBps()
	capScaled := new(big.Int).Mul(big.NewInt(int64(capBps)), big.NewInt(int64(totalWeight)))
	if sumProd.Cmp(capScaled) > 0 {
		floor := rouletteRTPFloor(sumProd, totalWeight)
		return nil, 0, fmt.Errorf("roulette wheel RTP > cap %d (floor=%d)", capBps, floor)
	}
	return items, rouletteRTPFloor(sumProd, totalWeight), nil
}

// loadRouletteResultFromSpin 读取 daily + 当前 quota，基于已存在 spin 构建幂等返回结果。
// 不改变 quota/daily（纯读）。
func loadRouletteResultFromSpin(db *gorm.DB, userID int, spin *GameRouletteSpin, idempotent bool) (RouletteSpinResult, error) {
	var daily GameRouletteDailyUser
	_ = db.Where("user_id = ? AND day = ?", userID, spin.Day).First(&daily).Error

	var u User
	if err := db.Select("quota").Where("id = ?", userID).First(&u).Error; err != nil {
		return RouletteSpinResult{}, err
	}
	return RouletteSpinResult{
		SpinID:          spin.Id,
		Day:             spin.Day,
		StakeQuota:      spin.StakeQuota,
		MultiplierBps:   spin.MultiplierBps,
		RawPrizeQuota:   spin.RawPrizeQuota,
		PrizeQuota:      spin.PrizeQuota,
		NetQuota:        spin.NetQuota,
		OutcomeKey:      spin.OutcomeKey,
		Capped:          spin.Capped,
		NewQuota:        u.Quota,
		DailySpinCount:  daily.SpinCount,
		DailySpinLimit:  operation_setting.GetRouletteDailySpinLimit(),
		DailyStakeQuota: daily.StakeQuota,
		DailyStakeLimit: operation_setting.GetRouletteMaxDailyStakeQuota(),
		Idempotent:      idempotent,
	}, nil
}

// ---------- Public APIs ----------

// GetRouletteStatus 返回当前 roulette 状态。
// disabled 时返回 enabled=false 的最小响应（不报错），便于前端 gating。
func GetRouletteStatus(userID int) (*RouletteStatusData, error) {
	setting := operation_setting.GetGameSetting()
	data := &RouletteStatusData{
		Enabled:         setting.RouletteEnabled,
		DailySpinLimit:  operation_setting.GetRouletteDailySpinLimit(),
		DailyStakeLimit: operation_setting.GetRouletteMaxDailyStakeQuota(),
		StakeMinQuota:   0,
		StakeMaxQuota:   0,
		MaxUserQuota:    operation_setting.GetRouletteMaxUserQuota(),
		RTPBps:          operation_setting.GetRouletteRTPBps(),
		Wheel:           []RouletteWheelItem{},
		MyRecentSpins:   []RouletteSpinView{},
	}
	if !setting.RouletteEnabled {
		return data, nil
	}

	stakeMin, stakeMax := operation_setting.GetRouletteStakeQuotaRange()
	data.StakeMinQuota = stakeMin
	data.StakeMaxQuota = stakeMax

	// wheel 解析校验：失败时仍返回 enabled=true，但 eligible=false，spin 也会 fail closed。
	items, wheelRTP, err := validatedRouletteWheel()
	if err != nil {
		data.Eligible = false
		data.IneligibleReason = "roulette 配置无效，请联系管理员"
		// 仍尝试读取 quota/daily 供展示
	} else {
		data.RTPBps = wheelRTP
		data.Wheel = items
	}

	var u User
	if err := DB.Select("quota").Where("id = ?", userID).First(&u).Error; err != nil {
		return nil, err
	}
	data.CurrentQuota = u.Quota

	day := rouletteCurrentDay()
	var daily GameRouletteDailyUser
	_ = DB.Where("user_id = ? AND day = ?", userID, day).First(&daily).Error
	data.DailySpinCount = daily.SpinCount
	data.DailyStakeQuota = daily.StakeQuota

	// 资格判定（仅供前端 gating；实际 spin 时事务内原子二次校验）
	// DailySpinLimit<=0 视为 fail-closed（禁止 spin），不当作 unlimited，避免误配放开无限刷量。
	data.Eligible = true
	if operation_setting.GetRouletteMaxUserQuota() > 0 && u.Quota >= operation_setting.GetRouletteMaxUserQuota() {
		data.Eligible = false
		data.IneligibleReason = fmt.Sprintf("当前额度已达 roulette 上限 %d", operation_setting.GetRouletteMaxUserQuota())
	} else if data.DailySpinLimit <= 0 || data.DailySpinCount >= data.DailySpinLimit {
		data.Eligible = false
		data.IneligibleReason = "今日已达 roulette spin 次数上限"
	} else if data.DailyStakeLimit > 0 && data.DailyStakeQuota >= data.DailyStakeLimit {
		data.Eligible = false
		data.IneligibleReason = fmt.Sprintf("今日累计 stake 已达上限 %d", data.DailyStakeLimit)
	} else if items == nil {
		data.Eligible = false
		if data.IneligibleReason == "" {
			data.IneligibleReason = "roulette 配置无效，请联系管理员"
		}
	}

	var recent []GameRouletteSpin
	if err := DB.Where("user_id = ?", userID).Order("id desc").Limit(rouletteMaxRecentSpins).Find(&recent).Error; err != nil {
		return nil, err
	}
	for _, s := range recent {
		data.MyRecentSpins = append(data.MyRecentSpins, RouletteSpinView{
			Id:            s.Id,
			Day:           s.Day,
			StakeQuota:    s.StakeQuota,
			MultiplierBps: s.MultiplierBps,
			RawPrizeQuota: s.RawPrizeQuota,
			PrizeQuota:    s.PrizeQuota,
			NetQuota:      s.NetQuota,
			OutcomeKey:    s.OutcomeKey,
			Capped:        s.Capped,
			CreatedAt:     s.CreatedAt,
		})
	}
	if data.MyRecentSpins == nil {
		data.MyRecentSpins = []RouletteSpinView{}
	}
	return data, nil
}

// SpinRoulette 付费即时 spin：先扣 stake，按加权 wheel 抽 multiplier，派 prize，net = prize - stake。
//
// 并发/幂等安全：
//   - (user_id, idempotency_key) 唯一约束：同 key 重试返回已存在结果，不重复扣/派奖。
//   - daily aggregate 行锁（SELECT FOR UPDATE）串行化同用户同日 spin，防止突破每日次数/stake 上限。
//   - 条件原子扣款 `WHERE id=? AND quota >= stake AND (max_user_quota==0 OR quota < max_user_quota)`，
//     RowsAffected==1 才继续，消除先查后扣的 TOCTOU 竞态。
//   - outcome 使用 crypto/rand 加权选择，禁止 math/rand。
//
// 事务隔离级别显式 READ COMMITTED（理由同 BuyLotteryTicket）：MySQL/InnoDB 默认 RR 会导致
// 事务内非锁定 SELECT 看不到并发 commit 的 daily 行，多实例可突破每日上限；RC 让事务内每条
// 非锁定 SELECT 取最新已提交快照。SQLite 忽略 Isolation（快照隔离更强且无此竞态）；PG 默认 RC。
func SpinRoulette(userID int, req *RouletteSpinRequest) (*RouletteSpinResult, error) {
	setting := operation_setting.GetGameSetting()
	if !setting.RouletteEnabled {
		return nil, errors.New("roulette 功能未启用")
	}
	if userID <= 0 {
		return nil, errors.New("invalid user id")
	}

	// 幂等键 sanitize/trim + 长度上限
	key := strings.TrimSpace(req.IdempotencyKey)
	if key == "" {
		return nil, errors.New("idempotency_key 不能为空")
	}
	if len(key) > 128 {
		return nil, errors.New("idempotency_key 长度不能超过 128")
	}

	// stake range check
	stakeMin, stakeMax := operation_setting.GetRouletteStakeQuotaRange()
	if req.StakeQuota < stakeMin {
		return nil, fmt.Errorf("投注额度不能低于 %d", stakeMin)
	}
	if req.StakeQuota > stakeMax {
		return nil, fmt.Errorf("投注额度不能高于 %d", stakeMax)
	}

	// wheel + RTP 校验（fail closed）
	items, _, err := validatedRouletteWheel()
	if err != nil {
		return nil, fmt.Errorf("roulette 配置无效: %w", err)
	}

	day := rouletteCurrentDay()
	maxUserQuota := operation_setting.GetRouletteMaxUserQuota()
	dailyLimit := operation_setting.GetRouletteDailySpinLimit()
	maxDailyStake := operation_setting.GetRouletteMaxDailyStakeQuota()
	stakeQuota := req.StakeQuota

	var (
		result        RouletteSpinResult
		postCommitLog string
		shouldLog     bool
	)

	err = DB.Transaction(func(tx *gorm.DB) error {
		// g. 幂等检查：同 (user_id, idempotency_key) 已存在则返回已有结果，不改 quota/daily。
		var existing GameRouletteSpin
		findErr := tx.Where("user_id = ? AND idempotency_key = ?", userID, key).First(&existing).Error
		if findErr == nil {
			res, lerr := loadRouletteResultFromSpin(tx, userID, &existing, true)
			if lerr != nil {
				return lerr
			}
			result = res
			return nil
		}
		if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}

		// f. create/select daily aggregate with OnConflict DoNothing; lock daily row。
		daily := GameRouletteDailyUser{UserID: userID, Day: day, CreatedAt: common.GetTimestamp()}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "day"}},
			DoNothing: true,
		}).Create(&daily).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND day = ?", userID, day).First(&daily).Error; err != nil {
			return err
		}

		// 幂等二次校验（must-fix 2）：daily 行锁已串行化同用户同日 spin。首请求 commit 释放锁后，
		// 本事务才能拿到 daily 锁；此时其 (user_id,idempotency_key) spin 已可见。
		// 若在 limit/stake cap/quota/max_user_quota 检查前发现 existing，必须直接返回已有结果，
		// 否则 retry 会被首请求用尽的 daily limit/stake cap 拦截，违反幂等语义。
		var existingAfterLock GameRouletteSpin
		if reErr := tx.Where("user_id = ? AND idempotency_key = ?", userID, key).First(&existingAfterLock).Error; reErr == nil {
			res, lerr := loadRouletteResultFromSpin(tx, userID, &existingAfterLock, true)
			if lerr != nil {
				return lerr
			}
			result = res
			return nil
		} else if !errors.Is(reErr, gorm.ErrRecordNotFound) {
			return reErr
		}

		// h. 每日 spin 次数 + 每日累计 stake 上限（在 debit 前校验）。
		// dailyLimit<=0 为 fail-closed（禁止 spin）：daily.SpinCount(>=0) >= 0 恒成立 → 拒绝。
		// 不把 <=0 视作 unlimited，避免误配放开无限刷量。
		if daily.SpinCount >= dailyLimit {
			return errors.New("今日已达 roulette spin 次数上限")
		}
		if maxDailyStake > 0 {
			newDailyStake := daily.StakeQuota + stakeQuota
			if newDailyStake > maxDailyStake {
				return fmt.Errorf("本次投注将超过每日 stake 上限 %d", maxDailyStake)
			}
		}

		// i. 原子条件扣款：quota >= stake 且（max_user_quota==0 或 quota < max_user_quota）。
		cond := "id = ? AND quota >= ?"
		args := []interface{}{userID, stakeQuota}
		if maxUserQuota > 0 {
			cond += " AND quota < ?"
			args = append(args, maxUserQuota)
		}
		res := tx.Model(&User{}).Where(cond, args...).Update("quota", gorm.Expr("quota - ?", stakeQuota))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			// 诊断具体原因
			var u User
			if qErr := tx.Select("quota").Where("id = ?", userID).First(&u).Error; qErr != nil {
				return errors.New("额度不足或用户不存在")
			}
			if maxUserQuota > 0 && u.Quota >= maxUserQuota {
				return fmt.Errorf("当前额度已达 roulette 上限 %d，不可 spin", maxUserQuota)
			}
			if u.Quota < stakeQuota {
				return errors.New("额度不足")
			}
			return errors.New("额度不足或不可 spin")
		}

		// 读取 username + 扣款后 quota
		var u User
		if qErr := tx.Select("username, quota").Where("id = ?", userID).First(&u).Error; qErr != nil {
			return qErr
		}
		username := u.Username
		newQuota := u.Quota

		// j. crypto/rand 加权 outcome（可通过 rouletteOutcomePickerFn seam 注入测试）
		outcome, oerr := rouletteOutcomePickerFn(items)
		if oerr != nil {
			return oerr
		}

		// k. raw_prize = floor(stake * multiplier_bps / 10000)，溢出安全。
		rawPrize, merr := safeFloorMulDiv(stakeQuota, outcome.MultiplierBps, 10000)
		if merr != nil {
			return merr
		}

		// 中奖 payout capped by max_user_quota 剩余空间（扣款后）。
		capped := false
		prize := rawPrize
		if maxUserQuota > 0 {
			room := maxUserQuota - newQuota
			if room < 0 {
				room = 0
			}
			if prize > room {
				prize = room
				capped = true
			}
		}
		net := prize - stakeQuota

		// l. 原子加 prize（prize 已 cap 到 room，无条件加安全）；创建 spin 行；更新 daily 聚合。
		// 三处写入均校验 RowsAffected==1，异常时返回错误触发 rollback，避免静默部分写入。
		if prize > 0 {
			prizeRes := tx.Model(&User{}).Where("id = ?", userID).Update("quota", gorm.Expr("quota + ?", prize))
			if prizeRes.Error != nil {
				return prizeRes.Error
			}
			if prizeRes.RowsAffected != 1 {
				return fmt.Errorf("roulette prize credit affected %d rows (expected 1)", prizeRes.RowsAffected)
			}
			var u2 User
			if qErr := tx.Select("quota").Where("id = ?", userID).First(&u2).Error; qErr != nil {
				return qErr
			}
			newQuota = u2.Quota
		}

		spin := GameRouletteSpin{
			UserID:         userID,
			Username:       username,
			Day:            day,
			IdempotencyKey: key,
			StakeQuota:     stakeQuota,
			MultiplierBps:  outcome.MultiplierBps,
			RawPrizeQuota:  rawPrize,
			PrizeQuota:     prize,
			NetQuota:       net,
			OutcomeKey:     outcome.Key,
			Capped:         capped,
			CreatedAt:      common.GetTimestamp(),
		}
		if err := tx.Create(&spin).Error; err != nil {
			errStr := strings.ToLower(err.Error())
			if strings.Contains(errStr, "duplicate") || strings.Contains(errStr, "unique") {
				// 并发同 idempotency_key：另一事务已 win，本事务 debit/prize 将随 rollback 撤销。
				return errRouletteIdempotencyConflict
			}
			return err
		}

		// 更新 daily 聚合（已锁行）。RowsAffected!=1 说明 daily 行异常消失，必须 rollback。
		dailyRes := tx.Model(&GameRouletteDailyUser{}).Where("id = ?", daily.Id).Updates(map[string]interface{}{
			"spin_count":  gorm.Expr("spin_count + ?", 1),
			"stake_quota": gorm.Expr("stake_quota + ?", stakeQuota),
			"prize_quota": gorm.Expr("prize_quota + ?", prize),
			"net_quota":   gorm.Expr("net_quota + ?", net),
			"updated_at":  common.GetTimestamp(),
		})
		if dailyRes.Error != nil {
			return dailyRes.Error
		}
		if dailyRes.RowsAffected != 1 {
			return fmt.Errorf("roulette daily aggregate update affected %d rows (expected 1)", dailyRes.RowsAffected)
		}

		result = RouletteSpinResult{
			SpinID:          spin.Id,
			Day:             day,
			StakeQuota:      stakeQuota,
			MultiplierBps:   outcome.MultiplierBps,
			RawPrizeQuota:   rawPrize,
			PrizeQuota:      prize,
			NetQuota:        net,
			OutcomeKey:      outcome.Key,
			Capped:          capped,
			NewQuota:        newQuota,
			DailySpinCount:  daily.SpinCount + 1,
			DailySpinLimit:  dailyLimit,
			DailyStakeQuota: daily.StakeQuota + stakeQuota,
			DailyStakeLimit: maxDailyStake,
			Idempotent:      false,
		}
		postCommitLog = fmt.Sprintf("roulette spin：投注 %s，outcome %s（%dx），中奖 %s，净 %+d",
			logger.LogQuota(stakeQuota), outcome.Key, outcome.MultiplierBps/10000,
			logger.LogQuota(prize), net)
		shouldLog = true
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})

	if err != nil {
		// 幂等冲突：rollback 已撤销本事务 debit/prize，重新读取已存在 spin 返回。
		if errors.Is(err, errRouletteIdempotencyConflict) {
			var existing GameRouletteSpin
			if reErr := DB.Where("user_id = ? AND idempotency_key = ?", userID, key).First(&existing).Error; reErr != nil {
				return nil, fmt.Errorf("roulette spin 并发冲突，请重试: %w", reErr)
			}
			res, lerr := loadRouletteResultFromSpin(DB, userID, &existing, true)
			if lerr != nil {
				return nil, lerr
			}
			return &res, nil
		}
		return nil, err
	}

	// commit 后写日志与失效缓存（不在事务内）
	if shouldLog {
		RecordLog(userID, LogTypeSystem, postCommitLog)
	}
	if invErr := InvalidateUserCache(userID); invErr != nil {
		common.SysLog(fmt.Sprintf("roulette cache invalidate failed for user %d: %v", userID, invErr))
	}
	return &result, nil
}
