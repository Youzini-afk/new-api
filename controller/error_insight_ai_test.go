package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareErrorInsightAIRelayRequestForcesJSONHeaders(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/risk_control/cases/2/analyze?source=admin", nil)
	request.Header.Del("Content-Type")

	require.NoError(t, prepareErrorInsightAIRelayRequest(request))
	assert.Equal(t, http.MethodPost, request.Method)
	assert.Equal(t, "/v1/chat/completions", request.URL.Path)
	assert.Empty(t, request.URL.RawQuery)
	assert.Equal(t, "application/json", request.Header.Get("Content-Type"))
	assert.Equal(t, "application/json", request.Header.Get("Accept"))
}

func TestEnsureErrorInsightAIModelPreservesBracketedModelName(t *testing.T) {
	const bracketedModel = "[R]deepseek-v4-flash"
	request := &dto.GeneralOpenAIRequest{Model: bracketedModel}
	info := &relaycommon.RelayInfo{
		OriginModelName: bracketedModel,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: bracketedModel,
		},
	}

	resolved, err := ensureErrorInsightAIModel(request, info, bracketedModel)
	require.NoError(t, err)
	assert.Equal(t, bracketedModel, resolved)
	assert.Equal(t, bracketedModel, request.Model)
	assert.Equal(t, bracketedModel, info.UpstreamModelName)
}

func TestEnsureErrorInsightAIModelUsesMappedModel(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{Model: "configured-model"}
	info := &relaycommon.RelayInfo{
		OriginModelName: "configured-model",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "mapped-model",
		},
	}

	resolved, err := ensureErrorInsightAIModel(request, info, "configured-model")
	require.NoError(t, err)
	assert.Equal(t, "mapped-model", resolved)
	assert.Equal(t, "mapped-model", request.Model)
	assert.Equal(t, "mapped-model", info.UpstreamModelName)
}

func TestEnsureErrorInsightAIModelFallsBackToConfiguredModel(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{}
	info := &relaycommon.RelayInfo{}

	resolved, err := ensureErrorInsightAIModel(request, info, " configured-model ")
	require.NoError(t, err)
	assert.Equal(t, "configured-model", resolved)
	assert.Equal(t, "configured-model", request.Model)
	assert.Equal(t, "configured-model", info.UpstreamModelName)
	assert.Equal(t, "configured-model", info.OriginModelName)
}

func TestEnsureErrorInsightAIRequestModelRepairsMissingOrEmptyModel(t *testing.T) {
	for name, input := range map[string]string{
		"missing": `{"messages":[{"role":"user","content":"test"}]}`,
		"empty":   `{"model":"","messages":[{"role":"user","content":"test"}]}`,
		"null":    `{"model":null,"messages":[{"role":"user","content":"test"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			output, repaired, err := ensureErrorInsightAIRequestModel([]byte(input), "mapped-model")
			require.NoError(t, err)
			assert.True(t, repaired)

			modelName, exists, err := getErrorInsightAIRequestModel(output)
			require.NoError(t, err)
			assert.True(t, exists)
			assert.Equal(t, "mapped-model", modelName)
		})
	}
}

func TestEnsureErrorInsightAIRequestModelPreservesNonEmptyOverride(t *testing.T) {
	input := []byte(`{"model":"override-model","messages":[]}`)
	output, repaired, err := ensureErrorInsightAIRequestModel(input, "mapped-model")
	require.NoError(t, err)
	assert.False(t, repaired)
	assert.Equal(t, input, output)
}

func TestGetErrorInsightAIRequestModelAllowsPathBasedModelRequests(t *testing.T) {
	modelName, exists, err := getErrorInsightAIRequestModel([]byte(`{"contents":[]}`))
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Empty(t, modelName)
}
