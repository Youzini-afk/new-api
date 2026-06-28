package system_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDiscordRegisterGate_EmptyString(t *testing.T) {
	cfg, err := ParseDiscordRegisterGate("")
	require.NoError(t, err)
	assert.Empty(t, cfg.Groups)
	assert.Empty(t, cfg.BanGroups)
}

func TestParseDiscordRegisterGate_InvalidJSON(t *testing.T) {
	_, err := ParseDiscordRegisterGate("{not-json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid discord register gate config")
}

func TestParseAndValidateDiscordRegisterGate_ValidNestedConfigNormalizes(t *testing.T) {
	raw := `{
		"fail_message": " denied ",
		"ban_message": " banned ",
		"groups": [{
			"name": " staff ",
			"rules": [{
				"guild_id": " 123 ",
				"role_ids": [" r1 ", "", "r1", "r2"],
				"role_match": " ALL ",
				"min_join_hours": 24
			}]
		}],
		"ban_groups": [{
			"name": " banned guild ",
			"rules": [{"guild_id": " 999 ", "role_match": ""}]
		}]
	}`
	cfg, err := ParseAndValidateDiscordRegisterGate(raw)
	require.NoError(t, err)
	require.Len(t, cfg.Groups, 1)
	require.Len(t, cfg.BanGroups, 1)

	assert.Equal(t, "denied", cfg.FailMessage)
	assert.Equal(t, "banned", cfg.BanMessage)
	assert.Equal(t, "staff", cfg.Groups[0].Name)
	require.Len(t, cfg.Groups[0].Rules, 1)
	assert.Equal(t, "123", cfg.Groups[0].Rules[0].GuildID)
	assert.Equal(t, []string{"r1", "r2"}, cfg.Groups[0].Rules[0].RoleIDs)
	assert.Equal(t, "all", cfg.Groups[0].Rules[0].RoleMatch)
	assert.Equal(t, 24, cfg.Groups[0].Rules[0].MinJoinHours)
	assert.Equal(t, "banned guild", cfg.BanGroups[0].Name)
	assert.Equal(t, "any", cfg.BanGroups[0].Rules[0].RoleMatch)
}

func TestValidateDiscordRegisterGate_EmptyConfigOK(t *testing.T) {
	// Empty config is valid for option persistence. Runtime evaluators fail
	// closed when a gate is enabled without rules.
	err := ValidateDiscordRegisterGate(DiscordRegisterGateConfig{})
	require.NoError(t, err)
}

