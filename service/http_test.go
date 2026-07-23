package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestShouldCopyUpstreamHeaderUsesSafeAllowlist(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	require.True(t, ShouldCopyUpstreamHeader(c, "Content-Type", []string{"audio/mpeg"}))
	require.True(t, ShouldCopyUpstreamHeader(c, "Content-Length", []string{"1234"}))
	require.True(t, ShouldCopyUpstreamHeader(c, "Content-Disposition", []string{"attachment"}))
	require.False(t, ShouldCopyUpstreamHeader(c, "Set-Cookie", []string{"provider=secret"}))
	require.False(t, ShouldCopyUpstreamHeader(c, "Location", []string{"https://private.example"}))
	require.False(t, ShouldCopyUpstreamHeader(c, "X-Debug-Key", []string{"sk-provider"}))
	require.False(t, ShouldCopyUpstreamHeader(c, "X-Upstream-Request-Id", []string{"provider-id"}))
	require.Equal(t, "provider-id", c.GetString(common.UpstreamRequestIdKey))
}

func TestIOCopyBytesGracefullyDoesNotExposeProviderHeaders(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":          []string{"application/octet-stream"},
			"Set-Cookie":            []string{"provider=secret"},
			"X-Upstream-Request-Id": []string{"private-id"},
		},
	}

	IOCopyBytesGracefully(c, response, []byte("safe-body"))

	require.Equal(t, "application/octet-stream", recorder.Header().Get("Content-Type"))
	require.Empty(t, recorder.Header().Get("Set-Cookie"))
	require.Empty(t, recorder.Header().Get("X-Upstream-Request-Id"))
	require.Equal(t, "safe-body", recorder.Body.String())
}
