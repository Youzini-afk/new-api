package helper

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestStreamResponseStartedDistinguishesPingFromData(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	require.NoError(t, PingData(c))
	require.False(t, HasStreamResponseStarted(c))
	require.NoError(t, StringData(c, `{"choices":[]}`))
	require.True(t, HasStreamResponseStarted(c))
}