func TestValidateDiscordRegisterGate_RejectsEmptyRules(t *testing.T) {
	err := ValidateDiscordRegisterGate(DiscordRegisterGateConfig{
		Groups: []DiscordGateGroup{{Name: "empty"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "groups[0].rules must not be empty")

	err = ValidateDiscordRegisterGate(DiscordRegisterGateConfig{
		BanGroups: []DiscordGateGroup{{Name: "empty"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ban_groups[0].rules must not be empty")
}

func TestValidateDiscordRegisterGate_RejectsNormalRuleWithoutRoleOrMinJoin(t *testing.T) {
	err := ValidateDiscordRegisterGate(DiscordRegisterGateConfig{
		Groups: []DiscordGateGroup{{Rules: []DiscordGateRule{{GuildID: "g1"}}}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must configure role_ids or min_join_hours")
}

func TestValidateDiscordRegisterGate_RejectsBanRuleMinJoinHours(t *testing.T) {
	err := ValidateDiscordRegisterGate(DiscordRegisterGateConfig{
		BanGroups: []DiscordGateGroup{{Rules: []DiscordGateRule{{GuildID: "g1", MinJoinHours: 1}}}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ban_groups[0].rules[0].min_join_hours must be 0")
}

func TestValidateDiscordRegisterGate_AllowsBanRuleWithGuildOnly(t *testing.T) {
	err := ValidateDiscordRegisterGate(DiscordRegisterGateConfig{
		BanGroups: []DiscordGateGroup{{Rules: []DiscordGateRule{{GuildID: "banned-guild"}}}},
	})
	require.NoError(t, err)
}

func TestValidateDiscordRegisterGate_RoleMatchRules(t *testing.T) {
	cfg, err := ParseAndValidateDiscordRegisterGate(`{
		"groups": [{"rules": [{"guild_id":"g1", "role_ids":["r1"], "role_match":""}]}]
	}`)
	require.NoError(t, err)
	assert.Equal(t, "any", cfg.Groups[0].Rules[0].RoleMatch)

	cfg, err = ParseAndValidateDiscordRegisterGate(`{
		"groups": [{"rules": [{"guild_id":"g1", "role_ids":["r1"], "role_match":"AnY"}]}]
	}`)
	require.NoError(t, err)
	assert.Equal(t, "any", cfg.Groups[0].Rules[0].RoleMatch)

	cfg, err = ParseAndValidateDiscordRegisterGate(`{
		"groups": [{"rules": [{"guild_id":"g1", "role_ids":["r1"], "role_match":"ALL"}]}]
	}`)
	require.NoError(t, err)
	assert.Equal(t, "all", cfg.Groups[0].Rules[0].RoleMatch)

	cfg, err = ParseAndValidateDiscordRegisterGate(`{
		"groups": [{"rules": [{"guild_id":"g1", "role_ids":["r1"], "role_match":"foo"}]}]
	}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "role_match must be")
	assert.Empty(t, cfg.Groups)
}

func TestValidateDiscordRegisterGate_AllowsDuplicateGuildAcrossGroups(t *testing.T) {
	err := ValidateDiscordRegisterGate(DiscordRegisterGateConfig{
		Groups: []DiscordGateGroup{
			{Rules: []DiscordGateRule{{GuildID: "g1", RoleIDs: []string{"r1"}}}},
			{Rules: []DiscordGateRule{{GuildID: "g1", MinJoinHours: 24}}},
		},
		BanGroups: []DiscordGateGroup{
			{Rules: []DiscordGateRule{{GuildID: "g1"}}},
		},
	})
	require.NoError(t, err)
}

func TestValidateDiscordRegisterGate_RejectsEmptyGuildAndNegativeMinJoin(t *testing.T) {
	err := ValidateDiscordRegisterGate(DiscordRegisterGateConfig{
		Groups: []DiscordGateGroup{{Rules: []DiscordGateRule{{GuildID: " ", RoleIDs: []string{"r1"}}}}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "guild_id must not be empty")

	err = ValidateDiscordRegisterGate(DiscordRegisterGateConfig{
		Groups: []DiscordGateGroup{{Rules: []DiscordGateRule{{GuildID: "g1", MinJoinHours: -1}}}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "min_join_hours must not be negative")
}

func TestParseAndValidateDiscordRegisterGate_ReturnsZeroConfigOnError(t *testing.T) {
	cfg, err := ParseAndValidateDiscordRegisterGate(`{
		"groups": [{"rules": [{"guild_id":"g1", "role_ids":["r1"], "role_match":"foo"}]}]
	}`)
	require.Error(t, err)
	assert.Empty(t, cfg.Groups)
	assert.Empty(t, cfg.BanGroups)
}

func TestNormalizeDiscordPatrolSettingsClampsAndDefaults(t *testing.T) {
	settings := DiscordSettings{
		LoginGatePatrolIntervalMinutes:  -1,
		LoginGatePatrolTargetSweepHours: 999,
		LoginGatePatrolMaxBatchSize:     0,
		LoginGatePatrolWorkerCount:      99,
		LoginGatePatrolMaxRPS:           -1,
		LoginGatePatrolMaxRetries:       -1,
	}
	NormalizeDiscordPatrolSettings(&settings)
	assert.Equal(t, 1, settings.LoginGatePatrolIntervalMinutes)
	assert.Equal(t, 168, settings.LoginGatePatrolTargetSweepHours)
	assert.Equal(t, 50000, settings.LoginGatePatrolMaxBatchSize)
	assert.Equal(t, 64, settings.LoginGatePatrolWorkerCount)
	assert.Equal(t, 1, settings.LoginGatePatrolMaxRPS)
	assert.Equal(t, 0, settings.LoginGatePatrolMaxRetries)
}

func TestValidateDiscordPatrolSetting(t *testing.T) {
	require.NoError(t, ValidateDiscordPatrolSetting("discord.login_gate_patrol_interval_minutes", 5))
	require.Error(t, ValidateDiscordPatrolSetting("discord.login_gate_patrol_interval_minutes", 0))
	require.Error(t, ValidateDiscordPatrolSetting("discord.login_gate_patrol_max_batch_size", 49))
	require.NoError(t, ValidateDiscordPatrolSetting("discord.login_gate_patrol_max_batch_size", 100000))
	require.Error(t, ValidateDiscordPatrolSetting("discord.login_gate_patrol_max_batch_size", 100001))
	require.NoError(t, ValidateDiscordPatrolSetting("discord.login_gate_patrol_max_retries", 0))
}
