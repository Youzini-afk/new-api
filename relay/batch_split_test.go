package relay

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractEmbeddingBatchItemsDoesNotSplitFlatTokenArray(t *testing.T) {
	_, items, batchable, err := extractEmbeddingBatchItems([]byte(`{"model":"m","input":[1,2,3,4]}`))
	require.NoError(t, err)
	assert.Len(t, items, 4)
	assert.False(t, batchable)

	_, items, batchable, err = extractEmbeddingBatchItems([]byte(`{"model":"m","input":[[1,2],[3,4]]}`))
	require.NoError(t, err)
	assert.Len(t, items, 2)
	assert.True(t, batchable)
}

func TestBuildRelayBatchChunksSplitsTwentySixIntoTwentyFiveAndOne(t *testing.T) {
	inputs := make([]string, 26)
	for i := range inputs {
		inputs[i] = fmt.Sprintf("item-%02d", i)
	}
	body, err := common.Marshal(map[string]any{"model": "m", "input": inputs})
	require.NoError(t, err)
	payload, items, batchable, err := extractEmbeddingBatchItems(body)
	require.NoError(t, err)
	require.True(t, batchable)

	chunks, err := buildRelayBatchChunks(payload, "input", items, 25, nil)
	require.NoError(t, err)
	require.Len(t, chunks, 2)
	assert.Len(t, chunks[0].items, 25)
	assert.Len(t, chunks[1].items, 1)
	assert.Equal(t, 25, chunks[1].offset)
}

func TestMaybeRelayEmbeddingInBatchesMergesResponseAndKeepsOriginalRequest(t *testing.T) {
	setRelayBatchSplitTestConfig(t, system_setting.RelayBatchSplitSetting{
		Version:    1,
		Enabled:    true,
		ChannelIDs: []int{41},
		Embedding:  system_setting.RelayBatchKindSetting{Enabled: true, BatchSize: 25, Concurrency: 2, MaxItems: 1000},
		Rerank:     system_setting.RelayBatchKindSetting{Enabled: false, BatchSize: 25, Concurrency: 1, MaxItems: 200},
	})

	var mutex sync.Mutex
	callSizes := make([]int, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		require.NoError(t, common.DecodeJson(request.Body, &payload))
		mutex.Lock()
		callSizes = append(callSizes, len(payload.Input))
		mutex.Unlock()

		data := make([]dto.FlexibleEmbeddingResponseItem, len(payload.Input))
		for i, input := range payload.Input {
			globalIndex := mustTrailingInt(t, input)
			data[i] = dto.FlexibleEmbeddingResponseItem{
				Object:    "embedding",
				Index:     i,
				Embedding: []float64{float64(globalIndex)},
			}
		}
		writeJSON(t, writer, dto.FlexibleEmbeddingResponse{
			Object: "list",
			Model:  payload.Model,
			Data:   data,
			Usage: dto.Usage{
				PromptTokens: len(payload.Input),
				TotalTokens:  len(payload.Input),
			},
		})
	}))
	defer server.Close()

	inputs := make([]any, 26)
	for i := range inputs {
		inputs[i] = fmt.Sprintf("item-%02d", i)
	}
	originalRequest := &dto.EmbeddingRequest{Model: "embedding-model", Input: inputs}
	body, err := common.Marshal(originalRequest)
	require.NoError(t, err)
	context, recorder := newRelayBatchTestContext(http.MethodPost, "/v1/embeddings", body)
	info := newRelayBatchTestInfo(server.URL, "/v1/embeddings", 41, relayconstant.RelayModeEmbeddings, types.RelayFormatEmbedding, originalRequest)

	handled, usage, apiErr := maybeRelayEmbeddingInBatches(context, info, body)
	require.True(t, handled)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 26, usage.PromptTokens)
	assert.Equal(t, 26, usage.TotalTokens)

	mutex.Lock()
	sort.Ints(callSizes)
	assert.Equal(t, []int{1, 25}, callSizes)
	mutex.Unlock()

	var response dto.FlexibleEmbeddingResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.Data, 26)
	for i, item := range response.Data {
		assert.Equal(t, i, item.Index)
		embedding, ok := item.Embedding.([]any)
		require.True(t, ok)
		require.Len(t, embedding, 1)
		assert.Equal(t, float64(i), embedding[0])
	}
	assert.Len(t, originalRequest.Input.([]any), 26, "audit request DTO must remain the original full request")
	require.NotNil(t, info.BatchSplit)
	assert.Equal(t, 2, info.BatchSplit.ChunkCount)
	assert.Equal(t, 2, info.BatchSplit.CompletedChunks)
	assert.Equal(t, 0, info.BatchSplit.FailedChunk)
}

