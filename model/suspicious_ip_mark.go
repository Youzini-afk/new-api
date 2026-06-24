package model

import (
	"context"
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SuspiciousIPMark stores suspicious IP marks aggregated during screening /
// interception workflows. Phase 5 exposes only model + list helpers used to
// enrich LogScreeningRecord listings; no public route is provided.
type SuspiciousIPMark struct {
	Id              int    `json:"id" gorm:"primaryKey"`
	UserId          int    `json:"user_id" gorm:"index:idx_suspicious_ip_user_ip,priority:1;uniqueIndex:idx_suspicious_ip_unique,priority:1"`
	Username        string `json:"username" gorm:"index;size:64;default:''"`
	Ip              string `json:"ip" gorm:"index:idx_suspicious_ip_user_ip,priority:2;size:64;default:'';uniqueIndex:idx_suspicious_ip_unique,priority:2"`
	Source          string `json:"source" gorm:"index;size:64;default:''"`
	Context         string `json:"context" gorm:"type:text"`
	BanContext      string `json:"ban_context" gorm:"type:text"`
	BanReason       string `json:"ban_reason" gorm:"type:varchar(512);default:''"`
	TriggerCount    int    `json:"trigger_count" gorm:"default:0"`
	LastTriggeredAt int64  `json:"last_triggered_at" gorm:"bigint;index"`
	CreatedAt       int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt       int64  `json:"updated_at" gorm:"bigint"`
}

func (m *SuspiciousIPMark) BeforeCreate(tx *gorm.DB) error {
	m.CreatedAt = common.GetTimestamp()
	m.UpdatedAt = m.CreatedAt
	if m.LastTriggeredAt == 0 {
		m.LastTriggeredAt = m.CreatedAt
	}
	if m.TriggerCount <= 0 {
		m.TriggerCount = 1
	}
	return nil
}

func (m *SuspiciousIPMark) BeforeUpdate(tx *gorm.DB) error {
	m.UpdatedAt = common.GetTimestamp()
	return nil
}

func normalizeSuspiciousIPMark(mark *SuspiciousIPMark) {
	if mark == nil {
		return
	}
	mark.Username = strings.TrimSpace(mark.Username)
	mark.Ip = strings.TrimSpace(mark.Ip)
	mark.Source = strings.TrimSpace(mark.Source)
	mark.Context = strings.TrimSpace(mark.Context)
	mark.BanContext = strings.TrimSpace(mark.BanContext)
	mark.BanReason = strings.TrimSpace(mark.BanReason)
	if mark.LastTriggeredAt <= 0 {
		mark.LastTriggeredAt = common.GetTimestamp()
	}
}

func UpsertSuspiciousIPMark(ctx context.Context, mark *SuspiciousIPMark) (created bool, err error) {
	if mark == nil {
		return false, errors.New("suspicious ip mark is nil")
	}
	normalizeSuspiciousIPMark(mark)
	if mark.UserId <= 0 {
		return false, errors.New("invalid user_id")
	}
	if mark.Ip == "" {
		return false, errors.New("ip is empty")
	}

	err = DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&SuspiciousIPMark{}).
			Where("user_id = ?", mark.UserId).
			Where("ip = ?", mark.Ip)
		var existing SuspiciousIPMark
		if e := query.First(&existing).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				if mark.TriggerCount <= 0 {
					mark.TriggerCount = 1
				}
				// Insert with OnConflict on the (user_id, ip) unique index so a
				// concurrent insert for the same key does not fail; it atomically
				// increments trigger_count instead.
				if e2 := tx.Clauses(clause.OnConflict{
					Columns: []clause.Column{
						{Name: "user_id"},
						{Name: "ip"},
					},
					DoUpdates: clause.Assignments(map[string]interface{}{
						"username":          mark.Username,
						"source":            mark.Source,
						"context":            mark.Context,
						"ban_context":       mark.BanContext,
						"ban_reason":        mark.BanReason,
						"last_triggered_at": mark.LastTriggeredAt,
						// Atomically increment trigger_count on conflict.
						"trigger_count": gorm.Expr("trigger_count + 1"),
					}),
				}).Create(mark).Error; e2 != nil {
					return e2
				}
				created = true
				return nil
			}
			return e
		}

		// Existing row: atomically increment trigger_count + update the other
		// fields in a single UPDATE (no read-modify-write race).
		updates := map[string]interface{}{
			"username":          mark.Username,
			"source":            mark.Source,
			"context":            mark.Context,
			"ban_context":       mark.BanContext,
			"ban_reason":        mark.BanReason,
			"last_triggered_at": mark.LastTriggeredAt,
			"trigger_count":     gorm.Expr("trigger_count + 1"),
		}
		if e := tx.Model(&SuspiciousIPMark{}).
			Where("user_id = ? AND ip = ?", mark.UserId, mark.Ip).
			Updates(updates).Error; e != nil {
			return e
		}
		created = false
		return nil
	})
	return created, err
}

func ListSuspiciousIPMarksByUserIDs(ctx context.Context, userIDs []int) (map[int][]SuspiciousIPMark, error) {
	result := make(map[int][]SuspiciousIPMark)
	if len(userIDs) == 0 {
		return result, nil
	}
	var records []SuspiciousIPMark
	if err := DB.WithContext(ctx).
		Where("user_id IN ?", userIDs).
		Order("last_triggered_at desc, id desc").
		Find(&records).Error; err != nil {
		return nil, err
	}
	for i := range records {
		record := records[i]
		result[record.UserId] = append(result[record.UserId], record)
	}
	return result, nil
}
