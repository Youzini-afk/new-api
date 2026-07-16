package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	openaiChannel "github.com/QuantumNous/new-api/relay/channel/openai"
	"github.com/QuantumNous/new-api/relay/channel/siliconflow"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type relayBatchChunk struct {
	index  int
	offset int
	items  []json.RawMessage
	body   []byte
}

type relayBatchHTTPResult struct {
	body         []byte
	responseMeta *http.Response
	err          *types.NewAPIError
}

func clearRelayBatchSplitInfo(c *gin.Context, info *relaycommon.RelayInfo) {
	if info != nil {
		info.BatchSplit = nil
	}
	if c != nil {
		c.Set(relaycommon.ContextKeyBatchSplitInfo, nil)
	}
}

func publishRelayBatchSplitInfo(c *gin.Context, info *relaycommon.RelayInfo, batchInfo *relaycommon.BatchSplitInfo) {
	info.BatchSplit = batchInfo
	c.Set(relaycommon.ContextKeyBatchSplitInfo, batchInfo)
}

func maybeRelayEmbeddingInBatches(c *gin.Context, info *relaycommon.RelayInfo, jsonData []byte) (bool, *dto.Usage, *types.NewAPIError) {
	setting, enabled := system_setting.MatchRelayBatchSplit(info.ChannelId, system_setting.RelayBatchKindEmbedding)
	if !enabled {
		return false, nil, nil
	}

	payload, items, batchable, err := extractEmbeddingBatchItems(jsonData)
	if err != nil {
		return true, nil, batchPayloadError(err)
	}
	if !batchable || len(items) <= setting.BatchSize {
		return false, nil, nil
	}
	if len(items) > setting.MaxItems {
		return true, nil, batchMaxItemsError(system_setting.RelayBatchKindEmbedding, len(items), setting.MaxItems)
	}
	if !supportsRelayBatchSplitAPI(info.ApiType) {
		return true, nil, unsupportedBatchAPIError(info.ApiType)
	}

	chunks, err := buildRelayBatchChunks(payload, "input", items, setting.BatchSize, nil)
	if err != nil {
		return true, nil, batchPayloadError(err)
	}
	encodingFormat, err := rawJSONOptionalString(payload["encoding_format"])
	if err != nil {
		return true, nil, batchPayloadError(fmt.Errorf("invalid encoding_format: %w", err))
	}
	batchInfo := newRelayBatchSplitInfo(info, system_setting.RelayBatchKindEmbedding, len(items), setting, len(chunks))
	publishRelayBatchSplitInfo(c, info, batchInfo)
	startedAt := time.Now()
	results, failedChunk, executionErr := executeRelayBatchChunks(c, info, chunks, setting.Concurrency)
	batchInfo.DurationMs = time.Since(startedAt).Milliseconds()
	batchInfo.CompletedChunks = countCompletedBatchChunks(results)
	if executionErr != nil {
		batchInfo.FailedChunk = failedChunk + 1
		return true, nil, executionErr
	}

	mergedData := make([]dto.FlexibleEmbeddingResponseItem, 0, len(items))
	var usage dto.Usage
	for i := range chunks {
		parsed, chunkUsage, parseErr := parseEmbeddingBatchChunk(results[i].body, info, chunks[i], encodingFormat)
		if parseErr != nil {
			batchInfo.FailedChunk = i + 1
			return true, nil, parseErr
		}
		for _, item := range parsed.Data {
			item.Index += chunks[i].offset
			mergedData = append(mergedData, item)
		}
		addRelayBatchUsage(&usage, chunkUsage)
	}
	sort.SliceStable(mergedData, func(i, j int) bool {
		return mergedData[i].Index < mergedData[j].Index
	})
	for index := range mergedData {
		if mergedData[index].Index != index {
			batchInfo.FailedChunk = 1
			return true, nil, batchResponseError(1, fmt.Errorf("embedding response index mismatch at %d", index))
		}
	}

	responseBody, err := mergeBatchResponseBody(results[0].body, "data", mergedData, usage)
	if err != nil {
		return true, nil, batchResponseError(1, err)
	}
	info.UpstreamRequestBodySize = totalRelayBatchRequestSize(chunks)
	info.SetFirstResponseTime()
	service.IOCopyBytesGracefully(c, results[0].responseMeta, responseBody)
	return true, &usage, nil
}

