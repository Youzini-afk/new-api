package model

import (
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
)

// Checkin 签到记录
type Checkin struct {
	Id           int    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId       int    `json:"user_id" gorm:"not null;uniqueIndex:idx_user_checkin_date"`
	CheckinDate  string `json:"checkin_date" gorm:"type:varchar(10);not null;uniqueIndex:idx_user_checkin_date"` // 格式: YYYY-MM-DD
	QuotaAwarded int    `json:"quota_awarded" gorm:"not null"`
	CreatedAt    int64  `json:"created_at" gorm:"bigint"`
}

// CheckinRecord 用于API返回的签到记录（不包含敏感字段）
type CheckinRecord struct {
	CheckinDate  string `json:"checkin_date"`
	QuotaAwarded int    `json:"quota_awarded"`
}

func (Checkin) TableName() string {
	return "checkins"
}

// GetUserCheckinRecords 获取用户在指定日期范围内的签到记录
func GetUserCheckinRecords(userId int, startDate, endDate string) ([]Checkin, error) {
	var records []Checkin
	err := DB.Where("user_id = ? AND checkin_date >= ? AND checkin_date <= ?",
		userId, startDate, endDate).
		Order("checkin_date DESC").
		Find(&records).Error
	return records, err
}

// HasCheckedInToday 检查用户今天是否已签到
func HasCheckedInToday(userId int) (bool, error) {
	today := time.Now().Format("2006-01-02")
	var count int64
	err := DB.Model(&Checkin{}).
		Where("user_id = ? AND checkin_date = ?", userId, today).
		Count(&count).Error
	return count > 0, err
}

