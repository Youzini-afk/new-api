package helper

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStringDataRestoresMappedResponseModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/v1/chat/completions", nil)
	c.Set(string(constant.ContextKeyResponseModelName), "public-model")

	err := StringData(c, `{"model":"provider/model-v2","choices":[]}`)
	require.NoError(t, err)
	assert.Contains(t, recorder.Body.String(), `data: {"model":"public-model","choices":[]}`)
}

func TestStringDataLeavesUnmappedResponseUntouched(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/v1/chat/completions", nil)

	err := StringData(c, `{"model":"provider/model-v2"}`)
	require.NoError(t, err)
	assert.Contains(t, recorder.Body.String(), `data: {"model":"provider/model-v2"}`)
}
