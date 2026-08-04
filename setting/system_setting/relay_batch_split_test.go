package system_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAndValidateRelayBatchSplitConfigNormalizesChannelIDs(t *testing.T) {
	raw := `{
		"version":1,
		"enabled":true,
		"channel_ids":[9,2,5],
		"embedding":{"enabled":true,"batch_size":25,"concurrency":2,"max_items":1000},
		"rerank":{"enabled":true,"batch_size":20,"concurrency":1,"max_items":200}
	}`

	setting, normalized, err := ParseAndValidateRelayBatchSplitConfig(raw)
	require.NoError(t, err)
	assert.Equal(t, []int{2, 5, 9}, setting.ChannelIDs)
	assert.JSONEq(t, `{
		"version":1,
		"enabled":true,
		"channel_ids":[2,5,9],
		"embedding":{"enabled":true,"batch_size":25,"concurrency":2,"max_items":1000},
		"rerank":{"enabled":true,"batch_size":20,"concurrency":1,"max_items":200}
	}`, normalized)
}

func TestParseAndValidateRelayBatchSplitConfigRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "duplicate channel", raw: `{"version":1,"channel_ids":[3,3],"embedding":{"batch_size":25,"concurrency":1,"max_items":1000},"rerank":{"batch_size":25,"concurrency":1,"max_items":200}}`},
		{name: "embedding batch too large", raw: `{"version":1,"channel_ids":[],"embedding":{"batch_size":1001,"concurrency":1,"max_items":1000},"rerank":{"batch_size":25,"concurrency":1,"max_items":200}}`},
		{name: "concurrency too large", raw: `{"version":1,"channel_ids":[],"embedding":{"batch_size":25,"concurrency":5,"max_items":1000},"rerank":{"batch_size":25,"concurrency":1,"max_items":200}}`},
		{name: "max below batch", raw: `{"version":1,"channel_ids":[],"embedding":{"batch_size":25,"concurrency":1,"max_items":20},"rerank":{"batch_size":25,"concurrency":1,"max_items":200}}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := ParseAndValidateRelayBatchSplitConfig(test.raw)
			require.Error(t, err)
		})
	}
}

func TestMatchRelayBatchSplitOnlyMatchesExplicitChannels(t *testing.T) {
	original := defaultRelayBatchSplitContainer.Config
	t.Cleanup(func() {
		defaultRelayBatchSplitContainer.Config = original
		RebuildRelayBatchSplitRuntime()
	})

	_, normalized, err := ParseAndValidateRelayBatchSplitConfig(`{
		"version":1,
		"enabled":true,
		"channel_ids":[17],
		"embedding":{"enabled":true,"batch_size":25,"concurrency":2,"max_items":1000},
		"rerank":{"enabled":false,"batch_size":25,"concurrency":1,"max_items":200}
	}`)
	require.NoError(t, err)
	defaultRelayBatchSplitContainer.Config = normalized
	RebuildRelayBatchSplitRuntime()

	embedding, matched := MatchRelayBatchSplit(17, RelayBatchKindEmbedding)
	require.True(t, matched)
	assert.Equal(t, 25, embedding.BatchSize)

	_, matched = MatchRelayBatchSplit(18, RelayBatchKindEmbedding)
	assert.False(t, matched)
	_, matched = MatchRelayBatchSplit(17, RelayBatchKindRerank)
	assert.False(t, matched)
}

func TestNormalizeRelayBatchSplitOptionPassesOtherKeysThrough(t *testing.T) {
	value := `{"anything":true}`
	normalized, err := NormalizeRelayBatchSplitOption("unrelated.option", value)
	require.NoError(t, err)
	assert.Equal(t, value, normalized)
}
