package service

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newMidjourneyRequestTestContext(t *testing.T) *gin.Context {
	t.Helper()
	if GetHttpClient() == nil {
		InitHttpClient()
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/mj/submit/imagine", nil)
	return c
}

func TestDoMidjourneyHttpRequestTreatsBusinessFailureAsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":23,"description":"raw queue failure sk-provider","result":"private-task-id","properties":{"node":"private"}}`))
	}))
	defer server.Close()
	c := newMidjourneyRequestTestContext(t)

	response, _, err := DoMidjourneyHttpRequest(c, 5*time.Second, server.URL)

	require.Error(t, err)
	require.Equal(t, 23, response.Response.Code)
	require.Contains(t, err.Error(), "raw queue failure")
	stored, exists := c.Get(ContextKeyMidjourneyUpstreamError)
	require.True(t, exists)
	require.Contains(t, stored.(error).Error(), "private-task-id")
}

func TestDoMidjourneyHttpRequestAllowsDocumentedSuccessCodes(t *testing.T) {
	for _, code := range []string{"1", "21", "22"} {
		t.Run(code, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"code":` + code + `,"description":"accepted","result":"task-id"}`))
			}))
			defer server.Close()
			c := newMidjourneyRequestTestContext(t)

			response, _, err := DoMidjourneyHttpRequest(c, 5*time.Second, server.URL)

			require.NoError(t, err)
			require.NotNil(t, response)
			wantCode, convertErr := strconv.Atoi(code)
			require.NoError(t, convertErr)
			require.Equal(t, wantCode, response.Response.Code)
		})
	}
}

func TestDoMidjourneyHttpRequestRejectsNonSuccessHTTPStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"code":1,"description":"misleading success","result":"private-id"}`))
	}))
	defer server.Close()
	c := newMidjourneyRequestTestContext(t)

	response, _, err := DoMidjourneyHttpRequest(c, 5*time.Second, server.URL)

	require.Error(t, err)
	require.Equal(t, http.StatusBadGateway, response.StatusCode)
	require.Equal(t, http.StatusBadGateway, c.GetInt(ContextKeyMidjourneyUpstreamStatus))
}
