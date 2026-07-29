package model

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ExternalQuotaKindDebit  = "debit"
	ExternalQuotaKindCredit = "credit"

	ExternalQuotaStatusProcessing = "processing"
	ExternalQuotaStatusCompleted  = "completed"
	ExternalQuotaStatusFailed     = "failed"

	ExternalQuotaErrorInsufficient = "insufficient_quota"
	ExternalQuotaErrorUserDisabled = "user_disabled"
	ExternalQuotaErrorUserNotFound = "user_not_found"
)

var (
	ErrExternalAuthCodeInvalid   = errors.New("external app authorization code is invalid or expired")
	ErrExternalOperationConflict = errors.New("external quota operation id was reused with different parameters")
	ErrExternalOperationNotFound = errors.New("external quota operation was not found")
	ErrExternalOperationInvalid  = errors.New("external quota operation is invalid")
	externalOperationIDPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)
)

// ExternalAppAuthCode stores only a SHA-256 digest of the short-lived code.
// The raw bearer code exists only in the browser redirect and is consumed once.
type ExternalAppAuthCode struct {
	Id        int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	AppId     string `json:"app_id" gorm:"column:app_id;type:varchar(64);not null;index"`
	CodeHash  string `json:"-" gorm:"column:code_hash;type:char(64);not null;uniqueIndex"`
	UserId    int    `json:"user_id" gorm:"column:user_id;not null;index"`
	ExpiresAt int64  `json:"expires_at" gorm:"column:expires_at;not null;index"`
	UsedAt    int64  `json:"used_at" gorm:"column:used_at;not null;default:0;index"`
	CreatedAt int64  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
}

