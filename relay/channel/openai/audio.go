package openai

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/governance"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func OpenaiTTSHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	// Delay committing the downstream response until the first valid stream frame
	// (or the complete non-stream body) has been validated. This preserves normal
	// channel retry for an upstream error envelope in the first response frame.
	defer service.CloseResponseBodyGracefully(resp)
	usage := &dto.Usage{}
	usage.PromptTokens = info.GetEstimatePromptTokens()
	usage.TotalTokens = info.GetEstimatePromptTokens()
	audioFormat := ttsAudioFormat(info)

	if info.IsStream {
		var streamErr *types.NewAPIError
		var streamedAudioBytes int
		helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
			upstreamErr := parseUpstreamStreamError(data, sr.Event())
			if upstreamErr == nil {
				upstreamErr = validateTTSStreamPayload(data)
			}
			if upstreamErr != nil {
				if !handleUpstreamStreamError(c, info, sr, upstreamErr, safeStreamErrorChat, helper.HasStreamResponseStarted(c)) {
					streamErr = upstreamErr
				}
				return
			}
			if service.SundaySearch(data, "usage") {
				var simpleResponse dto.SimpleResponse
				if err := common.Unmarshal([]byte(data), &simpleResponse); err != nil {
					logger.LogError(c, err.Error())
					sr.Error(err)
				} else if simpleResponse.Usage.TotalTokens != 0 {
					usage.PromptTokens = simpleResponse.Usage.InputTokens
					usage.CompletionTokens = simpleResponse.OutputTokens
					usage.TotalTokens = simpleResponse.TotalTokens
				}
			}
			if err := helper.StringData(c, data); err != nil {
				sr.Error(err)
				return
			}
			streamedAudioBytes += ttsStreamAudioBytes(data)
		})
		if streamErr != nil {
			return nil, streamErr
		}
		applyPartialTTSStreamUsage(usage, streamedAudioBytes, audioFormat)
	} else {
		common.SetContextKey(c, constant.ContextKeyLocalCountTokens, true)
		// 读取响应体到缓冲区
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
		}
		if upstreamErr := governance.ParseUpstreamErrorEnvelope(bodyBytes); upstreamErr != nil {
			return nil, upstreamErr
		}
		if err := validateTTSAudioResponse(resp, bodyBytes); err != nil {
			return nil, err
		}

		// Only copy upstream headers after the body has been confirmed to be audio.
		// Otherwise a retry or safe JSON error could inherit audio/provider headers.
		service.IOCopyBytesGracefully(c, resp, bodyBytes)

		// 计算音频时长并更新 usage
		var duration float64
		var durationErr error

		if audioFormat == "pcm" {
			// PCM 格式没有文件头，根据 OpenAI TTS 的 PCM 参数计算时长
			// 采样率: 24000 Hz, 位深度: 16-bit (2 bytes), 声道数: 1
			const sampleRate = 24000
			const bytesPerSample = 2
			const channels = 1
			duration = float64(len(bodyBytes)) / float64(sampleRate*bytesPerSample*channels)
		} else {
			ext := "." + audioFormat
			reader := bytes.NewReader(bodyBytes)
			duration, durationErr = common.GetAudioDuration(c.Request.Context(), reader, ext)
		}

		usage.PromptTokensDetails.TextTokens = usage.PromptTokens

		if durationErr != nil {
			logger.LogWarn(c, fmt.Sprintf("failed to get audio duration: %v", durationErr))
			// 如果无法获取时长，则设置保底的 CompletionTokens，根据body大小计算
			sizeInKB := float64(len(bodyBytes)) / 1000.0
			estimatedTokens := int(math.Ceil(sizeInKB)) // 粗略估算每KB约等于1 token
			usage.CompletionTokens = estimatedTokens
			usage.CompletionTokenDetails.AudioTokens = estimatedTokens
		} else if duration > 0 {
			// 计算 token: ceil(duration) / 60.0 * 1000，即每分钟 1000 tokens
			completionTokens := int(math.Round(math.Ceil(duration) / 60.0 * 1000))
			usage.CompletionTokens = completionTokens
			usage.CompletionTokenDetails.AudioTokens = completionTokens
		}
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}

	return usage, nil
}

func ttsAudioFormat(info *relaycommon.RelayInfo) string {
	if info != nil {
		if audioReq, ok := info.Request.(*dto.AudioRequest); ok && strings.TrimSpace(audioReq.ResponseFormat) != "" {
			return strings.ToLower(strings.TrimSpace(audioReq.ResponseFormat))
		}
	}
	return "mp3"
}

