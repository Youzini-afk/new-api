package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestIOCopyBytesGracefullyRestoresMappedResponseModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set(string(constant.ContextKeyResponseModelName), "public-model")

	IOCopyBytesGracefully(c, nil, []byte(`{"model":"provider/model-v2","value":1}`))

	assert.Equal(t, 200, recorder.Code)
	assert.JSONEq(t, `{"model":"public-model","value":1}`, recorder.Body.String())
	assert.Equal(t, "34", recorder.Header().Get("Content-Length"))
}
