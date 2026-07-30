package model

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/external_app_setting"
	"gorm.io/gorm"
)

var externalGameOptionsUpdateMutex sync.Mutex

// updateExternalGameOptionsAtomic persists and publishes related external game
// fields as one coherent snapshot. Generic bulk option publishing updates
// registered config fields one at a time, which would briefly expose mixed
// app-ID/secret/redirect values to concurrent HMAC requests.
func updateExternalGameOptionsAtomic(values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	externalGameOptionsUpdateMutex.Lock()
	defer externalGameOptionsUpdateMutex.Unlock()
	if config.GlobalConfig.Get(external_app_setting.ConfigName) == nil {
		return fmt.Errorf("external game config is not registered")
	}

	candidate := external_app_setting.GetStoredSettings()
	for key, value := range values {
		switch key {
		case external_app_setting.OptionEnabled:
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("invalid external game enabled value: %w", err)
			}
			candidate.Enabled = parsed
		case external_app_setting.OptionAppId:
			candidate.AppId = value
		case external_app_setting.OptionAppSecret:
			candidate.AppSecret = value
		case external_app_setting.OptionRedirectUri:
			candidate.RedirectUri = value
		case external_app_setting.OptionCodeTTLSeconds:
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("invalid external game authorization code TTL: %w", err)
			}
			candidate.CodeTTLSeconds = parsed
		case external_app_setting.OptionSignatureToleranceSeconds:
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("invalid external game signature tolerance: %w", err)
			}
			candidate.SignatureToleranceSeconds = parsed
		default:
			return fmt.Errorf("unsupported external game option %q", key)
		}
	}

	prepared, err := external_app_setting.PrepareForStorage(candidate)
	if err != nil {
		return err
	}
	if err := external_app_setting.ApplyEnvironmentOverrides(prepared).ValidateConfiguration(); err != nil {
		return err
	}
	normalized := external_app_setting.OptionValues(prepared, true)
	preparedValues := make(map[string]string, len(values))
	configValues := make(map[string]string, len(values))
	for key := range values {
		value := normalized[key]
		preparedValues[key] = value
		configValues[strings.TrimPrefix(key, external_app_setting.OptionPrefix)] = value
	}

	if err := DB.Transaction(func(tx *gorm.DB) error {
		for key, value := range preparedValues {
			option := Option{Key: key}
			if err := tx.FirstOrCreate(&option, Option{Key: key}).Error; err != nil {
				return err
			}
			option.Value = value
			if err := tx.Save(&option).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}

	found, err := config.GlobalConfig.Update(external_app_setting.ConfigName, configValues)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("external game config is not registered")
	}
	common.OptionMapRWMutex.Lock()
	for key, value := range preparedValues {
		common.OptionMap[key] = value
	}
	common.OptionMapRWMutex.Unlock()
	return nil
}
