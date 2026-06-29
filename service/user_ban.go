package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

// BanUserAndDisableTokens bans a user and disables all their API tokens in a
// single DB transaction so a token-disable failure rolls back the user ban
// (no half-banned state). Admin/root users (role >= RoleAdminUser) are
// protected and rejected. Cache invalidation + RecordLog happen AFTER the
// transaction commits. No ban_sync / external bot integration.
func BanUserAndDisableTokens(user *model.User, reason string) error {
	if user == nil || user.Id <= 0 {
		return fmt.Errorf("invalid user")
	}
	if user.Role >= common.RoleAdminUser {
		return fmt.Errorf("cannot ban admin/root user")
	}

	user.Status = common.UserStatusDisabled
	if trimmedReason := strings.TrimSpace(reason); trimmedReason != "" {
		current := strings.TrimSpace(user.Remark)
		if current == "" {
			user.Remark = trimmedReason
		} else {
			exists := false
			for _, item := range strings.Split(current, "\n") {
				if strings.TrimSpace(item) == trimmedReason {
					exists = true
					break
				}
			}
			if exists {
				user.Remark = current
			} else {
				user.Remark = current + "\n" + trimmedReason
			}
		}
		if len([]rune(user.Remark)) > 255 {
			user.Remark = string([]rune(user.Remark)[:255])
		}
	}

	// Atomically ban the user + disable all tokens in one transaction. A
	// token-disable failure rolls back the user.Status update so we never leave
	// a user disabled with active tokens.
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		var current model.User
		if err := tx.Select("id", "role").First(&current, user.Id).Error; err != nil {
			return err
		}
		if current.Role >= common.RoleAdminUser {
			return fmt.Errorf("cannot ban admin/root user")
		}
		userUpdate := tx.Model(&model.User{}).Where("id = ? AND role < ?", user.Id, common.RoleAdminUser).
			Updates(map[string]interface{}{
				"status": common.UserStatusDisabled,
				"remark": user.Remark,
			})
		if userUpdate.Error != nil {
			return userUpdate.Error
		}
		if userUpdate.RowsAffected == 0 {
			if err := ensureUserStillNonAdmin(tx, user.Id, "cannot ban admin/root user"); err != nil {
				return err
			}
		}
		if err := tx.Model(&model.Token{}).
			Where("user_id = ?", user.Id).
			Update("status", common.TokenStatusDisabled).Error; err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}

	// Cache invalidation + manage log happen only after the transaction commits.
	if err := model.InvalidateUserCache(user.Id); err != nil {
		common.SysLog("ban user cache invalidate failed: " + err.Error())
	}
	if err := model.InvalidateUserTokensCache(user.Id); err != nil {
		common.SysLog("ban user token cache invalidate failed: " + err.Error())
	}
	return nil
}

