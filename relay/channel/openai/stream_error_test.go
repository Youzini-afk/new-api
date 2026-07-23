package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/governance"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newOpenAIStreamTestContext(t *testing.T, path, body string, relayMode int) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	c.Set(common.RequestIdKey, "local-stream-test-id")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "test-model",
		},
		IsStream:        true,
		DisablePing:     true,
		RelayMode:       relayMode,
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "test-model",
	}
	info.SetEstimatePromptTokens(3)
	return c, recorder, resp, info
}

func configureStreamTest(t *testing.T) {
	t.Helper()
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
}

func TestParseUpstreamStreamError(t *testing.T) {
	tests := []struct {
		name string
		data string
		want bool
	}{
		{"top-level error", `{"error":{"message":"provider secret","type":"server_error","code":"node_failed"}}`, true},
		{"responses failed", `{"type":"response.failed","response":{"error":{"message":"provider secret","code":"server_error"}}}`, true},
		{"typed error", `{"type":"error","message":"provider secret"}`, true},
		{"malformed payload", `all nodes failed to stream`, true},
		{"normal chat chunk", `{"choices":[{"delta":{"content":"the word error is normal text"}}]}`, false},
		{"normal responses event", `{"type":"response.output_text.delta","delta":"error"}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, parseUpstreamStreamError(tt.data) != nil)
		})
	}
}

func TestOpenaiHandlerRejectsUntypedErrorEnvelopeBeforeForwarding(t *testing.T) {
	body := `{"error":{"message":"raw-nonstream-secret https://private.example sk-upstream","code":"provider_failed"}}`
	c, recorder, resp, info := newOpenAIStreamTestContext(t, "/v1/chat/completions", body, relayconstant.RelayModeChatCompletions)
	info.IsStream = false
	resp.Header.Set("Content-Type", "application/json")

	usage, err := OpenaiHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Contains(t, err.ToOpenAIError().Message, "raw-nonstream-secret")
	require.Empty(t, recorder.Body.String())
}

func TestOpenaiHandlerRejectsMessageOnlyUnexpectedPayload(t *testing.T) {
	body := `{"message":"raw message-only failure https://private.example sk-upstream"}`
	c, recorder, resp, info := newOpenAIStreamTestContext(t, "/v1/chat/completions", body, relayconstant.RelayModeChatCompletions)
	info.IsStream = false
	resp.Header.Set("Content-Type", "application/json")

	usage, err := OpenaiHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Contains(t, err.ToOpenAIError().Message, "raw message-only failure")
	require.Empty(t, recorder.Body.String())
}

func TestOaiStreamHandlerSanitizesMidStreamError(t *testing.T) {
	configureStreamTest(t)
	body := strings.Join([]string{
		`data: {"id":"chatcmpl-test","object":"chat.completion.chunk","model":"test-model","choices":[{"index":0,"delta":{"content":"visible-one"}}]}`,
		``,
		`data: {"id":"chatcmpl-test","object":"chat.completion.chunk","model":"test-model","choices":[{"index":0,"delta":{"content":"buffered-two"}}]}`,
		``,
		`data: {"error":{"message":"all nodes failed to stream at https://secret.example/v1 using sk-secret123456 (request id: upstream-req-999)","type":"server_error","code":"node_failed"}}`,
		``,
		`data: {"id":"chatcmpl-test","object":"chat.completion.chunk","model":"test-model","choices":[{"index":0,"delta":{"content":"must-not-appear"}}]}`,
		``,
	}, "\n")
	c, recorder, resp, info := newOpenAIStreamTestContext(t, "/v1/chat/completions", body, relayconstant.RelayModeChatCompletions)

	usage, err := OaiStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.NotNil(t, usage)
	require.NotNil(t, governance.HandledStreamError(c))
	require.True(t, helper.HasStreamResponseStarted(c))
	require.True(t, info.StreamStatus.HasErrors())

	clientBody := recorder.Body.String()
	require.Contains(t, clientBody, "visible-one")
	require.NotContains(t, clientBody, "buffered-two")
	require.NotContains(t, clientBody, "must-not-appear")
	require.Contains(t, clientBody, `"error"`)
	require.Contains(t, clientBody, "local-stream-test-id")
	require.NotContains(t, clientBody, "all nodes failed")
	require.NotContains(t, clientBody, "secret.example")
	require.NotContains(t, clientBody, "sk-secret123456")
	require.NotContains(t, clientBody, "upstream-req-999")
	require.NotContains(t, clientBody, "[DONE]")
}

func TestOaiStreamHandlerReturnsFirstFrameErrorForRetry(t *testing.T) {
	configureStreamTest(t)
	body := "data: {\"error\":{\"message\":\"first frame provider secret\",\"type\":\"server_error\"}}\n\n"
	c, recorder, resp, info := newOpenAIStreamTestContext(t, "/v1/chat/completions", body, relayconstant.RelayModeChatCompletions)

	usage, err := OaiStreamHandler(c, info, resp)
	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Nil(t, governance.HandledStreamError(c))
	require.False(t, helper.HasStreamResponseStarted(c))
	require.Empty(t, recorder.Body.String())
	require.True(t, info.StreamStatus.HasErrors())
}

func TestOaiStreamHandlerJoinsMultilineErrorWithoutEventName(t *testing.T) {
	configureStreamTest(t)
	body := "data: {\"error\":{\"message\":\"multiline provider secret\",\n" +
		"data: \"type\":\"server_error\",\"code\":\"node_failed\"}}\n\n"
	c, recorder, resp, info := newOpenAIStreamTestContext(t, "/v1/chat/completions", body, relayconstant.RelayModeChatCompletions)

	usage, err := OaiStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Contains(t, err.ToOpenAIError().Message, "multiline provider secret")
	require.False(t, helper.HasStreamResponseStarted(c))
	require.Empty(t, recorder.Body.String())
}

func TestOaiStreamHandlerRejectsMessageOnlyChunkBeforeForwarding(t *testing.T) {
	configureStreamTest(t)
	body := "data: {\"message\":\"raw stream failure sk-upstream\"}\n\n"
	c, recorder, resp, info := newOpenAIStreamTestContext(t, "/v1/chat/completions", body, relayconstant.RelayModeChatCompletions)

	usage, err := OaiStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Contains(t, err.ToOpenAIError().Message, "raw stream failure")
	require.Empty(t, recorder.Body.String())
}

func TestOaiResponsesStreamHandlerSanitizesMidStreamError(t *testing.T) {
	configureStreamTest(t)
	body := strings.Join([]string{
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","delta":"visible response text"}`,
		``,
		`event: response.failed`,
		`data: {"type":"response.failed","response":{"error":{"message":"response provider secret at https://secret.example with sk-secret987654","type":"server_error","code":"node_failed"}}}`,
		``,
	}, "\n")
	c, recorder, resp, info := newOpenAIStreamTestContext(t, "/v1/responses", body, relayconstant.RelayModeResponses)

	usage, err := OaiResponsesStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.NotNil(t, usage)
	require.NotNil(t, governance.HandledStreamError(c))
	require.True(t, info.StreamStatus.HasErrors())

	clientBody := recorder.Body.String()
	require.Contains(t, clientBody, "visible response text")
	require.Contains(t, clientBody, "event: response.error")
	require.Contains(t, clientBody, "local-stream-test-id")
	require.NotContains(t, clientBody, "response provider secret")
	require.NotContains(t, clientBody, "secret.example")
	require.NotContains(t, clientBody, "sk-secret987654")
}
