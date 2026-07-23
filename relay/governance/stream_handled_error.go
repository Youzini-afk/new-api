package governance

import (
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const handledStreamErrorContextKey = "relay_handled_stream_error"

// MarkHandledStreamError records an upstream stream error whose safe client
// response has already been emitted by the stream handler. The controller can
// still run channel health/error-insight processing without retrying, writing a
// second response, or refunding already-produced partial output.
func MarkHandledStreamError(c *gin.Context, err *types.NewAPIError) {
	if c == nil || err == nil {
		return
	}
	c.Set(handledStreamErrorContextKey, err)
}

func HandledStreamError(c *gin.Context) *types.NewAPIError {
	if c == nil {
		return nil
	}
	value, ok := c.Get(handledStreamErrorContextKey)
	if !ok {
		return nil
	}
	err, _ := value.(*types.NewAPIError)
	return err
}
