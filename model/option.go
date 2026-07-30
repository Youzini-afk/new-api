package model

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/external_app_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/performance_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"gorm.io/gorm"
)

type Option struct {
	Key   string `json:"key" gorm:"primaryKey"`
	Value string `json:"value"`
}

var relayErrorGovernanceUpdateMutex sync.Mutex

func AllOption() ([]*Option, error) {
	var options []*Option
	var err error
	err = DB.Find(&options).Error
	return options, err
}

func InitOptionMap() {
	common.OptionMapRWMutex.Lock()
	common.OptionMap = make(map[string]string)

	// 添加原有的系统配置
	common.OptionMap["FileUploadPermission"] = strconv.Itoa(common.FileUploadPermission)
	common.OptionMap["FileDownloadPermission"] = strconv.Itoa(common.FileDownloadPermission)
	common.OptionMap["ImageUploadPermission"] = strconv.Itoa(common.ImageUploadPermission)
	common.OptionMap["ImageDownloadPermission"] = strconv.Itoa(common.ImageDownloadPermission)
	common.OptionMap["PasswordLoginEnabled"] = strconv.FormatBool(common.PasswordLoginEnabled)
	common.OptionMap["PasswordRegisterEnabled"] = strconv.FormatBool(common.PasswordRegisterEnabled)
	common.OptionMap["EmailVerificationEnabled"] = strconv.FormatBool(common.EmailVerificationEnabled)
	common.OptionMap["GitHubOAuthEnabled"] = strconv.FormatBool(common.GitHubOAuthEnabled)
	common.OptionMap["LinuxDOOAuthEnabled"] = strconv.FormatBool(common.LinuxDOOAuthEnabled)
	common.OptionMap["TelegramOAuthEnabled"] = strconv.FormatBool(common.TelegramOAuthEnabled)
	common.OptionMap["WeChatAuthEnabled"] = strconv.FormatBool(common.WeChatAuthEnabled)
	common.OptionMap["TurnstileCheckEnabled"] = strconv.FormatBool(common.TurnstileCheckEnabled)
	common.OptionMap["RegisterEnabled"] = strconv.FormatBool(common.RegisterEnabled)
	common.OptionMap["AutomaticDisableChannelEnabled"] = strconv.FormatBool(common.AutomaticDisableChannelEnabled)
	common.OptionMap["AutomaticEnableChannelEnabled"] = strconv.FormatBool(common.AutomaticEnableChannelEnabled)
	common.OptionMap["LogConsumeEnabled"] = strconv.FormatBool(common.LogConsumeEnabled)
	common.OptionMap["DisplayInCurrencyEnabled"] = strconv.FormatBool(common.DisplayInCurrencyEnabled)
	common.OptionMap["DisplayTokenStatEnabled"] = strconv.FormatBool(common.DisplayTokenStatEnabled)
	common.OptionMap["DrawingEnabled"] = strconv.FormatBool(common.DrawingEnabled)
	common.OptionMap["TaskEnabled"] = strconv.FormatBool(common.TaskEnabled)
	common.OptionMap["DataExportEnabled"] = strconv.FormatBool(common.DataExportEnabled)
	common.OptionMap["ChannelDisableThreshold"] = strconv.FormatFloat(common.ChannelDisableThreshold, 'f', -1, 64)
	common.OptionMap["EmailDomainRestrictionEnabled"] = strconv.FormatBool(common.EmailDomainRestrictionEnabled)
	common.OptionMap["EmailAliasRestrictionEnabled"] = strconv.FormatBool(common.EmailAliasRestrictionEnabled)
	common.OptionMap["EmailDomainWhitelist"] = strings.Join(common.EmailDomainWhitelist, ",")
	common.OptionMap["SMTPServer"] = ""
	common.OptionMap["SMTPFrom"] = ""
	common.OptionMap["SMTPPort"] = strconv.Itoa(common.SMTPPort)
	common.OptionMap["SMTPAccount"] = ""
	common.OptionMap["SMTPToken"] = ""
	common.OptionMap["SMTPSSLEnabled"] = strconv.FormatBool(common.SMTPSSLEnabled)
	common.OptionMap["SMTPStartTLSEnabled"] = strconv.FormatBool(common.SMTPStartTLSEnabled)
	common.OptionMap["SMTPInsecureSkipVerify"] = strconv.FormatBool(common.SMTPInsecureSkipVerify)
	common.OptionMap["SMTPForceAuthLogin"] = strconv.FormatBool(common.SMTPForceAuthLogin)
	common.OptionMap["Notice"] = ""
	common.OptionMap["About"] = ""
	common.OptionMap["HomePageContent"] = ""
	common.OptionMap["Footer"] = common.Footer
	common.OptionMap["SystemName"] = common.SystemName
	common.OptionMap["Logo"] = common.Logo
	common.OptionMap["ServerAddress"] = ""
	common.OptionMap["WorkerUrl"] = system_setting.WorkerUrl
	common.OptionMap["WorkerValidKey"] = system_setting.WorkerValidKey
	common.OptionMap["WorkerAllowHttpImageRequestEnabled"] = strconv.FormatBool(system_setting.WorkerAllowHttpImageRequestEnabled)
	common.OptionMap["PayAddress"] = ""
	common.OptionMap["CustomCallbackAddress"] = ""
	common.OptionMap["EpayId"] = ""
	common.OptionMap["EpayKey"] = ""
	common.OptionMap["Price"] = strconv.FormatFloat(operation_setting.Price, 'f', -1, 64)
	common.OptionMap["USDExchangeRate"] = strconv.FormatFloat(operation_setting.USDExchangeRate, 'f', -1, 64)
	common.OptionMap["MinTopUp"] = strconv.Itoa(operation_setting.MinTopUp)
	common.OptionMap["StripeMinTopUp"] = strconv.Itoa(setting.StripeMinTopUp)
	common.OptionMap["StripeApiSecret"] = setting.StripeApiSecret
	common.OptionMap["StripeWebhookSecret"] = setting.StripeWebhookSecret
	common.OptionMap["StripePriceId"] = setting.StripePriceId
	common.OptionMap["StripeUnitPrice"] = strconv.FormatFloat(setting.StripeUnitPrice, 'f', -1, 64)
	common.OptionMap["StripePromotionCodesEnabled"] = strconv.FormatBool(setting.StripePromotionCodesEnabled)
	common.OptionMap["CreemApiKey"] = setting.CreemApiKey
	common.OptionMap["CreemProducts"] = setting.CreemProducts
	common.OptionMap["CreemTestMode"] = strconv.FormatBool(setting.CreemTestMode)
	common.OptionMap["CreemWebhookSecret"] = setting.CreemWebhookSecret
	common.OptionMap["WaffoEnabled"] = strconv.FormatBool(setting.WaffoEnabled)
	common.OptionMap["WaffoApiKey"] = setting.WaffoApiKey
	common.OptionMap["WaffoPrivateKey"] = setting.WaffoPrivateKey
	common.OptionMap["WaffoPublicCert"] = setting.WaffoPublicCert
	common.OptionMap["WaffoSandboxPublicCert"] = setting.WaffoSandboxPublicCert
	common.OptionMap["WaffoSandboxApiKey"] = setting.WaffoSandboxApiKey
	common.OptionMap["WaffoSandboxPrivateKey"] = setting.WaffoSandboxPrivateKey
	common.OptionMap["WaffoSandbox"] = strconv.FormatBool(setting.WaffoSandbox)
	common.OptionMap["WaffoMerchantId"] = setting.WaffoMerchantId
	common.OptionMap["WaffoNotifyUrl"] = setting.WaffoNotifyUrl
	common.OptionMap["WaffoReturnUrl"] = setting.WaffoReturnUrl
	common.OptionMap["WaffoSubscriptionReturnUrl"] = setting.WaffoSubscriptionReturnUrl
	common.OptionMap["WaffoCurrency"] = setting.WaffoCurrency
	common.OptionMap["WaffoUnitPrice"] = strconv.FormatFloat(setting.WaffoUnitPrice, 'f', -1, 64)
	common.OptionMap["WaffoMinTopUp"] = strconv.Itoa(setting.WaffoMinTopUp)
	common.OptionMap["WaffoPayMethods"] = setting.WaffoPayMethods2JsonString()
	common.OptionMap["WaffoPancakeMerchantID"] = setting.WaffoPancakeMerchantID
	common.OptionMap["WaffoPancakePrivateKey"] = setting.WaffoPancakePrivateKey
	common.OptionMap["WaffoPancakeReturnURL"] = setting.WaffoPancakeReturnURL
	common.OptionMap["WaffoPancakeUnitPrice"] = strconv.FormatFloat(setting.WaffoPancakeUnitPrice, 'f', -1, 64)
	common.OptionMap["WaffoPancakeMinTopUp"] = strconv.Itoa(setting.WaffoPancakeMinTopUp)
	common.OptionMap["WaffoPancakeStoreID"] = setting.WaffoPancakeStoreID
	common.OptionMap["WaffoPancakeProductID"] = setting.WaffoPancakeProductID
	common.OptionMap["TopupGroupRatio"] = common.TopupGroupRatio2JSONString()
	common.OptionMap["Chats"] = setting.Chats2JsonString()
	common.OptionMap["AutoGroups"] = setting.AutoGroups2JsonString()
	common.OptionMap["DefaultUseAutoGroup"] = strconv.FormatBool(setting.DefaultUseAutoGroup)
	common.OptionMap["PayMethods"] = operation_setting.PayMethods2JsonString()
	common.OptionMap["GitHubClientId"] = ""
	common.OptionMap["GitHubClientSecret"] = ""
	common.OptionMap["TelegramBotToken"] = ""
	common.OptionMap["TelegramBotName"] = ""
	common.OptionMap["WeChatServerAddress"] = ""
	common.OptionMap["WeChatServerToken"] = ""
	common.OptionMap["WeChatAccountQRCodeImageURL"] = ""
	common.OptionMap["TurnstileSiteKey"] = ""
	common.OptionMap["TurnstileSecretKey"] = ""
	common.OptionMap["QuotaForNewUser"] = strconv.Itoa(common.QuotaForNewUser)
	common.OptionMap["QuotaForInviter"] = strconv.Itoa(common.QuotaForInviter)
	common.OptionMap["QuotaForInvitee"] = strconv.Itoa(common.QuotaForInvitee)
	common.OptionMap["QuotaRemindThreshold"] = strconv.Itoa(common.QuotaRemindThreshold)
	common.OptionMap["PreConsumedQuota"] = strconv.Itoa(common.PreConsumedQuota)
	common.OptionMap["ModelRequestRateLimitCount"] = strconv.Itoa(setting.ModelRequestRateLimitCount)
	common.OptionMap["ModelRequestRateLimitDurationMinutes"] = strconv.Itoa(setting.ModelRequestRateLimitDurationMinutes)
	common.OptionMap["ModelRequestRateLimitSuccessCount"] = strconv.Itoa(setting.ModelRequestRateLimitSuccessCount)
	common.OptionMap["ModelRequestRateLimitGroup"] = setting.ModelRequestRateLimitGroup2JSONString()
	common.OptionMap["ModelRatio"] = ratio_setting.ModelRatio2JSONString()
	common.OptionMap["ModelPrice"] = ratio_setting.ModelPrice2JSONString()
	common.OptionMap["CacheRatio"] = ratio_setting.CacheRatio2JSONString()
	common.OptionMap["CreateCacheRatio"] = ratio_setting.CreateCacheRatio2JSONString()
	common.OptionMap["GroupRatio"] = ratio_setting.GroupRatio2JSONString()
	common.OptionMap["GroupGroupRatio"] = ratio_setting.GroupGroupRatio2JSONString()
	common.OptionMap["UserUsableGroups"] = setting.UserUsableGroups2JSONString()
	common.OptionMap["CompletionRatio"] = ratio_setting.CompletionRatio2JSONString()
	common.OptionMap["ImageRatio"] = ratio_setting.ImageRatio2JSONString()
	common.OptionMap["AudioRatio"] = ratio_setting.AudioRatio2JSONString()
	common.OptionMap["AudioCompletionRatio"] = ratio_setting.AudioCompletionRatio2JSONString()
	common.OptionMap["TopUpLink"] = common.TopUpLink
	//common.OptionMap["ChatLink"] = common.ChatLink
	//common.OptionMap["ChatLink2"] = common.ChatLink2
	common.OptionMap["QuotaPerUnit"] = strconv.FormatFloat(common.QuotaPerUnit, 'f', -1, 64)
	common.OptionMap["RetryTimes"] = strconv.Itoa(common.RetryTimes)
	common.OptionMap["DataExportInterval"] = strconv.Itoa(common.DataExportInterval)
	common.OptionMap["DataExportDefaultTime"] = common.DataExportDefaultTime
	common.OptionMap["DefaultCollapseSidebar"] = strconv.FormatBool(common.DefaultCollapseSidebar)
	common.OptionMap["MjNotifyEnabled"] = strconv.FormatBool(setting.MjNotifyEnabled)
	common.OptionMap["MjAccountFilterEnabled"] = strconv.FormatBool(setting.MjAccountFilterEnabled)
	common.OptionMap["MjModeClearEnabled"] = strconv.FormatBool(setting.MjModeClearEnabled)
	common.OptionMap["MjForwardUrlEnabled"] = strconv.FormatBool(setting.MjForwardUrlEnabled)
	common.OptionMap["MjActionCheckSuccessEnabled"] = strconv.FormatBool(setting.MjActionCheckSuccessEnabled)
	common.OptionMap["CheckSensitiveEnabled"] = strconv.FormatBool(setting.CheckSensitiveEnabled)
	common.OptionMap["DemoSiteEnabled"] = strconv.FormatBool(operation_setting.DemoSiteEnabled)
	common.OptionMap["SelfUseModeEnabled"] = strconv.FormatBool(operation_setting.SelfUseModeEnabled)
	common.OptionMap["ModelRequestRateLimitEnabled"] = strconv.FormatBool(setting.ModelRequestRateLimitEnabled)
	common.OptionMap["CheckSensitiveOnPromptEnabled"] = strconv.FormatBool(setting.CheckSensitiveOnPromptEnabled)
	// Phase 5 — Sensitive UA/Prompt extended settings.
	// NOTE: CheckSensitiveAutoBanSyncEnabled / AutoBanSync are intentionally NOT
	// registered here — ban_sync is deprecated for this branch.
	common.OptionMap["CheckSensitiveOnUAEnabled"] = strconv.FormatBool(setting.CheckSensitiveOnUAEnabled)
	common.OptionMap["CheckSensitiveOnEmptyUAEnabled"] = strconv.FormatBool(setting.CheckSensitiveOnEmptyUAEnabled)
	common.OptionMap["CheckSensitiveOnEmptyUAAutoBanEnabled"] = strconv.FormatBool(setting.CheckSensitiveOnEmptyUAAutoBanEnabled)
	common.OptionMap["StopOnSensitiveEnabled"] = strconv.FormatBool(setting.StopOnSensitiveEnabled)
	common.OptionMap["SensitiveWords"] = setting.SensitiveWordsToString()
	common.OptionMap["SensitiveUABlockedRegexes"] = setting.UABlockedRegexesToString()
	common.OptionMap["SensitivePromptRegexRules"] = setting.SensitivePromptRegexRulesToString()
	common.OptionMap["SensitiveUARegexRules"] = setting.SensitiveUARegexRulesToString()
	common.OptionMap["SensitiveUAGroupRegexRules"] = setting.SensitiveUAGroupRegexRulesToString()
	common.OptionMap["SensitivePromptBlockedMessage"] = setting.SensitivePromptBlockedMessage
	common.OptionMap["SensitiveUABlockedMessage"] = setting.SensitiveUABlockedMessage
	common.OptionMap["SensitiveEmptyUABlockedMessage"] = setting.SensitiveEmptyUABlockedMessage
	common.OptionMap["SensitiveEmptyUABlockedHTTPStatusCode"] = strconv.Itoa(setting.SensitiveEmptyUABlockedHTTPStatusCode)
	common.OptionMap["SensitiveEmptyUABlockedErrorCode"] = setting.SensitiveEmptyUABlockedErrorCode
	common.OptionMap["StreamCacheQueueLength"] = strconv.Itoa(setting.StreamCacheQueueLength)
	common.OptionMap["AutomaticDisableKeywords"] = operation_setting.AutomaticDisableKeywordsToString()
	common.OptionMap["AutomaticDisableStatusCodes"] = operation_setting.AutomaticDisableStatusCodesToString()
	common.OptionMap["AutomaticRetryStatusCodes"] = operation_setting.AutomaticRetryStatusCodesToString()
	common.OptionMap["ExposeRatioEnabled"] = strconv.FormatBool(ratio_setting.IsExposeRatioEnabled())

	// 自动添加所有注册的模型配置
	modelConfigs := config.GlobalConfig.ExportAllConfigs()
	for k, v := range modelConfigs {
		common.OptionMap[k] = v
	}

	common.OptionMapRWMutex.Unlock()
	loadOptionsFromDatabase()
}