func TestMaybeRelayEmbeddingInBatchesPreservesJinaBase64Encoding(t *testing.T) {
	setRelayBatchSplitTestConfig(t, system_setting.RelayBatchSplitSetting{
		Version:    1,
		Enabled:    true,
		ChannelIDs: []int{44},
		Embedding:  system_setting.RelayBatchKindSetting{Enabled: true, BatchSize: 25, Concurrency: 2, MaxItems: 1000},
		Rerank:     system_setting.RelayBatchKindSetting{Enabled: false, BatchSize: 25, Concurrency: 1, MaxItems: 200},
	})

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			Model          string   `json:"model"`
			Input          []string `json:"input"`
			EncodingFormat string   `json:"encoding_format"`
		}
		require.NoError(t, common.DecodeJson(request.Body, &payload))
		assert.Equal(t, "base64", payload.EncodingFormat)

		data := make([]dto.FlexibleEmbeddingResponseItem, len(payload.Input))
		for i, input := range payload.Input {
			globalIndex := mustTrailingInt(t, input)
			data[i] = dto.FlexibleEmbeddingResponseItem{
				Object:    "embedding",
				Index:     i,
				Embedding: []float64{float64(globalIndex), 0.5},
			}
		}
		writeJSON(t, writer, dto.FlexibleEmbeddingResponse{
			Object: "list",
			Model:  payload.Model,
			Data:   data,
			Usage:  dto.Usage{PromptTokens: len(payload.Input), TotalTokens: len(payload.Input)},
		})
	}))
	defer server.Close()

	inputs := make([]any, 26)
	for i := range inputs {
		inputs[i] = fmt.Sprintf("item-%02d", i)
	}
	originalRequest := &dto.EmbeddingRequest{
		Model:          "embedding-model",
		Input:          inputs,
		EncodingFormat: "base64",
	}
	body, err := common.Marshal(originalRequest)
	require.NoError(t, err)
	context, recorder := newRelayBatchTestContext(http.MethodPost, "/v1/embeddings", body)
	info := newRelayBatchTestInfo(server.URL, "/v1/embeddings", 44, relayconstant.RelayModeEmbeddings, types.RelayFormatEmbedding, originalRequest)
	info.ChannelType = constant.ChannelTypeJina
	info.ApiType = constant.APITypeJina

	handled, usage, apiErr := maybeRelayEmbeddingInBatches(context, info, body)
	require.True(t, handled)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 26, usage.TotalTokens)

	var response dto.FlexibleEmbeddingResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.Data, 26)
	for i, item := range response.Data {
		assert.Equal(t, i, item.Index)
		encoded, ok := item.Embedding.(string)
		require.True(t, ok)
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		require.NoError(t, err)
		require.Len(t, decoded, 8)
		assert.Equal(t, float32(i), math.Float32frombits(binary.LittleEndian.Uint32(decoded[0:4])))
		assert.Equal(t, float32(0.5), math.Float32frombits(binary.LittleEndian.Uint32(decoded[4:8])))
	}
}

