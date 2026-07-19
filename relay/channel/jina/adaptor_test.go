package jina

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertEmbeddingRequestPreservesEncodingFormat(t *testing.T) {
	request := dto.EmbeddingRequest{
		Model:          "embedding-model",
		Input:          "test input",
		EncodingFormat: "base64",
	}

	converted, err := (&Adaptor{}).ConvertEmbeddingRequest(nil, nil, request)
	require.NoError(t, err)

	embeddingRequest, ok := converted.(dto.EmbeddingRequest)
	require.True(t, ok)
	assert.Equal(t, "base64", embeddingRequest.EncodingFormat)
}
