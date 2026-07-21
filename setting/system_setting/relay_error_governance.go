package system_setting

import (
	"sync/atomic"

	"github.com/QuantumNous/new-api/setting/config"
)

const (
	RelayErrorGovernanceOptionKey            = "relay_error_governance"
	RelayErrorGovernanceEnabledOptionKey     = RelayErrorGovernanceOptionKey + ".enabled"
	RelayErrorGovernanceRulesOptionKey       = RelayErrorGovernanceOptionKey + ".rules"
	RelayErrorGovernanceCustomRulesOptionKey = RelayErrorGovernanceOptionKey + ".custom_rules"
)

// Built-in relay error rule codes are defined in the setting package so both
// configuration validation and the runtime classifier share one canonical
// vocabulary without creating an import cycle.
const (
	RelayErrorRuleInsufficientUserQuota = "insufficient_user_quota"
	RelayErrorRuleInvalidMaxTokens      = "invalid_max_tokens"
	RelayErrorRuleMaxTokensNeedStream   = "max_tokens_requires_stream"
	RelayErrorRuleInvalidBudgetTokens   = "invalid_budget_tokens"
	RelayErrorRuleInvalidStreamOptions  = "invalid_stream_options"
	RelayErrorRuleInvalidMessageRole    = "invalid_message_role"
	RelayErrorRuleInvalidImageURL       = "invalid_image_url"
	RelayErrorRuleContextLengthExceeded = "context_length_exceeded"
	RelayErrorRuleContentFiltered       = "content_filtered"
	RelayErrorRuleModelNotFound         = "model_not_found"
	RelayErrorRuleNoAvailableChannel    = "no_available_channel"
	RelayErrorRuleModelNotPermitted     = "model_not_permitted"
	RelayErrorRuleRiskControlRestricted = "risk_control_restricted"
	RelayErrorRuleBehaviorBanned        = "behavior_banned"
	RelayErrorRuleUpstreamRateLimited   = "upstream_rate_limited"
	RelayErrorRuleUpstreamTimeout       = "upstream_timeout"
	RelayErrorRuleUpstreamBadResponse   = "upstream_bad_response"
	RelayErrorRuleUpstreamUnavailable   = "upstream_unavailable"
	RelayErrorRuleStreamInterrupted     = "stream_interrupted"
	RelayErrorRuleInternalError         = "internal_error"
)

// RelayErrorGovernanceRuleConfig is the admin-configurable overlay for a single
// governance rule. Only Enabled and Message can be overridden — the rule's
// Status/Type/Code/Param are fixed in the code (security: admin can't change
// HTTP status codes or error types, only enable/disable and customize message).
type RelayErrorGovernanceRuleConfig struct {
	Enabled *bool  `json:"enabled,omitempty"`
	Message string `json:"message,omitempty"`
}

type RelayErrorGovernanceCustomRuleConfig struct {
	Enabled          bool   `json:"enabled"`
	RuleCode         string `json:"rule_code"`
	Category         string `json:"category,omitempty"`
	MatchType        string `json:"match_type"`
	MatchPattern     string `json:"match_pattern"`
	SafeErrorCode    string `json:"safe_error_code"`
	SafeErrorType    string `json:"safe_error_type"`
	SafeErrorMessage string `json:"safe_error_message"`
	StatusCode       int    `json:"status_code,omitempty"`
}

// RelayErrorGovernanceSetting is the registered system-level config for relay
// error governance. Admins configure this in System Settings.
type RelayErrorGovernanceSetting struct {
	// Enabled controls the global governance toggle. When false, governance
	// falls back to masked original messages (pre-existing behavior).
	Enabled bool `json:"enabled"`
	// Rules is an optional per-rule override map keyed by rule code. Missing
	// rules use built-in defaults. This mirrors the JSON structure:
	// {"version":1,"rules":{"internal_error":{"enabled":false,"message":"..."}}}
	Rules       map[string]RelayErrorGovernanceRuleConfig `json:"rules,omitempty"`
	CustomRules []RelayErrorGovernanceCustomRuleConfig    `json:"custom_rules,omitempty"`
}

var defaultRelayErrorGovernanceSetting = RelayErrorGovernanceSetting{
	Enabled: true,
}

var relayErrorGovernanceSnapshot atomic.Pointer[RelayErrorGovernanceSetting]

func init() {
	config.GlobalConfig.Register(RelayErrorGovernanceOptionKey, &defaultRelayErrorGovernanceSetting)
	RebuildRelayErrorGovernanceRuntime()
}

// RebuildRelayErrorGovernanceRuntime publishes one immutable configuration
// snapshot. The registered config is copied while the config manager read lock
// is held, so readers never observe partially-updated Rules/CustomRules fields.
func RebuildRelayErrorGovernanceRuntime() {
	setting := RelayErrorGovernanceSetting{Enabled: true}
	if found, err := config.GlobalConfig.Snapshot(RelayErrorGovernanceOptionKey, &setting); err != nil || !found {
		setting = RelayErrorGovernanceSetting{Enabled: true}
	}
	PublishRelayErrorGovernanceRuntime(setting)
}

// PublishRelayErrorGovernanceRuntime atomically replaces the immutable runtime
// snapshot. Normal write paths update the registered config and then call
// RebuildRelayErrorGovernanceRuntime; this lower-level helper is also useful in
// isolated governance tests that intentionally exercise legacy configurations.
func PublishRelayErrorGovernanceRuntime(setting RelayErrorGovernanceSetting) {
	copy := cloneRelayErrorGovernanceSetting(setting)
	relayErrorGovernanceSnapshot.Store(&copy)
}

// GetRelayErrorGovernanceSetting returns the current immutable snapshot.
// Callers must treat the returned value as read-only.
func GetRelayErrorGovernanceSetting() *RelayErrorGovernanceSetting {
	setting := relayErrorGovernanceSnapshot.Load()
	if setting == nil {
		RebuildRelayErrorGovernanceRuntime()
		setting = relayErrorGovernanceSnapshot.Load()
	}
	return setting
}

func cloneRelayErrorGovernanceSetting(setting RelayErrorGovernanceSetting) RelayErrorGovernanceSetting {
	copy := RelayErrorGovernanceSetting{Enabled: setting.Enabled}
	if setting.Rules != nil {
		copy.Rules = make(map[string]RelayErrorGovernanceRuleConfig, len(setting.Rules))
		for code, rule := range setting.Rules {
			clonedRule := rule
			if rule.Enabled != nil {
				enabled := *rule.Enabled
				clonedRule.Enabled = &enabled
			}
			copy.Rules[code] = clonedRule
		}
	}
	if setting.CustomRules != nil {
		copy.CustomRules = append([]RelayErrorGovernanceCustomRuleConfig(nil), setting.CustomRules...)
	}
	return copy
}
