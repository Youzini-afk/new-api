package service

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/stretchr/testify/assert"
)

func TestShouldChatCompletionsUseResponsesChannelPolicy(t *testing.T) {
	policy := model_setting.ChatCompletionsToResponsesPolicy{
		Enabled:       true,
		AllChannels:   true,
		ModelPatterns: []string{"^gpt-5$"},
	}

	tests := []struct {
		name               string
		channelType        int
		model              string
		passThroughGlobal  bool
		passThroughChannel bool
		want               bool
	}{
		{
			name:               "responses channel forces conversion despite passthrough",
			channelType:        constant.ChannelTypeResponses,
			model:              "any-model",
			passThroughGlobal:  true,
			passThroughChannel: true,
			want:               true,
		},
		{
			name:        "ordinary OpenAI remains disabled without global policy",
			channelType: constant.ChannelTypeOpenAI,
			model:       "gpt-5",
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.channelType == constant.ChannelTypeOpenAI {
				assert.True(t, ShouldChatCompletionsUseResponsesPolicy(policy, 0, tt.channelType, tt.model))
				assert.Equal(t, tt.want, ShouldChatCompletionsUseResponses(0, tt.channelType, tt.model, tt.passThroughGlobal, tt.passThroughChannel))
				return
			}
			assert.Equal(t, tt.want, ShouldChatCompletionsUseResponses(0, tt.channelType, tt.model, tt.passThroughGlobal, tt.passThroughChannel))
		})
	}

	assert.False(t, ShouldChatCompletionsUseResponses(
		0,
		constant.ChannelTypeOpenAI,
		"gpt-5",
		true,
		false,
	))
}