func TestMaybeRelayRerankInBatchesSupportsJinaAndAppliesGlobalTopN(t *testing.T) {
	setRelayBatchSplitTestConfig(t, system_setting.RelayBatchSplitSetting{
		Version:    1,
		Enabled:    true,
		ChannelIDs: []int{42},
		Embedding:  system_setting.RelayBatchKindSetting{Enabled: false, BatchSize: 25, Concurrency: 1, MaxItems: 1000},
		Rerank:     system_setting.RelayBatchKindSetting{Enabled: true, BatchSize: 25, Concurrency: 2, MaxItems: 200},
	})

	var mutex sync.Mutex
	callSizes := make([]int, 0, 2)
	localTopNs := make([]int, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			Documents []string `json:"documents"`
			TopN      int      `json:"top_n"`
		}
		require.NoError(t, common.DecodeJson(request.Body, &payload))
		mutex.Lock()
		callSizes = append(callSizes, len(payload.Documents))
		localTopNs = append(localTopNs, payload.TopN)
		mutex.Unlock()

		results := make([]dto.RerankResponseResult, len(payload.Documents))
		for i, document := range payload.Documents {
			globalIndex := mustTrailingInt(t, document)
			results[i] = dto.RerankResponseResult{
				Index:          i,
				RelevanceScore: float64(globalIndex),
				Document:       "upstream-document-must-not-leak",
			}
		}
		sort.Slice(results, func(i, j int) bool { return results[i].RelevanceScore > results[j].RelevanceScore })
		if payload.TopN < len(results) {
			results = results[:payload.TopN]
		}
		writeJSON(t, writer, dto.RerankResponse{
			Results: results,
			Usage: dto.Usage{
				PromptTokens: len(payload.Documents) + 1,
				TotalTokens:  len(payload.Documents) + 1,
			},
		})
	}))
	defer server.Close()

	documents := make([]any, 30)
	for i := range documents {
		documents[i] = fmt.Sprintf("doc-%02d", i)
	}
	topN := 3
	returnDocuments := true
	originalRequest := &dto.RerankRequest{
		Model:           "rerank-model",
		Query:           "query",
		Documents:       documents,
		TopN:            &topN,
		ReturnDocuments: &returnDocuments,
	}
	body, err := common.Marshal(originalRequest)
	require.NoError(t, err)
	context, recorder := newRelayBatchTestContext(http.MethodPost, "/v1/rerank", body)
	info := newRelayBatchTestInfo(server.URL, "/v1/rerank", 42, relayconstant.RelayModeRerank, types.RelayFormatRerank, originalRequest)
	info.ChannelType = constant.ChannelTypeJina
	info.ApiType = constant.APITypeJina

	handled, usage, apiErr := maybeRelayRerankInBatches(context, info, body)
	require.True(t, handled)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 32, usage.PromptTokens, "query is consumed once per upstream chunk")

	mutex.Lock()
	sort.Ints(callSizes)
	sort.Ints(localTopNs)
	assert.Equal(t, []int{5, 25}, callSizes)
	assert.Equal(t, []int{3, 3}, localTopNs)
	mutex.Unlock()

	var response dto.RerankResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.Results, 3)
	assert.Equal(t, []int{29, 28, 27}, []int{response.Results[0].Index, response.Results[1].Index, response.Results[2].Index})
	assert.Equal(t, "doc-29", response.Results[0].Document)
	assert.Equal(t, "doc-28", response.Results[1].Document)
	assert.Equal(t, "doc-27", response.Results[2].Document)
	assert.Len(t, originalRequest.Documents, 30)
}