// BanUserForDiscordPatrolAndDisableTokens atomically records the Discord patrol
// ban decision, disables the user, and revokes all API tokens. Admin/root users
// are protected by a transaction-local role read before token revocation.
func BanUserForDiscordPatrolAndDisableTokens(user *model.User, evaluatedDiscordID, evaluatedEncryptedRefreshToken, banReason, gateReason, gateMessage string, checkedAt int64) error {
	if user == nil || user.Id <= 0 {
		return fmt.Errorf("invalid user")
	}
	evaluatedDiscordID = strings.TrimSpace(evaluatedDiscordID)
	if evaluatedDiscordID == "" {
		return fmt.Errorf("discord id is required")
	}
	evaluatedEncryptedRefreshToken = strings.TrimSpace(evaluatedEncryptedRefreshToken)
	if evaluatedEncryptedRefreshToken == "" {
		return fmt.Errorf("discord refresh token is required")
	}
	if user.Role >= common.RoleAdminUser {
		return fmt.Errorf("cannot ban admin/root user")
	}
	if checkedAt <= 0 {
		checkedAt = common.GetTimestamp()
	}
	banReason = strings.TrimSpace(banReason)

	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		var current model.User
		if err := tx.Select("id", "role", "status", "remark", "discord_gate_exempt", "discord_id", "discord_refresh_token").First(&current, user.Id).Error; err != nil {
			return err
		}
		if current.Role >= common.RoleAdminUser {
			return fmt.Errorf("cannot ban admin/root user")
		}
		if current.Status != common.UserStatusEnabled {
			return fmt.Errorf("cannot ban disabled user")
		}
		if current.DiscordGateExempt {
			return fmt.Errorf("cannot ban discord gate exempt user")
		}
		if strings.TrimSpace(current.DiscordId) != evaluatedDiscordID {
			return fmt.Errorf("discord binding changed")
		}
		if strings.TrimSpace(current.DiscordRefreshToken) != evaluatedEncryptedRefreshToken {
			return fmt.Errorf("discord refresh token changed")
		}
		remark := strings.TrimSpace(current.Remark)
		if banReason != "" {
			if remark == "" {
				remark = banReason
			} else {
				exists := false
				for _, item := range strings.Split(remark, "\n") {
					if strings.TrimSpace(item) == banReason {
						exists = true
						break
					}
				}
				if !exists {
					remark += "\n" + banReason
				}
			}
		}
		if len([]rune(remark)) > 255 {
			remark = string([]rune(remark)[:255])
		}
		userUpdate := tx.Model(&model.User{}).Where("id = ? AND status = ? AND role < ? AND (discord_gate_exempt IS NULL OR discord_gate_exempt = ?) AND discord_id = ? AND discord_refresh_token = ?", user.Id, common.UserStatusEnabled, common.RoleAdminUser, false, evaluatedDiscordID, evaluatedEncryptedRefreshToken).Updates(map[string]interface{}{
			"status":                     common.UserStatusDisabled,
			"remark":                     remark,
			"discord_gate_passed":        false,
			"discord_last_check_at":      checkedAt,
			"discord_last_check_result":  "ban_matched",
			"discord_last_check_reason":  gateReason,
			"discord_gate_message":       gateMessage,
			"discord_patrol_retry_at":    0,
			"discord_patrol_retry_count": 0,
			"discord_patrol_last_error":  "",
		})
		if userUpdate.Error != nil {
			return userUpdate.Error
		}
		if userUpdate.RowsAffected == 0 {
			return fmt.Errorf("cannot ban user")
		}
		if err := tx.Model(&model.Token{}).Where("user_id = ?", user.Id).Update("status", common.TokenStatusDisabled).Error; err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}

	if err := model.InvalidateUserCache(user.Id); err != nil {
		common.SysLog("discord patrol ban cache invalidate failed: " + err.Error())
	}
	if err := model.InvalidateUserTokensCache(user.Id); err != nil {
		common.SysLog("discord patrol ban token cache invalidate failed: " + err.Error())
	}
	return nil
}

