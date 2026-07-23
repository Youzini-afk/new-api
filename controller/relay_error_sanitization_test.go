package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/governance"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestShouldRetryStopsAfterSemanticStreamOutput(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	helper.MarkStreamResponseStarted(c)
	err := types.NewOpenAIError(errors.New("upstream failed"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)

	require.False(t, shouldRetry(c, err, 2))
}

func TestWriteCommittedSafeRelayErrorUsesResponsesProtocol(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	safe := governance.SafeErrorPayload{
		StatusCode: http.StatusBadGateway,
		OpenAIError: types.OpenAIError{
			Message: "safe response error",
			Type:    "server_error",
			Code:    "internal_error",
		},
	}

	writeCommittedSafeRelayError(c, types.RelayFormatOpenAIResponses, safe)

	body := recorder.Body.String()
	require.Contains(t, body, "event: response.error")
	require.Contains(t, body, `"type":"response.error"`)
	require.Contains(t, body, "safe response error")
}

func TestRespondTaskErrorSanitizesUpstreamDetailAndData(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", nil)
	c.Set(common.RequestIdKey, "local-task-test-id")
	taskErr := &dto.TaskError{
		Code:       "provider_node_failed",
		Message:    "all nodes failed at https://secret.example using sk-secret123456 (request id: upstream-999)",
		Data:       map[string]any{"raw": "provider-private-data"},
		StatusCode: http.StatusBadGateway,
		Error:      errors.New("provider internal error"),
	}

	respondTaskError(c, taskErr)

	require.GreaterOrEqual(t, recorder.Code, http.StatusBadRequest)
	body := recorder.Body.String()
	require.NotContains(t, body, "all nodes failed")
	require.NotContains(t, body, "secret.example")
	require.NotContains(t, body, "sk-secret123456")
	require.NotContains(t, body, "upstream-999")
	require.NotContains(t, body, "provider-private-data")
	require.Contains(t, body, "local-task-test-id")

	var response dto.TaskError
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Empty(t, response.Data)
	require.NotEmpty(t, response.Code)
	require.NotEmpty(t, response.Message)
}

func TestResetUncommittedStreamHeadersAllowsJSONError(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	helper.SetEventStreamHeaders(c)
	require.Equal(t, "text/event-stream", c.Writer.Header().Get("Content-Type"))

	resetUncommittedStreamHeaders(c)
	c.JSON(http.StatusBadGateway, gin.H{"error": "safe"})

	require.Equal(t, "application/json; charset=utf-8", recorder.Header().Get("Content-Type"))
	require.Empty(t, recorder.Header().Get("Transfer-Encoding"))
	require.JSONEq(t, `{"error":"safe"}`, recorder.Body.String())
}
