package system_setting

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const (
	discordGateRoleMatchAny = "any"
	discordGateRoleMatchAll = "all"
)

// DiscordRegisterGateConfig is the Discord OAuth gate configuration ported
// from gy, without ban_sync / external-bot coupling.
//
// Semantics:
//   - A group passes when all of its rules pass.
//   - The allow-list passes when any group in Groups passes.
//   - The deny-list hits when any group in BanGroups passes, and ban wins
//     before allow-list evaluation.
//
// An entirely empty config is valid for option persistence. Runtime evaluators
// must fail closed when a gate is enabled but no rules are configured.
type DiscordRegisterGateConfig struct {
	FailMessage string             `json:"fail_message"`
	BanMessage  string             `json:"ban_message"`
	Groups      []DiscordGateGroup `json:"groups"`
	BanGroups   []DiscordGateGroup `json:"ban_groups"`
}

// DiscordGateGroup is a named conjunction of Discord membership rules.
type DiscordGateGroup struct {
	Name  string            `json:"name,omitempty"`
	Rules []DiscordGateRule `json:"rules"`
}

// DiscordGateRule describes one guild membership condition.
type DiscordGateRule struct {
	GuildID      string   `json:"guild_id"`
	RoleIDs      []string `json:"role_ids,omitempty"`
	RoleMatch    string   `json:"role_match,omitempty"`
	MinJoinHours int      `json:"min_join_hours,omitempty"`
}

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
func NormalizeDiscordRegisterGate(cfg *DiscordRegisterGateConfig) {
	if cfg == nil {
		return
	}
	cfg.FailMessage = strings.TrimSpace(cfg.FailMessage)
	cfg.BanMessage = strings.TrimSpace(cfg.BanMessage)
	normalizeDiscordGateGroups(cfg.Groups)
	normalizeDiscordGateGroups(cfg.BanGroups)
}

func normalizeDiscordGateGroups(groups []DiscordGateGroup) {
	for groupIndex := range groups {
		group := &groups[groupIndex]
		group.Name = strings.TrimSpace(group.Name)
		for ruleIndex := range group.Rules {
			rule := &group.Rules[ruleIndex]
			rule.GuildID = strings.TrimSpace(rule.GuildID)
			rule.RoleMatch = normalizeDiscordGateRoleMatch(rule.RoleMatch)
			rule.RoleIDs = dedupeDiscordGateStrings(normalizeDiscordGateStrings(rule.RoleIDs))
		}
	}
}

func normalizeDiscordGateRoleMatch(raw string) string {
	switch roleMatch := strings.ToLower(strings.TrimSpace(raw)); roleMatch {
	case "":
		return discordGateRoleMatchAny
	case discordGateRoleMatchAny, discordGateRoleMatchAll:
		return roleMatch
	default:
		// Unknown non-empty value: leave it lowercased so Validate can reject it.
		return roleMatch
	}
}

func normalizeDiscordGateStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}
	return result
}

func dedupeDiscordGateStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

// ValidateDiscordRegisterGate checks the gate config for invalid entries. An
// empty config (no groups and no ban groups) is valid for option persistence;
// runtime evaluators must fail closed if a gate is enabled with no rules.
func ValidateDiscordRegisterGate(cfg DiscordRegisterGateConfig) error {
	NormalizeDiscordRegisterGate(&cfg)
	if err := validateDiscordGateGroups("groups", cfg.Groups, false); err != nil {
		return err
	}
	if err := validateDiscordGateGroups("ban_groups", cfg.BanGroups, true); err != nil {
		return err
	}
	return nil
}

func validateDiscordGateGroups(field string, groups []DiscordGateGroup, isBan bool) error {
	for groupIndex, group := range groups {
		if len(group.Rules) == 0 {
			return fmt.Errorf("discord register gate: %s[%d].rules must not be empty", field, groupIndex)
		}
		for ruleIndex, rule := range group.Rules {
			if strings.TrimSpace(rule.GuildID) == "" {
				return fmt.Errorf("discord register gate: %s[%d].rules[%d].guild_id must not be empty", field, groupIndex, ruleIndex)
			}
			if rule.RoleMatch != discordGateRoleMatchAny && rule.RoleMatch != discordGateRoleMatchAll {
				return fmt.Errorf("discord register gate: %s[%d].rules[%d].role_match must be \"any\" or \"all\", got %q", field, groupIndex, ruleIndex, rule.RoleMatch)
			}
			if rule.MinJoinHours < 0 {
				return fmt.Errorf("discord register gate: %s[%d].rules[%d].min_join_hours must not be negative", field, groupIndex, ruleIndex)
			}
			if isBan {
				if rule.MinJoinHours != 0 {
					return fmt.Errorf("discord register gate: ban_groups[%d].rules[%d].min_join_hours must be 0", groupIndex, ruleIndex)
				}
				continue
			}
			if len(rule.RoleIDs) == 0 && rule.MinJoinHours == 0 {
				return fmt.Errorf("discord register gate: groups[%d].rules[%d] must configure role_ids or min_join_hours", groupIndex, ruleIndex)
			}
		}
	}
	return nil
}

// ParseAndValidateDiscordRegisterGate parses, normalizes and validates a raw
// JSON config string. On success it returns the normalized config; on error it
// returns the zero config and the error.
func ParseAndValidateDiscordRegisterGate(raw string) (DiscordRegisterGateConfig, error) {
	cfg, err := ParseDiscordRegisterGate(raw)
	if err != nil {
		return DiscordRegisterGateConfig{}, err
	}
	NormalizeDiscordRegisterGate(&cfg)
	if err := ValidateDiscordRegisterGate(cfg); err != nil {
		return DiscordRegisterGateConfig{}, err
	}
	return cfg, nil
}