func ttsStreamAudioBytes(data string) int {
	var chunk struct {
		Type  string `json:"type"`
		Audio string `json:"audio"`
		Delta string `json:"delta"`
	}
	if common.Unmarshal([]byte(data), &chunk) != nil {
		return 0
	}
	encoded := chunk.Audio
	if encoded == "" && strings.Contains(strings.ToLower(chunk.Type), "audio") {
		encoded = chunk.Delta
	}
	if encoded == "" {
		return 0
	}
	if comma := strings.IndexByte(encoded, ','); comma >= 0 && strings.Contains(strings.ToLower(encoded[:comma]), "base64") {
		encoded = encoded[comma+1:]
	}
	if decoded, err := base64.StdEncoding.DecodeString(encoded); err == nil {
		return len(decoded)
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(encoded); err == nil {
		return len(decoded)
	}
	// Some compatible providers place raw delta text in this field. Counting its
	// bytes is safer than treating already-delivered audio as free.
	return len(encoded)
}

func applyPartialTTSStreamUsage(usage *dto.Usage, audioBytes int, audioFormat string) {
	if usage == nil {
		return
	}
	usage.PromptTokensDetails.TextTokens = usage.PromptTokens
	if audioBytes <= 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
		return
	}

	estimatedTokens := 0
	if audioFormat == "pcm" {
		const pcmBytesPerSecond = 24000 * 2
		duration := float64(audioBytes) / float64(pcmBytesPerSecond)
		estimatedTokens = int(math.Round(math.Ceil(duration) / 60.0 * 1000))
	} else {
		estimatedTokens = int(math.Ceil(float64(audioBytes) / 1000.0))
	}
	if estimatedTokens < 1 {
		estimatedTokens = 1
	}
	if usage.CompletionTokens < estimatedTokens {
		usage.CompletionTokens = estimatedTokens
	}
	if usage.CompletionTokenDetails.AudioTokens == 0 {
		usage.CompletionTokenDetails.AudioTokens = usage.CompletionTokens
	}
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
}

func validateTTSAudioResponse(resp *http.Response, body []byte) *types.NewAPIError {
	contentType := ""
	if resp != nil {
		contentType = strings.ToLower(strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0]))
	}
	invalidType := strings.HasPrefix(contentType, "text/") ||
		strings.Contains(contentType, "json") ||
		strings.Contains(contentType, "xml") ||
		strings.Contains(contentType, "html") ||
		contentType == "text/event-stream"
	if len(body) == 0 || invalidType || looksLikeTextTTSBody(body) {
		preview := body
		if len(preview) > 64<<10 {
			preview = preview[:64<<10]
		}
		return types.NewOpenAIError(
			fmt.Errorf("upstream TTS returned a non-audio response (content-type %q): %q", contentType, preview),
			types.ErrorCodeBadResponseBody,
			http.StatusBadGateway,
		)
	}
	return nil
}

func looksLikeTextTTSBody(body []byte) bool {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return true
	}
	if trimmed[0] == '{' || trimmed[0] == '[' || trimmed[0] == '<' {
		return true
	}
	sample := trimmed
	if len(sample) > 4096 {
		sample = sample[:4096]
	}
	printable := 0
	for _, value := range sample {
		if value == '\n' || value == '\r' || value == '\t' || (value >= 0x20 && value <= 0x7e) {
			printable++
		}
	}
	return len(sample) >= 16 && printable*100/len(sample) >= 95
}

func OpenaiSTTHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, responseFormat string) (*types.NewAPIError, *dto.Usage) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError), nil
	}
	if upstreamErr := governance.ParseUpstreamErrorEnvelope(responseBody); upstreamErr != nil {
		return upstreamErr, nil
	}
	if payloadErr := validateSTTResponse(responseFormat, responseBody); payloadErr != nil {
		return payloadErr, nil
	}
	// 写入新的 response body
	service.IOCopyBytesGracefully(c, resp, responseBody)

	var responseData struct {
		Usage *dto.Usage `json:"usage"`
	}
	if err := common.Unmarshal(responseBody, &responseData); err == nil && responseData.Usage != nil {
		if responseData.Usage.TotalTokens > 0 {
			usage := responseData.Usage
			if usage.PromptTokens == 0 {
				usage.PromptTokens = usage.InputTokens
			}
			if usage.CompletionTokens == 0 {
				usage.CompletionTokens = usage.OutputTokens
			}
			return nil, usage
		}
	}

	usage := &dto.Usage{}
	usage.PromptTokens = info.GetEstimatePromptTokens()
	usage.CompletionTokens = 0
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	return nil, usage
}

func validateSTTResponse(responseFormat string, body []byte) *types.NewAPIError {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return unexpectedUpstreamPayloadError("speech-to-text", body)
	}

	if json.Valid(trimmed) && trimmed[0] == '{' {
		var payload map[string]json.RawMessage
		if common.Unmarshal(trimmed, &payload) == nil {
			if _, ok := payload["text"]; ok {
				return nil
			}
			return unexpectedUpstreamPayloadError("speech-to-text", body)
		}
	}

	format := strings.ToLower(strings.TrimSpace(responseFormat))
	if format == "" || format == "json" || format == "verbose_json" || format == "diarized_json" {
		return unexpectedUpstreamPayloadError("speech-to-text", body)
	}

	lower := strings.ToLower(string(trimmed))
	for _, prefix := range []string{
		"all nodes failed",
		"internal server error",
		"service internal error",
		"upstream request failed",
		"bad gateway",
		"service unavailable",
		"model name not specified",
		"no available channel",
	} {
		if strings.HasPrefix(lower, prefix) {
			return unexpectedUpstreamPayloadError("speech-to-text", body)
		}
	}
	return nil
}
