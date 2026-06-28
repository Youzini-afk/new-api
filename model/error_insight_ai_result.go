package model

import (
	"context"
	"time"

	"gorm.io/gorm"
)

const ErrorInsightAIResultStatusDraft = "draft"
const ErrorInsightAIResultStatusApproved = "approved"

type ErrorInsightAIResult struct {
	Id                  int    `json:"id" gorm:"primaryKey"`
	NormalizedSignature string `json:"normalized_signature" gorm:"type:varchar(64);uniqueIndex;not null"`
	Rules               string `json:"rules" gorm:"type:text"`
	Raw                 string `json:"raw" gorm:"type:text"`
	Status              string `json:"status" gorm:"type:varchar(32);index;default:'draft'"`
	CreatedBy           int    `json:"created_by" gorm:"index;default:0"`
	CreatedAt           int64  `json:"created_at" gorm:"bigint;index"`
	UpdatedAt           int64  `json:"updated_at" gorm:"bigint;index"`
}

func UpsertErrorInsightAIResult(ctx context.Context, signature string, createdBy int, rules string, raw string) error {
	now := time.Now().Unix()
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing ErrorInsightAIResult
		err := tx.Where("normalized_signature = ?", signature).First(&existing).Error
		if err == nil {
			return tx.Model(&existing).Updates(map[string]interface{}{
				"rules":      rules,
				"raw":        raw,
				"status":     ErrorInsightAIResultStatusDraft,
				"created_by": createdBy,
				"updated_at": now,
			}).Error
		}
		if err != gorm.ErrRecordNotFound {
			return err
		}
		return tx.Create(&ErrorInsightAIResult{
			NormalizedSignature: signature,
			Rules:               rules,
			Raw:                 raw,
			Status:              ErrorInsightAIResultStatusDraft,
			CreatedBy:           createdBy,
			CreatedAt:           now,
			UpdatedAt:           now,
		}).Error
	})
}

func GetErrorInsightAIResult(ctx context.Context, signature string) (*ErrorInsightAIResult, error) {
	var result ErrorInsightAIResult
	err := DB.WithContext(ctx).Where("normalized_signature = ?", signature).First(&result).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func MarkErrorInsightAIResultApproved(ctx context.Context, signature string) error {
	if signature == "" {
		return nil
	}
	return DB.WithContext(ctx).
		Model(&ErrorInsightAIResult{}).
		Where("normalized_signature = ?", signature).
		Updates(map[string]interface{}{
			"status":     ErrorInsightAIResultStatusApproved,
			"updated_at": time.Now().Unix(),
		}).Error
}

func ExistingErrorInsightAIResultSignatures(ctx context.Context, signatures []string) (map[string]bool, error) {
	result := make(map[string]bool, len(signatures))
	if len(signatures) == 0 {
		return result, nil
	}
	var rows []string
	if err := DB.WithContext(ctx).
		Model(&ErrorInsightAIResult{}).
		Where("normalized_signature IN ?", signatures).
		Pluck("normalized_signature", &rows).Error; err != nil {
		return result, err
	}
	for _, signature := range rows {
		result[signature] = true
	}
	return result, nil
}
