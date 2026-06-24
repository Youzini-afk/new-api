package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withCheckinSetting 暂存并替换全局 CheckinSetting。
func withCheckinSetting(t *testing.T, mutate func(s *operation_setting.CheckinSetting)) {
	t.Helper()
	orig := *operation_setting.GetCheckinSetting()
	mutate(operation_setting.GetCheckinSetting())
	t.Cleanup(func() {
		*operation_setting.GetCheckinSetting() = orig
	})
}

func insertCheckinTestUser(t *testing.T, id int, quota int) *User {
	t.Helper()
	u := &User{
		Id:       id,
		Username: "checkin_user_" + common.GetRandomString(6),
		Password: "password1234",
		Quota:    quota,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  common.GetRandomString(8),
	}
	require.NoError(t, DB.Create(u).Error)
	return u
}

// TestUserCheckin_MaxUserQuota_BlocksRichUser: 用户当前额度已达上限时不可签到。
func TestUserCheckin_MaxUserQuota_BlocksRichUser(t *testing.T) {
	truncateTables(t)
	withCheckinSetting(t, func(s *operation_setting.CheckinSetting) {
		s.Enabled = true
		s.MinQuota = 1000
		s.MaxQuota = 1000
		s.MaxUserQuota = 50000
	})

	const userID = 2101
	insertCheckinTestUser(t, userID, 50000) // 等于阈值，应被拒绝

	_, err := UserCheckin(userID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "上限")

	// 不应创建签到记录
	has, err := HasCheckedInToday(userID)
	require.NoError(t, err)
	assert.False(t, has)

	// 额度未变
	var u User
	require.NoError(t, DB.Select("quota").Where("id = ?", userID).First(&u).Error)
	assert.Equal(t, 50000, u.Quota)
}

// TestUserCheckin_MaxUserQuota_AllowsBelowThreshold: 用户额度低于上限时可签到并扣奖励。
func TestUserCheckin_MaxUserQuota_AllowsBelowThreshold(t *testing.T) {
	truncateTables(t)
	withCheckinSetting(t, func(s *operation_setting.CheckinSetting) {
		s.Enabled = true
		s.MinQuota = 1000
		s.MaxQuota = 1000
		s.MaxUserQuota = 50000
	})

	const userID = 2102
	insertCheckinTestUser(t, userID, 49000) // 低于阈值

	checkin, err := UserCheckin(userID)
	require.NoError(t, err)
	require.NotNil(t, checkin)
	assert.Equal(t, 1000, checkin.QuotaAwarded)

	// 额度增加
	var u User
	require.NoError(t, DB.Select("quota").Where("id = ?", userID).First(&u).Error)
	assert.Equal(t, 49000+1000, u.Quota)
}

// TestUserCheckin_MaxUserQuota_Zero_DisablesLimit: MaxUserQuota=0 表示不限制。
func TestUserCheckin_MaxUserQuota_Zero_DisablesLimit(t *testing.T) {
	truncateTables(t)
	withCheckinSetting(t, func(s *operation_setting.CheckinSetting) {
		s.Enabled = true
		s.MinQuota = 500
		s.MaxQuota = 500
		s.MaxUserQuota = 0 // 不限制
	})

	const userID = 2103
	insertCheckinTestUser(t, userID, 1_000_000) // 额度很高，但仍应可签到

	checkin, err := UserCheckin(userID)
	require.NoError(t, err)
	assert.Equal(t, 500, checkin.QuotaAwarded)

	var u User
	require.NoError(t, DB.Select("quota").Where("id = ?", userID).First(&u).Error)
	assert.Equal(t, 1_000_000+500, u.Quota)
}

// TestUserCheckin_FixedReward_MinEqualsMax: min==max 时奖励固定，不应使用 rand.Intn。
func TestUserCheckin_FixedReward_MinEqualsMax(t *testing.T) {
	truncateTables(t)
	withCheckinSetting(t, func(s *operation_setting.CheckinSetting) {
		s.Enabled = true
		s.MinQuota = 2500
		s.MaxQuota = 2500 // min==max
		s.MaxUserQuota = 0
	})

	const userID = 2104
	insertCheckinTestUser(t, userID, 1000)

	checkin, err := UserCheckin(userID)
	require.NoError(t, err)
	assert.Equal(t, 2500, checkin.QuotaAwarded)

	// 多次签到场景不能复现（同日去重），但可断言奖励始终等于 min==max 的固定值
	var u User
	require.NoError(t, DB.Select("quota").Where("id = ?", userID).First(&u).Error)
	assert.Equal(t, 1000+2500, u.Quota)
}

// TestUserCheckin_DuplicateSameDay_NoDoubleAward: 同日重复签到应被唯一约束/检查拦截，不重复加额度。
func TestUserCheckin_DuplicateSameDay_NoDoubleAward(t *testing.T) {
	truncateTables(t)
	withCheckinSetting(t, func(s *operation_setting.CheckinSetting) {
		s.Enabled = true
		s.MinQuota = 1000
		s.MaxQuota = 1000
		s.MaxUserQuota = 0
	})

	const userID = 2105
	insertCheckinTestUser(t, userID, 0)

	// 第一次签到成功
	_, err := UserCheckin(userID)
	require.NoError(t, err)

	// 第二次同日签到应失败
	_, err = UserCheckin(userID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "已签到")

	// 额度仅增加一次
	var u User
	require.NoError(t, DB.Select("quota").Where("id = ?", userID).First(&u).Error)
	assert.Equal(t, 1000, u.Quota)

	// 仅一条签到记录
	var checkinCount int64
	require.NoError(t, DB.Model(&Checkin{}).Where("user_id = ?", userID).Count(&checkinCount).Error)
	assert.Equal(t, int64(1), checkinCount)
}