// UserCheckin 执行用户签到。
// 统一 SQLite/MySQL/PostgreSQL 事务路径：
//   - 唯一约束 (user_id, checkin_date) 防止重复签到；
//   - quota 增加使用条件原子更新 `WHERE id=? AND quota < max_user_quota`（MaxUserQuota>0 时）
//     并检查 RowsAffected，消除事务外预检的 TOCTOU 竞态；
//   - 失败时 rollback 已创建的 checkin 记录并返回清晰错误。
//
// 业务语义：当前 quota < MaxUserQuota 即可领奖（允许领奖后超过阈值）。
func UserCheckin(userId int) (*Checkin, error) {
	setting := operation_setting.GetCheckinSetting()
	if !setting.Enabled {
		return nil, errors.New("签到功能未启用")
	}

	// 检查今天是否已签到（提前快速返回；并发重复由唯一约束兜底）
	hasChecked, err := HasCheckedInToday(userId)
	if err != nil {
		return nil, err
	}
	if hasChecked {
		return nil, errors.New("今日已签到")
	}

	// 计算随机额度奖励
	quotaAwarded := setting.MinQuota
	if setting.MaxQuota > setting.MinQuota {
		quotaAwarded = setting.MinQuota + rand.Intn(setting.MaxQuota-setting.MinQuota+1)
	}

	today := time.Now().Format("2006-01-02")
	checkin := &Checkin{
		UserId:       userId,
		CheckinDate:  today,
		QuotaAwarded: quotaAwarded,
		CreatedAt:    time.Now().Unix(),
	}

	// 统一事务路径：SQLite/MySQL/PostgreSQL 均使用事务保证原子性。
	// quota 更新带条件 RowsAffected 校验，避免先查后扣的 TOCTOU 竞态。
	err = DB.Transaction(func(tx *gorm.DB) error {
		// 步骤1: 创建签到记录。
		// 数据库唯一约束 (user_id, checkin_date) 防止并发重复签到。
		if err := tx.Create(checkin).Error; err != nil {
			return errors.New("签到失败，请稍后重试")
		}

		// 步骤2: 原子条件增加用户额度。
		// MaxUserQuota > 0 时附加 `AND quota < max_user_quota` 条件；
		// RowsAffected != 1 说明用户不存在或已达额度上限，rollback 签到记录。
		cond := "id = ?"
		args := []interface{}{userId}
		if setting.MaxUserQuota > 0 {
			cond += " AND quota < ?"
			args = append(args, setting.MaxUserQuota)
		}
		res := tx.Model(&User{}).Where(cond, args...).
			Update("quota", gorm.Expr("quota + ?", quotaAwarded))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			if setting.MaxUserQuota > 0 {
				return fmt.Errorf("当前额度已达签到上限 %d，请消耗后再签到", setting.MaxUserQuota)
			}
			return errors.New("签到失败：用户不存在或更新额度出错")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// 事务成功后，异步刷新缓存
	go func() {
		_ = cacheIncrUserQuota(userId, int64(quotaAwarded))
	}()

	return checkin, nil
}

// getUserQuotaForCheckin 读取用户当前额度（用于签到风控判定展示，非原子操作）。
func getUserQuotaForCheckin(userId int) (int, error) {
	var user User
	if err := DB.Select("quota").Where("id = ?", userId).First(&user).Error; err != nil {
		return 0, err
	}
	return user.Quota, nil
}

// GetUserCheckinStats 获取用户签到统计信息
func GetUserCheckinStats(userId int, month string) (map[string]interface{}, error) {
	// 获取指定月份的所有签到记录
	startDate := month + "-01"
	endDate := month + "-31"

	records, err := GetUserCheckinRecords(userId, startDate, endDate)
	if err != nil {
		return nil, err
	}

	// 转换为不包含敏感字段的记录
	checkinRecords := make([]CheckinRecord, len(records))
	for i, r := range records {
		checkinRecords[i] = CheckinRecord{
			CheckinDate:  r.CheckinDate,
			QuotaAwarded: r.QuotaAwarded,
		}
	}

	// 检查今天是否已签到
	hasCheckedToday, _ := HasCheckedInToday(userId)

	// 获取用户所有时间的签到统计
	var totalCheckins int64
	var totalQuota int64
	DB.Model(&Checkin{}).Where("user_id = ?", userId).Count(&totalCheckins)
	DB.Model(&Checkin{}).Where("user_id = ?", userId).Select("COALESCE(SUM(quota_awarded), 0)").Scan(&totalQuota)

	// 计算额度风控资格
	setting := operation_setting.GetCheckinSetting()
	maxUserQuota := setting.MaxUserQuota
	eligible := false
	ineligibleReason := ""
	currentQuota := 0
	if maxUserQuota > 0 {
		currentQuota, err = getUserQuotaForCheckin(userId)
		if err != nil {
			// 优先返回 quota 读取错误，不吞掉。
			return nil, err
		}
		if currentQuota >= maxUserQuota {
			eligible = false
			ineligibleReason = fmt.Sprintf("当前额度已达签到上限 %d", maxUserQuota)
		} else {
			eligible = true
		}
	} else {
		// 未配置上限时，符合资格
		eligible = true
	}
	// 今日已签到的用户视为今日不再可签到
	if hasCheckedToday {
		eligible = false
		if ineligibleReason == "" {
			ineligibleReason = "今日已签到"
		}
	}

	return map[string]interface{}{
		"total_quota":       totalQuota,       // 所有时间累计获得的额度
		"total_checkins":    totalCheckins,    // 所有时间累计签到次数
		"checkin_count":     len(records),     // 本月签到次数
		"checked_in_today":  hasCheckedToday,  // 今天是否已签到
		"records":           checkinRecords,   // 本月签到记录详情（不含id和user_id）
		"max_user_quota":    maxUserQuota,     // 签到额度上限（0 表示不限制）
		"current_quota":     currentQuota,     // 用户当前额度
		"eligible":          eligible,         // 当前是否可签到
		"ineligible_reason": ineligibleReason, // 不可签到原因（eligible=true 时为空）
	}, nil
}
