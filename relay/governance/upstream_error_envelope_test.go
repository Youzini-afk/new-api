package governance

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseUpstreamErrorEnvelopeVariants(t *testing.T) {
	tests := []struct {
		name       string
		payload    string
		wantMsg    string
		wantStatus int
	}{
		{
			name:       "openai",
			payload:    `{"error":{"message":"raw openai detail","type":"server_error","code":"upstream_failed"}}`,
			wantMsg:    "raw openai detail",
			wantStatus: http.StatusBadGateway,
		},
		{
			name:       "gemini",
			payload:    `{"error":{"code":429,"message":"raw gemini detail","status":"RESOURCE_EXHAUSTED"}}`,
			wantMsg:    "raw gemini detail",
			wantStatus: http.StatusTooManyRequests,
		},
		{
			name:       "responses failed",
			payload:    `{"type":"response.failed","response":{"error":{"message":"raw responses detail","type":"server_error"}}}`,
			wantMsg:    "raw responses detail",
			wantStatus: http.StatusBadGateway,
		},
		{
			name:       "zhipu legacy",
			payload:    `{"success":false,"code":500,"msg":"raw zhipu detail"}`,
			wantMsg:    "raw zhipu detail",
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "top level numeric status",
			payload:    `{"status":500,"message":"raw node failure"}`,
			wantMsg:    "raw node failure",
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "top level numeric code",
			payload:    `{"code":503,"msg":"raw provider unavailable"}`,
			wantMsg:    "raw provider unavailable",
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "string failure status",
			payload:    `{"status":"failed","message":"raw failed response"}`,
			wantMsg:    "raw failed response",
			wantStatus: http.StatusBadGateway,
		},
		{
			name:       "numeric false success",
			payload:    `{"success":0,"message":"raw legacy failure"}`,
			wantMsg:    "raw legacy failure",
			wantStatus: http.StatusBadGateway,
		},
		{
			name:       "nested realtime response failure",
			payload:    `{"type":"response.done","response":{"status":"failed","status_details":{"error":{"message":"raw realtime failure","code":"provider_failed"}}}}`,
			wantMsg:    "raw realtime failure",
			wantStatus: http.StatusBadGateway,
		},
		{
			name:       "data error",
			payload:    `{"data":{"error":{"message":"raw nested data failure"}}}`,
			wantMsg:    "raw nested data failure",
			wantStatus: http.StatusBadGateway,
		},
		{
			name:       "duplicate error key cannot hide first error",
			payload:    `{"error":{"message":"raw first failure"},"error":null}`,
			wantMsg:    "raw first failure",
			wantStatus: http.StatusBadGateway,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ParseUpstreamErrorEnvelope([]byte(test.payload))
			require.NotNil(t, err)
			require.Equal(t, test.wantMsg, err.ToOpenAIError().Message)
			require.Equal(t, test.wantStatus, err.StatusCode)
		})
	}
}

func TestParseUpstreamErrorEnvelopeIgnoresOrdinaryPayloads(t *testing.T) {
	require.Nil(t, ParseUpstreamErrorEnvelope([]byte(`{"message":"ordinary model output","success":true}`)))
	require.Nil(t, ParseUpstreamErrorEnvelope([]byte(`{"status":200,"code":200,"message":"ordinary success"}`)))
	require.Nil(t, ParseUpstreamErrorEnvelope([]byte(`not json`)))
	require.Nil(t, ParseUpstreamErrorEnvelope([]byte(`{"error":null,"choices":[]}`)))
	require.Nil(t, ParseUpstreamErrorEnvelope(bytes.Repeat([]byte{0xff, 0x00, 0x01}, 4096)))
}

func TestParseUpstreamStreamErrorRejectsMalformedPayload(t *testing.T) {
	err := ParseUpstreamStreamError("all nodes failed to stream: private-upstream-id")
	require.NotNil(t, err)
	require.Equal(t, http.StatusBadGateway, err.StatusCode)
	require.Contains(t, err.ToOpenAIError().Message, "private-upstream-id")
}

func TestParseUpstreamStreamEventUsesExplicitErrorEventName(t *testing.T) {
	err := ParseUpstreamStreamEvent("response.failed", `{"message":"raw event-only secret"}`)
	require.NotNil(t, err)
	require.Contains(t, err.ToOpenAIError().Message, "raw event-only secret")
}
