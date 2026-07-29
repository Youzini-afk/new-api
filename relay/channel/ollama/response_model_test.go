package ollama

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newMappedOllamaTestContext(body string) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: "NV",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "z-ai/glm-5.2",
			IsModelMapped:     true,
		},
	}
	return c, recorder, resp, info
}

func TestOllamaChatHandlerRestoresMappedResponseModel(t *testing.T) {
	body := `{"model":"z-ai/glm-5.2","created_at":"2026-07-29T12:00:00Z","message":{"role":"assistant","content":"ok"},"done":true,"done_reason":"stop","prompt_eval_count":2,"eval_count":1}`
	c, recorder, resp, info := newMappedOllamaTestContext(body)

	usage, err := ollamaChatHandler(c, info, resp)
	require.Nil(t, err)
	require.NotNil(t, usage)

	var response map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "NV", response["model"])
}

func TestOllamaStreamHandlerRestoresMappedResponseModel(t *testing.T) {
	body := strings.Join([]string{
		`{"model":"z-ai/glm-5.2","created_at":"2026-07-29T12:00:00Z","message":{"role":"assistant","content":"ok"},"done":false}`,
		`{"model":"z-ai/glm-5.2","created_at":"2026-07-29T12:00:01Z","done":true,"done_reason":"stop","prompt_eval_count":2,"eval_count":1}`,
	}, "\n")
	c, recorder, resp, info := newMappedOllamaTestContext(body)

	usage, err := ollamaStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Contains(t, recorder.Body.String(), `"model":"NV"`)
	require.NotContains(t, recorder.Body.String(), `z-ai/glm-5.2`)
}