// TestUserCheckin_RandomReward_WithinRange: min < max 时奖励在 [MinQuota, MaxQuota] 范围内。
func TestUserCheckin_RandomReward_WithinRange(t *testing.T) {
	truncateTables(t)
	withCheckinSetting(t, func(s *operation_setting.CheckinSetting) {
		s.Enabled = true
		s.MinQuota = 100
		s.MaxQuota = 500
		s.MaxUserQuota = 0
	})

	const userID = 2106
	insertCheckinTestUser(t, userID, 0)

	checkin, err := UserCheckin(userID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, checkin.QuotaAwarded, 100)
	assert.LessOrEqual(t, checkin.QuotaAwarded, 500)
}

// TestGetUserCheckinStats_ExposesMaxUserQuotaAndEligible: stats 应暴露 max_user_quota / eligible / ineligible_reason。
func TestGetUserCheckinStats_ExposesMaxUserQuotaAndEligible(t *testing.T) {
	truncateTables(t)
	withCheckinSetting(t, func(s *operation_setting.CheckinSetting) {
		s.Enabled = true
		s.MinQuota = 1000
		s.MaxQuota = 1000
		s.MaxUserQuota = 50000
	})

	const userID = 2107
	insertCheckinTestUser(t, userID, 60000) // 已超阈值

	stats, err := GetUserCheckinStats(userID, "2099-01")
	require.NoError(t, err)
	require.NotNil(t, stats)

	assert.Equal(t, 50000, stats["max_user_quota"])
	assert.Equal(t, false, stats["eligible"])
	assert.NotEmpty(t, stats["ineligible_reason"])
	assert.Equal(t, 60000, stats["current_quota"])

	// 用户在阈值以下时 eligible=true
	const userID2 = 2108
	insertCheckinTestUser(t, userID2, 1000)
	stats2, err := GetUserCheckinStats(userID2, "2099-01")
	require.NoError(t, err)
	require.NotNil(t, stats2)
	assert.Equal(t, true, stats2["eligible"])
	assert.Empty(t, stats2["ineligible_reason"])
}

// TestUserCheckin_Disabled: checkin 关闭时返回错误。
func TestUserCheckin_Disabled(t *testing.T) {
	truncateTables(t)
	withCheckinSetting(t, func(s *operation_setting.CheckinSetting) {
		s.Enabled = false
	})

	const userID = 2109
	insertCheckinTestUser(t, userID, 0)

	_, err := UserCheckin(userID)
	require.Error(t, err)
}

// TestUserCheckin_MaxUserQuota_BoundaryMinusOne_AllowsAndMayExceedAfter:
// 用户额度恰好低于上限 1 时可签到，且领奖后额度可超过阈值（业务语义：领奖前 < MaxUserQuota 即可）。
// 该 case 同时验证条件更新 `WHERE quota < max_user_quota` 的边界（quota == MaxUserQuota-1 通过）。
func TestUserCheckin_MaxUserQuota_BoundaryMinusOne_AllowsAndMayExceedAfter(t *testing.T) {
	truncateTables(t)
	withCheckinSetting(t, func(s *operation_setting.CheckinSetting) {
		s.Enabled = true
		s.MinQuota = 1000
		s.MaxQuota = 1000
		s.MaxUserQuota = 50000
	})

	const userID = 2110
	insertCheckinTestUser(t, userID, 49999) // 恰好低于阈值 1

	checkin, err := UserCheckin(userID)
	require.NoError(t, err)
	assert.Equal(t, 1000, checkin.QuotaAwarded)

	// 领奖后额度 50999 > 50000，超过阈值，符合"领奖后允许超过"语义。
	var u User
	require.NoError(t, DB.Select("quota").Where("id = ?", userID).First(&u).Error)
	assert.Equal(t, 49999+1000, u.Quota)
	assert.Greater(t, u.Quota, 50000, "post-award quota may exceed MaxUserQuota per spec")
}

// TestUserCheckin_MaxUserQuota_AtThreshold_Rejected:
// 用户额度恰好等于 MaxUserQuota 时被拒（条件为 `quota < max_user_quota`，等于不通过）。
func TestUserCheckin_MaxUserQuota_AtThreshold_Rejected(t *testing.T) {
	truncateTables(t)
	withCheckinSetting(t, func(s *operation_setting.CheckinSetting) {
		s.Enabled = true
		s.MinQuota = 1000
		s.MaxQuota = 1000
		s.MaxUserQuota = 50000
	})

	const userID = 2111
	insertCheckinTestUser(t, userID, 50000) // 恰好等于阈值

	_, err := UserCheckin(userID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "上限")

	// 签到记录应被 rollback（事务回滚）。
	has, hasErr := HasCheckedInToday(userID)
	require.NoError(t, hasErr)
	assert.False(t, has, "checkin record must be rolled back on quota rejection")

	// 额度不变。
	var u User
	require.NoError(t, DB.Select("quota").Where("id = ?", userID).First(&u).Error)
	assert.Equal(t, 50000, u.Quota)
}