// ExternalQuotaOperation is the durable idempotency boundary for every quota
// movement requested by the trusted game application. The unique composite
// key means a retry can return the original result without changing quota.
type ExternalQuotaOperation struct {
	Id          int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	AppId       string `json:"app_id" gorm:"column:app_id;type:varchar(64);not null;uniqueIndex:idx_external_quota_operation,priority:1"`
	OperationId string `json:"operation_id" gorm:"column:operation_id;type:varchar(128);not null;uniqueIndex:idx_external_quota_operation,priority:2"`
	UserId      int    `json:"user_id" gorm:"column:user_id;not null;index"`
	Kind        string `json:"kind" gorm:"column:kind;type:varchar(16);not null"`
	Amount      int    `json:"amount" gorm:"column:amount;not null"`
	Status      string `json:"status" gorm:"column:status;type:varchar(16);not null;index"`
	ErrorCode   string `json:"error_code,omitempty" gorm:"column:error_code;type:varchar(64);not null;default:''"`
	QuotaAfter  int    `json:"quota_after" gorm:"column:quota_after;not null;default:0"`
	CreatedAt   int64  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt   int64  `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

func CreateExternalAppAuthCode(appID string, userID int, ttl time.Duration) (string, int64, error) {
	appID = strings.TrimSpace(appID)
	if appID == "" || userID <= 0 || ttl <= 0 {
		return "", 0, ErrExternalOperationInvalid
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", 0, err
	}
	code := base64.RawURLEncoding.EncodeToString(raw)
	expiresAt := time.Now().Add(ttl).Unix()
	record := ExternalAppAuthCode{
		AppId:     appID,
		CodeHash:  externalAuthCodeHash(code),
		UserId:    userID,
		ExpiresAt: expiresAt,
	}
	if err := DB.Create(&record).Error; err != nil {
		return "", 0, err
	}
	return code, expiresAt, nil
}

func ConsumeExternalAppAuthCode(appID string, rawCode string) (*User, error) {
	appID = strings.TrimSpace(appID)
	rawCode = strings.TrimSpace(rawCode)
	if appID == "" || rawCode == "" {
		return nil, ErrExternalAuthCodeInvalid
	}
	now := time.Now().Unix()
	var user User
	err := DB.Transaction(func(tx *gorm.DB) error {
		var record ExternalAppAuthCode
		if err := tx.Where("app_id = ? AND code_hash = ?", appID, externalAuthCodeHash(rawCode)).First(&record).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrExternalAuthCodeInvalid
			}
			return err
		}
		result := tx.Model(&ExternalAppAuthCode{}).
			Where("id = ? AND used_at = 0 AND expires_at >= ?", record.Id, now).
			Update("used_at", now)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrExternalAuthCodeInvalid
		}
		if err := tx.First(&user, record.UserId).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrExternalAuthCodeInvalid
			}
			return err
		}
		if user.Status != common.UserStatusEnabled {
			return ErrExternalAuthCodeInvalid
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// ApplyExternalQuotaOperation applies one immutable idempotent quota movement.
// The operation row is claimed before quota changes and both writes are in the
// same local transaction. A duplicate request therefore either observes the
// committed result or waits for/loses to the unique key without double spend.
func ApplyExternalQuotaOperation(appID, operationID string, userID int, kind string, amount int) (*ExternalQuotaOperation, bool, error) {
	appID = strings.TrimSpace(appID)
	operationID = strings.TrimSpace(operationID)
	kind = strings.ToLower(strings.TrimSpace(kind))
	if appID == "" || !externalOperationIDPattern.MatchString(operationID) || userID <= 0 || amount <= 0 ||
		(kind != ExternalQuotaKindDebit && kind != ExternalQuotaKindCredit) {
		return nil, false, ErrExternalOperationInvalid
	}

	operation := ExternalQuotaOperation{
		AppId:       appID,
		OperationId: operationID,
		UserId:      userID,
		Kind:        kind,
		Amount:      amount,
		Status:      ExternalQuotaStatusProcessing,
	}
	applied := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		claim := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "app_id"}, {Name: "operation_id"}},
			DoNothing: true,
		}).Create(&operation)
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected == 0 {
			if err := tx.Where("app_id = ? AND operation_id = ?", appID, operationID).First(&operation).Error; err != nil {
				return err
			}
			if operation.UserId != userID || operation.Kind != kind || operation.Amount != amount {
				return ErrExternalOperationConflict
			}
			return nil
		}

		var update *gorm.DB
		if kind == ExternalQuotaKindDebit {
			update = tx.Model(&User{}).
				Where("id = ? AND status = ? AND quota >= ?", userID, common.UserStatusEnabled, amount).
				Update("quota", gorm.Expr("quota - ?", amount))
		} else {
			update = tx.Model(&User{}).
				Where("id = ?", userID).
				Update("quota", gorm.Expr("quota + ?", amount))
		}
		if update.Error != nil {
			return update.Error
		}

		var current User
		if err := tx.Select("id", "status", "quota").First(&current, userID).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			operation.Status = ExternalQuotaStatusFailed
			operation.ErrorCode = ExternalQuotaErrorUserNotFound
		} else if update.RowsAffected != 1 {
			operation.Status = ExternalQuotaStatusFailed
			operation.QuotaAfter = current.Quota
			if kind == ExternalQuotaKindDebit && current.Status != common.UserStatusEnabled {
				operation.ErrorCode = ExternalQuotaErrorUserDisabled
			} else {
				operation.ErrorCode = ExternalQuotaErrorInsufficient
			}
		} else {
			operation.Status = ExternalQuotaStatusCompleted
			operation.ErrorCode = ""
			operation.QuotaAfter = current.Quota
			applied = true
		}

		return tx.Model(&ExternalQuotaOperation{}).Where("id = ?", operation.Id).Updates(map[string]interface{}{
			"status":      operation.Status,
			"error_code":  operation.ErrorCode,
			"quota_after": operation.QuotaAfter,
		}).Error
	})
	if err != nil {
		return nil, false, err
	}
	if applied {
		if err := InvalidateUserCache(userID); err != nil {
			common.SysLog(fmt.Sprintf("external quota operation cache invalidate failed for user %d: %v", userID, err))
		}
		verb := "转入外部游戏"
		if kind == ExternalQuotaKindCredit {
			verb = "从外部游戏转回"
		}
		RecordLog(userID, LogTypeSystem, fmt.Sprintf("%s额度 %d，操作号 %s", verb, amount, operationID))
	}
	return &operation, applied, nil
}

func GetExternalQuotaOperation(appID, operationID string) (*ExternalQuotaOperation, error) {
	var operation ExternalQuotaOperation
	err := DB.Where("app_id = ? AND operation_id = ?", strings.TrimSpace(appID), strings.TrimSpace(operationID)).First(&operation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrExternalOperationNotFound
	}
	if err != nil {
		return nil, err
	}
	return &operation, nil
}

func externalAuthCodeHash(rawCode string) string {
	digest := sha256.Sum256([]byte(rawCode))
	return hex.EncodeToString(digest[:])
}
