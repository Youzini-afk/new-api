package openai

import (
	"encoding/json"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/governance"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
)

type safeStreamErrorStyle int

const (
	safeStreamErrorChat safeStreamErrorStyle = iota
	safeStreamErrorResponses
	safeStreamErrorImage
)

// parseUpstreamStreamError recognizes OpenAI-compatible error envelopes before
// a stream handler forwards their raw data. It intentionally requires an error
// field/type (or response.failed) so normal model text mentioning "error" is not
// misclassified.
func parseUpstreamStreamError(data string, event ...string) *types.NewAPIError {
	if len(event) > 0 {
		return governance.ParseUpstreamStreamEvent(event[0], data)
	}
	return governance.ParseUpstreamStreamError(data)
}

func parseUpstreamErrorEnvelope(data []byte) *types.NewAPIError {
	return governance.ParseUpstreamErrorEnvelope(data)
}

func unexpectedUpstreamPayloadError(scope string, data []byte) *types.NewAPIError {
	preview := data
	if len(preview) > 64<<10 {
		preview = preview[:64<<10]
	}
	return types.NewOpenAIError(
		fmt.Errorf("upstream %s returned an unexpected payload: %s", scope, preview),
		types.ErrorCodeBadResponseBody,
		http.StatusBadGateway,
	)
}

func validateOpenAIChatStreamPayload(data string) *types.NewAPIError {
	var payload struct {
		ID                  string          `json:"id"`
		Object              string          `json:"object"`
		Choices             json.RawMessage `json:"choices"`
		Usage               json.RawMessage `json:"usage"`
		PromptFilterResults json.RawMessage `json:"prompt_filter_results"`
	}
	if common.Unmarshal([]byte(data), &payload) != nil {
		return unexpectedUpstreamPayloadError("chat stream", []byte(data))
	}
	if strings.TrimSpace(payload.ID) != "" || strings.TrimSpace(payload.Object) != "" ||
		len(payload.Choices) > 0 || len(payload.Usage) > 0 || len(payload.PromptFilterResults) > 0 {
		return nil
	}
	return unexpectedUpstreamPayloadError("chat stream", []byte(data))
}

func validateResponsesStreamPayload(data string) *types.NewAPIError {
	var payload struct {
		Type string `json:"type"`
	}
	if common.Unmarshal([]byte(data), &payload) != nil || strings.TrimSpace(payload.Type) == "" {
		return unexpectedUpstreamPayloadError("responses stream", []byte(data))
	}
	return nil
}

func validateImageStreamPayload(data string) *types.NewAPIError {
	var payload struct {
		Type    string          `json:"type"`
		Data    json.RawMessage `json:"data"`
		URL     string          `json:"url"`
		B64JSON string          `json:"b64_json"`
		Usage   json.RawMessage `json:"usage"`
	}
	if common.Unmarshal([]byte(data), &payload) != nil {
		return unexpectedUpstreamPayloadError("image stream", []byte(data))
	}
	if strings.TrimSpace(payload.Type) != "" || len(payload.Data) > 0 || payload.URL != "" || payload.B64JSON != "" || len(payload.Usage) > 0 {
		return nil
	}
	return unexpectedUpstreamPayloadError("image stream", []byte(data))
}

func validateTTSStreamPayload(data string) *types.NewAPIError {
	var payload struct {
		Type  string          `json:"type"`
		Audio string          `json:"audio"`
		Delta string          `json:"delta"`
		Usage json.RawMessage `json:"usage"`
	}
	if common.Unmarshal([]byte(data), &payload) != nil {
		return unexpectedUpstreamPayloadError("TTS stream", []byte(data))
	}
	if strings.TrimSpace(payload.Type) != "" || payload.Audio != "" || payload.Delta != "" || len(payload.Usage) > 0 {
		return nil
	}
	return unexpectedUpstreamPayloadError("TTS stream", []byte(data))
}

// handleUpstreamStreamError stops the upstream scanner. Before any semantic
// downstream data it leaves the error for the controller so normal channel
// retry remains available. After semantic output it emits a safe protocol-
// compatible error event, records the original for admin/channel processing,
// and reports the error as handled so partial usage can still be settled.
func handleUpstreamStreamError(
	c *gin.Context,
	info *relaycommon.RelayInfo,
	sr *helper.StreamResult,
	err *types.NewAPIError,
	style safeStreamErrorStyle,
	clientDataSent bool,
) bool {
	if err == nil {
		return false
	}
	sr.Stop(err)
	if !clientDataSent {
		return false
	}

	safe := governance.SanitizeRelayErrorForClient(c, err)
	_ = writeSafeStreamError(c, info, safe, style)
	governance.MarkHandledStreamError(c, err)
	return true
}

func writeSafeStreamError(c *gin.Context, info *relaycommon.RelayInfo, safe governance.SafeErrorPayload, style safeStreamErrorStyle) error {
	if info != nil && info.RelayFormat == types.RelayFormatClaude {
		return helper.ClaudeData(c, dto.ClaudeResponse{
			Type:  "error",
			Error: safe.ClaudeError(),
		})
	}
	if info != nil && info.RelayFormat == types.RelayFormatGemini {
		return helper.ObjectData(c, map[string]any{"error": safe.GeminiError()})
	}

	switch style {
	case safeStreamErrorResponses:
		payload := map[string]any{
			"type":  "response.error",
			"error": safe.OpenAIError,
		}
		data, err := common.Marshal(payload)
		if err != nil {
			return err
		}
		helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: "response.error"}, string(data))
		return nil
	case safeStreamErrorImage:
		payload := map[string]any{
			"type":  "error",
			"error": safe.OpenAIError,
		}
		data, err := common.Marshal(payload)
		if err != nil {
			return err
		}
		helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: "error"}, string(data))
		return nil
	default:
		if err := helper.ObjectData(c, map[string]any{"error": safe.OpenAIError}); err != nil {
			return err
		}
		return nil
	}
}
