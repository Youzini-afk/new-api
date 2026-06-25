package model

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TopUp struct {
	Id              int     `json:"id"`
	UserId          int     `json:"user_id" gorm:"index"`
	Amount          int64   `json:"amount"`
	Money           float64 `json:"money"`
	TradeNo         string  `json:"trade_no" gorm:"unique;type:varchar(255);index"`
	PaymentMethod   string  `json:"payment_method" gorm:"type:varchar(50)"`
	PaymentProvider string  `json:"payment_provider" gorm:"type:varchar(50);default:''"`
	CreateTime      int64   `json:"create_time"`
	CompleteTime    int64   `json:"complete_time"`
	Status          string  `json:"status"`
}

const (
	PaymentMethodStripe       = "stripe"
	PaymentMethodCreem        = "creem"
	PaymentMethodWaffo        = "waffo"
	PaymentMethodWaffoPancake = "waffo_pancake"
	PaymentMethodBalance      = "balance"
)

const (
	PaymentProviderEpay         = "epay"
	PaymentProviderStripe       = "stripe"
	PaymentProviderCreem        = "creem"
	PaymentProviderWaffo        = "waffo"
	PaymentProviderWaffoPancake = "waffo_pancake"
	PaymentProviderBalance      = "balance"
)

var (
	ErrPaymentMethodMismatch = errors.New("payment method mismatch")
	ErrTopUpNotFound         = errors.New("topup not found")
	ErrTopUpStatusInvalid    = errors.New("topup status invalid")
	ErrPaidMoneyMismatch     = errors.New("paid money mismatch")
	// Stripe-specific validation errors. The webhook amount_total cents
	// check covers paid-money mismatch via ErrPaidMoneyMismatch; these cover
	// the remaining Stripe checkout fields the snapshot must agree on.
	ErrStripeCurrencyMismatch = errors.New("stripe currency mismatch")
	ErrStripeCustomerMismatch = errors.New("stripe customer mismatch")
)

func (topUp *TopUp) Insert() error {
	var err error
	err = DB.Create(topUp).Error
	return err
}

func (topUp *TopUp) Update() error {
	var err error
	err = DB.Save(topUp).Error
	return err
}

func GetTopUpById(id int) *TopUp {
	var topUp *TopUp
	var err error
	err = DB.Where("id = ?", id).First(&topUp).Error
	if err != nil {
		return nil
	}
	return topUp
}

func GetTopUpByTradeNo(tradeNo string) *TopUp {
	var topUp *TopUp
	var err error
	err = DB.Where("trade_no = ?", tradeNo).First(&topUp).Error
	if err != nil {
		return nil
	}
	return topUp
}

func UpdatePendingTopUpStatus(tradeNo string, expectedPaymentProvider string, targetStatus string) error {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		topUp := &TopUp{}
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return ErrTopUpNotFound
		}
		if expectedPaymentProvider != "" && topUp.PaymentProvider != expectedPaymentProvider {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}

		topUp.Status = targetStatus
		return tx.Save(topUp).Error
	})
}

