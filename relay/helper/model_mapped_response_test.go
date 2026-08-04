package helper

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelMappedHelperSetsAndClearsResponseAliasAcrossRetries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Set(string(constant.ContextKeyOriginalModel), "public-model")
	c.Set("model_mapping", `{"public-model":"provider/model-v2"}`)

	info := &relaycommon.RelayInfo{
		OriginModelName: "public-model",
		ChannelMeta:     &relaycommon.ChannelMeta{},
	}
	firstRequest := &dto.GeneralOpenAIRequest{Model: "public-model"}
	require.NoError(t, ModelMappedHelper(c, info, firstRequest))
	assert.True(t, info.IsModelMapped)
	assert.Equal(t, "provider/model-v2", info.UpstreamModelName)
	assert.Equal(t, "provider/model-v2", firstRequest.Model)
	assert.Equal(t, "public-model", c.GetString(string(constant.ContextKeyResponseModelName)))

	// Simulate a retry on another channel without a model mapping.
	c.Set("model_mapping", "{}")
	secondRequest := &dto.GeneralOpenAIRequest{Model: "public-model"}
	require.NoError(t, ModelMappedHelper(c, info, secondRequest))
	assert.False(t, info.IsModelMapped)
	assert.Equal(t, "public-model", info.UpstreamModelName)
	assert.Equal(t, "public-model", secondRequest.Model)
	assert.Empty(t, c.GetString(string(constant.ContextKeyResponseModelName)))
}

func TestModelMappedHelperPreservesCompactPublicAlias(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	c.Set(string(constant.ContextKeyOriginalModel), "public-model-openai-compact")
	c.Set("model_mapping", `{"public-model":"provider/model-v2"}`)

	info := &relaycommon.RelayInfo{
		OriginModelName: "public-model-openai-compact",
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		ChannelMeta:     &relaycommon.ChannelMeta{},
	}
	request := &dto.GeneralOpenAIRequest{Model: "public-model-openai-compact"}
	require.NoError(t, ModelMappedHelper(c, info, request))

	assert.Equal(t, "provider/model-v2", request.Model)
	assert.Equal(t, "provider/model-v2-openai-compact", info.OriginModelName)
	assert.Equal(t, "public-model-openai-compact", info.ResponseModelName(info.UpstreamModelName))
	assert.Equal(t, "public-model-openai-compact", c.GetString(string(constant.ContextKeyResponseModelName)))
}

func TestModelMappedHelperTreatsCompactSelfMappingAsUnmapped(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	c.Set(string(constant.ContextKeyOriginalModel), "public-model-openai-compact")
	c.Set("model_mapping", `{"public-model":"public-model"}`)

	info := &relaycommon.RelayInfo{
		OriginModelName: "public-model-openai-compact",
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		ChannelMeta:     &relaycommon.ChannelMeta{},
	}
	request := &dto.GeneralOpenAIRequest{Model: "public-model-openai-compact"}
	require.NoError(t, ModelMappedHelper(c, info, request))

	assert.False(t, info.IsModelMapped)
	assert.Equal(t, "public-model", request.Model)
	assert.Equal(t, "public-model-openai-compact", info.OriginModelName)
	assert.Empty(t, c.GetString(string(constant.ContextKeyResponseModelName)))
}