// BanUserForDiscordBanPatrolAndDisableTokens atomically records a confirmed
// ban-only Discord patrol hit, disables the user, and revokes all API tokens.
// The transaction rechecks that the user is still enabled, non-admin, not
// Discord-gate exempt, still bound to the evaluated Discord account, and still
// has a refresh token before applying the ban.
func BanUserForDiscordBanPatrolAndDisableTokens(user *model.User, evaluatedDiscordID, evaluatedEncryptedRefreshToken, banReason, patrolReason string, checkedAt int64) error {
	if user == nil || user.Id <= 0 {
		return fmt.Errorf("invalid user")
	}
	evaluatedDiscordID = strings.TrimSpace(evaluatedDiscordID)
	if evaluatedDiscordID == "" {
		return fmt.Errorf("discord id is required")
	}
	evaluatedEncryptedRefreshToken = strings.TrimSpace(evaluatedEncryptedRefreshToken)
	if evaluatedEncryptedRefreshToken == "" {
		return fmt.Errorf("discord refresh token is required")
	}
	if user.Role >= common.RoleAdminUser {
		return fmt.Errorf("cannot ban admin/root user")
	}
	if checkedAt <= 0 {
		checkedAt = common.GetTimestamp()
	}
	banReason = strings.TrimSpace(banReason)

	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		var current model.User
		if err := tx.Select("id", "role", "status", "remark", "discord_gate_exempt", "discord_id", "discord_refresh_token").First(&current, user.Id).Error; err != nil {
			return err
		}
		if current.Role >= common.RoleAdminUser {
			return fmt.Errorf("cannot ban admin/root user")
		}
		if current.Status != common.UserStatusEnabled {
			return fmt.Errorf("cannot ban disabled user")
		}
		if current.DiscordGateExempt {
			return fmt.Errorf("cannot ban discord gate exempt user")
		}
		if strings.TrimSpace(current.DiscordId) != evaluatedDiscordID {
			return fmt.Errorf("discord binding changed")
		}
		if strings.TrimSpace(current.DiscordRefreshToken) != evaluatedEncryptedRefreshToken {
			return fmt.Errorf("discord refresh token changed")
		}
		remark := strings.TrimSpace(current.Remark)
		if banReason != "" {
			if remark == "" {
				remark = banReason
			} else {
				exists := false
				for _, item := range strings.Split(remark, "\n") {
					if strings.TrimSpace(item) == banReason {
						exists = true
						break
					}
				}
				if !exists {
					remark += "\n" + banReason
				}
			}
		}
		if len([]rune(remark)) > 255 {
			remark = string([]rune(remark)[:255])
		}
		userUpdate := tx.Model(&model.User{}).
			Where("id = ? AND status = ? AND role < ? AND (discord_gate_exempt IS NULL OR discord_gate_exempt = ?) AND discord_id = ? AND discord_refresh_token = ?", user.Id, common.UserStatusEnabled, common.RoleAdminUser, false, evaluatedDiscordID, evaluatedEncryptedRefreshToken).
			Updates(map[string]interface{}{
				"status":                               common.UserStatusDisabled,
				"remark":                               remark,
				"discord_gate_passed":                  false,
				"discord_ban_patrol_last_check_at":     checkedAt,
				"discord_ban_patrol_last_check_result": "ban_matched",
				"discord_ban_patrol_last_check_reason": patrolReason,
				"discord_ban_patrol_retry_at":          0,
				"discord_ban_patrol_retry_count":       0,
				"discord_ban_patrol_last_error":        "",
			})
		if userUpdate.Error != nil {
			return userUpdate.Error
		}
		if userUpdate.RowsAffected == 0 {
			return fmt.Errorf("cannot ban user")
		}
		if err := tx.Model(&model.Token{}).Where("user_id = ?", user.Id).Update("status", common.TokenStatusDisabled).Error; err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}

	if err := model.InvalidateUserCache(user.Id); err != nil {
		common.SysLog("discord ban patrol cache invalidate failed: " + err.Error())
	}
	if err := model.InvalidateUserTokensCache(user.Id); err != nil {
		common.SysLog("discord ban patrol token cache invalidate failed: " + err.Error())
	}
	return nil
}

// MarkDiscordGateFailedAndDisableTokens revokes a non-admin user's API tokens
// because they no longer satisfy the Discord allow gate, but it does not disable
// the user account. This is intentionally separate from BanUserAndDisableTokens:
// missing a required Discord guild/role is a reauthorization problem, not a ban.
func MarkDiscordGateFailedAndDisableTokens(user *model.User, evaluatedDiscordID, evaluatedEncryptedRefreshToken, reason, message string, checkedAt int64) error {
	if user == nil || user.Id <= 0 {
		return fmt.Errorf("invalid user")
	}
	evaluatedDiscordID = strings.TrimSpace(evaluatedDiscordID)
	if evaluatedDiscordID == "" {
		return fmt.Errorf("discord id is required")
	}
	evaluatedEncryptedRefreshToken = strings.TrimSpace(evaluatedEncryptedRefreshToken)
	if evaluatedEncryptedRefreshToken == "" {
		return fmt.Errorf("discord refresh token is required")
	}
	if user.Role >= common.RoleAdminUser {
		return fmt.Errorf("cannot disable admin/root tokens")
	}
	reason = strings.TrimSpace(reason)
	message = strings.TrimSpace(message)
	if checkedAt <= 0 {
		checkedAt = common.GetTimestamp()
	}

	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		var current model.User
		if err := tx.Select("id", "role", "status", "discord_gate_exempt", "discord_id", "discord_refresh_token").First(&current, user.Id).Error; err != nil {
			return err
		}
		if current.Role >= common.RoleAdminUser {
			return fmt.Errorf("cannot disable admin/root tokens")
		}
		if current.Status != common.UserStatusEnabled {
			return fmt.Errorf("cannot disable tokens for disabled user")
		}
		if current.DiscordGateExempt {
			return fmt.Errorf("cannot disable tokens for discord gate exempt user")
		}
		if strings.TrimSpace(current.DiscordId) != evaluatedDiscordID {
			return fmt.Errorf("discord binding changed")
		}
		if strings.TrimSpace(current.DiscordRefreshToken) != evaluatedEncryptedRefreshToken {
			return fmt.Errorf("discord refresh token changed")
		}
		userUpdate := tx.Model(&model.User{}).
			Where("id = ? AND status = ? AND role < ? AND (discord_gate_exempt IS NULL OR discord_gate_exempt = ?) AND discord_id = ? AND discord_refresh_token = ?", user.Id, common.UserStatusEnabled, common.RoleAdminUser, false, evaluatedDiscordID, evaluatedEncryptedRefreshToken).
			Updates(map[string]interface{}{
				"discord_gate_passed":        false,
				"discord_last_check_at":      checkedAt,
				"discord_last_check_result":  "allow_failed",
				"discord_last_check_reason":  reason,
				"discord_gate_message":       message,
				"discord_patrol_retry_at":    0,
				"discord_patrol_retry_count": 0,
				"discord_patrol_last_error":  "",
			})
		if userUpdate.Error != nil {
			return userUpdate.Error
		}
		if userUpdate.RowsAffected == 0 {
			return fmt.Errorf("cannot disable tokens")
		}
		if err := tx.Model(&model.Token{}).
			Where("user_id = ?", user.Id).
			Update("status", common.TokenStatusDisabled).Error; err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}

	if err := model.InvalidateUserCache(user.Id); err != nil {
		common.SysLog("discord gate token disable cache invalidate failed: " + err.Error())
	}
	if err := model.InvalidateUserTokensCache(user.Id); err != nil {
		common.SysLog("discord gate token cache invalidate failed: " + err.Error())
	}
	return nil
}

