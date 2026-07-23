package zhipu

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/governance"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newZhipuErrorTestContext(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "local-zhipu-request")
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "chatglm"}}
	return c, recorder, resp, info
}

func TestZhipuStreamFirstErrorRemainsRetryable(t *testing.T) {
	c, recorder, resp, info := newZhipuErrorTestContext(t,
		"data:{\"success\":false,\"code\":500,\"msg\":\"raw-zhipu-secret\"}\n")

	usage, err := zhipuStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Empty(t, recorder.Body.String())
	require.Nil(t, governance.HandledStreamError(c))
}

func TestZhipuStreamSanitizesMidstreamError(t *testing.T) {
	body := "data:safe-zhipu-delta\n" +
		"data:{\"success\":false,\"code\":500,\"msg\":\"raw-zhipu-secret https://private.example sk-upstream\"}\n" +
		"data:must-not-be-forwarded\n"
	c, recorder, resp, info := newZhipuErrorTestContext(t, body)

	usage, err := zhipuStreamHandler(c, info, resp)

	require.NotNil(t, usage)
	require.Greater(t, usage.TotalTokens, 0)
	require.Nil(t, err)
	clientBody := recorder.Body.String()
	require.Contains(t, clientBody, "safe-zhipu-delta")
	require.NotContains(t, clientBody, "raw-zhipu-secret")
	require.NotContains(t, clientBody, "private.example")
	require.NotContains(t, clientBody, "sk-upstream")
	require.NotContains(t, clientBody, "must-not-be-forwarded")
	require.NotContains(t, clientBody, "[DONE]")
	require.NotNil(t, governance.HandledStreamError(c))
}

func TestZhipuStreamRecognizesRawErrorEvent(t *testing.T) {
	c, recorder, resp, info := newZhipuErrorTestContext(t,
		"event:error\n"+
			"data:raw-zhipu-event-secret https://private.example sk-upstream\n\n")

	usage, err := zhipuStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Contains(t, err.ToOpenAIError().Message, "raw-zhipu-event-secret")
	require.Empty(t, recorder.Body.String())
	require.Nil(t, governance.HandledStreamError(c))
}
