package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDiscordGatePatrolBatchSize(t *testing.T) {
	assert.Equal(t, 1, discordGatePatrolBatchSize(0, 5, 24, 1000))
	assert.Equal(t, 174, discordGatePatrolBatchSize(50000, 5, 24, 1000))
	assert.Equal(t, 1000, discordGatePatrolBatchSize(50000, 60, 1, 1000))
}
