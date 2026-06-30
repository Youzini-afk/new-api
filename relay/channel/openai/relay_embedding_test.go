package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newEmbeddingTestContext(t *testing.T, upstreamBody string, request *dto.EmbeddingRequest) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	t.Helper()
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(upstreamBody)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{},
		Request:     request,
	}
	info.SetEstimatePromptTokens(7)
	return c, recorder, resp, info
}

func TestOpenaiEmbeddingHandlerValidSingleStringNumeric(t *testing.T) {
	request := &dto.EmbeddingRequest{Model: "text-embedding-3-small", Input: "hello"}
	c, recorder, resp, info := newEmbeddingTestContext(t, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"model":"text-embedding-3-small","usage":{"prompt_tokens":3,"total_tokens":3}}`, request)

	usage, apiErr := OpenaiEmbeddingHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 3, usage.PromptTokens)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"data"`)
	assert.Contains(t, recorder.Body.String(), `"embedding"`)
}

func TestOpenaiEmbeddingHandlerRejectsEmptyData(t *testing.T) {
	request := &dto.EmbeddingRequest{Model: "text-embedding-3-small", Input: "hello"}
	c, recorder, resp, info := newEmbeddingTestContext(t, `{"object":"list","data":[],"model":"text-embedding-3-small","usage":{"prompt_tokens":3,"total_tokens":3}}`, request)

	usage, apiErr := OpenaiEmbeddingHandler(c, info, resp)

	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeBadResponseBody, apiErr.GetErrorCode())
	assert.NotContains(t, recorder.Body.String(), `"data":[]`)
}

func TestOpenaiEmbeddingHandlerRejectsPartialBatch(t *testing.T) {
	request := &dto.EmbeddingRequest{Model: "text-embedding-3-small", Input: []any{"one", "two"}}
	c, recorder, resp, info := newEmbeddingTestContext(t, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1]}],"model":"text-embedding-3-small","usage":{"prompt_tokens":3,"total_tokens":3}}`, request)

	usage, apiErr := OpenaiEmbeddingHandler(c, info, resp)

	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeBadResponseBody, apiErr.GetErrorCode())
	assert.Empty(t, recorder.Body.String())
}

func TestOpenaiEmbeddingHandlerRejectsProviderNativeShape(t *testing.T) {
	request := &dto.EmbeddingRequest{Model: "text-embedding-3-small", Input: "hello"}
	c, recorder, resp, info := newEmbeddingTestContext(t, `{"embeddings":[[0.1,0.2]],"usage":{"prompt_tokens":3,"total_tokens":3}}`, request)

	usage, apiErr := OpenaiEmbeddingHandler(c, info, resp)

	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeBadResponseBody, apiErr.GetErrorCode())
	assert.Empty(t, recorder.Body.String())
}

func TestOpenaiEmbeddingHandlerAcceptsBase64Embedding(t *testing.T) {
	request := &dto.EmbeddingRequest{Model: "text-embedding-3-small", Input: "hello", EncodingFormat: "base64"}
	c, recorder, resp, info := newEmbeddingTestContext(t, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":"AAAA"}],"model":"text-embedding-3-small","usage":{"prompt_tokens":3,"total_tokens":3}}`, request)

	usage, apiErr := OpenaiEmbeddingHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Contains(t, recorder.Body.String(), `"embedding":"AAAA"`)
}

func TestOpenaiEmbeddingHandlerFillsMissingUsage(t *testing.T) {
	request := &dto.EmbeddingRequest{Model: "text-embedding-3-small", Input: "hello"}
	c, recorder, resp, info := newEmbeddingTestContext(t, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"model":"text-embedding-3-small"}`, request)

	usage, apiErr := OpenaiEmbeddingHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 7, usage.PromptTokens)
	assert.Equal(t, 7, usage.TotalTokens)
	var body map[string]interface{}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
	usageMap, ok := body["usage"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(7), usageMap["prompt_tokens"])
	assert.Equal(t, float64(7), usageMap["total_tokens"])
}

func TestOpenaiEmbeddingHandlerPreservesPromptTokensWhenFillingTotal(t *testing.T) {
	request := &dto.EmbeddingRequest{Model: "text-embedding-3-small", Input: "hello"}
	c, recorder, resp, info := newEmbeddingTestContext(t, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"model":"text-embedding-3-small","usage":{"prompt_tokens":3}}`, request)

	usage, apiErr := OpenaiEmbeddingHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 3, usage.PromptTokens)
	assert.Equal(t, 3, usage.TotalTokens)
	var body map[string]interface{}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
	usageMap, ok := body["usage"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(3), usageMap["prompt_tokens"])
	assert.Equal(t, float64(3), usageMap["total_tokens"])
}