// isBannedBanSyncOptionKey reports whether `key` is a deprecated ban_sync
// legacy option key that must never be loaded, stored, or applied at runtime.
// ban_sync (gy's "auto joint ban" external bot integration) is deprecated for
// this branch: no route/table/model/service/dto depends on it, so its option
// keys must not enter common.OptionMap nor the options table.
//
// The match is case-insensitive on the key suffix/token to also catch legacy
// spellings like "AutoBanSync" / "CheckSensitiveAutoBanSyncEnabled".
func isBannedBanSyncOptionKey(key string) bool {
	k := strings.TrimSpace(key)
	if k == "" {
		return false
	}
	lower := strings.ToLower(k)
	if lower == "autobansync" || lower == "checksensitiveautobansyncenabled" {
		return true
	}
	// Also reject any dotted config key whose leaf token is a ban_sync key, e.g.
	// "ban_sync.enabled" or "ban_sync.foo" — none of these are registered configs
	// in this branch, so they are silently dropped rather than stored.
	if strings.HasPrefix(lower, "ban_sync.") {
		return true
	}
	return false
}

func loadOptionsFromDatabase() {
	relayErrorGovernanceUpdateMutex.Lock()
	defer relayErrorGovernanceUpdateMutex.Unlock()
	options, _ := AllOption()
	relayErrorGovernanceValues := make(map[string]string, 4)
	for _, option := range options {
		if isBannedBanSyncOptionKey(option.Key) {
			// Deprecated ban_sync legacy keys must never re-enter OptionMap.
			common.SysLog("skipping deprecated ban_sync option key on load: " + option.Key)
			continue
		}
		if system_setting.IsRelayErrorGovernanceOptionKey(option.Key) {
			relayErrorGovernanceValues[option.Key] = option.Value
			continue
		}
		err := updateOptionMap(option.Key, option.Value)
		if err != nil {
			common.SysLog("failed to update option map: " + err.Error())
		}
	}
	if len(relayErrorGovernanceValues) > 0 {
		prepared, err := prepareRelayErrorGovernanceRuntimeLoad(relayErrorGovernanceValues)
		if err != nil {
			common.SysLog("failed to rebuild relay error governance config: " + err.Error())
		} else if err := applyRelayErrorGovernanceRuntimeOptions(prepared); err != nil {
			common.SysLog("failed to publish relay error governance config: " + err.Error())
		}
	}
}