func Recharge(referenceId string, customerId string, callerIp string) (err error) {
	if referenceId == "" {
		return errors.New("未提供支付单号")
	}

	var quota float64
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", referenceId).First(topUp).Error
		if err != nil {
			return errors.New("充值订单不存在")
		}

		if topUp.PaymentProvider != PaymentProviderStripe {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}

		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		err = tx.Save(topUp).Error
		if err != nil {
			return err
		}

		quota = topUp.Money * common.QuotaPerUnit
		err = tx.Model(&User{}).Where("id = ?", topUp.UserId).Updates(map[string]interface{}{"stripe_customer": customerId, "quota": gorm.Expr("quota + ?", quota)}).Error
		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		common.SysError("topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}

	RecordTopupLog(topUp.UserId, fmt.Sprintf("使用在线充值成功，充值金额: %v，支付金额：%d", logger.FormatQuota(int(quota)), topUp.Amount), callerIp, topUp.PaymentMethod, PaymentMethodStripe)

	return nil
}

// topUpQueryWindowSeconds 限制充值记录查询的时间窗口（秒）。
const topUpQueryWindowSeconds int64 = 30 * 24 * 60 * 60

// topUpQueryCutoff 返回允许查询的最早 create_time（秒级 Unix 时间戳）。
func topUpQueryCutoff() int64 {
	return common.GetTimestamp() - topUpQueryWindowSeconds
}

func GetUserTopUps(userId int, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	// Start transaction
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	cutoff := topUpQueryCutoff()

	// Get total count within transaction
	err = tx.Model(&TopUp{}).Where("user_id = ? AND create_time >= ?", userId, cutoff).Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Get paginated topups within same transaction
	err = tx.Where("user_id = ? AND create_time >= ?", userId, cutoff).Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Commit transaction
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return topups, total, nil
}

// GetAllTopUps 获取全平台的充值记录（管理员使用，不限制时间窗口）
func GetAllTopUps(pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	if err = tx.Model(&TopUp{}).Count(&total).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return topups, total, nil
}

// searchTopUpCountHardLimit 搜索充值记录时 COUNT 的安全上限，
// 防止对超大表执行无界 COUNT 触发 DoS。
const searchTopUpCountHardLimit = 10000

// SearchUserTopUps 按订单号搜索某用户的充值记录
func SearchUserTopUps(userId int, keyword string, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&TopUp{}).Where("user_id = ? AND create_time >= ?", userId, topUpQueryCutoff())
	if keyword != "" {
		pattern, perr := sanitizeLikePattern(keyword)
		if perr != nil {
			tx.Rollback()
			return nil, 0, perr
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}

	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to count search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

// SearchAllTopUps 按订单号搜索全平台充值记录（管理员使用，不限制时间窗口）
func SearchAllTopUps(keyword string, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&TopUp{})
	if keyword != "" {
		pattern, perr := sanitizeLikePattern(keyword)
		if perr != nil {
			tx.Rollback()
			return nil, 0, perr
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}

	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to count search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		common.SysError("failed to search topups: " + err.Error())
		return nil, 0, errors.New("搜索充值记录失败")
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

// ManualCompleteTopUp 管理员手动完成订单并给用户充值
func ManualCompleteTopUp(tradeNo string, callerIp string) error {
	if tradeNo == "" {
		return errors.New("未提供订单号")
	}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	var userId int
	var quotaToAdd int
	var payMoney float64
	var paymentMethod string

	err := DB.Transaction(func(tx *gorm.DB) error {
		topUp := &TopUp{}
		// 行级锁，避免并发补单
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return errors.New("充值订单不存在")
		}

		// 幂等处理：已成功直接返回
		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("订单状态不是待支付，无法补单")
		}

		// 计算应充值额度：统一使用 Amount * QuotaPerUnit。Stripe 钱包充值
		// 的 TopUp.Amount 已是归一化后的充值金额单位（见 controller 的
		// quoteStripeTopUp），Money 仅作为支付金额快照供 webhook 校验，不再
		// 参与额度计算。其他订单（如易支付）Amount 本就是美元数量。
		dAmount := decimal.NewFromInt(topUp.Amount)
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		quotaToAdd = int(dAmount.Mul(dQuotaPerUnit).IntPart())
		if quotaToAdd <= 0 {
			return errors.New("无效的充值额度")
		}

		// 标记完成
		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		// 增加用户额度（立即写库，保持一致性）
		if err := tx.Model(&User{}).Where("id = ?", topUp.UserId).Update("quota", gorm.Expr("quota + ?", quotaToAdd)).Error; err != nil {
			return err
		}

		userId = topUp.UserId
		payMoney = topUp.Money
		paymentMethod = topUp.PaymentMethod
		return nil
	})

	if err != nil {
		return err
	}

	// 事务外记录日志，避免阻塞
	RecordTopupLog(userId, fmt.Sprintf("管理员补单成功，充值金额: %v，支付金额：%f", logger.FormatQuota(quotaToAdd), payMoney), callerIp, paymentMethod, "admin")
	return nil
}
func RechargeCreem(referenceId string, customerEmail string, customerName string, callerIp string) (err error) {
	if referenceId == "" {
		return errors.New("未提供支付单号")
	}

	var quota int64
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", referenceId).First(topUp).Error
		if err != nil {
			return errors.New("充值订单不存在")
		}

		if topUp.PaymentProvider != PaymentProviderCreem {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}

		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		err = tx.Save(topUp).Error
		if err != nil {
			return err
		}

		// Creem 直接使用 Amount 作为充值额度（整数）
		quota = topUp.Amount

		// 构建更新字段，优先使用邮箱，如果邮箱为空则使用用户名
		updateFields := map[string]interface{}{
			"quota": gorm.Expr("quota + ?", quota),
		}

		// 如果有客户邮箱，尝试更新用户邮箱（仅当用户邮箱为空时）
		if customerEmail != "" {
			// 先检查用户当前邮箱是否为空
			var user User
			err = tx.Where("id = ?", topUp.UserId).First(&user).Error
			if err != nil {
				return err
			}

			// 如果用户邮箱为空，则更新为支付时使用的邮箱
			if user.Email == "" {
				updateFields["email"] = customerEmail
			}
		}

		err = tx.Model(&User{}).Where("id = ?", topUp.UserId).Updates(updateFields).Error
		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		common.SysError("creem topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}

	RecordTopupLog(topUp.UserId, fmt.Sprintf("使用Creem充值成功，充值额度: %v，支付金额：%.2f", quota, topUp.Money), callerIp, topUp.PaymentMethod, PaymentMethodCreem)

	return nil
}

func RechargeWaffo(tradeNo string, callerIp string) (err error) {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}

	var quotaToAdd int
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(topUp).Error
		if err != nil {
			return errors.New("充值订单不存在")
		}

		if topUp.PaymentProvider != PaymentProviderWaffo {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status == common.TopUpStatusSuccess {
			return nil // 幂等：已成功直接返回
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}

		dAmount := decimal.NewFromInt(topUp.Amount)
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		quotaToAdd = int(dAmount.Mul(dQuotaPerUnit).IntPart())
		if quotaToAdd <= 0 {
			return errors.New("无效的充值额度")
		}

		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		if err := tx.Model(&User{}).Where("id = ?", topUp.UserId).Update("quota", gorm.Expr("quota + ?", quotaToAdd)).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		common.SysError("waffo topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}

	if quotaToAdd > 0 {
		RecordTopupLog(topUp.UserId, fmt.Sprintf("Waffo充值成功，充值额度: %v，支付金额: %.2f", logger.FormatQuota(quotaToAdd), topUp.Money), callerIp, topUp.PaymentMethod, PaymentMethodWaffo)
	}

	return nil
}

func RechargeWaffoPancake(tradeNo string) (err error) {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}

	var quotaToAdd int
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Set("gorm:query_option", "FOR UPDATE").Where(refCol+" = ?", tradeNo).First(topUp).Error
		if err != nil {
			return errors.New("充值订单不存在")
		}

		if topUp.PaymentProvider != PaymentProviderWaffoPancake {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}

		quotaToAdd = int(decimal.NewFromInt(topUp.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart())
		if quotaToAdd <= 0 {
			return errors.New("无效的充值额度")
		}

		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		if err := tx.Model(&User{}).Where("id = ?", topUp.UserId).Update("quota", gorm.Expr("quota + ?", quotaToAdd)).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		common.SysError("waffo pancake topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}

	if quotaToAdd > 0 {
		RecordLog(topUp.UserId, LogTypeTopup, fmt.Sprintf("Waffo Pancake充值成功，充值额度: %v，支付金额: %.2f", logger.FormatQuota(quotaToAdd), topUp.Money))
	}

	return nil
}

// EpayTopUpCompletion captures the result of an Epay top-up completion.
type EpayTopUpCompletion struct {
	// Completed is true only when this call transitioned the order from
	// pending to success. A duplicate success notify whose verified payload
	// still matches the stored method/money returns Completed=false with a nil
	// error so callers can ack success without re-processing; a duplicate whose
	// method or paid money differs returns the corresponding mismatch error.
	Completed     bool
	UserId        int
	QuotaToAdd    int
	PayMoney      float64
	PaymentMethod string
}

// CompleteEpayTopUp finalizes a pending Epay top-up inside a single DB
// transaction. It locks the topup row, validates provider/payment-method/paid
// money, then atomically transitions the order from pending to success via a
// conditional UPDATE (CAS) and increments user quota.
//
// The conditional UPDATE — not the row lock — is the authoritative concurrency
// guard: even when the row lock is a no-op (SQLite) or two instances race,
// exactly one completion's `UPDATE ... WHERE status=pending` matches and
// credits quota; the loser sees RowsAffected=0 and resolves as an idempotent
// duplicate. An already-success order returns Completed=false with a nil error
// when the verified callback still matches the stored method/money, or a
// mismatch error otherwise. The caller must ack "success" to the gateway only
// after this returns nil.
func CompleteEpayTopUp(tradeNo string, paymentMethod string, paidMoney string, callerIp string) (*EpayTopUpCompletion, error) {
	if tradeNo == "" {
		return nil, errors.New("未提供支付单号")
	}

	completion := &EpayTopUpCompletion{}
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return ErrTopUpNotFound
		}

		if topUp.PaymentProvider != PaymentProviderEpay {
			return ErrPaymentMethodMismatch
		}

		// Idempotent duplicate: an already-success order must not re-add quota,
		// flip status, or write a second log. The verified callback payload is
		// still cross-checked against the stored method/money so a mismatched
		// duplicate surfaces as an error rather than being silently swallowed.
		if topUp.Status == common.TopUpStatusSuccess {
			if err := validateEpayCallbackMatches(topUp, paymentMethod, paidMoney); err != nil {
				return err
			}
			completion.Completed = false
			completion.UserId = topUp.UserId
			completion.PaymentMethod = topUp.PaymentMethod
			completion.PayMoney = topUp.Money
			return nil
		}

		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}

		// Validate the verified callback payload against the stored order
		// before attempting the transition.
		if err := validateEpayCallbackMatches(topUp, paymentMethod, paidMoney); err != nil {
			return err
		}

		dAmount := decimal.NewFromInt(topUp.Amount)
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		quotaToAdd := int(dAmount.Mul(dQuotaPerUnit).IntPart())
		if quotaToAdd <= 0 {
			return errors.New("无效的充值额度")
		}

		// CAS transition: flip pending -> success only if the row is still
		// pending. This is the authoritative concurrency guard — the row lock is
		// best-effort. A concurrent winner leaves status=success, so this UPDATE
		// affects 0 rows and we fall through to duplicate/status handling below.
		result := tx.Model(&TopUp{}).Where("id = ? AND status = ?", topUp.Id, common.TopUpStatusPending).Updates(map[string]interface{}{
			"status":        common.TopUpStatusSuccess,
			"complete_time": common.GetTimestamp(),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			// Lost the race to a concurrent completion. Re-read to decide: an
			// already-success order is an idempotent duplicate (after re-checking
			// the callback); any other status is a status error.
			refreshed := &TopUp{}
			if err := tx.Where("id = ?", topUp.Id).First(refreshed).Error; err != nil {
				return ErrTopUpNotFound
			}
			if refreshed.Status == common.TopUpStatusSuccess {
				if err := validateEpayCallbackMatches(refreshed, paymentMethod, paidMoney); err != nil {
					return err
				}
				completion.Completed = false
				completion.UserId = refreshed.UserId
				completion.PaymentMethod = refreshed.PaymentMethod
				completion.PayMoney = refreshed.Money
				return nil
			}
			return ErrTopUpStatusInvalid
		}

		// Increment user quota in the same transaction; require exactly one row
		// affected so a missing user rolls back the whole completion (order
		// stays pending) instead of leaving an ack'd-but-uncredited order.
		quotaResult := tx.Model(&User{}).Where("id = ?", topUp.UserId).Update("quota", gorm.Expr("quota + ?", quotaToAdd))
		if quotaResult.Error != nil {
			return quotaResult.Error
		}
		if quotaResult.RowsAffected != 1 {
			return fmt.Errorf("user quota update affected %d rows, expected 1 (user_id=%d)", quotaResult.RowsAffected, topUp.UserId)
		}

		completion.Completed = true
		completion.UserId = topUp.UserId
		completion.QuotaToAdd = quotaToAdd
		completion.PayMoney = topUp.Money
		completion.PaymentMethod = topUp.PaymentMethod
		return nil
	})

	if err != nil {
		return nil, err
	}

	// Record log outside the transaction to avoid coupling log failures with the
	// completion commit; only on an actual pending->success transition.
	if completion.Completed {
		RecordTopupLog(completion.UserId, fmt.Sprintf("使用在线充值成功，充值金额: %v，支付金额：%f", logger.FormatQuota(completion.QuotaToAdd), completion.PayMoney), callerIp, completion.PaymentMethod, PaymentProviderEpay)
	}

	return completion, nil
}

// validateEpayCallbackMatches cross-checks the verified callback payload
// against the stored order: the payment method must match exactly (no silent
// rewrite) and the paid money must equal the stored money at cent precision
// (decimal equality tolerates float drift like "9.9900" == "9.99"). Used for
// both the pending transition and the already-success duplicate path so a
// mismatched duplicate is reported rather than silently ack'd.
func validateEpayCallbackMatches(topUp *TopUp, paymentMethod string, paidMoney string) error {
	if topUp.PaymentMethod != paymentMethod {
		return ErrPaymentMethodMismatch
	}
	topUpMoneyStr := strconv.FormatFloat(topUp.Money, 'f', 2, 64)
	expectedMoney, err := decimal.NewFromString(topUpMoneyStr)
	if err != nil {
		return fmt.Errorf("invalid topup money %q: %w", topUpMoneyStr, err)
	}
	actualMoney, err := decimal.NewFromString(paidMoney)
	if err != nil {
		return fmt.Errorf("invalid paid money %q: %w", paidMoney, err)
	}
	if !actualMoney.Equal(expectedMoney) {
		return ErrPaidMoneyMismatch
	}
	return nil
}

// StripeTopUpCompletion captures the result of a Stripe wallet topup
// completion. Completed is true only when this call transitioned the order
// from pending to success; a duplicate success notify whose verified payload
// still matches the stored snapshot returns Completed=false with a nil error
// so the webhook can ack 200 without re-processing, while a duplicate whose
// amount/customer/currency differs returns the corresponding mismatch error.
type StripeTopUpCompletion struct {
	Completed     bool
	UserId        int
	QuotaToAdd    int
	PayMoney      float64
	PaymentMethod string
	CustomerID    string
}

// CompleteStripeTopUp finalizes a pending Stripe wallet topup inside a single
// DB transaction. It locks the topup row, then validates the verified webhook
// payload against the stored snapshot — provider, checkout status, payment
// status, mode, currency, amount_total cents, and customer — before
// atomically transitioning pending -> success via a conditional UPDATE (CAS)
// and incrementing user quota / setting stripe_customer in the same
// transaction.
//
// The conditional UPDATE — not the row lock — is the authoritative concurrency
// guard: exactly one completion's `UPDATE ... WHERE status=pending` matches and
// credits quota; the loser sees RowsAffected=0 and resolves as an idempotent
// duplicate. An already-success order returns Completed=false with a nil error
// when the verified payload still matches, or a mismatch error otherwise. The
// caller must ack 200 to Stripe only after this returns nil.
//
// Quota to add is `TopUp.Amount * common.QuotaPerUnit` (Amount is the
// normalized recharge unit captured at creation, NOT Money). Money is the
// expected paid money snapshot in USD and is only used for the amount_total
// cents check.
func CompleteStripeTopUp(tradeNo string, customerID string, amountTotal string, currency string, checkoutStatus string, paymentStatus string, mode string, callerIp string) (*StripeTopUpCompletion, error) {
	if tradeNo == "" {
		return nil, errors.New("未提供支付单号")
	}

	completion := &StripeTopUpCompletion{}
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return ErrTopUpNotFound
		}

		if topUp.PaymentProvider != PaymentProviderStripe {
			return ErrPaymentMethodMismatch
		}

		// Read the user best-effort for the customer-match check. Used by the
		// already-success duplicate and pending transition validation paths; a
		// missing user on the pending path is caught later by the quota-update
		// RowsAffected==1 guard (order stays pending).
		storedUser, userFound := loadStripeUserForTx(tx, topUp.UserId)

		// Idempotent duplicate: an already-success order must not re-add quota,
		// flip status, or write a second log. The verified payload is still
		// cross-checked against the stored snapshot so a mismatched duplicate
		// surfaces as an error rather than being silently swallowed.
		if topUp.Status == common.TopUpStatusSuccess {
			if err := validateStripeCallbackMatches(topUp, storedUser, customerID, amountTotal, currency, checkoutStatus, paymentStatus, mode); err != nil {
				return err
			}
			completion.Completed = false
			completion.UserId = topUp.UserId
			completion.PaymentMethod = topUp.PaymentMethod
			completion.PayMoney = topUp.Money
			completion.CustomerID = strings.TrimSpace(customerID)
			return nil
		}

		// Non-pending (failed/expired) orders cannot be completed; do not
		// validate the payload (mirrors CompleteEpayTopUp) — the status itself
		// is the rejection reason.
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}

		// Pending transition: validate the verified payload against the stored
		// snapshot before attempting the CAS.
		if err := validateStripeCallbackMatches(topUp, storedUser, customerID, amountTotal, currency, checkoutStatus, paymentStatus, mode); err != nil {
			return err
		}

		dAmount := decimal.NewFromInt(topUp.Amount)
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		quotaToAdd := int(dAmount.Mul(dQuotaPerUnit).IntPart())
		if quotaToAdd <= 0 {
			return errors.New("无效的充值额度")
		}

		// CAS transition: flip pending -> success only if the row is still
		// pending. This is the authoritative concurrency guard — the row lock
		// is best-effort (no-op on SQLite). A concurrent winner leaves
		// status=success, so this UPDATE affects 0 rows and we fall through to
		// duplicate handling below.
		result := tx.Model(&TopUp{}).Where("id = ? AND status = ?", topUp.Id, common.TopUpStatusPending).Updates(map[string]interface{}{
			"status":        common.TopUpStatusSuccess,
			"complete_time": common.GetTimestamp(),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			// Lost the race to a concurrent completion. Re-read to decide: an
			// already-success order is an idempotent duplicate (after re-checking
			// the payload); any other status is a status error.
			refreshed := &TopUp{}
			if err := tx.Where("id = ?", topUp.Id).First(refreshed).Error; err != nil {
				return ErrTopUpNotFound
			}
			if refreshed.Status == common.TopUpStatusSuccess {
				// The winner may have just set stripe_customer; re-read the user
				// so the customer-match check reflects the committed state.
				refreshedUser, _ := loadStripeUserForTx(tx, refreshed.UserId)
				if err := validateStripeCallbackMatches(refreshed, refreshedUser, customerID, amountTotal, currency, checkoutStatus, paymentStatus, mode); err != nil {
					return err
				}
				completion.Completed = false
				completion.UserId = refreshed.UserId
				completion.PaymentMethod = refreshed.PaymentMethod
				completion.PayMoney = refreshed.Money
				completion.CustomerID = strings.TrimSpace(customerID)
				return nil
			}
			return ErrTopUpStatusInvalid
		}

		// Increment user quota and set stripe_customer in the same
		// transaction; require exactly one row affected so a missing user
		// rolls back the whole completion (order stays pending) instead of
		// leaving an ack'd-but-uncredited order. stripe_customer is only set
		// when the user does not already have one — validateStripeCallbackMatches
		// already rejected a mismatch against an existing StripeCustomer.
		updateFields := map[string]interface{}{
			"quota": gorm.Expr("quota + ?", quotaToAdd),
		}
		if userFound && strings.TrimSpace(storedUser.StripeCustomer) == "" {
			updateFields["stripe_customer"] = strings.TrimSpace(customerID)
		}
		userResult := tx.Model(&User{}).Where("id = ?", topUp.UserId).Updates(updateFields)
		if userResult.Error != nil {
			return userResult.Error
		}
		if userResult.RowsAffected != 1 {
			return fmt.Errorf("user quota update affected %d rows, expected 1 (user_id=%d)", userResult.RowsAffected, topUp.UserId)
		}

		completion.Completed = true
		completion.UserId = topUp.UserId
		completion.QuotaToAdd = quotaToAdd
		completion.PayMoney = topUp.Money
		completion.PaymentMethod = topUp.PaymentMethod
		completion.CustomerID = strings.TrimSpace(customerID)
		return nil
	})

	if err != nil {
		return nil, err
	}

	// Record log outside the transaction to avoid coupling log failures with
	// the completion commit; only on an actual pending->success transition.
	if completion.Completed {
		RecordTopupLog(completion.UserId, fmt.Sprintf("使用在线充值成功，充值金额: %v，支付金额：%f", logger.FormatQuota(completion.QuotaToAdd), completion.PayMoney), callerIp, completion.PaymentMethod, PaymentProviderStripe)
	}

	return completion, nil
}

