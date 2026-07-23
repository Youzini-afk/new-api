package service

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/gin-gonic/gin"
)

func CloseResponseBodyGracefully(httpResponse *http.Response) {
	if httpResponse == nil || httpResponse.Body == nil {
		return
	}
	err := httpResponse.Body.Close()
	if err != nil {
		common.SysError("failed to close response body: " + err.Error())
	}
}

// ShouldCopyUpstreamHeader applies a deliberately small allowlist. Provider
// request IDs, cookies, redirects and debug headers are internal metadata and
// must not cross the relay boundary. Content headers required to decode a
// successful audio/image/file response remain compatible.
func ShouldCopyUpstreamHeader(c *gin.Context, k string, v []string) bool {
	canonical := http.CanonicalHeaderKey(strings.TrimSpace(k))
	if isUpstreamRequestIDHeader(canonical) {
		if c != nil && len(v) > 0 {
			c.Set(common.UpstreamRequestIdKey, v[0])
		}
		return false
	}

	switch canonical {
	case "Content-Type",
		"Content-Length",
		"Content-Encoding",
		"Content-Disposition",
		"Cache-Control",
		"Expires",
		"Etag",
		"Last-Modified",
		"Accept-Ranges",
		"Content-Range",
		"Retry-After",
		"Vary",
		"X-Content-Type-Options":
		return true
	default:
		return false
	}
}

func isUpstreamRequestIDHeader(name string) bool {
	if strings.EqualFold(name, common.RequestIdKey) {
		return true
	}
	switch strings.ToLower(name) {
	case "x-request-id",
		"request-id",
		"x-upstream-request-id",
		"x-amzn-requestid",
		"x-amz-request-id",
		"x-google-request-id",
		"openai-request-id",
		"cf-ray":
		return true
	default:
		return false
	}
}

func IOCopyBytesGracefully(c *gin.Context, src *http.Response, data []byte) {
	if c.Writer == nil {
		return
	}

	body := io.NopCloser(bytes.NewBuffer(data))

	// We shouldn't set the header before we parse the response body, because the parse part may fail.
	// And then we will have to send an error response, but in this case, the header has already been set.
	// So the httpClient will be confused by the response.
	// For example, Postman will report error, and we cannot check the response at all.
	if src != nil {
		for k, v := range src.Header {
			if !ShouldCopyUpstreamHeader(c, k, v) {
				continue
			}
			c.Writer.Header().Set(k, v[0])
		}
	}

	// set Content-Length header manually BEFORE calling WriteHeader
	c.Writer.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))

	// Write header with status code (this sends the headers)
	if src != nil {
		c.Writer.WriteHeader(src.StatusCode)
	} else {
		c.Writer.WriteHeader(http.StatusOK)
	}

	_, err := io.Copy(c.Writer, body)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("failed to copy response body: %s", err.Error()))
	}
	c.Writer.Flush()
}