func maybeRelayRerankInBatches(c *gin.Context, info *relaycommon.RelayInfo, jsonData []byte) (bool, *dto.Usage, *types.NewAPIError) {
	setting, enabled := system_setting.MatchRelayBatchSplit(info.ChannelId, system_setting.RelayBatchKindRerank)
	if !enabled {
		return false, nil, nil
	}

	payload, documents, err := extractArrayPayloadField(jsonData, "documents")
	if err != nil {
		return true, nil, batchPayloadError(err)
	}
	if len(documents) <= setting.BatchSize {
		return false, nil, nil
	}
	if len(documents) > setting.MaxItems {
		return true, nil, batchMaxItemsError(system_setting.RelayBatchKindRerank, len(documents), setting.MaxItems)
	}
	if !supportsRelayBatchSplitAPI(info.ApiType) {
		return true, nil, unsupportedBatchAPIError(info.ApiType)
	}

	topN, err := rawJSONOptionalInt(payload["top_n"])
	if err != nil {
		return true, nil, batchPayloadError(fmt.Errorf("invalid top_n: %w", err))
	}
	returnDocuments, err := rawJSONOptionalBool(payload["return_documents"])
	if err != nil {
		return true, nil, batchPayloadError(fmt.Errorf("invalid return_documents: %w", err))
	}
	query, err := rawJSONOptionalString(payload["query"])
	if err != nil {
		return true, nil, batchPayloadError(fmt.Errorf("invalid query: %w", err))
	}

	mutate := func(chunkPayload map[string]json.RawMessage, itemCount int) error {
		if topN == nil {
			return nil
		}
		localTopN := *topN
		if localTopN > itemCount {
			localTopN = itemCount
		}
		data, err := common.Marshal(localTopN)
		if err != nil {
			return err
		}
		chunkPayload["top_n"] = data
		return nil
	}
	chunks, err := buildRelayBatchChunks(payload, "documents", documents, setting.BatchSize, mutate)
	if err != nil {
		return true, nil, batchPayloadError(err)
	}
	batchInfo := newRelayBatchSplitInfo(info, system_setting.RelayBatchKindRerank, len(documents), setting, len(chunks))
	publishRelayBatchSplitInfo(c, info, batchInfo)
	startedAt := time.Now()
	results, failedChunk, executionErr := executeRelayBatchChunks(c, info, chunks, setting.Concurrency)
	batchInfo.DurationMs = time.Since(startedAt).Milliseconds()
	batchInfo.CompletedChunks = countCompletedBatchChunks(results)
	if executionErr != nil {
		batchInfo.FailedChunk = failedChunk + 1
		return true, nil, executionErr
	}

	mergedResults := make([]dto.RerankResponseResult, 0, len(documents))
	var usage dto.Usage
	for i := range chunks {
		expectedResults := len(chunks[i].items)
		if topN != nil && *topN < expectedResults {
			expectedResults = *topN
		}
		parsed, chunkUsage, parseErr := parseRerankBatchChunk(results[i].body, info, chunks[i], query, expectedResults)
		if parseErr != nil {
			batchInfo.FailedChunk = i + 1
			return true, nil, parseErr
		}
		for _, result := range parsed.Results {
			result.Index += chunks[i].offset
			result.Document = nil
			mergedResults = append(mergedResults, result)
		}
		addRelayBatchUsage(&usage, chunkUsage)
	}
	sort.SliceStable(mergedResults, func(i, j int) bool {
		if mergedResults[i].RelevanceScore == mergedResults[j].RelevanceScore {
			return mergedResults[i].Index < mergedResults[j].Index
		}
		return mergedResults[i].RelevanceScore > mergedResults[j].RelevanceScore
	})
	if topN != nil && *topN < len(mergedResults) {
		mergedResults = mergedResults[:*topN]
	}
	if returnDocuments {
		for i := range mergedResults {
			document, decodeErr := decodeRawJSONValue(documents[mergedResults[i].Index])
			if decodeErr != nil {
				return true, nil, batchPayloadError(decodeErr)
			}
			mergedResults[i].Document = document
		}
	}

	responseBody, err := mergeBatchResponseBody(results[0].body, "results", mergedResults, usage)
	if err != nil {
		return true, nil, batchResponseError(1, err)
	}
	info.UpstreamRequestBodySize = totalRelayBatchRequestSize(chunks)
	info.SetFirstResponseTime()
	service.IOCopyBytesGracefully(c, results[0].responseMeta, responseBody)
	return true, &usage, nil
}

