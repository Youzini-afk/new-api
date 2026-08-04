package openai

import (
	"encoding/base64"
	"encoding/binary"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
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
	assert.Contains(t, recorder.Body.String(), `"embedding"`)
}

func TestOpenaiEmbeddingHandlerRejectsEmptyOrPartialData(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		payload string
	}{
		{name: "empty", input: "hello", payload: `{"object":"list","data":[],"usage":{"prompt_tokens":3,"total_tokens":3}}`},
		{name: "partial", input: []any{"one", "two"}, payload: `{"object":"list","data":[{"index":0,"embedding":[0.1]}],"usage":{"prompt_tokens":3,"total_tokens":3}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := &dto.EmbeddingRequest{Model: "text-embedding-3-small", Input: test.input}
			c, recorder, resp, info := newEmbeddingTestContext(t, test.payload, request)
			usage, apiErr := OpenaiEmbeddingHandler(c, info, resp)
			assert.Nil(t, usage)
			require.NotNil(t, apiErr)
			assert.Equal(t, types.ErrorCodeBadResponseBody, apiErr.GetErrorCode())
			assert.Empty(t, recorder.Body.String())
		})
	}
}

func TestOpenaiEmbeddingHandlerConvertsNumericEmbeddingToBase64(t *testing.T) {
	request := &dto.EmbeddingRequest{Model: "text-embedding-3-small", Input: "hello", EncodingFormat: "base64"}
	c, recorder, resp, info := newEmbeddingTestContext(t, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[1,-2.5]}],"model":"text-embedding-3-small","usage":{"prompt_tokens":3,"total_tokens":3}}`, request)

	usage, apiErr := OpenaiEmbeddingHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	var response dto.FlexibleEmbeddingResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.Data, 1)
	encoded, ok := response.Data[0].Embedding.(string)
	require.True(t, ok)
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	require.Len(t, decoded, 8)
	assert.Equal(t, float32(1), math.Float32frombits(binary.LittleEndian.Uint32(decoded[0:4])))
	assert.Equal(t, float32(-2.5), math.Float32frombits(binary.LittleEndian.Uint32(decoded[4:8])))
}

func TestOpenaiEmbeddingHandlerFillsMissingUsage(t *testing.T) {
	request := &dto.EmbeddingRequest{Model: "text-embedding-3-small", Input: "hello"}
	c, recorder, resp, info := newEmbeddingTestContext(t, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"model":"text-embedding-3-small"}`, request)

	usage, apiErr := OpenaiEmbeddingHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 7, usage.PromptTokens)
	assert.Equal(t, 7, usage.TotalTokens)
	assert.Contains(t, recorder.Body.String(), `"prompt_tokens":7`)
}
