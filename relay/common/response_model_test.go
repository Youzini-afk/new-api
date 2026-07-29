package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mappedResponseInfo() *RelayInfo {
	return &RelayInfo{
		OriginModelName: "NV",
		ChannelMeta: &ChannelMeta{
			UpstreamModelName: "z-ai/glm-5.2",
			IsModelMapped:     true,
		},
	}
}

func TestResponseModelName(t *testing.T) {
	t.Run("mapped model uses requested alias", func(t *testing.T) {
		assert.Equal(t, "NV", mappedResponseInfo().ResponseModelName("z-ai/glm-5.2"))
	})

	t.Run("unmapped model keeps upstream response", func(t *testing.T) {
		info := &RelayInfo{ChannelMeta: &ChannelMeta{UpstreamModelName: "gpt-4o"}}
		assert.Equal(t, "gpt-4o-2024-11-20", info.ResponseModelName("gpt-4o-2024-11-20"))
	})

	t.Run("missing mapping metadata is safe", func(t *testing.T) {
		var nilInfo *RelayInfo
		assert.Equal(t, "gpt-4o", nilInfo.ResponseModelName("gpt-4o"))
		assert.Equal(t, "gpt-4o", (&RelayInfo{}).ResponseModelName("gpt-4o"))
	})
}

func TestRewriteResponseModel(t *testing.T) {
	info := mappedResponseInfo()

	t.Run("rewrites supported protocol locations only", func(t *testing.T) {
		body := []byte(`{
			"model":"z-ai/glm-5.2",
			"modelVersion":"z-ai/glm-5.2",
			"response":{"model":"z-ai/glm-5.2","modelVersion":"z-ai/glm-5.2","output":[{"model":"tool-owned-model"}]},
			"message":{"model":"z-ai/glm-5.2"},
			"session":{"model":"z-ai/glm-5.2"},
			"metadata":{"model":"user-owned-model"},
			"large_id":9007199254740993
		}`)

		got, err := info.RewriteResponseModel(body)
		require.NoError(t, err)
		assert.JSONEq(t, `{
			"model":"NV",
			"modelVersion":"NV",
			"response":{"model":"NV","modelVersion":"NV","output":[{"model":"tool-owned-model"}]},
			"message":{"model":"NV"},
			"session":{"model":"NV"},
			"metadata":{"model":"user-owned-model"},
			"large_id":9007199254740993
		}`, string(got))
		assert.Contains(t, string(got), "9007199254740993")
	})

	t.Run("leaves payload without known model fields byte-identical", func(t *testing.T) {
		body := []byte(`{"metadata":{"model":"user-owned-model"},"value":1}`)
		got, err := info.RewriteResponseModel(body)
		require.NoError(t, err)
		assert.Equal(t, body, got)
	})

	t.Run("leaves unmapped and non-json payloads unchanged", func(t *testing.T) {
		body := []byte(`{"model":"z-ai/glm-5.2"}`)
		unmapped := &RelayInfo{ChannelMeta: &ChannelMeta{}}
		got, err := unmapped.RewriteResponseModel(body)
		require.NoError(t, err)
		assert.Equal(t, body, got)

		audio := []byte{0xff, 0xfb, 0x90, 0x64}
		got, err = info.RewriteResponseModel(audio)
		require.NoError(t, err)
		assert.Equal(t, audio, got)
	})
}
