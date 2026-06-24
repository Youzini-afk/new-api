package model

import (
	"context"
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// UABlockLog stores UA interception records for suspicious-user inspection.
//
// Bool fields intentionally carry NO gorm default tag (AGENTS.md): Go zero
// value + BeforeCreate/normalization handle defaults, avoiding cross-dialect
// AutoMigrate churn on bool columns.
type UABlockLog struct {
	Id                int    `json:"id" gorm:"primaryKey"`
	UserId            int    `json:"user_id" gorm:"index"`
	Username          string `json:"username" gorm:"index;size:64;default:''"`
	Ip                string `json:"ip" gorm:"index;size:64;default:''"`
	UserAgent         string `json:"user_agent" gorm:"type:varchar(512);default:''"`
	RequestHeadersRaw string `json:"request_headers_raw" gorm:"type:text"`
	RequestParamsRaw  string `json:"request_params_raw" gorm:"type:text"`
	RulePattern       string `json:"rule_pattern" gorm:"index;size:255;default:''"`
	RuleMessage       string `json:"rule_message" gorm:"type:varchar(1024);default:''"`
	ErrorCode         string `json:"error_code" gorm:"size:64;default:''"`
	HTTPStatusCode    int    `json:"http_status_code" gorm:"default:400"`
	RequestPath       string `json:"request_path" gorm:"type:varchar(255);index;default:''"`
	IsEmptyUA         bool   `json:"is_empty_ua" gorm:"index"`
	AutoBanConfigured bool   `json:"auto_ban_configured" gorm:"index"`
	AutoBanned        bool   `json:"auto_banned" gorm:"index"`
	BanReason         string `json:"ban_reason" gorm:"type:varchar(255);default:''"`
	MatchedAt         int64  `json:"matched_at" gorm:"bigint;index"`
	CreatedAt         int64  `json:"created_at" gorm:"bigint;index"`
	UpdatedAt         int64  `json:"updated_at" gorm:"bigint"`
}

func (r *UABlockLog) BeforeCreate(tx *gorm.DB) error {
	r.CreatedAt = common.GetTimestamp()
	r.UpdatedAt = r.CreatedAt
	if r.MatchedAt == 0 {
		r.MatchedAt = r.CreatedAt
	}
	return nil
}

func (r *UABlockLog) BeforeUpdate(tx *gorm.DB) error {
	r.UpdatedAt = common.GetTimestamp()
	return nil
}

func normalizeUABlockLog(record *UABlockLog) {
	if record == nil {
		return
	}
	record.Username = strings.TrimSpace(record.Username)
	record.Ip = strings.TrimSpace(record.Ip)
	record.UserAgent = strings.TrimSpace(record.UserAgent)
	record.RulePattern = strings.TrimSpace(record.RulePattern)
	record.RuleMessage = strings.TrimSpace(record.RuleMessage)
	record.ErrorCode = strings.TrimSpace(record.ErrorCode)
	record.RequestPath = strings.TrimSpace(record.RequestPath)
	record.BanReason = strings.TrimSpace(record.BanReason)
	if record.HTTPStatusCode < 100 || record.HTTPStatusCode > 599 {
		record.HTTPStatusCode = 400
	}
}

// CreateUABlockLog persists an UA interception record.
func CreateUABlockLog(ctx context.Context, record *UABlockLog) error {
	if record == nil {
		return errors.New("ua block log is nil")
	}
	normalizeUABlockLog(record)
	return DB.WithContext(ctx).Create(record).Error
}
