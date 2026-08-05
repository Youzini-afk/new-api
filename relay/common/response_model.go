package common

import (
	"strings"

	"github.com/QuantumNous/new-api/constant"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var responseModelPaths = []string{
	"model",
	"response.model",
	"message.model",
	"session.model",
	"modelVersion",
	"response.modelVersion",
}

// ResponseModelName returns the model name that may be exposed to the client.
// Internal routing, billing, and logging must continue to use UpstreamModelName.
func (info *RelayInfo) ResponseModelName(upstreamModel string) string {
	if info == nil || info.ChannelMeta == nil {
		return upstreamModel
	}
	if publicModel := strings.TrimSpace(info.ResponseModelAlias); publicModel != "" {
		if info.ChannelMeta.IsModelMapped || info.RelayMode == relayconstant.RelayModeResponsesCompact {
			return publicModel
		}
	}
	if !info.ChannelMeta.IsModelMapped {
		return upstreamModel
	}
	if originModel := strings.TrimSpace(info.OriginModelName); originModel != "" {
		return originModel
	}
	return upstreamModel
}

// ResponseModelNameFromContext returns the public alias selected by
// ModelMappedHelper for this specific channel attempt. An empty result means
// that the response must be left untouched.
func ResponseModelNameFromContext(c *gin.Context) string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.GetString(string(constant.ContextKeyResponseModelName)))
}

// RewriteResponseModel restores a mapped request's public model alias in known
// protocol response locations. For compact requests this is the suffix-free
// model submitted by the client. Unrelated JSON fields are preserved; non-JSON
// bodies and payloads without a known model field remain byte-identical.
func RewriteResponseModel(body []byte, publicModel string) ([]byte, error) {
	publicModel = strings.TrimSpace(publicModel)
	if publicModel == "" || !gjson.ValidBytes(body) {
		return body, nil
	}

	rewritten := body
	for _, path := range responseModelPaths {
		if !gjson.GetBytes(rewritten, path).Exists() {
			continue
		}
		var err error
		rewritten, err = sjson.SetBytes(rewritten, path, publicModel)
		if err != nil {
			return body, err
		}
	}
	return rewritten, nil
}

func RewriteResponseModelFromContext(c *gin.Context, body []byte) ([]byte, error) {
	return RewriteResponseModel(body, ResponseModelNameFromContext(c))
}

func (info *RelayInfo) RewriteResponseModel(body []byte) ([]byte, error) {
	return RewriteResponseModel(body, info.ResponseModelName(""))
}
