package openai

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func OaiResponsesCompactionHandler(c *gin.Context, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	if upstreamErr := parseUpstreamErrorEnvelope(responseBody); upstreamErr != nil {
		return nil, upstreamErr
	}

	var compactResp dto.OpenAIResponsesCompactionResponse
	if err := common.Unmarshal(responseBody, &compactResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := compactResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}
	if strings.TrimSpace(compactResp.ID) == "" && strings.TrimSpace(compactResp.Object) == "" && len(bytes.TrimSpace(compactResp.Output)) == 0 {
		return nil, unexpectedUpstreamPayloadError("responses compaction", responseBody)
	}

	service.IOCopyBytesGracefully(c, resp, responseBody)

	usage := dto.Usage{}
	if compactResp.Usage != nil {
		usage.PromptTokens = compactResp.Usage.InputTokens
		usage.CompletionTokens = compactResp.Usage.OutputTokens
		usage.TotalTokens = compactResp.Usage.TotalTokens
		if compactResp.Usage.InputTokensDetails != nil {
			usage.PromptTokensDetails.CachedTokens = compactResp.Usage.InputTokensDetails.CachedTokens
		}
	}

	return &usage, nil
}
