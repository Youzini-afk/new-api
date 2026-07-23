package openai

import (
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/governance"
	"github.com/stretchr/testify/require"
)

func TestOpenaiTTSStreamFirstErrorRemainsRetryable(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := "data: {\"error\":{\"message\":\"raw-tts-secret\",\"type\":\"server_error\"}}\n\ndata: [DONE]\n\n"
	c, recorder, resp, info := newImageTestContext(t, body, "text/event-stream", true)

	usage, err := OpenaiTTSHandler(c, resp, info)

	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Empty(t, recorder.Body.String())
	require.Nil(t, governance.HandledStreamError(c))
}

func TestOpenaiTTSStreamSanitizesErrorAfterAudioData(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	audioDelta := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 2048)))
	body := "data: {\"audio\":\"" + audioDelta + "\"}\n\n" +
		"data: {\"error\":{\"message\":\"raw-tts-secret https://private.example sk-upstream\",\"type\":\"server_error\"}}\n\n" +
		"data: {\"audio\":\"must-not-be-forwarded\"}\n\n"
	c, recorder, resp, info := newImageTestContext(t, body, "text/event-stream", true)

	usage, err := OpenaiTTSHandler(c, resp, info)

	require.NotNil(t, usage)
	require.Nil(t, err)
	clientBody := recorder.Body.String()
	require.Contains(t, clientBody, audioDelta)
	require.NotContains(t, clientBody, "raw-tts-secret")
	require.NotContains(t, clientBody, "private.example")
	require.NotContains(t, clientBody, "sk-upstream")
	require.NotContains(t, clientBody, "must-not-be-forwarded")
	require.NotNil(t, governance.HandledStreamError(c))
	require.Greater(t, usage.CompletionTokens, 0)
	require.Greater(t, usage.CompletionTokenDetails.AudioTokens, 0)
}

func TestOpenaiTTSRejectsTextBodyBeforeCopyingUpstreamHeaders(t *testing.T) {
	c, recorder, resp, info := newImageTestContext(t, "all nodes failed: raw-tts-secret", "text/plain", false)
	resp.Header.Set("X-Upstream-Request-Id", "provider-private-id")
	info.Request = &dto.AudioRequest{ResponseFormat: "mp3"}

	usage, err := OpenaiTTSHandler(c, resp, info)

	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, http.StatusBadGateway, err.StatusCode)
	require.Empty(t, recorder.Body.String())
	require.Empty(t, recorder.Header().Get("Content-Type"))
	require.Empty(t, recorder.Header().Get("X-Upstream-Request-Id"))
}

func TestOpenaiSTTRejectsUnexpected200Payload(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		contentType    string
		responseFormat string
	}{
		{
			name:           "message-only JSON",
			body:           `{"message":"raw-stt-provider-secret"}`,
			contentType:    "application/json",
			responseFormat: "json",
		},
		{
			name:           "plain infrastructure failure",
			body:           "all nodes failed to stream raw-stt-provider-secret",
			contentType:    "text/plain",
			responseFormat: "text",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, recorder, resp, info := newImageTestContext(t, test.body, test.contentType, false)

			err, usage := OpenaiSTTHandler(c, resp, info, test.responseFormat)

			require.NotNil(t, err)
			require.Nil(t, usage)
			require.Contains(t, err.ToOpenAIError().Message, "raw-stt-provider-secret")
			require.Empty(t, recorder.Body.String())
		})
	}
}

func TestOpenaiSTTAllowsPlainTextTranscript(t *testing.T) {
	c, recorder, resp, info := newImageTestContext(t, "a normal transcript", "text/plain", false)

	err, usage := OpenaiSTTHandler(c, resp, info, "text")

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, "a normal transcript", recorder.Body.String())
}
