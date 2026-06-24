package system_setting

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// DiscordRegisterGateConfig is the typed Phase 6.1 Discord gate configuration.
// It carries only Discord-gate semantics ported from the gy gate config — no
// ban_sync / AutoBanSync / external-bot fields.
//
// This config is a data contract only: the gate evaluator itself is NOT
// implemented in this phase. When RegisterGateEnabled is true the Discord
// provider fails closed with a clear error rather than evaluating these rules.
type DiscordRegisterGateConfig struct {
	// Groups are the allow-list rules: a user must satisfy the role-match
	// condition across these (guild, role) entries.
	Groups []DiscordGateGroupRule `json:"groups"`
	// BanGroups are guild IDs in which any member is rejected outright.
	BanGroups []string `json:"ban_groups"`
	// RoleMatch is "any" (default) or "all": whether a user must match any one
	// rule or all rules in Groups. Empty is treated as "any".
	RoleMatch string `json:"role_match"`
	// MinJoinHours is the minimum account age (in hours) the Discord user must
	// have before passing the gate. 0 means no minimum.
	MinJoinHours int `json:"min_join_hours"`
	// FailMessage is shown to the user when the gate rejects them.
	FailMessage string `json:"fail_message"`
	// BanMessage is shown when the user is rejected via BanGroups.
	BanMessage string `json:"ban_message"`
}

// DiscordGateGroupRule pairs a Discord guild with the role IDs that satisfy it.
type DiscordGateGroupRule struct {
	GuildID string   `json:"guild_id"`
	RoleIDs []string `json:"role_ids"`
}

const (
	discordGateRoleMatchAny = "any"
	discordGateRoleMatchAll = "all"

	// discordGateMinAuditInterval is the smallest non-zero audit interval.
	discordGateMinAuditInterval = 1
	// discordGateMinAuditBatch is the smallest non-zero audit batch size.
	discordGateMinAuditBatch = 1
)

// ParseDiscordRegisterGate decodes a JSON string into a
// DiscordRegisterGateConfig. An empty string yields the zero config (no error).
func ParseDiscordRegisterGate(raw string) (DiscordRegisterGateConfig, error) {
	cfg := DiscordRegisterGateConfig{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return cfg, nil
	}
	if err := common.UnmarshalJsonStr(raw, &cfg); err != nil {
		return DiscordRegisterGateConfig{}, fmt.Errorf("invalid discord register gate config: %w", err)
	}
	return cfg, nil
}

// NormalizeDiscordRegisterGate fills defaults and trims fields in place. It is
// idempotent and never returns an error: callers that need to reject invalid
// input (e.g. an unknown role_match value like "foo") must use
// ValidateDiscordRegisterGate.
//
// role_match handling:
//   - empty / whitespace -> "any" (the documented default)
//   - "any"/"all" in any case / with surrounding whitespace -> canonical "any"/"all"
//   - any other non-empty value (e.g. "foo") is left UNCHANGED here so that
//     ValidateDiscordRegisterGate can surface it as an error rather than
//     silently coercing it to "any". This prevents illegal values from being
//     persisted through the no-error Normalize path.
//
// It does NOT clamp min_join_hours — negative values are left for
// ValidateDiscordRegisterGate to reject, so invalid input is surfaced rather
// than silently hidden.
func NormalizeDiscordRegisterGate(cfg *DiscordRegisterGateConfig) {
	if cfg == nil {
		return
	}
	trimmed := strings.ToLower(strings.TrimSpace(cfg.RoleMatch))
	switch trimmed {
	case "":
		cfg.RoleMatch = discordGateRoleMatchAny
	case discordGateRoleMatchAny, discordGateRoleMatchAll:
		cfg.RoleMatch = trimmed
	default:
		// Unknown non-empty value: leave it in place (lowercased) so Validate
		// can reject it. Do NOT silently coerce to "any".
		cfg.RoleMatch = trimmed
	}
	cfg.FailMessage = strings.TrimSpace(cfg.FailMessage)
	cfg.BanMessage = strings.TrimSpace(cfg.BanMessage)
}

