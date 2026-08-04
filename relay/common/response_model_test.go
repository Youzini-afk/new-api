package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mappedResponseInfo() *RelayInfo {
	return &RelayInfo{
		OriginModelName:    "public-model",
		ResponseModelAlias: "public-model",
		ChannelMeta: &ChannelMeta{
			UpstreamModelName: "provider/model-v2",
			IsModelMapped:     true,
		},
	}
}

func TestResponseModelName(t *testing.T) {
	t.Run("mapped model uses requested alias", func(t *testing.T) {
		assert.Equal(t, "public-model", mappedResponseInfo().ResponseModelName("provider/model-v2"))
	})

	t.Run("unmapped model keeps upstream response", func(t *testing.T) {
		info := &RelayInfo{ChannelMeta: &ChannelMeta{UpstreamModelName: "gpt-4o"}}
		assert.Equal(t, "gpt-4o-2024-11-20", info.ResponseModelName("gpt-4o-2024-11-20"))
	})
}

func TestRewriteResponseModel(t *testing.T) {
	body := []byte(`{
		"model":"provider/model-v2",
		"modelVersion":"provider/model-v2",
		"response":{"model":"provider/model-v2","modelVersion":"provider/model-v2","output":[{"model":"tool-owned-model"}]},
		"message":{"model":"provider/model-v2"},
		"session":{"model":"provider/model-v2"},
		"metadata":{"model":"user-owned-model"},
		"large_id":9007199254740993
	}`)

	got, err := mappedResponseInfo().RewriteResponseModel(body)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"model":"public-model",
		"modelVersion":"public-model",
		"response":{"model":"public-model","modelVersion":"public-model","output":[{"model":"tool-owned-model"}]},
		"message":{"model":"public-model"},
		"session":{"model":"public-model"},
		"metadata":{"model":"user-owned-model"},
		"large_id":9007199254740993
	}`, string(got))
	assert.Contains(t, string(got), "9007199254740993")

	withoutKnownField := []byte(`{"metadata":{"model":"user-owned-model"},"value":1}`)
	got, err = RewriteResponseModel(withoutKnownField, "public-model")
	require.NoError(t, err)
	assert.Equal(t, withoutKnownField, got)

	nonJSON := []byte{0xff, 0xfb, 0x90, 0x64}
	got, err = RewriteResponseModel(nonJSON, "public-model")
	require.NoError(t, err)
	assert.Equal(t, nonJSON, got)
}

func TestRewriteResponseModelFromContext(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	body := []byte(`{"model":"provider/model-v2"}`)

	got, err := RewriteResponseModelFromContext(c, body)
	require.NoError(t, err)
	assert.Equal(t, body, got)

	c.Set(string(constant.ContextKeyResponseModelName), "public-model")
	got, err = RewriteResponseModelFromContext(c, body)
	require.NoError(t, err)
	assert.JSONEq(t, `{"model":"public-model"}`, string(got))
}