func ensureUserStillNonAdmin(tx *gorm.DB, userID int, message string) error {
	var current model.User
	if err := tx.Select("id", "role").First(&current, userID).Error; err != nil {
		return err
	}
	if current.Role >= common.RoleAdminUser {
		return errors.New(message)
	}
	return nil
}

// AppendUserRemarkLine appends a single remark line to the user, de-duplicating
// an identical existing line. The remark field is capped at 255 runes.
func AppendUserRemarkLine(userId int, line string) (string, error) {
	line = strings.TrimSpace(line)
	if userId <= 0 || line == "" {
		return "", nil
	}

	var user model.User
	if err := model.DB.Select("id", "remark").First(&user, userId).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}

	current := strings.TrimSpace(user.Remark)
	if current != "" {
		for _, item := range strings.Split(current, "\n") {
			if strings.TrimSpace(item) == line {
				return current, nil
			}
		}
		current = current + "\n" + line
	} else {
		current = line
	}

	if len([]rune(current)) > 255 {
		current = string([]rune(current)[:255])
	}

	if err := model.DB.Model(&model.User{}).Where("id = ?", userId).Update("remark", current).Error; err != nil {
		return "", err
	}
	if err := model.InvalidateUserCache(userId); err != nil {
		common.SysLog("append user remark cache invalidate failed: " + err.Error())
	}
	return current, nil
}

// MarkSuspiciousIPInput captures the data needed to upsert a suspicious IP mark
// during a local auto-ban. Source is one of: prompt_auto_ban / ua_auto_ban /
// empty_ua_auto_ban. No ban_sync / external bot fields.
type MarkSuspiciousIPInput struct {
	UserID      int
	Username    string
	IP          string
	Source      string
	Context     string
	BanContext  string
	BanReason   string
	TriggeredAt int64
}

// MarkSuspiciousIP upserts a local suspicious-IP mark via
// model.UpsertSuspiciousIPMark (which increments TriggerCount on repeat hits).
// It is the only suspicious-IP path — no ban_sync, no external bot.
func MarkSuspiciousIP(ctx context.Context, input MarkSuspiciousIPInput) (*model.SuspiciousIPMark, bool, error) {
	if input.UserID <= 0 {
		return nil, false, errors.New("invalid user id")
	}
	ip := strings.TrimSpace(input.IP)
	if ip == "" {
		return nil, false, errors.New("ip is empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	triggeredAt := input.TriggeredAt
	if triggeredAt <= 0 {
		triggeredAt = common.GetTimestamp()
	}

	mark := &model.SuspiciousIPMark{
		UserId:          input.UserID,
		Username:        strings.TrimSpace(input.Username),
		Ip:              ip,
		Source:          strings.TrimSpace(input.Source),
		Context:         strings.TrimSpace(input.Context),
		BanContext:      strings.TrimSpace(input.BanContext),
		BanReason:       strings.TrimSpace(input.BanReason),
		LastTriggeredAt: triggeredAt,
		TriggerCount:    1,
	}
	created, err := model.UpsertSuspiciousIPMark(ctx, mark)
	if err != nil {
		return nil, false, err
	}
	return mark, created, nil
}