// loadStripeUserForTx reads the user row inside the completion transaction so
// the customer-match check reflects the in-flight state. Returns (nil, false)
// when the user is missing (or any read error) instead of erroring, so the
// caller can route the missing-user case through the RowsAffected==1
// quota-update guard.
func loadStripeUserForTx(tx *gorm.DB, userID int) (*User, bool) {
	if tx == nil || userID == 0 {
		return nil, false
	}
	user := &User{}
	if err := tx.Where("id = ?", userID).First(user).Error; err != nil {
		return nil, false
	}
	return user, true
}

// validateStripeCallbackMatches cross-checks the verified webhook payload
// against the stored snapshot. Used for both the pending transition and the
// already-success duplicate path so a mismatched duplicate is reported rather
// than silently ack'd. Checks:
//   - checkout status == "complete"
//   - payment status == "paid"
//   - mode == "payment" (topup branch; empty mode is rejected)
//   - currency == "USD" (uppercase trim; no multi-currency schema)
//   - webhook customer not empty, and if the user already has a StripeCustomer
//     it must equal the webhook customer
//   - amount_total (Stripe minor units) == TopUp.Money rounded to cents
//
// Paid-money mismatch surfaces as ErrPaidMoneyMismatch (same sentinel as Epay);
// currency and customer mismatches surface as the Stripe-specific sentinels so
// callers and tests can tell them apart.
func validateStripeCallbackMatches(topUp *TopUp, user *User, customerID string, amountTotal string, currency string, checkoutStatus string, paymentStatus string, mode string) error {
	if checkoutStatus != "complete" {
		return fmt.Errorf("stripe checkout status %q is not complete", checkoutStatus)
	}
	if paymentStatus != "paid" {
		return fmt.Errorf("stripe payment status %q is not paid", paymentStatus)
	}
	if mode != "payment" {
		return fmt.Errorf("stripe checkout mode %q is not payment", mode)
	}
	cur := strings.ToUpper(strings.TrimSpace(currency))
	if cur != "USD" {
		return ErrStripeCurrencyMismatch
	}
	cid := strings.TrimSpace(customerID)
	if cid == "" {
		return ErrStripeCustomerMismatch
	}
	if user != nil {
		stored := strings.TrimSpace(user.StripeCustomer)
		if stored != "" && stored != cid {
			return ErrStripeCustomerMismatch
		}
	}
	// amount_total is Stripe minor units (cents). The snapshot TopUp.Money is
	// the expected paid money in USD, so cents = round(Money * 100). Decimal
	// equality tolerates float drift; a mismatched amount surfaces as
	// ErrPaidMoneyMismatch so the order stays pending for reconciliation.
	expectedCents := decimal.NewFromFloat(topUp.Money).Mul(decimal.NewFromInt(100)).Round(0)
	actualCents, err := decimal.NewFromString(amountTotal)
	if err != nil {
		return fmt.Errorf("invalid amount_total %q: %w", amountTotal, err)
	}
	if !actualCents.Equal(expectedCents) {
		return ErrPaidMoneyMismatch
	}
	return nil
}
