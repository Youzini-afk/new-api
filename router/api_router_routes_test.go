package router

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUsageLogRoutesAcceptNoTrailingSlash(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	SetApiRouter(r)

	routes := map[string]bool{}
	for _, route := range r.Routes() {
		routes[route.Method+" "+route.Path] = true
	}

	for _, path := range []string{"/api/log", "/api/mj", "/api/task"} {
		require.True(t, routes[http.MethodGet+" "+path], "missing route %s", path)
		assert.True(t, routes[http.MethodGet+" "+path+"/"], "missing trailing slash route %s/", path)
	}
}
