package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBatchSplitInfoAdminMapContainsMetadataOnly(t *testing.T) {
	info := &BatchSplitInfo{
		Kind:            "embedding",
		ChannelID:       9,
		ItemCount:       26,
		BatchSize:       25,
		ChunkCount:      2,
		Concurrency:     2,
		CompletedChunks: 2,
		DurationMs:      18,
	}

	metadata := info.AdminMap()
	assert.Equal(t, "embedding", metadata["kind"])
	assert.Equal(t, 26, metadata["item_count"])
	assert.Equal(t, 2, metadata["chunk_count"])
	for _, contentKey := range []string{"input", "query", "documents", "text", "request"} {
		assert.NotContains(t, metadata, contentKey)
	}
}