func SyncOptions(frequency int) {
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		common.SysLog("syncing options from database")
		loadOptionsFromDatabase()
	}
}

func UpdateOption(key string, value string) error {
	// Deprecated ban_sync legacy keys are rejected: not written to DB and not
	// stored in OptionMap. This prevents stale frontends/scripts from
	// re-persisting them. Returns nil so generic option callers succeed without
	// surfacing a ban_sync-specific error.
	if isBannedBanSyncOptionKey(key) {
		common.SysLog("rejecting deprecated ban_sync option key on update: " + key)
		return nil
	}
	if strings.HasPrefix(key, external_app_setting.OptionPrefix) {
		if !external_app_setting.IsOptionKey(key) {
			return fmt.Errorf("unsupported external game option %q", key)
		}
		return UpdateOptionsBulk(map[string]string{key: value})
	}
	// Relay error governance is a multi-key configuration. Route every aggregate
	// or dotted write through the bulk snapshot path so a caller cannot update one
	// runtime field while leaving the persisted aggregate stale.
	if system_setting.IsRelayErrorGovernanceOptionKey(key) {
		return UpdateOptionsBulk(map[string]string{key: value})
	}
	// Validate sensitive regex options before writing to DB so internal callers
	// that bypass the controller cannot persist illegal regex/status/code.
	if err := setting.ValidateSensitiveRegexOptions(key, value); err != nil {
		return err
	}
	// Phase 10C — validate/normalize the short-message extra billing config
	// so internal callers that bypass the controller cannot persist an
	// invalid config (and `config.UpdateConfigFromMap` no longer silently
	// drops malformed JSON). The normalized JSON string is what gets written
	// to the DB and OptionMap, keeping the persisted format stable.
	normalizedValue, err := operation_setting.NormalizeShortMsgExtraBillingOption(key, value)
	if err != nil {
		return err
	}
	normalizedValue, err = system_setting.NormalizeRelayBatchSplitOption(key, normalizedValue)
	if err != nil {
		return err
	}
	normalizedValue, err = system_setting.NormalizeRelayErrorGovernanceOption(key, normalizedValue)
	if err != nil {
		return err
	}
	value = normalizedValue
	// Save to database first
	option := Option{
		Key: key,
	}
	// https://gorm.io/docs/update.html#Save-All-Fields
	DB.FirstOrCreate(&option, Option{Key: key})
	option.Value = value
	// Save is a combination function.
	// If save value does not contain primary key, it will execute Create,
	// otherwise it will execute Update (with all fields).
	DB.Save(&option)
	// Update OptionMap
	return updateOptionMap(key, value)
}

