package gemini

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/governance"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newGeminiErrorTestContext(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	t.Helper()
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini:test", nil)
	c.Set(common.RequestIdKey, "local-gemini-request")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatGemini,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-test"},
	}
	return c, recorder, resp, info
}

func TestGeminiNativeStreamFirstErrorRemainsRetryable(t *testing.T) {
	c, recorder, resp, info := newGeminiErrorTestContext(t,
		"data: {\"error\":{\"code\":503,\"message\":\"raw-gemini-secret\",\"status\":\"UNAVAILABLE\"}}\n\n")

	usage, err := GeminiTextGenerationStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Empty(t, recorder.Body.String())
	require.Nil(t, governance.HandledStreamError(c))
}

func TestGeminiNativeStreamSanitizesMidstreamError(t *testing.T) {
	body := strings.Join([]string{
		`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"safe-gemini-delta"}]}}]}`,
		``,
		`data: {"error":{"code":503,"message":"raw-gemini-secret https://private.example sk-upstream","status":"UNAVAILABLE"}}`,
		``,
		`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"must-not-be-forwarded"}]}}]}`,
		``,
	}, "\n")
	c, recorder, resp, info := newGeminiErrorTestContext(t, body)

	usage, err := GeminiTextGenerationStreamHandler(c, info, resp)

	require.NotNil(t, usage)
	require.Nil(t, err)
	clientBody := recorder.Body.String()
	require.Contains(t, clientBody, "safe-gemini-delta")
	require.Contains(t, clientBody, `"status":"INTERNAL"`)
	require.NotContains(t, clientBody, "raw-gemini-secret")
	require.NotContains(t, clientBody, "private.example")
	require.NotContains(t, clientBody, "sk-upstream")
	require.NotContains(t, clientBody, "must-not-be-forwarded")
	require.NotNil(t, governance.HandledStreamError(c))
}

func TestGeminiConvertedStreamDoesNotAppendSuccessAfterError(t *testing.T) {
	body := strings.Join([]string{
		`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"safe-gemini-delta"}]}}]}`,
		``,
		`data: {"error":{"code":503,"message":"raw-gemini-secret","status":"UNAVAILABLE"}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	c, recorder, resp, info := newGeminiErrorTestContext(t, body)
	info.RelayFormat = types.RelayFormatOpenAI

	usage, err := GeminiChatStreamHandler(c, info, resp)

	require.NotNil(t, usage)
	require.Nil(t, err)
	clientBody := recorder.Body.String()
	require.Contains(t, clientBody, "safe-gemini-delta")
	require.Contains(t, clientBody, `"error"`)
	require.NotContains(t, clientBody, "raw-gemini-secret")
	require.NotContains(t, clientBody, "data: [DONE]")
	require.NotNil(t, governance.HandledStreamError(c))
}

func TestGeminiConvertedNonStreamHandlersReturnUpstreamEnvelope(t *testing.T) {
	tests := []struct {
		name string
		run  func(*gin.Context, *relaycommon.RelayInfo, *http.Response) *types.NewAPIError
	}{
		{
			name: "chat",
			run: func(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) *types.NewAPIError {
				_, err := GeminiChatHandler(c, info, resp)
				return err
			},
		},
		{
			name: "embedding",
			run: func(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) *types.NewAPIError {
				_, err := GeminiEmbeddingHandler(c, info, resp)
				return err
			},
		},
		{
			name: "image",
			run: func(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) *types.NewAPIError {
				_, err := GeminiImageHandler(c, info, resp)
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, recorder, _, info := newGeminiErrorTestContext(t, "")
			body := `{"error":{"code":503,"message":"raw-converted-gemini-secret","status":"UNAVAILABLE"}}`
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(body)),
			}

			err := test.run(c, info, resp)

			require.NotNil(t, err)
			require.Equal(t, http.StatusServiceUnavailable, err.StatusCode)
			require.Contains(t, err.ToOpenAIError().Message, "raw-converted-gemini-secret")
			require.Empty(t, recorder.Body.String())
		})
	}
}

func TestGeminiNativeNonStreamHandlersRejectUnexpectedPayload(t *testing.T) {
	body := `{"message":"raw native Gemini failure https://private.example sk-upstream"}`

	t.Run("chat", func(t *testing.T) {
		c, recorder, _, info := newGeminiErrorTestContext(t, "")
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}

		usage, err := GeminiTextGenerationHandler(c, info, resp)

		require.Nil(t, usage)
		require.NotNil(t, err)
		require.Contains(t, err.ToOpenAIError().Message, "raw native Gemini failure")
		require.Empty(t, recorder.Body.String())
	})

	for _, batch := range []bool{false, true} {
		name := "single embedding"
		if batch {
			name = "batch embedding"
		}
		t.Run(name, func(t *testing.T) {
			c, recorder, _, info := newGeminiErrorTestContext(t, "")
			info.IsGeminiBatchEmbedding = batch
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(body)),
			}

			usage, err := NativeGeminiEmbeddingHandler(c, resp, info)

			require.Nil(t, usage)
			require.NotNil(t, err)
			require.Contains(t, err.ToOpenAIError().Message, "raw native Gemini failure")
			require.Empty(t, recorder.Body.String())
		})
	}
}