func extractEmbeddingBatchItems(jsonData []byte) (map[string]json.RawMessage, []json.RawMessage, bool, error) {
	payload, items, err := extractArrayPayloadField(jsonData, "input")
	if err != nil {
		return nil, nil, false, err
	}
	if len(items) == 0 {
		return payload, items, false, nil
	}
	for _, item := range items {
		if common.GetJsonType(item) != "number" {
			return payload, items, true, nil
		}
	}
	// A flat numeric array is one tokenized input, not many embedding inputs.
	return payload, items, false, nil
}

func extractArrayPayloadField(jsonData []byte, field string) (map[string]json.RawMessage, []json.RawMessage, error) {
	var payload map[string]json.RawMessage
	if err := common.Unmarshal(jsonData, &payload); err != nil {
		return nil, nil, err
	}
	raw, exists := payload[field]
	if !exists {
		return nil, nil, fmt.Errorf("missing %s field", field)
	}
	if common.GetJsonType(raw) != "array" {
		return payload, nil, nil
	}
	var items []json.RawMessage
	if err := common.Unmarshal(raw, &items); err != nil {
		return nil, nil, err
	}
	return payload, items, nil
}

func buildRelayBatchChunks(
	payload map[string]json.RawMessage,
	field string,
	items []json.RawMessage,
	batchSize int,
	mutate func(map[string]json.RawMessage, int) error,
) ([]relayBatchChunk, error) {
	chunks := make([]relayBatchChunk, 0, (len(items)+batchSize-1)/batchSize)
	for start := 0; start < len(items); start += batchSize {
		end := start + batchSize
		if end > len(items) {
			end = len(items)
		}
		chunkPayload := make(map[string]json.RawMessage, len(payload))
		for key, value := range payload {
			chunkPayload[key] = value
		}
		chunkItems := items[start:end]
		itemData, err := common.Marshal(chunkItems)
		if err != nil {
			return nil, err
		}
		chunkPayload[field] = itemData
		if mutate != nil {
			if err := mutate(chunkPayload, len(chunkItems)); err != nil {
				return nil, err
			}
		}
		body, err := common.Marshal(chunkPayload)
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, relayBatchChunk{
			index:  len(chunks),
			offset: start,
			items:  chunkItems,
			body:   body,
		})
	}
	return chunks, nil
}