// ValidateDiscordRegisterGate checks the gate config for invalid entries:
// empty guild/role, negative min_join_hours, or illegal role_match. An empty
// config (no rules) is valid — it means "no gate rules configured".
//
// The cfg argument is normalized in place semantics only for this call's
// local copy (it is passed by value); to obtain the normalized config use
// ParseAndValidateDiscordRegisterGate.
func ValidateDiscordRegisterGate(cfg DiscordRegisterGateConfig) error {
	NormalizeDiscordRegisterGate(&cfg)

	// role_match must be empty (normalized to "any") or one of the two
	// canonical values. Unknown non-empty values are rejected so they cannot
	// be persisted through the validator.
	switch cfg.RoleMatch {
	case discordGateRoleMatchAny, discordGateRoleMatchAll:
		// ok
	default:
		return fmt.Errorf("discord register gate: role_match must be \"any\" or \"all\", got %q", cfg.RoleMatch)
	}

	if cfg.MinJoinHours < 0 {
		return fmt.Errorf("discord register gate: min_join_hours must not be negative")
	}

	seenGuilds := make(map[string]struct{}, len(cfg.Groups))
	for i, rule := range cfg.Groups {
		guild := strings.TrimSpace(rule.GuildID)
		if guild == "" {
			return fmt.Errorf("discord register gate: groups[%d].guild_id must not be empty", i)
		}
		if _, dup := seenGuilds[guild]; dup {
			return fmt.Errorf("discord register gate: duplicate guild_id %q in groups", guild)
		}
		seenGuilds[guild] = struct{}{}

		if len(rule.RoleIDs) == 0 {
			return fmt.Errorf("discord register gate: groups[%d].role_ids must not be empty (guild %s)", i, guild)
		}
		for j, rid := range rule.RoleIDs {
			if strings.TrimSpace(rid) == "" {
				return fmt.Errorf("discord register gate: groups[%d].role_ids[%d] must not be empty (guild %s)", i, j, guild)
			}
		}
	}

	seenBan := make(map[string]struct{}, len(cfg.BanGroups))
	for i, gid := range cfg.BanGroups {
		guild := strings.TrimSpace(gid)
		if guild == "" {
			return fmt.Errorf("discord register gate: ban_groups[%d] must not be empty", i)
		}
		if _, dup := seenBan[guild]; dup {
			return fmt.Errorf("discord register gate: duplicate guild_id %q in ban_groups", guild)
		}
		seenBan[guild] = struct{}{}
	}

	return nil
}

// ValidateDiscordAuditSettings checks the audit interval/batch fields. Zero
// means "not configured" and is allowed; positive values must be >= the
// minimum. Negative values are rejected.
func ValidateDiscordAuditSettings(intervalMinutes, batchSize int) error {
	if intervalMinutes < 0 {
		return fmt.Errorf("discord login gate audit: interval_minutes must not be negative")
	}
	if intervalMinutes > 0 && intervalMinutes < discordGateMinAuditInterval {
		return fmt.Errorf("discord login gate audit: interval_minutes must be at least %d", discordGateMinAuditInterval)
	}
	if batchSize < 0 {
		return fmt.Errorf("discord login gate audit: batch_size must not be negative")
	}
	if batchSize > 0 && batchSize < discordGateMinAuditBatch {
		return fmt.Errorf("discord login gate audit: batch_size must be at least %d", discordGateMinAuditBatch)
	}
	return nil
}

// ParseAndValidateDiscordRegisterGate parses, normalizes and validates a raw
// JSON config string. On success it returns the NORMALIZED config (empty
// role_match becomes "any"; whitespace/case canonicalized) so callers that
// persist the value store the canonical form. On validation error (including
// an unknown role_match like "foo") it returns the zero config and the error,
// so an illegal value can never be persisted through this path.
func ParseAndValidateDiscordRegisterGate(raw string) (DiscordRegisterGateConfig, error) {
	cfg, err := ParseDiscordRegisterGate(raw)
	if err != nil {
		return DiscordRegisterGateConfig{}, err
	}
	// Normalize first so role_match empty -> "any" etc. happen on the value
	// we return, then validate the normalized form.
	NormalizeDiscordRegisterGate(&cfg)
	if err := ValidateDiscordRegisterGate(cfg); err != nil {
		return DiscordRegisterGateConfig{}, err
	}
	return cfg, nil
}
