package system_setting

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

// RelayParamRecordSetting defines global request parameter recording configuration.
// Note: This config is system-level only (admin editable). No per-user override.
type RelayParamRecordSetting struct {
	Fields                         map[string][]string `json:"fields"`
	MaxValueBytes                  int                 `json:"max_value_bytes"`
	SystemDeveloperMaxBytes        int                 `json:"system_developer_max_bytes"`
	ObservedSemanticCaptureEnabled bool                `json:"observed_semantic_capture_enabled"`
	ObservedSemanticFields         []string            `json:"observed_semantic_fields"`
	ObservedSemanticMaxBytes       int                 `json:"observed_semantic_max_bytes"`
	MessageKeepUserCount           int                 `json:"message_keep_user_count"`
	MessageKeepAssistantCount      int                 `json:"message_keep_assistant_count"`
	MessageKeepSystemCount         int                 `json:"message_keep_system_count"`
	MessageRetentionDays           int                 `json:"message_retention_days"`
}

const (
	defaultRelayParamRecordMaxValueBytes             = 4096
	defaultRelayParamRecordSystemDeveloperMaxBytes   = 100
	defaultRelayParamRecordObservedSemanticMaxBytes  = 1024
	defaultRelayParamRecordMessageKeepUserCount      = 3
	defaultRelayParamRecordMessageKeepAssistantCount = 3
	defaultRelayParamRecordMessageKeepSystemCount    = 2
	defaultRelayParamRecordMessageRetentionDays      = 7
)

var defaultRelayParamRecordObservedSemanticFields = []string{
	"messages",
	"input",
	"contents",
	"query",
	"documents",
	"prompt",
	"text",
}

var defaultRelayParamRecordSetting = RelayParamRecordSetting{
	Fields:                         DefaultRelayParamRecordFields(),
	MaxValueBytes:                  defaultRelayParamRecordMaxValueBytes,
	SystemDeveloperMaxBytes:        defaultRelayParamRecordSystemDeveloperMaxBytes,
	ObservedSemanticCaptureEnabled: true,
	ObservedSemanticFields:         DefaultRelayParamRecordObservedSemanticFields(),
	ObservedSemanticMaxBytes:       defaultRelayParamRecordObservedSemanticMaxBytes,
	MessageKeepUserCount:           defaultRelayParamRecordMessageKeepUserCount,
	MessageKeepAssistantCount:      defaultRelayParamRecordMessageKeepAssistantCount,
	MessageKeepSystemCount:         defaultRelayParamRecordMessageKeepSystemCount,
	MessageRetentionDays:           defaultRelayParamRecordMessageRetentionDays,
}

func init() {
	config.GlobalConfig.Register("relay_param_record", &defaultRelayParamRecordSetting)
}

// GetRelayParamRecordSetting returns the current setting with safe defaults.
func GetRelayParamRecordSetting() *RelayParamRecordSetting {
	if defaultRelayParamRecordSetting.MaxValueBytes <= 0 {
		defaultRelayParamRecordSetting.MaxValueBytes = defaultRelayParamRecordMaxValueBytes
	}
	if defaultRelayParamRecordSetting.SystemDeveloperMaxBytes <= 0 {
		defaultRelayParamRecordSetting.SystemDeveloperMaxBytes = defaultRelayParamRecordSystemDeveloperMaxBytes
	}
	if defaultRelayParamRecordSetting.Fields == nil || len(defaultRelayParamRecordSetting.Fields) == 0 {
		defaultRelayParamRecordSetting.Fields = DefaultRelayParamRecordFields()
	}
	defaultRelayParamRecordSetting.ObservedSemanticFields = normalizeRelayParamRecordObservedSemanticFields(defaultRelayParamRecordSetting.ObservedSemanticFields)
	if len(defaultRelayParamRecordSetting.ObservedSemanticFields) == 0 {
		defaultRelayParamRecordSetting.ObservedSemanticFields = DefaultRelayParamRecordObservedSemanticFields()
	}
	if defaultRelayParamRecordSetting.ObservedSemanticMaxBytes <= 0 {
		defaultRelayParamRecordSetting.ObservedSemanticMaxBytes = defaultRelayParamRecordObservedSemanticMaxBytes
	}
	if defaultRelayParamRecordSetting.MessageKeepUserCount < 0 {
		defaultRelayParamRecordSetting.MessageKeepUserCount = 0
	}
	if defaultRelayParamRecordSetting.MessageKeepAssistantCount < 0 {
		defaultRelayParamRecordSetting.MessageKeepAssistantCount = 0
	}
	if defaultRelayParamRecordSetting.MessageKeepSystemCount < 0 {
		defaultRelayParamRecordSetting.MessageKeepSystemCount = 0
	}
	if defaultRelayParamRecordSetting.MessageRetentionDays <= 0 {
		defaultRelayParamRecordSetting.MessageRetentionDays = defaultRelayParamRecordMessageRetentionDays
	}
	return &defaultRelayParamRecordSetting
}