func executeRelayBatchChunks(c *gin.Context, info *relaycommon.RelayInfo, chunks []relayBatchChunk, concurrency int) ([]relayBatchHTTPResult, int, *types.NewAPIError) {
	batchContext, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	results := make([]relayBatchHTTPResult, len(chunks))
	jobs := make(chan int, len(chunks))
	for i := range chunks {
		jobs <- i
	}
	close(jobs)

	workerCount := concurrency
	if workerCount > len(chunks) {
		workerCount = len(chunks)
	}
	var waitGroup sync.WaitGroup
	waitGroup.Add(workerCount)
	for worker := 0; worker < workerCount; worker++ {
		go func() {
			defer waitGroup.Done()
			for {
				select {
				case <-batchContext.Done():
					return
				case index, ok := <-jobs:
					if !ok {
						return
					}
					results[index] = executeRelayBatchChunk(batchContext, c, info, chunks[index])
					if results[index].err != nil {
						cancel()
						return
					}
				}
			}
		}()
	}
	waitGroup.Wait()

	failedIndex := -1
	var canceledError *types.NewAPIError
	var canceledIndex int
	for i := range results {
		if results[i].err == nil {
			continue
		}
		if !errors.Is(results[i].err, context.Canceled) {
			return results, i, results[i].err
		}
		if canceledError == nil {
			canceledError = results[i].err
			canceledIndex = i
		}
	}
	if canceledError != nil {
		return results, canceledIndex, canceledError
	}
	if batchContext.Err() != nil {
		failedIndex = 0
		return results, failedIndex, types.NewOpenAIError(batchContext.Err(), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	return results, failedIndex, nil
}

func executeRelayBatchChunk(ctx context.Context, c *gin.Context, info *relaycommon.RelayInfo, chunk relayBatchChunk) relayBatchHTTPResult {
	chunkContext := c.Copy()
	chunkContext.Request = c.Request.Clone(ctx)
	chunkContext.Request.Body = http.NoBody
	chunkContext.Request.ContentLength = 0

	chunkInfo := *info
	if info.ChannelMeta != nil {
		channelMeta := *info.ChannelMeta
		chunkInfo.ChannelMeta = &channelMeta
	}
	chunkInfo.BatchSplit = nil
	chunkInfo.UpstreamRequestBodySize = int64(len(chunk.body))

	adaptor := GetAdaptor(chunkInfo.ApiType)
	if adaptor == nil {
		return relayBatchHTTPResult{err: types.NewError(fmt.Errorf("invalid api type: %d", chunkInfo.ApiType), types.ErrorCodeInvalidApiType)}
	}
	adaptor.Init(&chunkInfo)
	response, err := adaptor.DoRequest(chunkContext, &chunkInfo, bytes.NewReader(chunk.body))
	if err != nil {
		return relayBatchHTTPResult{err: types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)}
	}
	httpResponse, ok := response.(*http.Response)
	if !ok || httpResponse == nil {
		return relayBatchHTTPResult{err: types.NewOpenAIError(fmt.Errorf("invalid upstream response type: %T", response), types.ErrorCodeBadResponse, http.StatusInternalServerError)}
	}
	if httpResponse.StatusCode != http.StatusOK {
		apiErr := service.RelayErrorHandler(ctx, httpResponse, false)
		service.ResetStatusCode(apiErr, c.GetString("status_code_mapping"))
		return relayBatchHTTPResult{err: apiErr}
	}
	body, readErr := io.ReadAll(httpResponse.Body)
	service.CloseResponseBodyGracefully(httpResponse)
	if readErr != nil {
		return relayBatchHTTPResult{err: types.NewOpenAIError(readErr, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)}
	}
	responseMeta := &http.Response{
		StatusCode: httpResponse.StatusCode,
		Header:     httpResponse.Header.Clone(),
	}
	return relayBatchHTTPResult{body: body, responseMeta: responseMeta}
}

func parseEmbeddingBatchChunk(body []byte, info *relaycommon.RelayInfo, chunk relayBatchChunk, encodingFormat string) (dto.FlexibleEmbeddingResponse, dto.Usage, *types.NewAPIError) {
	if apiErr := openAIErrorFromBatchBody(body, http.StatusOK); apiErr != nil {
		return dto.FlexibleEmbeddingResponse{}, dto.Usage{}, apiErr
	}
	var response dto.FlexibleEmbeddingResponse
	if err := common.Unmarshal(body, &response); err != nil {
		return response, dto.Usage{}, batchResponseError(chunk.index+1, err)
	}
	input, err := decodeRawJSONValues(chunk.items)
	if err != nil {
		return response, dto.Usage{}, batchResponseError(chunk.index+1, err)
	}
	request := &dto.EmbeddingRequest{Input: input, EncodingFormat: encodingFormat}
	if err := openaiChannel.ValidateOpenAIEmbeddingResponse(request, &response); err != nil {
		return response, dto.Usage{}, batchResponseError(chunk.index+1, err)
	}
	seen := make(map[int]struct{}, len(response.Data))
	for _, item := range response.Data {
		if item.Index < 0 || item.Index >= len(chunk.items) {
			return response, dto.Usage{}, batchResponseError(chunk.index+1, fmt.Errorf("embedding response index %d is out of range", item.Index))
		}
		if _, exists := seen[item.Index]; exists {
			return response, dto.Usage{}, batchResponseError(chunk.index+1, fmt.Errorf("embedding response index %d is duplicated", item.Index))
		}
		seen[item.Index] = struct{}{}
	}
	usage := response.Usage
	normalizeRelayBatchUsage(&usage, service.CountTokenInput(input, info.OriginModelName))
	return response, usage, nil
}

func parseRerankBatchChunk(body []byte, info *relaycommon.RelayInfo, chunk relayBatchChunk, query string, expectedResults int) (dto.RerankResponse, dto.Usage, *types.NewAPIError) {
	if apiErr := openAIErrorFromBatchBody(body, http.StatusOK); apiErr != nil {
		return dto.RerankResponse{}, dto.Usage{}, apiErr
	}
	var response dto.RerankResponse
	if err := common.Unmarshal(body, &response); err != nil {
		return response, dto.Usage{}, batchResponseError(chunk.index+1, err)
	}
	if len(response.Results) != expectedResults {
		return response, dto.Usage{}, batchResponseError(chunk.index+1, fmt.Errorf("rerank response result count mismatch: expected %d, got %d", expectedResults, len(response.Results)))
	}
	seen := make(map[int]struct{}, len(response.Results))
	for _, result := range response.Results {
		if result.Index < 0 || result.Index >= len(chunk.items) {
			return response, dto.Usage{}, batchResponseError(chunk.index+1, fmt.Errorf("rerank response index %d is out of range", result.Index))
		}
		if _, exists := seen[result.Index]; exists {
			return response, dto.Usage{}, batchResponseError(chunk.index+1, fmt.Errorf("rerank response index %d is duplicated", result.Index))
		}
		seen[result.Index] = struct{}{}
	}
	values, err := decodeRawJSONValues(chunk.items)
	if err != nil {
		return response, dto.Usage{}, batchResponseError(chunk.index+1, err)
	}
	estimateInput := make([]any, 0, len(values)+1)
	if query != "" {
		estimateInput = append(estimateInput, query)
	}
	estimateInput = append(estimateInput, values...)
	usage := response.Usage
	if info.ApiType == constant.APITypeSiliconFlow && usage.TotalTokens == 0 {
		var siliconFlowResponse siliconflow.SFRerankResponse
		if err := common.Unmarshal(body, &siliconFlowResponse); err == nil {
			usage.PromptTokens = siliconFlowResponse.Meta.Tokens.InputTokens
			usage.CompletionTokens = siliconFlowResponse.Meta.Tokens.OutputTokens
			usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
		}
	}
	normalizeRelayBatchUsage(&usage, service.CountTokenInput(estimateInput, info.OriginModelName))
	return response, usage, nil
}

func openAIErrorFromBatchBody(body []byte, statusCode int) *types.NewAPIError {
	var response dto.OpenAITextResponse
	if err := common.Unmarshal(body, &response); err != nil {
		return nil
	}
	if openAIError := response.GetOpenAIError(); openAIError != nil && openAIError.Type != "" {
		return types.WithOpenAIError(*openAIError, statusCode)
	}
	return nil
}

func normalizeRelayBatchUsage(usage *dto.Usage, fallbackPromptTokens int) {
	if usage.PromptTokens == 0 {
		switch {
		case usage.TotalTokens > 0:
			usage.PromptTokens = usage.TotalTokens
		case usage.InputTokens > 0:
			usage.PromptTokens = usage.InputTokens
		default:
			usage.PromptTokens = fallbackPromptTokens
		}
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
}

func addRelayBatchUsage(target *dto.Usage, source dto.Usage) {
	target.PromptTokens += source.PromptTokens
	target.CompletionTokens += source.CompletionTokens
	target.TotalTokens += source.TotalTokens
	target.PromptCacheHitTokens += source.PromptCacheHitTokens
	target.InputTokens += source.InputTokens
	target.OutputTokens += source.OutputTokens
	target.ClaudeCacheCreation5mTokens += source.ClaudeCacheCreation5mTokens
	target.ClaudeCacheCreation1hTokens += source.ClaudeCacheCreation1hTokens
	target.PromptTokensDetails.CachedTokens += source.PromptTokensDetails.CachedTokens
	target.PromptTokensDetails.CachedCreationTokens += source.PromptTokensDetails.CachedCreationTokens
	target.PromptTokensDetails.TextTokens += source.PromptTokensDetails.TextTokens
	target.PromptTokensDetails.AudioTokens += source.PromptTokensDetails.AudioTokens
	target.PromptTokensDetails.ImageTokens += source.PromptTokensDetails.ImageTokens
	target.CompletionTokenDetails.TextTokens += source.CompletionTokenDetails.TextTokens
	target.CompletionTokenDetails.AudioTokens += source.CompletionTokenDetails.AudioTokens
	target.CompletionTokenDetails.ImageTokens += source.CompletionTokenDetails.ImageTokens
	target.CompletionTokenDetails.ReasoningTokens += source.CompletionTokenDetails.ReasoningTokens
	if source.InputTokensDetails != nil {
		if target.InputTokensDetails == nil {
			target.InputTokensDetails = &dto.InputTokenDetails{}
		}
		target.InputTokensDetails.CachedTokens += source.InputTokensDetails.CachedTokens
		target.InputTokensDetails.CachedCreationTokens += source.InputTokensDetails.CachedCreationTokens
		target.InputTokensDetails.TextTokens += source.InputTokensDetails.TextTokens
		target.InputTokensDetails.AudioTokens += source.InputTokensDetails.AudioTokens
		target.InputTokensDetails.ImageTokens += source.InputTokensDetails.ImageTokens
	}
	if target.UsageSemantic == "" {
		target.UsageSemantic = source.UsageSemantic
	}
	if target.UsageSource == "" {
		target.UsageSource = source.UsageSource
	}
	addRelayBatchCost(&target.Cost, source.Cost)
}

func addRelayBatchCost(target *any, source any) {
	if source == nil {
		return
	}
	sourceValue, sourceNumeric := relayBatchNumericValue(source)
	if !sourceNumeric {
		if *target == nil {
			*target = source
		}
		return
	}
	targetValue, targetNumeric := relayBatchNumericValue(*target)
	if !targetNumeric {
		targetValue = 0
	}
	*target = targetValue + sourceValue
}

func relayBatchNumericValue(value any) (float64, bool) {
	switch number := value.(type) {
	case float64:
		return number, true
	case float32:
		return float64(number), true
	case int:
		return float64(number), true
	case int64:
		return float64(number), true
	case int32:
		return float64(number), true
	case uint:
		return float64(number), true
	case uint64:
		return float64(number), true
	case uint32:
		return float64(number), true
	default:
		return 0, false
	}
}

func mergeBatchResponseBody(original []byte, resultField string, results any, usage dto.Usage) ([]byte, error) {
	var payload map[string]json.RawMessage
	if err := common.Unmarshal(original, &payload); err != nil {
		return nil, err
	}
	resultData, err := common.Marshal(results)
	if err != nil {
		return nil, err
	}
	usageData, err := common.Marshal(relayBatchUsageMap(usage))
	if err != nil {
		return nil, err
	}
	payload[resultField] = resultData
	payload["usage"] = usageData
	delete(payload, "error")
	return common.Marshal(payload)
}

func relayBatchUsageMap(usage dto.Usage) map[string]any {
	result := map[string]any{
		"prompt_tokens":     usage.PromptTokens,
		"completion_tokens": usage.CompletionTokens,
		"total_tokens":      usage.TotalTokens,
	}
	if usage.PromptCacheHitTokens != 0 {
		result["prompt_cache_hit_tokens"] = usage.PromptCacheHitTokens
	}
	if usage.InputTokens != 0 {
		result["input_tokens"] = usage.InputTokens
	}
	if usage.OutputTokens != 0 {
		result["output_tokens"] = usage.OutputTokens
	}
	if usage.UsageSemantic != "" {
		result["usage_semantic"] = usage.UsageSemantic
	}
	if usage.UsageSource != "" {
		result["usage_source"] = usage.UsageSource
	}
	if usage.PromptTokensDetails != (dto.InputTokenDetails{}) {
		result["prompt_tokens_details"] = usage.PromptTokensDetails
	}
	if usage.CompletionTokenDetails != (dto.OutputTokenDetails{}) {
		result["completion_tokens_details"] = usage.CompletionTokenDetails
	}
	if usage.InputTokensDetails != nil {
		result["input_tokens_details"] = usage.InputTokensDetails
	}
	if usage.ClaudeCacheCreation5mTokens != 0 {
		result["claude_cache_creation_5_m_tokens"] = usage.ClaudeCacheCreation5mTokens
	}
	if usage.ClaudeCacheCreation1hTokens != 0 {
		result["claude_cache_creation_1_h_tokens"] = usage.ClaudeCacheCreation1hTokens
	}
	if usage.Cost != nil {
		result["cost"] = usage.Cost
	}
	return result
}

func decodeRawJSONValues(items []json.RawMessage) ([]any, error) {
	values := make([]any, len(items))
	for i := range items {
		value, err := decodeRawJSONValue(items[i])
		if err != nil {
			return nil, err
		}
		values[i] = value
	}
	return values, nil
}

func decodeRawJSONValue(raw json.RawMessage) (any, error) {
	var value any
	if err := common.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func rawJSONOptionalInt(raw json.RawMessage) (*int, error) {
	if len(raw) == 0 || common.GetJsonType(raw) == "null" {
		return nil, nil
	}
	var value int
	if err := common.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	if value < 0 {
		return nil, fmt.Errorf("must not be negative")
	}
	return &value, nil
}

func rawJSONOptionalBool(raw json.RawMessage) (bool, error) {
	if len(raw) == 0 || common.GetJsonType(raw) == "null" {
		return false, nil
	}
	var value bool
	if err := common.Unmarshal(raw, &value); err != nil {
		return false, err
	}
	return value, nil
}

func rawJSONOptionalString(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || common.GetJsonType(raw) == "null" {
		return "", nil
	}
	var value string
	if err := common.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	return value, nil
}

func newRelayBatchSplitInfo(info *relaycommon.RelayInfo, kind string, itemCount int, setting system_setting.RelayBatchKindSetting, chunkCount int) *relaycommon.BatchSplitInfo {
	return &relaycommon.BatchSplitInfo{
		Kind:        kind,
		ChannelID:   info.ChannelId,
		ItemCount:   itemCount,
		BatchSize:   setting.BatchSize,
		ChunkCount:  chunkCount,
		Concurrency: setting.Concurrency,
	}
}

func supportsRelayBatchSplitAPI(apiType int) bool {
	switch apiType {
	case constant.APITypeOpenAI, constant.APITypeSiliconFlow:
		return true
	default:
		return false
	}
}

func countCompletedBatchChunks(results []relayBatchHTTPResult) int {
	completed := 0
	for i := range results {
		if results[i].err == nil && results[i].responseMeta != nil {
			completed++
		}
	}
	return completed
}

func totalRelayBatchRequestSize(chunks []relayBatchChunk) int64 {
	var total int64
	for i := range chunks {
		total += int64(len(chunks[i].body))
	}
	return total
}

func batchPayloadError(err error) *types.NewAPIError {
	return types.NewErrorWithStatusCode(fmt.Errorf("批量拆分请求失败: %w", err), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

func batchMaxItemsError(kind string, itemCount, maxItems int) *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		fmt.Errorf("%s 请求包含 %d 项，超过批量拆分配置上限 %d", kind, itemCount, maxItems),
		types.ErrorCodeInvalidRequest,
		http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(),
	)
}

func unsupportedBatchAPIError(apiType int) *types.NewAPIError {
	return types.NewOpenAIError(
		fmt.Errorf("所选渠道的 API 类型 %d 暂不支持安全的批量拆分", apiType),
		types.ErrorCodeInvalidApiType,
		http.StatusBadGateway,
	)
}

func batchResponseError(chunkNumber int, err error) *types.NewAPIError {
	return types.NewOpenAIError(
		fmt.Errorf("批量拆分第 %d 批响应无效: %w", chunkNumber, err),
		types.ErrorCodeBadResponseBody,
		http.StatusInternalServerError,
	)
}
