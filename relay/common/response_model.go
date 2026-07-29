package common

import (
	"strings"

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
	if info == nil || info.ChannelMeta == nil || !info.ChannelMeta.IsModelMapped {
		return upstreamModel
	}
	if originModel := strings.TrimSpace(info.OriginModelName); originModel != "" {
		return originModel
	}
	return upstreamModel
}

// RewriteResponseModel restores a mapped request's public model alias in known
// protocol response locations while preserving unrelated JSON fields verbatim.
// Non-JSON bodies (for example TTS audio) and responses without a model field
// are returned unchanged.
func (info *RelayInfo) RewriteResponseModel(body []byte) ([]byte, error) {
	publicModel := info.ResponseModelName("")
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