// DefaultRelayParamRecordFields returns the default (checked) fields per group.
func DefaultRelayParamRecordFields() map[string][]string {
	return map[string][]string{
		"openai": {
			"model",
			"stream_options.include_usage",
			"max_completion_tokens",
			"verbosity",
			"top_p",
			"stop",
			"frequency_penalty",
			"response_format.type",
			"encoding_format",
			"parallel_tool_calls",
			"tool_choice",
			"logprobs",
			"dimensions",
			"audio",
			"stream",
			"max_tokens",
			"reasoning_effort",
			"temperature",
			"top_k",
			"n",
			"presence_penalty",
			"seed",
			"top_logprobs",
			"modalities",
		},
		"openai_responses": {
			"model",
			"max_output_tokens",
			"parallel_tool_calls",
			"reasoning.effort",
			"stream",
			"truncation",
			"max_tool_calls",
			"previous_response_id",
			"reasoning.summary",
			"temperature",
			"tool_choice",
			"top_p",
		},
		"embeddings": {
			"model",
			"encoding_format",
			"dimensions",
		},
		"images": {
			"model",
			"n",
			"quality",
			"size",
			"response_format",
			"moderation",
			"output_compression",
			"watermark",
			"output_format",
			"partial_images",
		},
		"audio": {
			"model",
			"voice",
			"response_format",
			"stream_format",
			"speed",
		},
		"claude": {
			"model",
			"max_tokens",
			"stop_sequences",
			"top_p",
			"stream",
			"tool_choice",
			"service_tier",
			"max_tokens_to_sample",
			"temperature",
			"top_k",
			"thinking",
			"metadata",
		},
		"gemini_chat": {
			"generationConfig",
			"toolConfig",
			"safetySettings",
			"tools",
			"cachedContent",
		},
		"gemini_embedding": {
			"model",
			"taskType",
			"outputDimensionality",
		},
		"gemini_batch_embedding": {},
		"rerank": {
			"model",
			"return_documents",
			"overlap_tokens",
			"top_n",
			"max_chunk_per_doc",
		},
	}
}

func cloneRelayParamRecordFields(source map[string][]string) map[string][]string {
	if source == nil {
		return nil
	}
	result := make(map[string][]string, len(source))
	for key, list := range source {
		if list == nil {
			result[key] = nil
			continue
		}
		cloned := make([]string, len(list))
		copy(cloned, list)
		result[key] = cloned
	}
	return result
}

// ResolveRelayParamRecordFields merges defaults with stored config.
// If the stored config is empty, defaults are used. Per-group overrides are honored.
func ResolveRelayParamRecordFields() map[string][]string {
	defaults := DefaultRelayParamRecordFields()
	configFields := GetRelayParamRecordSetting().Fields

	result := cloneRelayParamRecordFields(defaults)
	if configFields != nil {
		for key, list := range configFields {
			result[key] = list
		}
	}

	// Prevent disabling all parameter records by an empty config.
	count := 0
	for _, list := range result {
		count += len(list)
	}
	if count == 0 {
		return cloneRelayParamRecordFields(defaults)
	}
	return result
}

func DefaultRelayParamRecordObservedSemanticFields() []string {
	result := make([]string, len(defaultRelayParamRecordObservedSemanticFields))
	copy(result, defaultRelayParamRecordObservedSemanticFields)
	return result
}

func normalizeRelayParamRecordObservedSemanticFields(fields []string) []string {
	if len(fields) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(fields))
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		normalized := strings.TrimSpace(field)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result
}
