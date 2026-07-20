package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/stretchr/testify/assert"
)

func TestDiscordGatePatrolBatchSize(t *testing.T) {
	assert.Equal(t, 1, discordGatePatrolBatchSize(0, 5, 24, 1000))
	assert.Equal(t, 174, discordGatePatrolBatchSize(50000, 5, 24, 1000))
	assert.Equal(t, 1000, discordGatePatrolBatchSize(50000, 60, 1, 1000))
}

func TestDiscordGatePatrolEffectiveBatchSize(t *testing.T) {
	settings := &system_setting.DiscordSettings{
		LoginGatePatrolIntervalMinutes:  2,
		LoginGatePatrolTargetSweepHours: 12,
		LoginGatePatrolMaxBatchSize:     50000,
	}

	assert.Equal(t, 50000, discordGatePatrolEffectiveBatchSize(discordGatePatrolModeManualBatch, 0, 100, settings))
	assert.Equal(t, 200, discordGatePatrolEffectiveBatchSize(discordGatePatrolModeManualBatch, 200, 100, settings))
	assert.Equal(t, 50000, discordGatePatrolEffectiveBatchSize(discordGatePatrolModeManualBatch, 100000, 100, settings))
	assert.Equal(t, 1, discordGatePatrolEffectiveBatchSize(discordGatePatrolModeScheduled, 0, 100, settings))
	assert.Equal(t, 139, discordGatePatrolEffectiveBatchSize(discordGatePatrolModeScheduled, 0, 50000, settings))
}

func TestDiscordBanPatrolHandlerTypeAndManualBatchSize(t *testing.T) {
	assert.Equal(t, "discord_ban_patrol", discordBanPatrolHandler{}.Type())
	assert.False(t, discordBanPatrolHandler{}.Enabled())
	settings := &system_setting.DiscordSettings{LoginGatePatrolMaxBatchSize: 50000}
	assert.Equal(t, 50000, discordGatePatrolEffectiveBatchSize(discordGatePatrolModeManualBatch, 0, 100, settings))
	assert.Equal(t, 200, discordGatePatrolEffectiveBatchSize(discordGatePatrolModeManualBatch, 200, 100, settings))
}

func TestDiscordGatePatrolHandlerUsesIndependentPatrolConfig(t *testing.T) {
	settings := system_setting.GetDiscordSettings()
	original := *settings
	t.Cleanup(func() { *settings = original })

	patrol := system_setting.DiscordRegisterGateConfig{Groups: []system_setting.DiscordGateGroup{{Rules: []system_setting.DiscordGateRule{{GuildID: "patrol", RoleIDs: []string{"role"}, RoleMatch: "any"}}}}}
	settings.Enabled = true
	settings.RegisterGateEnabled = false
	settings.LoginGateEnabled = false
	settings.LoginGatePatrolEnabled = true
	settings.PatrolGate = &patrol
	assert.True(t, discordGatePatrolHandler{}.Enabled())

	empty := system_setting.DiscordRegisterGateConfig{}
	settings.PatrolGate = &empty
	assert.False(t, discordGatePatrolHandler{}.Enabled())
}
