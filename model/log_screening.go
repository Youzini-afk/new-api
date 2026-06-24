package model

import (
	"context"
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// LogScreeningRecord stores screening results for suspicious users.
//
// Dialect-safety notes:
//   - The Go field Window is exposed as JSON key "window" but mapped to the
//     non-reserved DB column "window_label" via the gorm column tag. The bare
//     identifier "window" is reserved in PostgreSQL and MySQL 8 (window
//     functions), so we avoid it as a physical column name and therefore avoid
//     any dialect-specific quoting in raw WHERE clauses.
//   - Bool fields intentionally carry NO gorm default tag. Per project
//     convention (AGENTS.md), boolean defaults are normalized in code
//     (Go zero value + BeforeCreate/normalization) to avoid cross-dialect
//     AutoMigrate churn.
type LogScreeningRecord struct {
	Id                  int    `json:"id" gorm:"primaryKey"`
	UserId              int    `json:"user_id" gorm:"index:idx_screen_user_token_ip,priority:1;uniqueIndex:idx_log_screening_unique,priority:1"`
	Username            string `json:"username" gorm:"index;size:64;default:''"`
	DiscordID           string `json:"discord_id" gorm:"size:64;default:''"`
	DiscordUID          int64  `json:"discord_uid" gorm:"default:0"`
	RiskLevel           string `json:"risk_level" gorm:"index;size:16;default:''"`
	ObservedUntil       int64  `json:"observed_until" gorm:"bigint;index"`
	RequireManualReview bool   `json:"require_manual_review" gorm:"index"`
	TokenName           string `json:"token_name" gorm:"index:idx_screen_user_token_ip,priority:2;size:128;default:''"`
	Ip                  string `json:"ip" gorm:"index:idx_screen_user_token_ip,priority:3;size:64;default:''"`
	RuleName            string `json:"rule_name" gorm:"index;size:128;uniqueIndex:idx_log_screening_unique,priority:2"`
	Window              string `json:"window" gorm:"column:window_label;index;size:16;uniqueIndex:idx_log_screening_unique,priority:3"`
	RequestCount        int    `json:"request_count" gorm:"default:0"`
	RPM                 int    `json:"rpm" gorm:"default:0"`
	RPH                 int    `json:"rph" gorm:"default:0"`
	TPM                 int    `json:"tpm" gorm:"default:0"`
	ParamHits           string `json:"param_hits" gorm:"type:text"`
	UAHits              string `json:"ua_hits" gorm:"type:text"`
	PromptDeltaCount    int    `json:"prompt_delta_count" gorm:"default:0"`
	PromptDeltaMax      int    `json:"prompt_delta_max" gorm:"default:0"`
	RequestPath         string `json:"request_path" gorm:"type:varchar(255);default:'';uniqueIndex:idx_log_screening_unique,priority:4"`
	WindowStart         int64  `json:"window_start" gorm:"bigint;index"`
	WindowEnd           int64  `json:"window_end" gorm:"bigint;index"`
	MatchedAt           int64  `json:"matched_at" gorm:"bigint;index"`
	ExpiresAt           int64  `json:"expires_at" gorm:"bigint;index"`
	CreatedAt           int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt           int64  `json:"updated_at" gorm:"bigint"`
	OperatorUserId      int    `json:"operator_user_id" gorm:"default:0"`
	OperatorName        string `json:"operator_name" gorm:"size:64;default:''"`
	ManualTriggered     bool   `json:"manual_triggered" gorm:"index"`
}

func (r *LogScreeningRecord) BeforeCreate(tx *gorm.DB) error {
	r.CreatedAt = common.GetTimestamp()
	r.UpdatedAt = r.CreatedAt
	if r.MatchedAt == 0 {
		r.MatchedAt = r.CreatedAt
	}
	return nil
}

func (r *LogScreeningRecord) BeforeUpdate(tx *gorm.DB) error {
	r.UpdatedAt = common.GetTimestamp()
	return nil
}

func normalizeLogScreeningRecord(record *LogScreeningRecord) {
	if record == nil {
		return
	}
	record.Username = strings.TrimSpace(record.Username)
	record.DiscordID = strings.TrimSpace(record.DiscordID)
	record.TokenName = strings.TrimSpace(record.TokenName)
	record.Ip = strings.TrimSpace(record.Ip)
	record.RiskLevel = strings.TrimSpace(record.RiskLevel)
	record.RuleName = strings.TrimSpace(record.RuleName)
	record.Window = strings.TrimSpace(record.Window)
	record.ParamHits = strings.TrimSpace(record.ParamHits)
	record.UAHits = strings.TrimSpace(record.UAHits)
	record.RequestPath = strings.TrimSpace(record.RequestPath)
	record.OperatorName = strings.TrimSpace(record.OperatorName)
}

// logScreeningWindowColumn is the physical column backing the Window field.
// Kept as a constant so model-layer raw queries reference a single source of
// truth; the column itself is non-reserved so no dialect quoting is needed.
const logScreeningWindowColumn = "window_label"

// UpsertLogScreeningRecord inserts or updates a screening record keyed by
// (user_id, rule_name, window, request_path). The WHERE clause references the
// non-reserved physical column name, so it is dialect-safe across SQLite,
// MySQL and PostgreSQL without any identifier quoting.
//
// Concurrency-safe: runs in a transaction with clause.OnConflict on the
// composite unique index idx_log_screening_unique, so concurrent inserts for
// the same key do not produce duplicate rows.
func UpsertLogScreeningRecord(ctx context.Context, record *LogScreeningRecord) (created bool, err error) {
	if record == nil {
		return false, errors.New("log screening record is nil")
	}
	normalizeLogScreeningRecord(record)

	err = DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&LogScreeningRecord{}).
			Where("user_id = ?", record.UserId).
			Where("rule_name = ?", record.RuleName).
			Where(logScreeningWindowColumn+" = ?", record.Window).
			Where("request_path = ?", record.RequestPath)
		var existing LogScreeningRecord
		if e := query.First(&existing).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				// Insert with OnConflict fallback so a concurrent insert on the
				// same unique key does not fail; it updates instead.
				if e2 := tx.Clauses(clause.OnConflict{
					Columns: []clause.Column{
						{Name: "user_id"},
						{Name: "rule_name"},
						{Name: logScreeningWindowColumn},
						{Name: "request_path"},
					},
					DoUpdates: clause.Assignments(map[string]interface{}{
						"username":              record.Username,
						"discord_id":            record.DiscordID,
						"discord_uid":           record.DiscordUID,
						"risk_level":            record.RiskLevel,
						"observed_until":        record.ObservedUntil,
						"require_manual_review": record.RequireManualReview,
						"token_name":            record.TokenName,
						"ip":                    record.Ip,
						"request_count":         record.RequestCount,
						"rpm":                   record.RPM,
						"rph":                   record.RPH,
						"tpm":                   record.TPM,
						"param_hits":            record.ParamHits,
						"ua_hits":               record.UAHits,
						"prompt_delta_count":    record.PromptDeltaCount,
						"prompt_delta_max":      record.PromptDeltaMax,
						"matched_at":            record.MatchedAt,
						"expires_at":            record.ExpiresAt,
						"operator_user_id":      record.OperatorUserId,
						"operator_name":         record.OperatorName,
						"manual_triggered":      record.ManualTriggered,
					}),
				}).Create(record).Error; e2 != nil {
					// OnConflict update means a concurrent insert won; the row
					// was updated, not created.
					created = false
					return e2
				}
				// If the conflict path was taken (row already existed), GORM
				// returns RowsAffected=0 for the INSERT; detect this to set
				// created=false. However, GORM's OnConflict with DoUpdates
				// returns RowsAffected=1 on both insert and update for SQLite/
				// MySQL, so we cannot reliably distinguish. The safest approach:
				// re-check after the clause whether the row pre-existed by
				// counting — but that reintroduces a race. Instead, we accept
				// that created=true may be inaccurate under concurrent inserts
				// (the caller treats created as "this call wrote a new row or
				// updated an existing one" which is functionally correct for
				// the summary counters — the remark-append path only fires on
				// created=true || manual, so a concurrent update with manual=true
				// is safe to fire the remark again because AppendUserRemarkLine
				// de-duplicates).
				created = true
				return nil
			}
			return e
		}

		existing.Username = record.Username
		existing.DiscordID = record.DiscordID
		existing.DiscordUID = record.DiscordUID
		existing.RiskLevel = record.RiskLevel
		existing.ObservedUntil = record.ObservedUntil
		existing.RequireManualReview = record.RequireManualReview
		existing.TokenName = record.TokenName
		existing.Ip = record.Ip
		existing.RuleName = record.RuleName
		existing.Window = record.Window
		existing.RequestPath = record.RequestPath
		existing.RequestCount = record.RequestCount
		existing.RPM = record.RPM
		existing.RPH = record.RPH
		existing.TPM = record.TPM
		existing.ParamHits = record.ParamHits
		existing.UAHits = record.UAHits
		existing.PromptDeltaCount = record.PromptDeltaCount
		existing.PromptDeltaMax = record.PromptDeltaMax
		existing.MatchedAt = record.MatchedAt
		existing.ExpiresAt = record.ExpiresAt
		if record.OperatorUserId != 0 {
			existing.OperatorUserId = record.OperatorUserId
			existing.OperatorName = record.OperatorName
		}
		if record.ManualTriggered {
			existing.ManualTriggered = true
		}

		if e := tx.Save(&existing).Error; e != nil {
			return e
		}
		created = false
		return nil
	})
	return created, err
}

// DeleteExpiredLogScreeningRecords removes expired screening records in batches.
func DeleteExpiredLogScreeningRecords(ctx context.Context, now int64, limit int) (int64, error) {
	if now <= 0 {
		now = common.GetTimestamp()
	}
	if limit <= 0 {
		limit = 1000
	}
	if limit > 10000 {
		limit = 10000
	}
	var total int64
	for {
		result := DB.WithContext(ctx).
			Where("expires_at > 0 AND expires_at < ?", now).
			Limit(limit).
			Delete(&LogScreeningRecord{})
		if result.Error != nil {
			return total, result.Error
		}
		total += result.RowsAffected
		if result.RowsAffected < int64(limit) {
			break
		}
	}
	return total, nil
}
