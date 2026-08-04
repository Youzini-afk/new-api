package system_setting

import (
	"fmt"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

const (
	RelayBatchSplitOptionKey = "relay_batch_split.config"

	RelayBatchKindEmbedding = "embedding"
	RelayBatchKindRerank    = "rerank"

	MaxEmbeddingBatchItems = 1000
	MaxRerankBatchItems    = 200
	MaxBatchConcurrency    = 4
)

// RelayBatchKindSetting controls one request family. BatchSize is the largest
// number of items sent upstream at once; MaxItems guards the original request.
type RelayBatchKindSetting struct {
	Enabled     bool `json:"enabled"`
	BatchSize   int  `json:"batch_size"`
	Concurrency int  `json:"concurrency"`
	MaxItems    int  `json:"max_items"`
}

// RelayBatchSplitSetting is stored atomically as one option. Administrators
// select channels explicitly; runtime matching never guesses from names or URLs.
type RelayBatchSplitSetting struct {
	Version    int                   `json:"version"`
	Enabled    bool                  `json:"enabled"`
	ChannelIDs []int                 `json:"channel_ids"`
	Embedding  RelayBatchKindSetting `json:"embedding"`
	Rerank     RelayBatchKindSetting `json:"rerank"`
}

type relayBatchSplitContainer struct {
	Config string `json:"config"`
}

type relayBatchSplitRuntime struct {
	config     RelayBatchSplitSetting
	channelIDs map[int]struct{}
}

var defaultRelayBatchSplitSetting = RelayBatchSplitSetting{
	Version:    1,
	Enabled:    false,
	ChannelIDs: []int{},
	Embedding: RelayBatchKindSetting{
		Enabled:     true,
		BatchSize:   25,
		Concurrency: 2,
		MaxItems:    MaxEmbeddingBatchItems,
	},
	Rerank: RelayBatchKindSetting{
		Enabled:     false,
		BatchSize:   25,
		Concurrency: 1,
		MaxItems:    MaxRerankBatchItems,
	},
}

var defaultRelayBatchSplitContainer = relayBatchSplitContainer{
	Config: mustMarshalRelayBatchSplitSetting(defaultRelayBatchSplitSetting),
}

var relayBatchSplitSnapshot atomic.Pointer[relayBatchSplitRuntime]

func init() {
	config.GlobalConfig.Register("relay_batch_split", &defaultRelayBatchSplitContainer)
	RebuildRelayBatchSplitRuntime()
}

func mustMarshalRelayBatchSplitSetting(setting RelayBatchSplitSetting) string {
	data, err := common.Marshal(setting)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// ParseAndValidateRelayBatchSplitConfig validates the complete option and
// returns deterministic JSON so semantically equal settings persist equally.
func ParseAndValidateRelayBatchSplitConfig(raw string) (RelayBatchSplitSetting, string, error) {
	var setting RelayBatchSplitSetting
	if strings.TrimSpace(raw) == "" {
		return setting, "", fmt.Errorf("batch splitting configuration cannot be empty")
	}
	if err := common.UnmarshalJsonStr(raw, &setting); err != nil {
		return setting, "", fmt.Errorf("batch splitting configuration must be valid JSON: %w", err)
	}
	if setting.Version == 0 {
		setting.Version = 1
	}
	if setting.Version != 1 {
		return setting, "", fmt.Errorf("unsupported batch splitting configuration version: %d", setting.Version)
	}

	seen := make(map[int]struct{}, len(setting.ChannelIDs))
	channelIDs := make([]int, 0, len(setting.ChannelIDs))
	for _, channelID := range setting.ChannelIDs {
		if channelID <= 0 {
			return setting, "", fmt.Errorf("channel IDs must be positive integers")
		}
		if _, exists := seen[channelID]; exists {
			return setting, "", fmt.Errorf("channel ID %d is duplicated", channelID)
		}
		seen[channelID] = struct{}{}
		channelIDs = append(channelIDs, channelID)
	}
	sort.Ints(channelIDs)
	setting.ChannelIDs = channelIDs
	if setting.Enabled && len(setting.ChannelIDs) == 0 {
		return setting, "", fmt.Errorf("select at least one channel before enabling batch splitting")
	}
	if setting.Enabled && !setting.Embedding.Enabled && !setting.Rerank.Enabled {
		return setting, "", fmt.Errorf("enable embedding or rerank splitting before enabling batch splitting")
	}

	if err := validateRelayBatchKindSetting("Embedding", setting.Embedding, MaxEmbeddingBatchItems); err != nil {
		return setting, "", err
	}
	if err := validateRelayBatchKindSetting("Rerank", setting.Rerank, MaxRerankBatchItems); err != nil {
		return setting, "", err
	}

	data, err := common.Marshal(setting)
	if err != nil {
		return setting, "", fmt.Errorf("failed to serialize batch splitting configuration: %w", err)
	}
	return setting, string(data), nil
}

func validateRelayBatchKindSetting(name string, setting RelayBatchKindSetting, hardMax int) error {
	if setting.BatchSize < 1 || setting.BatchSize > hardMax {
		return fmt.Errorf("%s batch size must be between 1 and %d", name, hardMax)
	}
	if setting.Concurrency < 1 || setting.Concurrency > MaxBatchConcurrency {
		return fmt.Errorf("%s concurrency must be between 1 and %d", name, MaxBatchConcurrency)
	}
	if setting.MaxItems < setting.BatchSize || setting.MaxItems > hardMax {
		return fmt.Errorf("%s maximum items must be between its batch size and %d", name, hardMax)
	}
	return nil
}

// NormalizeRelayBatchSplitOption validates only this option key and leaves all
// unrelated settings untouched.
func NormalizeRelayBatchSplitOption(key, value string) (string, error) {
	if key != RelayBatchSplitOptionKey {
		return value, nil
	}
	_, normalized, err := ParseAndValidateRelayBatchSplitConfig(value)
	return normalized, err
}

// RebuildRelayBatchSplitRuntime atomically publishes an immutable lookup
// snapshot. Invalid persisted values fail closed instead of enabling splitting.
func RebuildRelayBatchSplitRuntime() {
	setting, _, err := ParseAndValidateRelayBatchSplitConfig(defaultRelayBatchSplitContainer.Config)
	if err != nil {
		setting = defaultRelayBatchSplitSetting
		setting.Enabled = false
	}
	channelIDs := make(map[int]struct{}, len(setting.ChannelIDs))
	for _, channelID := range setting.ChannelIDs {
		channelIDs[channelID] = struct{}{}
	}
	relayBatchSplitSnapshot.Store(&relayBatchSplitRuntime{
		config:     setting,
		channelIDs: channelIDs,
	})
}

func GetRelayBatchSplitSetting() RelayBatchSplitSetting {
	runtime := relayBatchSplitSnapshot.Load()
	if runtime == nil {
		setting := defaultRelayBatchSplitSetting
		setting.ChannelIDs = append([]int(nil), setting.ChannelIDs...)
		return setting
	}
	setting := runtime.config
	setting.ChannelIDs = append([]int(nil), runtime.config.ChannelIDs...)
	return setting
}

// MatchRelayBatchSplit returns a rule only for an explicitly selected channel.
func MatchRelayBatchSplit(channelID int, kind string) (RelayBatchKindSetting, bool) {
	runtime := relayBatchSplitSnapshot.Load()
	if runtime == nil || !runtime.config.Enabled {
		return RelayBatchKindSetting{}, false
	}
	if _, exists := runtime.channelIDs[channelID]; !exists {
		return RelayBatchKindSetting{}, false
	}
	switch kind {
	case RelayBatchKindEmbedding:
		return runtime.config.Embedding, runtime.config.Embedding.Enabled
	case RelayBatchKindRerank:
		return runtime.config.Rerank, runtime.config.Rerank.Enabled
	default:
		return RelayBatchKindSetting{}, false
	}
}
