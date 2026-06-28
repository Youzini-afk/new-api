package system_setting

import "github.com/QuantumNous/new-api/setting/config"

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
	Rules       map[string]RelayErrorGovernanceRuleConfig       `json:"rules,omitempty"`
	CustomRules []RelayErrorGovernanceCustomRuleConfig          `json:"custom_rules,omitempty"`
}

var defaultRelayErrorGovernanceSetting = RelayErrorGovernanceSetting{
	Enabled: true,
}

func init() {
	config.GlobalConfig.Register("relay_error_governance", &defaultRelayErrorGovernanceSetting)
}

// GetRelayErrorGovernanceSetting returns the current governance setting. The
// returned pointer is the live config object — callers must not mutate it
// concurrently without holding the config lock.
func GetRelayErrorGovernanceSetting() *RelayErrorGovernanceSetting {
	return &defaultRelayErrorGovernanceSetting
}
