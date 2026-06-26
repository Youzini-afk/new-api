package system_setting

import "github.com/QuantumNous/new-api/setting/config"

// LogScreening window identifiers and secondary-match modes.
const (
	LogScreeningWindow1h  = "1h"
	LogScreeningWindow24h = "24h"

	MinLogScreeningExpireDays = 7

	LogScreeningSecondaryModeOr  = "or"
	LogScreeningSecondaryModeAll = "all"
)

// LogScreeningParamRule defines a per-field numeric threshold checked against
// recorded request parameters for a screening rule.
type LogScreeningParamRule struct {
	Field string  `json:"field"`
	Op    string  `json:"op"`
	Value float64 `json:"value"`
}

// LogScreeningRule defines a single suspicious-traffic screening rule.
type LogScreeningRule struct {
	Name             string                  `json:"name"`
	Enabled          bool                    `json:"enabled"`
	Window           string                  `json:"window"`
	RequestCount     int                     `json:"request_count"`
	RPM              int                     `json:"rpm"`
	RPH              int                     `json:"rph"`
	TPM              int                     `json:"tpm"`
	ParamRules       []LogScreeningParamRule `json:"param_rules"`
	PromptDelta      int                     `json:"prompt_delta"`
	PromptDeltaCount int                     `json:"prompt_delta_count"`
	UABlacklist      []string                `json:"ua_blacklist"`
	UADirect         []string                `json:"ua_direct"`
	SecondaryMode    string                  `json:"secondary_mode"`
}

// LogScreeningSetting is the registered system-level config for log screening.
type LogScreeningSetting struct {
	Enabled    bool               `json:"enabled"`
	Rules      []LogScreeningRule `json:"rules"`
	ExpireDays int                `json:"expire_days"`
}

var defaultLogScreeningSetting = LogScreeningSetting{
	Enabled:    true,
	ExpireDays: MinLogScreeningExpireDays,
	Rules: []LogScreeningRule{
		{
			Name:             "高频调用(1h)",
			Enabled:          false,
			Window:           LogScreeningWindow1h,
			RequestCount:     0,
			RPM:              0,
			RPH:              0,
			TPM:              0,
			ParamRules:       []LogScreeningParamRule{},
			PromptDelta:      0,
			PromptDeltaCount: 0,
			UABlacklist:      []string{},
			UADirect:         []string{},
			SecondaryMode:    LogScreeningSecondaryModeOr,
		},
		{
			Name:             "高频调用(24h)",
			Enabled:          false,
			Window:           LogScreeningWindow24h,
			RequestCount:     0,
			RPM:              0,
			RPH:              0,
			TPM:              0,
			ParamRules:       []LogScreeningParamRule{},
			PromptDelta:      0,
			PromptDeltaCount: 0,
			UABlacklist:      []string{},
			UADirect:         []string{},
			SecondaryMode:    LogScreeningSecondaryModeOr,
		},
	},
}

func init() {
	config.GlobalConfig.Register("log_screening", &defaultLogScreeningSetting)
}

// GetLogScreeningSetting returns the current setting with safe defaults applied
// (non-nil slices, default window/secondary mode when empty).
func GetLogScreeningSetting() *LogScreeningSetting {
	if defaultLogScreeningSetting.ExpireDays < MinLogScreeningExpireDays {
		defaultLogScreeningSetting.ExpireDays = MinLogScreeningExpireDays
	}
	if defaultLogScreeningSetting.Rules == nil {
		defaultLogScreeningSetting.Rules = []LogScreeningRule{}
	}
	for i := range defaultLogScreeningSetting.Rules {
		if defaultLogScreeningSetting.Rules[i].Window == "" {
			defaultLogScreeningSetting.Rules[i].Window = LogScreeningWindow1h
		}
		if defaultLogScreeningSetting.Rules[i].ParamRules == nil {
			defaultLogScreeningSetting.Rules[i].ParamRules = []LogScreeningParamRule{}
		}
		if defaultLogScreeningSetting.Rules[i].UABlacklist == nil {
			defaultLogScreeningSetting.Rules[i].UABlacklist = []string{}
		}
		if defaultLogScreeningSetting.Rules[i].UADirect == nil {
			defaultLogScreeningSetting.Rules[i].UADirect = []string{}
		}
		if defaultLogScreeningSetting.Rules[i].SecondaryMode == "" {
			defaultLogScreeningSetting.Rules[i].SecondaryMode = LogScreeningSecondaryModeOr
		}
	}
	return &defaultLogScreeningSetting
}
