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

	maxEmbeddingBatchItems = 1000
	maxRerankBatchItems    = 200
	maxBatchConcurrency    = 4
)

// RelayBatchKindSetting controls one relay request family. BatchSize is the
// largest number of items sent in one upstream request, while MaxItems is a
// local guardrail for the original client request.
type RelayBatchKindSetting struct {
	Enabled     bool `json:"enabled"`
	BatchSize   int  `json:"batch_size"`
	Concurrency int  `json:"concurrency"`
	MaxItems    int  `json:"max_items"`
}

// RelayBatchSplitSetting is persisted atomically as one JSON option. Channels
// are selected explicitly by administrators; no URL or channel-name inference
// is performed at runtime.
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
		MaxItems:    maxEmbeddingBatchItems,
	},
	Rerank: RelayBatchKindSetting{
		Enabled:     false,
		BatchSize:   25,
		Concurrency: 1,
		MaxItems:    maxRerankBatchItems,
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

// ParseAndValidateRelayBatchSplitConfig validates the complete atomic option
// and returns its deterministic JSON representation.
func ParseAndValidateRelayBatchSplitConfig(raw string) (RelayBatchSplitSetting, string, error) {
	var setting RelayBatchSplitSetting
	if strings.TrimSpace(raw) == "" {
		return setting, "", fmt.Errorf("批量拆分配置不能为空")
	}
	if err := common.UnmarshalJsonStr(raw, &setting); err != nil {
		return setting, "", fmt.Errorf("批量拆分配置必须是合法 JSON: %w", err)
	}
	if setting.Version == 0 {
		setting.Version = 1
	}
	if setting.Version != 1 {
		return setting, "", fmt.Errorf("不支持的批量拆分配置版本: %d", setting.Version)
	}

	seen := make(map[int]struct{}, len(setting.ChannelIDs))
	channelIDs := make([]int, 0, len(setting.ChannelIDs))
	for _, channelID := range setting.ChannelIDs {
		if channelID <= 0 {
			return setting, "", fmt.Errorf("渠道 ID 必须为正整数")
		}
		if _, exists := seen[channelID]; exists {
			return setting, "", fmt.Errorf("渠道 ID %d 重复", channelID)
		}
		seen[channelID] = struct{}{}
		channelIDs = append(channelIDs, channelID)
	}
	sort.Ints(channelIDs)
	setting.ChannelIDs = channelIDs

	if err := validateRelayBatchKindSetting("Embedding", setting.Embedding, maxEmbeddingBatchItems); err != nil {
		return setting, "", err
	}
	if err := validateRelayBatchKindSetting("Rerank", setting.Rerank, maxRerankBatchItems); err != nil {
		return setting, "", err
	}

	data, err := common.Marshal(setting)
	if err != nil {
		return setting, "", fmt.Errorf("序列化批量拆分配置失败: %w", err)
	}
	return setting, string(data), nil
}

func validateRelayBatchKindSetting(name string, setting RelayBatchKindSetting, hardMax int) error {
	if setting.BatchSize < 1 || setting.BatchSize > hardMax {
		return fmt.Errorf("%s 单批数量必须在 1 到 %d 之间", name, hardMax)
	}
	if setting.Concurrency < 1 || setting.Concurrency > maxBatchConcurrency {
		return fmt.Errorf("%s 并发数必须在 1 到 %d 之间", name, maxBatchConcurrency)
	}
	if setting.MaxItems < setting.BatchSize || setting.MaxItems > hardMax {
		return fmt.Errorf("%s 最大请求数量必须在单批数量与 %d 之间", name, hardMax)
	}
	return nil
}

// NormalizeRelayBatchSplitOption leaves unrelated options untouched.
func NormalizeRelayBatchSplitOption(key, value string) (string, error) {
	if key != RelayBatchSplitOptionKey {
		return value, nil
	}
	_, normalized, err := ParseAndValidateRelayBatchSplitConfig(value)
	return normalized, err
}

// RebuildRelayBatchSplitRuntime publishes an immutable snapshot after the
// registered config has changed. Invalid persisted legacy values fail closed.
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

// GetRelayBatchSplitSetting returns a defensive copy suitable for display.
func GetRelayBatchSplitSetting() RelayBatchSplitSetting {
	runtime := relayBatchSplitSnapshot.Load()
	if runtime == nil {
		return defaultRelayBatchSplitSetting
	}
	setting := runtime.config
	setting.ChannelIDs = append([]int(nil), runtime.config.ChannelIDs...)
	return setting
}

// MatchRelayBatchSplit returns the active rule only for explicitly selected
// channels. Callers still decide whether the request is large enough to split.
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
