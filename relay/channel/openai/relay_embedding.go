package openai

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel/openrouter"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func OpenaiEmbeddingHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	logger.LogDebug(c, "upstream embedding response body: %s", responseBody)

	if info.ChannelType == constant.ChannelTypeOpenRouter && info.ChannelOtherSettings.IsOpenRouterEnterprise() {
		var enterpriseResponse openrouter.OpenRouterEnterpriseResponse
		if err := common.Unmarshal(responseBody, &enterpriseResponse); err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
		if !enterpriseResponse.Success {
			logger.LogError(c, fmt.Sprintf("openrouter enterprise response success=false, data: %s", enterpriseResponse.Data))
			return nil, types.NewOpenAIError(fmt.Errorf("openrouter response success=false"), types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
		responseBody = enterpriseResponse.Data
	}

	var simpleResponse dto.OpenAITextResponse
	if err := common.Unmarshal(responseBody, &simpleResponse); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := simpleResponse.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	var embeddingResponse dto.FlexibleEmbeddingResponse
	if err := common.Unmarshal(responseBody, &embeddingResponse); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	request, _ := info.Request.(*dto.EmbeddingRequest)
	encodingModified, err := NormalizeOpenAIEmbeddingResponseEncoding(request, &embeddingResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if err := ValidateOpenAIEmbeddingResponse(request, &embeddingResponse); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	usageModified := false
	if embeddingResponse.Usage.PromptTokens == 0 {
		if embeddingResponse.Usage.TotalTokens > 0 {
			embeddingResponse.Usage.PromptTokens = embeddingResponse.Usage.TotalTokens
		} else {
			embeddingResponse.Usage.PromptTokens = info.GetEstimatePromptTokens()
		}
		usageModified = true
	}
	if embeddingResponse.Usage.TotalTokens == 0 {
		embeddingResponse.Usage.TotalTokens = embeddingResponse.Usage.PromptTokens + embeddingResponse.Usage.CompletionTokens
		usageModified = true
	}
	applyUsagePostProcessing(info, &embeddingResponse.Usage, responseBody)
	if encodingModified || usageModified {
		var bodyMap map[string]interface{}
		if err := common.Unmarshal(responseBody, &bodyMap); err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
		if encodingModified {
			bodyMap["data"] = embeddingResponse.Data
		}
		if usageModified {
			bodyMap["usage"] = embeddingResponse.Usage
		}
		responseBody, err = common.Marshal(bodyMap)
		if err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
	}

	service.IOCopyBytesGracefully(c, resp, responseBody)
	return &embeddingResponse.Usage, nil
}

// NormalizeOpenAIEmbeddingResponseEncoding converts numeric embeddings to the
// OpenAI-compatible base64 representation when the client requested it but the
// upstream provider returned float arrays instead.
func NormalizeOpenAIEmbeddingResponseEncoding(request *dto.EmbeddingRequest, response *dto.FlexibleEmbeddingResponse) (bool, error) {
	if request == nil || response == nil || !strings.EqualFold(strings.TrimSpace(request.EncodingFormat), "base64") {
		return false, nil
	}

	modified := false
	for i := range response.Data {
		if _, ok := response.Data[i].Embedding.(string); ok {
			continue
		}
		values, ok := openAIEmbeddingFloat32Values(response.Data[i].Embedding)
		if !ok || len(values) == 0 {
			return false, fmt.Errorf("embedding response data[%d] cannot be converted to base64", i)
		}
		buffer := make([]byte, len(values)*4)
		for index, value := range values {
			binary.LittleEndian.PutUint32(buffer[index*4:], math.Float32bits(value))
		}
		response.Data[i].Embedding = base64.StdEncoding.EncodeToString(buffer)
		modified = true
	}
	return modified, nil
}

func openAIEmbeddingFloat32Values(value any) ([]float32, bool) {
	switch embedding := value.(type) {
	case []any:
		values := make([]float32, len(embedding))
		for i, item := range embedding {
			converted, ok := openAIEmbeddingNumberToFloat32(item)
			if !ok {
				return nil, false
			}
			values[i] = converted
		}
		return values, true
	case []float64:
		values := make([]float32, len(embedding))
		for i, item := range embedding {
			values[i] = float32(item)
		}
		return values, true
	case []float32:
		return embedding, true
	case []int:
		values := make([]float32, len(embedding))
		for i, item := range embedding {
			values[i] = float32(item)
		}
		return values, true
	default:
		return nil, false
	}
}

func openAIEmbeddingNumberToFloat32(value any) (float32, bool) {
	switch number := value.(type) {
	case float64:
		return float32(number), true
	case float32:
		return number, true
	case int:
		return float32(number), true
	case int64:
		return float32(number), true
	case int32:
		return float32(number), true
	case uint:
		return float32(number), true
	case uint64:
		return float32(number), true
	case uint32:
		return float32(number), true
	default:
		return 0, false
	}
}

func ValidateOpenAIEmbeddingResponse(request *dto.EmbeddingRequest, response *dto.FlexibleEmbeddingResponse) error {
	if response == nil || len(response.Data) == 0 {
		return fmt.Errorf("embedding response data is empty")
	}
	if expected := expectedOpenAIEmbeddingCount(request); expected > 0 && len(response.Data) != expected {
		return fmt.Errorf("embedding response data count mismatch: expected %d, got %d", expected, len(response.Data))
	}
	allowBase64 := request != nil && strings.EqualFold(strings.TrimSpace(request.EncodingFormat), "base64")
	for i, item := range response.Data {
		if !validOpenAIEmbeddingValue(item.Embedding, allowBase64) {
			return fmt.Errorf("embedding response data[%d] has invalid embedding", i)
		}
	}
	return nil
}

func expectedOpenAIEmbeddingCount(request *dto.EmbeddingRequest) int {
	if request == nil || request.Input == nil {
		return 0
	}
	switch input := request.Input.(type) {
	case string:
		return 1
	case []string:
		return len(input)
	case []any:
		if len(input) == 0 {
			return 0
		}
		if allNumbers(input) {
			return 1
		}
		return len(input)
	default:
		return 1
	}
}

func allNumbers(values []any) bool {
	for _, value := range values {
		switch value.(type) {
		case float64, float32, int, int64, int32, uint, uint64, uint32:
		default:
			return false
		}
	}
	return len(values) > 0
}

func validOpenAIEmbeddingValue(value any, allowBase64 bool) bool {
	if allowBase64 {
		if s, ok := value.(string); ok {
			return strings.TrimSpace(s) != ""
		}
	}
	switch embedding := value.(type) {
	case []any:
		if len(embedding) == 0 {
			return false
		}
		for _, number := range embedding {
			if !isEmbeddingNumber(number) {
				return false
			}
		}
		return true
	case []float64:
		return len(embedding) > 0
	case []float32:
		return len(embedding) > 0
	case []int:
		return len(embedding) > 0
	}
	return false
}

func isEmbeddingNumber(value any) bool {
	switch value.(type) {
	case float64, float32, int, int64, int32, uint, uint64, uint32:
		return true
	default:
		return false
	}
}