// UpdateOptionsBulk persists multiple key/value pairs in a single database
// transaction, then dispatches them through updateOptionMap in one pass. If
// any DB write fails the whole transaction rolls back and no in-memory state
// is touched — safe for callers that must commit a set of related options
// atomically (e.g. payment gateway binding).
//
// Deprecated ban_sync legacy keys inside `values` are silently skipped: not
// written to DB, not stored in OptionMap.
func UpdateOptionsBulk(values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	hasExternalGame := false
	for key := range values {
		if strings.HasPrefix(key, external_app_setting.OptionPrefix) {
			hasExternalGame = true
			break
		}
	}
	if hasExternalGame {
		for key := range values {
			if !external_app_setting.IsOptionKey(key) {
				return fmt.Errorf("external game options cannot be mixed with unrelated option %q", key)
			}
		}
		return updateExternalGameOptionsAtomic(values)
	}
	hasRelayErrorGovernance := false
	for key := range values {
		if system_setting.IsRelayErrorGovernanceOptionKey(key) {
			hasRelayErrorGovernance = true
			break
		}
	}
	if hasRelayErrorGovernance {
		relayErrorGovernanceUpdateMutex.Lock()
		defer relayErrorGovernanceUpdateMutex.Unlock()
		if config.GlobalConfig.Get(system_setting.RelayErrorGovernanceOptionKey) == nil {
			return fmt.Errorf("relay error governance config is not registered")
		}
		for key := range values {
			if !system_setting.IsRelayErrorGovernanceOptionKey(key) && !isBannedBanSyncOptionKey(key) {
				return fmt.Errorf("relay error governance options cannot be mixed with unrelated option %q", key)
			}
		}
	}

	preparedValues := values
	var relayErrorGovernanceSetting *system_setting.RelayErrorGovernanceSetting
	if hasRelayErrorGovernance {
		var err error
		preparedValues, relayErrorGovernanceSetting, err = prepareRelayErrorGovernanceBulk(values)
		if err != nil {
			return err
		}
	}
	// Filter out deprecated ban_sync legacy keys before touching the DB.
	filtered := make(map[string]string, len(preparedValues))
	for k, v := range preparedValues {
		if isBannedBanSyncOptionKey(k) {
			common.SysLog("rejecting deprecated ban_sync option key on bulk update: " + k)
			continue
		}
		// Validate sensitive regex options so internal callers cannot persist
		// illegal regex/status/code via the bulk path.
		if err := setting.ValidateSensitiveRegexOptions(k, v); err != nil {
			return err
		}
		// Phase 10C — validate/normalize the short-message extra billing
		// config so internal callers cannot persist an invalid config via
		// the bulk path either. Substitute the normalized JSON string for
		// the original so the persisted shape stays stable.
		normalizedValue, err := operation_setting.NormalizeShortMsgExtraBillingOption(k, v)
		if err != nil {
			return err
		}
		normalizedValue, err = system_setting.NormalizeRelayBatchSplitOption(k, normalizedValue)
		if err != nil {
			return err
		}
		normalizedValue, err = system_setting.NormalizeRelayErrorGovernanceOption(k, normalizedValue)
		if err != nil {
			return err
		}
		filtered[k] = normalizedValue
	}
	if len(filtered) == 0 {
		return nil
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		for k, v := range filtered {
			option := Option{Key: k}
			if err := tx.FirstOrCreate(&option, Option{Key: k}).Error; err != nil {
				return err
			}
			option.Value = v
			if err := tx.Save(&option).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if relayErrorGovernanceSetting != nil {
		governanceValues := make(map[string]string, 4)
		for key, value := range filtered {
			if system_setting.IsRelayErrorGovernanceOptionKey(key) {
				governanceValues[key] = value
			}
		}
		if err := applyRelayErrorGovernanceRuntimeOptions(governanceValues); err != nil {
			return err
		}
	}
	for k, v := range filtered {
		if system_setting.IsRelayErrorGovernanceOptionKey(k) {
			continue
		}
		if err := updateOptionMap(k, v); err != nil {
			return err
		}
	}
	return nil
}

// UpdateRelayErrorGovernanceSetting validates and atomically persists the
// aggregate governance setting together with the dotted keys that drive the
// live config. All callers use this helper so no write path can silently drift
// from the runtime configuration.
func UpdateRelayErrorGovernanceSetting(input system_setting.RelayErrorGovernanceSetting) (system_setting.RelayErrorGovernanceSetting, error) {
	normalized, err := system_setting.NormalizeRelayErrorGovernanceSetting(input)
	if err != nil {
		return system_setting.RelayErrorGovernanceSetting{}, err
	}
	values, err := marshalRelayErrorGovernanceOptions(normalized)
	if err != nil {
		return system_setting.RelayErrorGovernanceSetting{}, err
	}
	if err := UpdateOptionsBulk(values); err != nil {
		return system_setting.RelayErrorGovernanceSetting{}, err
	}
	return normalized, nil
}

// prepareRelayErrorGovernanceRuntimeLoad keeps legacy reads deliberately more
// permissive than writes: syntactically valid historical rules are loaded even
// when the current strict writer would reject their status/pattern/code. The
// runtime classifier still applies its defensive fallbacks. At the same time we
// rebuild one coherent four-key snapshot so malformed or stale aggregate values
// cannot make DB, OptionMap, registered config, and runtime disagree.
func prepareRelayErrorGovernanceRuntimeLoad(values map[string]string) (map[string]string, error) {
	current := system_setting.GetRelayErrorGovernanceSetting()
	if current == nil {
		return nil, fmt.Errorf("relay error governance runtime is unavailable")
	}
	candidate := *current
	hasDotted := false
	for _, key := range []string{
		system_setting.RelayErrorGovernanceEnabledOptionKey,
		system_setting.RelayErrorGovernanceRulesOptionKey,
		system_setting.RelayErrorGovernanceCustomRulesOptionKey,
	} {
		if _, ok := values[key]; ok {
			hasDotted = true
			break
		}
	}
	if raw, ok := values[system_setting.RelayErrorGovernanceOptionKey]; ok {
		var aggregate system_setting.RelayErrorGovernanceSetting
		if err := common.UnmarshalJsonStr(raw, &aggregate); err != nil {
			if !hasDotted {
				return nil, fmt.Errorf("persisted %s must be valid JSON: %w", system_setting.RelayErrorGovernanceOptionKey, err)
			}
			common.SysLog("ignoring malformed persisted relay_error_governance aggregate because dotted runtime keys are available")
		} else {
			candidate = aggregate
		}
	}
	if raw, ok := values[system_setting.RelayErrorGovernanceEnabledOptionKey]; ok {
		enabled, err := strconv.ParseBool(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("persisted %s must be a boolean", system_setting.RelayErrorGovernanceEnabledOptionKey)
		}
		candidate.Enabled = enabled
	}
	if raw, ok := values[system_setting.RelayErrorGovernanceRulesOptionKey]; ok {
		var rules map[string]system_setting.RelayErrorGovernanceRuleConfig
		if err := common.UnmarshalJsonStr(raw, &rules); err != nil {
			return nil, fmt.Errorf("persisted %s must be valid JSON: %w", system_setting.RelayErrorGovernanceRulesOptionKey, err)
		}
		candidate.Rules = rules
	}
	if raw, ok := values[system_setting.RelayErrorGovernanceCustomRulesOptionKey]; ok {
		var customRules []system_setting.RelayErrorGovernanceCustomRuleConfig
		if err := common.UnmarshalJsonStr(raw, &customRules); err != nil {
			return nil, fmt.Errorf("persisted %s must be valid JSON: %w", system_setting.RelayErrorGovernanceCustomRulesOptionKey, err)
		}
		candidate.CustomRules = customRules
	}
	return marshalRelayErrorGovernanceOptions(candidate)
}

func prepareRelayErrorGovernanceBulk(values map[string]string) (map[string]string, *system_setting.RelayErrorGovernanceSetting, error) {
	current := system_setting.GetRelayErrorGovernanceSetting()
	if current == nil {
		return nil, nil, fmt.Errorf("relay error governance runtime is unavailable")
	}
	candidate := *current
	aggregateSupplied := false
	if raw, ok := values[system_setting.RelayErrorGovernanceOptionKey]; ok {
		parsed, _, err := system_setting.ParseAndValidateRelayErrorGovernanceSetting(raw)
		if err != nil {
			return nil, nil, err
		}
		candidate = parsed
		aggregateSupplied = true
	}

	for key := range values {
		if !system_setting.IsRelayErrorGovernanceOptionKey(key) {
			continue
		}
		if _, err := system_setting.NormalizeRelayErrorGovernanceOption(key, values[key]); err != nil {
			return nil, nil, err
		}
	}

	for _, key := range []string{
		system_setting.RelayErrorGovernanceEnabledOptionKey,
		system_setting.RelayErrorGovernanceRulesOptionKey,
		system_setting.RelayErrorGovernanceCustomRulesOptionKey,
	} {
		raw, ok := values[key]
		if !ok {
			continue
		}
		updated, err := applyRelayErrorGovernanceOption(candidate, key, raw)
		if err != nil {
			return nil, nil, err
		}
		if aggregateSupplied && !relayErrorGovernanceFieldEqual(candidate, updated, key) {
			return nil, nil, fmt.Errorf("%s conflicts with %s", key, system_setting.RelayErrorGovernanceOptionKey)
		}
		candidate = updated
	}

	normalized, err := system_setting.NormalizeRelayErrorGovernanceSetting(candidate)
	if err != nil {
		return nil, nil, err
	}
	governanceValues, err := marshalRelayErrorGovernanceOptions(normalized)
	if err != nil {
		return nil, nil, err
	}
	prepared := make(map[string]string, len(values)+4)
	for key, value := range values {
		if !system_setting.IsRelayErrorGovernanceOptionKey(key) {
			prepared[key] = value
		}
	}
	for key, value := range governanceValues {
		prepared[key] = value
	}
	return prepared, &normalized, nil
}

func applyRelayErrorGovernanceOption(setting system_setting.RelayErrorGovernanceSetting, key string, value string) (system_setting.RelayErrorGovernanceSetting, error) {
	normalized, err := system_setting.NormalizeRelayErrorGovernanceOption(key, value)
	if err != nil {
		return system_setting.RelayErrorGovernanceSetting{}, err
	}
	switch key {
	case system_setting.RelayErrorGovernanceEnabledOptionKey:
		setting.Enabled, err = strconv.ParseBool(normalized)
	case system_setting.RelayErrorGovernanceRulesOptionKey:
		err = common.UnmarshalJsonStr(normalized, &setting.Rules)
	case system_setting.RelayErrorGovernanceCustomRulesOptionKey:
		err = common.UnmarshalJsonStr(normalized, &setting.CustomRules)
	default:
		return system_setting.RelayErrorGovernanceSetting{}, fmt.Errorf("unsupported relay error governance option %q", key)
	}
	if err != nil {
		return system_setting.RelayErrorGovernanceSetting{}, err
	}
	return setting, nil
}

func relayErrorGovernanceFieldEqual(left system_setting.RelayErrorGovernanceSetting, right system_setting.RelayErrorGovernanceSetting, key string) bool {
	switch key {
	case system_setting.RelayErrorGovernanceEnabledOptionKey:
		return left.Enabled == right.Enabled
	case system_setting.RelayErrorGovernanceRulesOptionKey:
		if len(left.Rules) == 0 && len(right.Rules) == 0 {
			return true
		}
		return reflect.DeepEqual(left.Rules, right.Rules)
	case system_setting.RelayErrorGovernanceCustomRulesOptionKey:
		if len(left.CustomRules) == 0 && len(right.CustomRules) == 0 {
			return true
		}
		return reflect.DeepEqual(left.CustomRules, right.CustomRules)
	default:
		return false
	}
}

func marshalRelayErrorGovernanceOptions(setting system_setting.RelayErrorGovernanceSetting) (map[string]string, error) {
	rules, err := common.Marshal(setting.Rules)
	if err != nil {
		return nil, err
	}
	customRules, err := common.Marshal(setting.CustomRules)
	if err != nil {
		return nil, err
	}
	full, err := common.Marshal(setting)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		system_setting.RelayErrorGovernanceEnabledOptionKey:     strconv.FormatBool(setting.Enabled),
		system_setting.RelayErrorGovernanceRulesOptionKey:       string(rules),
		system_setting.RelayErrorGovernanceCustomRulesOptionKey: string(customRules),
		system_setting.RelayErrorGovernanceOptionKey:            string(full),
	}, nil
}

func applyRelayErrorGovernanceRuntimeOptions(values map[string]string) error {
	configMap := make(map[string]string, 3)
	if value, ok := values[system_setting.RelayErrorGovernanceEnabledOptionKey]; ok {
		configMap["enabled"] = value
	}
	if value, ok := values[system_setting.RelayErrorGovernanceRulesOptionKey]; ok {
		configMap["rules"] = value
	}
	if value, ok := values[system_setting.RelayErrorGovernanceCustomRulesOptionKey]; ok {
		configMap["custom_rules"] = value
	}
	if len(configMap) == 0 {
		return fmt.Errorf("relay error governance runtime options are incomplete")
	}
	found, err := config.GlobalConfig.Update(system_setting.RelayErrorGovernanceOptionKey, configMap)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("relay error governance config is not registered")
	}
	system_setting.RebuildRelayErrorGovernanceRuntime()
	common.OptionMapRWMutex.Lock()
	for key, value := range values {
		common.OptionMap[key] = value
	}
	common.OptionMapRWMutex.Unlock()
	return nil
}