func TestMaybeRelayEmbeddingInBatchesDoesNotWritePartialResponseOnFailure(t *testing.T) {
	setRelayBatchSplitTestConfig(t, system_setting.RelayBatchSplitSetting{
		Version:    1,
		Enabled:    true,
		ChannelIDs: []int{43},
		Embedding:  system_setting.RelayBatchKindSetting{Enabled: true, BatchSize: 25, Concurrency: 1, MaxItems: 1000},
		Rerank:     system_setting.RelayBatchKindSetting{Enabled: false, BatchSize: 25, Concurrency: 1, MaxItems: 200},
	})

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			Input []string `json:"input"`
		}
		require.NoError(t, common.DecodeJson(request.Body, &payload))
		if len(payload.Input) == 1 {
			writer.WriteHeader(http.StatusInternalServerError)
			_, _ = writer.Write([]byte(`{"error":{"message":"failed","type":"upstream_error","code":"failed"}}`))
			return
		}
		data := make([]dto.FlexibleEmbeddingResponseItem, len(payload.Input))
		for i := range data {
			data[i] = dto.FlexibleEmbeddingResponseItem{Index: i, Embedding: []float64{1}}
		}
		writeJSON(t, writer, dto.FlexibleEmbeddingResponse{Data: data, Usage: dto.Usage{PromptTokens: len(data), TotalTokens: len(data)}})
	}))
	defer server.Close()

	inputs := make([]any, 26)
	for i := range inputs {
		inputs[i] = fmt.Sprintf("item-%02d", i)
	}
	originalRequest := &dto.EmbeddingRequest{Model: "embedding-model", Input: inputs}
	body, err := common.Marshal(originalRequest)
	require.NoError(t, err)
	context, recorder := newRelayBatchTestContext(http.MethodPost, "/v1/embeddings", body)
	info := newRelayBatchTestInfo(server.URL, "/v1/embeddings", 43, relayconstant.RelayModeEmbeddings, types.RelayFormatEmbedding, originalRequest)

	handled, usage, apiErr := maybeRelayEmbeddingInBatches(context, info, body)
	require.True(t, handled)
	assert.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.Empty(t, recorder.Body.Bytes(), "no partial successful chunk may be written to the client")
	require.NotNil(t, info.BatchSplit)
	assert.Equal(t, 2, info.BatchSplit.FailedChunk)
	assert.Equal(t, 1, info.BatchSplit.CompletedChunks)
}

func setRelayBatchSplitTestConfig(t *testing.T, setting system_setting.RelayBatchSplitSetting) {
	t.Helper()
	registered := config.GlobalConfig.Get("relay_batch_split")
	require.NotNil(t, registered)
	original, err := config.ConfigToMap(registered)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, config.UpdateConfigFromMap(registered, original))
		system_setting.RebuildRelayBatchSplitRuntime()
	})

	raw, err := common.Marshal(setting)
	require.NoError(t, err)
	_, normalized, err := system_setting.ParseAndValidateRelayBatchSplitConfig(string(raw))
	require.NoError(t, err)
	require.NoError(t, config.UpdateConfigFromMap(registered, map[string]string{"config": normalized}))
	system_setting.RebuildRelayBatchSplitRuntime()
}

func newRelayBatchTestContext(method, target string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	service.InitHttpClient()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(method, target, strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	context.Request = request
	return context, recorder
}

func newRelayBatchTestInfo(baseURL, path string, channelID, relayMode int, relayFormat types.RelayFormat, request dto.Request) *relaycommon.RelayInfo {
	modelName := relayBatchTestModelName(request)
	return &relaycommon.RelayInfo{
		StartTime:       time.Now(),
		OriginModelName: modelName,
		RequestURLPath:  path,
		RelayMode:       relayMode,
		RelayFormat:     relayFormat,
		Request:         request,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			ChannelId:         channelID,
			ChannelBaseUrl:    baseURL,
			ApiType:           constant.APITypeOpenAI,
			ApiKey:            "test-key",
			UpstreamModelName: modelName,
		},
	}
}

func relayBatchTestModelName(request dto.Request) string {
	switch typed := request.(type) {
	case *dto.EmbeddingRequest:
		return typed.Model
	case *dto.RerankRequest:
		return typed.Model
	default:
		return "test-model"
	}
}

func writeJSON(t *testing.T, writer http.ResponseWriter, value any) {
	t.Helper()
	data, err := common.Marshal(value)
	require.NoError(t, err)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, err = writer.Write(data)
	require.NoError(t, err)
}

func mustTrailingInt(t *testing.T, value string) int {
	t.Helper()
	parts := strings.Split(value, "-")
	require.NotEmpty(t, parts)
	number, err := strconv.Atoi(parts[len(parts)-1])
	require.NoError(t, err)
	return number
}
