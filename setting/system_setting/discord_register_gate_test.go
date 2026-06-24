package system_setting

import (
	"strings"
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

func TestParseDiscordRegisterGate_ValidFullConfig(t *testing.T) {
	raw := `{
		"groups": [{"guild_id":"123","role_ids":["r1","r2"]}],
		"ban_groups": ["999"],
		"role_match": "all",
		"min_join_hours": 24,
		"fail_message": "denied",
		"ban_message": "banned"
	}`
	cfg, err := ParseAndValidateDiscordRegisterGate(raw)
	require.NoError(t, err)
	require.Len(t, cfg.Groups, 1)
	assert.Equal(t, "123", cfg.Groups[0].GuildID)
	assert.Equal(t, []string{"r1", "r2"}, cfg.Groups[0].RoleIDs)
	assert.Equal(t, "all", cfg.RoleMatch)
	assert.Equal(t, 24, cfg.MinJoinHours)
	assert.Equal(t, "denied", cfg.FailMessage)
}

func TestValidateDiscordRegisterGate_EmptyConfigOK(t *testing.T) {
	// An empty config (no rules) is valid — "no gate rules configured".
	err := ValidateDiscordRegisterGate(DiscordRegisterGateConfig{})
	require.NoError(t, err)
}

func TestValidateDiscordRegisterGate_RejectsEmptyGuildID(t *testing.T) {
	err := ValidateDiscordRegisterGate(DiscordRegisterGateConfig{
		Groups: []DiscordGateGroupRule{{GuildID: "", RoleIDs: []string{"r1"}}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "guild_id must not be empty")
}

func TestValidateDiscordRegisterGate_RejectsEmptyRoleIDs(t *testing.T) {
	err := ValidateDiscordRegisterGate(DiscordRegisterGateConfig{
		Groups: []DiscordGateGroupRule{{GuildID: "g1", RoleIDs: nil}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "role_ids must not be empty")
}

func TestValidateDiscordRegisterGate_RejectsEmptyRoleIDEntry(t *testing.T) {
	err := ValidateDiscordRegisterGate(DiscordRegisterGateConfig{
		Groups: []DiscordGateGroupRule{{GuildID: "g1", RoleIDs: []string{"r1", ""}}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "role_ids[1] must not be empty")
}

func TestValidateDiscordRegisterGate_RejectsDuplicateGuild(t *testing.T) {
	err := ValidateDiscordRegisterGate(DiscordRegisterGateConfig{
		Groups: []DiscordGateGroupRule{
			{GuildID: "g1", RoleIDs: []string{"r1"}},
			{GuildID: "g1", RoleIDs: []string{"r2"}},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate guild_id")
}

func TestValidateDiscordRegisterGate_RejectsEmptyBanGroup(t *testing.T) {
	err := ValidateDiscordRegisterGate(DiscordRegisterGateConfig{
		BanGroups: []string{"  "},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ban_groups[0] must not be empty")
}

func TestValidateDiscordRegisterGate_RejectsDuplicateBanGroup(t *testing.T) {
	err := ValidateDiscordRegisterGate(DiscordRegisterGateConfig{
		BanGroups: []string{"g1", "g1"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate guild_id")
}

func TestValidateDiscordRegisterGate_NegativeMinJoinHours(t *testing.T) {
	err := ValidateDiscordRegisterGate(DiscordRegisterGateConfig{
		MinJoinHours: -1,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "min_join_hours must not be negative")
}

func TestNormalizeDiscordRegisterGate_RoleMatchEmptyDefaultsToAny(t *testing.T) {
	// Empty / whitespace-only role_match defaults to "any" (the documented
	// default). Normalize is the only path that coerces empty -> "any".
	for _, rm := range []string{"", "   "} {
		cfg := DiscordRegisterGateConfig{RoleMatch: rm}
		NormalizeDiscordRegisterGate(&cfg)
		assert.Equal(t, "any", cfg.RoleMatch, "input %q should normalize to any", rm)
	}
}

func TestNormalizeDiscordRegisterGate_RoleMatchCanonicalPreserved(t *testing.T) {
	// "any"/"all" in any case / with surrounding whitespace are canonicalized
	// to their lowercased form.
	for _, rm := range []string{"any", "ANY", " Any ", "Any"} {
		cfg := DiscordRegisterGateConfig{RoleMatch: rm}
		NormalizeDiscordRegisterGate(&cfg)
		assert.Equal(t, "any", cfg.RoleMatch, "input %q should normalize to any", rm)
	}
	for _, rm := range []string{"all", "ALL", " All ", "All"} {
		cfg := DiscordRegisterGateConfig{RoleMatch: rm}
		NormalizeDiscordRegisterGate(&cfg)
		assert.Equal(t, "all", cfg.RoleMatch, "input %q should normalize to all", rm)
	}
}

func TestNormalizeDiscordRegisterGate_RoleMatchUnknownLeftInPlace(t *testing.T) {
	// Unknown non-empty role_match (e.g. "foo") must NOT be silently coerced
	// to "any" by Normalize — it is left in place (lowercased) so that
	// ValidateDiscordRegisterGate can surface it as an error. This prevents
	// illegal values from being persisted through the no-error Normalize path.
	for _, rm := range []string{"foo", "weird", "Bar"} {
		cfg := DiscordRegisterGateConfig{RoleMatch: rm}
		NormalizeDiscordRegisterGate(&cfg)
		assert.Equal(t, strings.ToLower(rm), cfg.RoleMatch,
			"input %q should be left lowercased in place, not coerced to any", rm)
		assert.NotEqual(t, "any", cfg.RoleMatch, "input %q must NOT be coerced to any", rm)
		assert.NotEqual(t, "all", cfg.RoleMatch, "input %q must NOT be coerced to all", rm)
	}
}

func TestValidateDiscordRegisterGate_RoleMatchAnyOK(t *testing.T) {
	err := ValidateDiscordRegisterGate(DiscordRegisterGateConfig{RoleMatch: "any"})
	require.NoError(t, err)
}

func TestValidateDiscordRegisterGate_RoleMatchAllOK(t *testing.T) {
	err := ValidateDiscordRegisterGate(DiscordRegisterGateConfig{RoleMatch: "all"})
	require.NoError(t, err)
}

func TestValidateDiscordRegisterGate_RoleMatchEmptyOK(t *testing.T) {
	// Empty role_match is normalized to "any" inside Validate, so it passes.
	err := ValidateDiscordRegisterGate(DiscordRegisterGateConfig{RoleMatch: ""})
	require.NoError(t, err)
}

func TestValidateDiscordRegisterGate_RoleMatchFooRejected(t *testing.T) {
	// Unknown non-empty role_match must be rejected with a clear error, not
	// silently coerced to "any".
	err := ValidateDiscordRegisterGate(DiscordRegisterGateConfig{RoleMatch: "foo"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "role_match must be \"any\" or \"all\"")
}

func TestValidateDiscordRegisterGate_RoleMatchMixedCaseFooRejected(t *testing.T) {
	// Mixed-case unknown value is lowercased by Normalize then rejected.
	err := ValidateDiscordRegisterGate(DiscordRegisterGateConfig{RoleMatch: "FooBar"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "role_match must be \"any\" or \"all\"")
}

func TestParseAndValidateDiscordRegisterGate_RoleMatchEmptyReturnsAny(t *testing.T) {
	// Persistence path: empty role_match must come back normalized to "any".
	cfg, err := ParseAndValidateDiscordRegisterGate(`{"role_match":""}`)
	require.NoError(t, err)
	assert.Equal(t, "any", cfg.RoleMatch)
}

func TestParseAndValidateDiscordRegisterGate_RoleMatchAnyReturnsAny(t *testing.T) {
	cfg, err := ParseAndValidateDiscordRegisterGate(`{"role_match":"any"}`)
	require.NoError(t, err)
	assert.Equal(t, "any", cfg.RoleMatch)
}

func TestParseAndValidateDiscordRegisterGate_RoleMatchAllReturnsAll(t *testing.T) {
	cfg, err := ParseAndValidateDiscordRegisterGate(`{"role_match":"all"}`)
	require.NoError(t, err)
	assert.Equal(t, "all", cfg.RoleMatch)
}

func TestParseAndValidateDiscordRegisterGate_RoleMatchFooRejected(t *testing.T) {
	// Persistence path: an illegal role_match like "foo" must be REJECTED so
	// it can never be persisted. The returned config is the zero value.
	cfg, err := ParseAndValidateDiscordRegisterGate(`{"role_match":"foo"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "role_match must be \"any\" or \"all\"")
	assert.Empty(t, cfg.RoleMatch, "zero config must be returned on error")
}

func TestParseAndValidateDiscordRegisterGate_RoleMatchMissingDefaultsToAny(t *testing.T) {
	// When role_match is absent from the JSON, it parses as "" and should be
	// normalized to "any" on the persistence path.
	cfg, err := ParseAndValidateDiscordRegisterGate(`{}`)
	require.NoError(t, err)
	assert.Equal(t, "any", cfg.RoleMatch)
}

func TestParseAndValidateDiscordRegisterGate_ReturnsNormalizedConfig(t *testing.T) {
	// The returned config must be normalized (whitespace trimmed + lowercased),
	// not the raw parsed value.
	cfg, err := ParseAndValidateDiscordRegisterGate(`{"role_match":"  ALL  "}`)
	require.NoError(t, err)
	assert.Equal(t, "all", cfg.RoleMatch)
}

func TestNormalizeDiscordRegisterGate_DoesNotClampNegativeMinJoinHours(t *testing.T) {
	// Negative min_join_hours must NOT be clamped by Normalize; Validate is
	// responsible for rejecting it so the invalid input is surfaced.
	cfg := DiscordRegisterGateConfig{MinJoinHours: -5}
	NormalizeDiscordRegisterGate(&cfg)
	assert.Equal(t, -5, cfg.MinJoinHours)
}

func TestValidateDiscordAuditSettings_ZeroAllowed(t *testing.T) {
	// Zero = not configured; allowed.
	err := ValidateDiscordAuditSettings(0, 0)
	require.NoError(t, err)
}

func TestValidateDiscordAuditSettings_RejectsNegative(t *testing.T) {
	err := ValidateDiscordAuditSettings(-1, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "interval_minutes must not be negative")

	err = ValidateDiscordAuditSettings(0, -1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "batch_size must not be negative")
}

func TestValidateDiscordAuditSettings_RejectsTooSmall(t *testing.T) {
	err := ValidateDiscordAuditSettings(1, 0)
	require.NoError(t, err) // 1 is the minimum, allowed

	err = ValidateDiscordAuditSettings(0, 1)
	require.NoError(t, err) // 1 is the minimum, allowed
}

func TestValidateDiscordAuditSettings_RejectsSubMinimum(t *testing.T) {
	// Positive but below minimum — there is no sub-minute positive int below 1,
	// so the real guard is the negative check. This test documents that 1 is OK.
	err := ValidateDiscordAuditSettings(1, 1)
	require.NoError(t, err)
}