func updateOptionMap(key string, value string) (err error) {
	common.OptionMapRWMutex.Lock()
	defer common.OptionMapRWMutex.Unlock()
	common.OptionMap[key] = value

	// 检查是否是模型配置 - 使用更规范的方式处理
	if handleConfigUpdate(key, value) {
		return nil // 已由配置系统处理
	}

	// 处理传统配置项...
	if strings.HasSuffix(key, "Permission") {
		intValue, _ := strconv.Atoi(value)
		switch key {
		case "FileUploadPermission":
			common.FileUploadPermission = intValue
		case "FileDownloadPermission":
			common.FileDownloadPermission = intValue
		case "ImageUploadPermission":
			common.ImageUploadPermission = intValue
		case "ImageDownloadPermission":
			common.ImageDownloadPermission = intValue
		}
	}
	if strings.HasSuffix(key, "Enabled") || key == "DefaultCollapseSidebar" || key == "DefaultUseAutoGroup" || key == "SMTPForceAuthLogin" || key == "SMTPInsecureSkipVerify" {
		boolValue := value == "true"
		switch key {
		case "PasswordRegisterEnabled":
			common.PasswordRegisterEnabled = boolValue
		case "PasswordLoginEnabled":
			common.PasswordLoginEnabled = boolValue
		case "EmailVerificationEnabled":
			common.EmailVerificationEnabled = boolValue
		case "GitHubOAuthEnabled":
			common.GitHubOAuthEnabled = boolValue
		case "LinuxDOOAuthEnabled":
			common.LinuxDOOAuthEnabled = boolValue
		case "WeChatAuthEnabled":
			common.WeChatAuthEnabled = boolValue
		case "TelegramOAuthEnabled":
			common.TelegramOAuthEnabled = boolValue
		case "TurnstileCheckEnabled":
			common.TurnstileCheckEnabled = boolValue
		case "RegisterEnabled":
			common.RegisterEnabled = boolValue
		case "EmailDomainRestrictionEnabled":
			common.EmailDomainRestrictionEnabled = boolValue
		case "EmailAliasRestrictionEnabled":
			common.EmailAliasRestrictionEnabled = boolValue
		case "AutomaticDisableChannelEnabled":
			common.AutomaticDisableChannelEnabled = boolValue
		case "AutomaticEnableChannelEnabled":
			common.AutomaticEnableChannelEnabled = boolValue
		case "LogConsumeEnabled":
			common.LogConsumeEnabled = boolValue
		case "DisplayInCurrencyEnabled":
			// 兼容旧字段：同步到新配置 general_setting.quota_display_type（运行时生效）
			// true -> USD, false -> TOKENS
			newVal := "USD"
			if !boolValue {
				newVal = "TOKENS"
			}
			if cfg := config.GlobalConfig.Get("general_setting"); cfg != nil {
				_ = config.UpdateConfigFromMap(cfg, map[string]string{"quota_display_type": newVal})
			}
		case "DisplayTokenStatEnabled":
			common.DisplayTokenStatEnabled = boolValue
		case "DrawingEnabled":
			common.DrawingEnabled = boolValue
		case "TaskEnabled":
			common.TaskEnabled = boolValue
		case "DataExportEnabled":
			common.DataExportEnabled = boolValue
		case "DefaultCollapseSidebar":
			common.DefaultCollapseSidebar = boolValue
		case "MjNotifyEnabled":
			setting.MjNotifyEnabled = boolValue
		case "MjAccountFilterEnabled":
			setting.MjAccountFilterEnabled = boolValue
		case "MjModeClearEnabled":
			setting.MjModeClearEnabled = boolValue
		case "MjForwardUrlEnabled":
			setting.MjForwardUrlEnabled = boolValue
		case "MjActionCheckSuccessEnabled":
			setting.MjActionCheckSuccessEnabled = boolValue
		case "CheckSensitiveEnabled":
			setting.CheckSensitiveEnabled = boolValue
		case "DemoSiteEnabled":
			operation_setting.DemoSiteEnabled = boolValue
		case "SelfUseModeEnabled":
			operation_setting.SelfUseModeEnabled = boolValue
		case "CheckSensitiveOnPromptEnabled":
			setting.CheckSensitiveOnPromptEnabled = boolValue
		case "CheckSensitiveOnUAEnabled":
			setting.CheckSensitiveOnUAEnabled = boolValue
		case "CheckSensitiveOnEmptyUAEnabled":
			setting.CheckSensitiveOnEmptyUAEnabled = boolValue
		case "CheckSensitiveOnEmptyUAAutoBanEnabled":
			setting.CheckSensitiveOnEmptyUAAutoBanEnabled = boolValue
		case "ModelRequestRateLimitEnabled":
			setting.ModelRequestRateLimitEnabled = boolValue
		case "StopOnSensitiveEnabled":
			setting.StopOnSensitiveEnabled = boolValue
		case "SMTPSSLEnabled":
			common.SMTPSSLEnabled = boolValue
		case "SMTPStartTLSEnabled":
			common.SMTPStartTLSEnabled = boolValue
		case "SMTPInsecureSkipVerify":
			common.SMTPInsecureSkipVerify = boolValue
		case "SMTPForceAuthLogin":
			common.SMTPForceAuthLogin = boolValue
		case "WorkerAllowHttpImageRequestEnabled":
			system_setting.WorkerAllowHttpImageRequestEnabled = boolValue
		case "DefaultUseAutoGroup":
			setting.DefaultUseAutoGroup = boolValue
		case "ExposeRatioEnabled":
			ratio_setting.SetExposeRatioEnabled(boolValue)
		}
	}
	switch key {
	case "EmailDomainWhitelist":
		common.EmailDomainWhitelist = strings.Split(value, ",")
	case "SMTPServer":
		common.SMTPServer = value
	case "SMTPPort":
		intValue, _ := strconv.Atoi(value)
		common.SMTPPort = intValue
	case "SMTPAccount":
		common.SMTPAccount = value
	case "SMTPFrom":
		common.SMTPFrom = value
	case "SMTPToken":
		common.SMTPToken = value
	case "ServerAddress":
		system_setting.ServerAddress = value
	case "WorkerUrl":
		system_setting.WorkerUrl = value
	case "WorkerValidKey":
		system_setting.WorkerValidKey = value
	case "PayAddress":
		operation_setting.PayAddress = value
	case "Chats":
		err = setting.UpdateChatsByJsonString(value)
	case "AutoGroups":
		err = setting.UpdateAutoGroupsByJsonString(value)
	case "CustomCallbackAddress":
		operation_setting.CustomCallbackAddress = value
	case "EpayId":
		operation_setting.EpayId = value
	case "EpayKey":
		operation_setting.EpayKey = value
	case "Price":
		operation_setting.Price, _ = strconv.ParseFloat(value, 64)
	case "USDExchangeRate":
		operation_setting.USDExchangeRate, _ = strconv.ParseFloat(value, 64)
	case "MinTopUp":
		operation_setting.MinTopUp, _ = strconv.Atoi(value)
	case "StripeApiSecret":
		setting.StripeApiSecret = value
	case "StripeWebhookSecret":
		setting.StripeWebhookSecret = value
	case "StripePriceId":
		setting.StripePriceId = value
	case "StripeUnitPrice":
		setting.StripeUnitPrice, _ = strconv.ParseFloat(value, 64)
	case "StripeMinTopUp":
		setting.StripeMinTopUp, _ = strconv.Atoi(value)
	case "StripePromotionCodesEnabled":
		setting.StripePromotionCodesEnabled = value == "true"
	case "CreemApiKey":
		setting.CreemApiKey = value
	case "CreemProducts":
		setting.CreemProducts = value
	case "CreemTestMode":
		setting.CreemTestMode = value == "true"
	case "CreemWebhookSecret":
		setting.CreemWebhookSecret = value
	case "WaffoEnabled":
		setting.WaffoEnabled = value == "true"
	case "WaffoApiKey":
		setting.WaffoApiKey = value
	case "WaffoPrivateKey":
		setting.WaffoPrivateKey = value
	case "WaffoPublicCert":
		setting.WaffoPublicCert = value
	case "WaffoSandboxPublicCert":
		setting.WaffoSandboxPublicCert = value
	case "WaffoSandboxApiKey":
		setting.WaffoSandboxApiKey = value
	case "WaffoSandboxPrivateKey":
		setting.WaffoSandboxPrivateKey = value
	case "WaffoSandbox":
		setting.WaffoSandbox = value == "true"
	case "WaffoMerchantId":
		setting.WaffoMerchantId = value
	case "WaffoNotifyUrl":
		setting.WaffoNotifyUrl = value
	case "WaffoReturnUrl":
		setting.WaffoReturnUrl = value
	case "WaffoSubscriptionReturnUrl":
		setting.WaffoSubscriptionReturnUrl = value
	case "WaffoCurrency":
		setting.WaffoCurrency = value
	case "WaffoUnitPrice":
		setting.WaffoUnitPrice, _ = strconv.ParseFloat(value, 64)
	case "WaffoMinTopUp":
		setting.WaffoMinTopUp, _ = strconv.Atoi(value)
	case "WaffoPancakeMerchantID":
		setting.WaffoPancakeMerchantID = value
	case "WaffoPancakePrivateKey":
		setting.WaffoPancakePrivateKey = value
	case "WaffoPancakeReturnURL":
		setting.WaffoPancakeReturnURL = value
	case "WaffoPancakeStoreID":
		setting.WaffoPancakeStoreID = value
	case "WaffoPancakeProductID":
		setting.WaffoPancakeProductID = value
	case "WaffoPancakeUnitPrice":
		setting.WaffoPancakeUnitPrice, _ = strconv.ParseFloat(value, 64)
	case "WaffoPancakeMinTopUp":
		setting.WaffoPancakeMinTopUp, _ = strconv.Atoi(value)
	case "TopupGroupRatio":
		err = common.UpdateTopupGroupRatioByJSONString(value)
	case "GitHubClientId":
		common.GitHubClientId = value
	case "GitHubClientSecret":
		common.GitHubClientSecret = value
	case "LinuxDOClientId":
		common.LinuxDOClientId = value
	case "LinuxDOClientSecret":
		common.LinuxDOClientSecret = value
	case "LinuxDOMinimumTrustLevel":
		common.LinuxDOMinimumTrustLevel, _ = strconv.Atoi(value)
	case "Footer":
		common.Footer = value
	case "SystemName":
		common.SystemName = value
	case "Logo":
		common.Logo = value
	case "WeChatServerAddress":
		common.WeChatServerAddress = value
	case "WeChatServerToken":
		common.WeChatServerToken = value
	case "WeChatAccountQRCodeImageURL":
		common.WeChatAccountQRCodeImageURL = value
	case "TelegramBotToken":
		common.TelegramBotToken = value
	case "TelegramBotName":
		common.TelegramBotName = value
	case "TurnstileSiteKey":
		common.TurnstileSiteKey = value
	case "TurnstileSecretKey":
		common.TurnstileSecretKey = value
	case "QuotaForNewUser":
		common.QuotaForNewUser, _ = strconv.Atoi(value)
	case "QuotaForInviter":
		common.QuotaForInviter, _ = strconv.Atoi(value)
	case "QuotaForInvitee":
		common.QuotaForInvitee, _ = strconv.Atoi(value)
	case "QuotaRemindThreshold":
		common.QuotaRemindThreshold, _ = strconv.Atoi(value)
	case "PreConsumedQuota":
		common.PreConsumedQuota, _ = strconv.Atoi(value)
	case "ModelRequestRateLimitCount":
		setting.ModelRequestRateLimitCount, _ = strconv.Atoi(value)
	case "ModelRequestRateLimitDurationMinutes":
		setting.ModelRequestRateLimitDurationMinutes, _ = strconv.Atoi(value)
	case "ModelRequestRateLimitSuccessCount":
		setting.ModelRequestRateLimitSuccessCount, _ = strconv.Atoi(value)
	case "ModelRequestRateLimitGroup":
		err = setting.UpdateModelRequestRateLimitGroupByJSONString(value)
	case "RetryTimes":
		common.RetryTimes, _ = strconv.Atoi(value)
	case "DataExportInterval":
		common.DataExportInterval, _ = strconv.Atoi(value)
	case "DataExportDefaultTime":
		common.DataExportDefaultTime = value
	case "ModelRatio":
		err = ratio_setting.UpdateModelRatioByJSONString(value)
	case "GroupRatio":
		err = ratio_setting.UpdateGroupRatioByJSONString(value)
	case "GroupGroupRatio":
		err = ratio_setting.UpdateGroupGroupRatioByJSONString(value)
	case "UserUsableGroups":
		err = setting.UpdateUserUsableGroupsByJSONString(value)
	case "CompletionRatio":
		err = ratio_setting.UpdateCompletionRatioByJSONString(value)
	case "ModelPrice":
		err = ratio_setting.UpdateModelPriceByJSONString(value)
	case "CacheRatio":
		err = ratio_setting.UpdateCacheRatioByJSONString(value)
	case "CreateCacheRatio":
		err = ratio_setting.UpdateCreateCacheRatioByJSONString(value)
	case "ImageRatio":
		err = ratio_setting.UpdateImageRatioByJSONString(value)
	case "AudioRatio":
		err = ratio_setting.UpdateAudioRatioByJSONString(value)
	case "AudioCompletionRatio":
		err = ratio_setting.UpdateAudioCompletionRatioByJSONString(value)
	case "TopUpLink":
		common.TopUpLink = value
	//case "ChatLink":
	//	common.ChatLink = value
	//case "ChatLink2":
	//	common.ChatLink2 = value
	case "ChannelDisableThreshold":
		common.ChannelDisableThreshold, _ = strconv.ParseFloat(value, 64)
	case "QuotaPerUnit":
		common.QuotaPerUnit, _ = strconv.ParseFloat(value, 64)
	case "SensitiveWords":
		setting.SensitiveWordsFromString(value)
	case "SensitiveUABlockedRegexes":
		setting.UABlockedRegexesFromString(value)
	case "SensitivePromptRegexRules":
		setting.SensitivePromptRegexRulesFromString(value)
	case "SensitiveUARegexRules":
		setting.SensitiveUARegexRulesFromString(value)
	case "SensitiveUAGroupRegexRules":
		setting.SensitiveUAGroupRegexRulesFromString(value)
	case "SensitivePromptBlockedMessage":
		setting.SensitivePromptBlockedMessage = strings.TrimSpace(value)
		if setting.SensitivePromptBlockedMessage == "" {
			setting.SensitivePromptBlockedMessage = "请求包含违规内容，已被系统拦截"
		}
	case "SensitiveUABlockedMessage":
		setting.SensitiveUABlockedMessage = strings.TrimSpace(value)
		if setting.SensitiveUABlockedMessage == "" {
			setting.SensitiveUABlockedMessage = "当前请求来源已被系统策略拦截"
		}
	case "SensitiveEmptyUABlockedMessage":
		setting.SensitiveEmptyUABlockedMessage = strings.TrimSpace(value)
	case "SensitiveEmptyUABlockedHTTPStatusCode":
		setting.SensitiveEmptyUABlockedHTTPStatusCode, _ = strconv.Atoi(strings.TrimSpace(value))
		if setting.SensitiveEmptyUABlockedHTTPStatusCode < 100 || setting.SensitiveEmptyUABlockedHTTPStatusCode > 599 {
			setting.SensitiveEmptyUABlockedHTTPStatusCode = setting.DefaultSensitiveStatusCode
		}
	case "SensitiveEmptyUABlockedErrorCode":
		setting.SensitiveEmptyUABlockedErrorCode = strings.TrimSpace(value)
		if setting.SensitiveEmptyUABlockedErrorCode == "" {
			setting.SensitiveEmptyUABlockedErrorCode = setting.DefaultSensitiveErrorCode
		}
	case "AutomaticDisableKeywords":
		operation_setting.AutomaticDisableKeywordsFromString(value)
	case "AutomaticDisableStatusCodes":
		err = operation_setting.AutomaticDisableStatusCodesFromString(value)
	case "AutomaticRetryStatusCodes":
		err = operation_setting.AutomaticRetryStatusCodesFromString(value)
	case "StreamCacheQueueLength":
		setting.StreamCacheQueueLength, _ = strconv.Atoi(value)
	case "PayMethods":
		err = operation_setting.UpdatePayMethodsByJsonString(value)
	case "WaffoPayMethods":
		// WaffoPayMethods is read directly from OptionMap via setting.GetWaffoPayMethods().
		// The value is already stored in OptionMap at the top of this function (line: common.OptionMap[key] = value).
		// No additional in-memory variable to update.
	}
	return err
}

