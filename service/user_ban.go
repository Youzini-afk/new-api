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
		if err := tx.Model(&model.User{}).Where("id = ?", user.Id).
			Updates(map[string]interface{}{
				"status": common.UserStatusDisabled,
				"remark": user.Remark,
			}).Error; err != nil {
			return err
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
	UserID       int
	Username     string
	IP           string
	Source       string
	Context      string
	BanContext   string
	BanReason    string
	TriggeredAt  int64
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