// handleConfigUpdate 处理分层配置更新，返回是否已处理
func handleConfigUpdate(key, value string) bool {
	parts := strings.SplitN(key, ".", 2)
	if len(parts) != 2 {
		return false // 不是分层配置
	}

	configName := parts[0]
	configKey := parts[1]

	// Apply the field while holding the config manager lock. Returning a live
	// pointer from Get and mutating it here used to race with config exports and
	// runtime readers.
	handled, err := config.GlobalConfig.Update(configName, map[string]string{
		configKey: value,
	})
	if err != nil || !handled {
		return false // 未注册的配置
	}

	// 特定配置的后处理
	if configName == "performance_setting" {
		performance_setting.UpdateAndSync()
	} else if configName == "tool_price_setting" {
		operation_setting.RebuildToolPriceIndex()
	} else if configName == "billing_setting" {
		InvalidatePricingCache()
		ratio_setting.InvalidateExposedDataCache()
	} else if configName == "theme" {
		system_setting.UpdateAndSyncTheme()
	} else if configName == "relay_batch_split" {
		system_setting.RebuildRelayBatchSplitRuntime()
	} else if configName == system_setting.RelayErrorGovernanceOptionKey {
		system_setting.RebuildRelayErrorGovernanceRuntime()
	}

	return true // 已处理
}
