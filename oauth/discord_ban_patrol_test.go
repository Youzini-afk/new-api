package oauth

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
)

func TestEvaluateDiscordBanPatrolOldMemberOnlyGuildBan(t *testing.T) {
	withDiscordMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/users/@me/guilds/banned-guild/member", r.URL.Path)
		_, _ = w.Write([]byte(discordTestMemberJSON(time.Now())))
	})
	cfg := system_setting.DiscordRegisterGateConfig{
		Groups:    []system_setting.DiscordGateGroup{{Rules: []system_setting.DiscordGateRule{{GuildID: "allowed-guild", RoleIDs: []string{"role"}}}}},
		BanGroups: []system_setting.DiscordGateGroup{{Rules: []system_setting.DiscordGateRule{{GuildID: "banned-guild"}}}},
	}
	system_setting.NormalizeDiscordRegisterGate(&cfg)

	outcome := evaluateDiscordBanPatrol(context.Background(), "access-token", "identify guilds.members.read", cfg)
	assert.Equal(t, DiscordPatrolOutcomeBanMatched, outcome.Result)
}

func TestEvaluateDiscordBanPatrolUnknownScopeGuildBanProbesMemberEndpoint(t *testing.T) {
	withDiscordMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/users/@me/guilds/banned-guild/member", r.URL.Path)
		_, _ = w.Write([]byte(discordTestMemberJSON(time.Now())))
	})
	cfg := system_setting.DiscordRegisterGateConfig{BanGroups: []system_setting.DiscordGateGroup{{Rules: []system_setting.DiscordGateRule{{GuildID: "banned-guild"}}}}}
	system_setting.NormalizeDiscordRegisterGate(&cfg)

	outcome := evaluateDiscordBanPatrol(context.Background(), "access-token", "", cfg)
	assert.Equal(t, DiscordPatrolOutcomeBanMatched, outcome.Result)
}

func TestEvaluateDiscordBanPatrolUnknownScopeNotFoundIsNoBan(t *testing.T) {
	withDiscordMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/users/@me/guilds/banned-guild/member", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	})
	cfg := system_setting.DiscordRegisterGateConfig{BanGroups: []system_setting.DiscordGateGroup{{Rules: []system_setting.DiscordGateRule{{GuildID: "banned-guild"}}}}}
	system_setting.NormalizeDiscordRegisterGate(&cfg)

	outcome := evaluateDiscordBanPatrol(context.Background(), "access-token", "", cfg)
	assert.Equal(t, DiscordPatrolOutcomePass, outcome.Result)
}

func TestEvaluateDiscordBanPatrolUnknownScopeUnauthorizedIsReauthRequired(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			withDiscordMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/users/@me/guilds/banned-guild/member", r.URL.Path)
				w.WriteHeader(status)
			})
			cfg := system_setting.DiscordRegisterGateConfig{BanGroups: []system_setting.DiscordGateGroup{{Rules: []system_setting.DiscordGateRule{{GuildID: "banned-guild"}}}}}
			system_setting.NormalizeDiscordRegisterGate(&cfg)

			outcome := evaluateDiscordBanPatrol(context.Background(), "access-token", "", cfg)
			assert.Equal(t, DiscordPatrolOutcomeReauthRequired, outcome.Result)
		})
	}
}

func TestEvaluateDiscordBanPatrolOldMemberOnlyNotFoundIsNoBan(t *testing.T) {
	withDiscordMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/users/@me/guilds/banned-guild/member", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	})
	cfg := system_setting.DiscordRegisterGateConfig{BanGroups: []system_setting.DiscordGateGroup{{Rules: []system_setting.DiscordGateRule{{GuildID: "banned-guild"}}}}}
	system_setting.NormalizeDiscordRegisterGate(&cfg)

	outcome := evaluateDiscordBanPatrol(context.Background(), "access-token", "identify guilds.members.read", cfg)
	assert.Equal(t, DiscordPatrolOutcomePass, outcome.Result)
}

func TestEvaluateDiscordBanPatrolOldMemberOnlyRateLimitedTransient(t *testing.T) {
	withDiscordMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/users/@me/guilds/banned-guild/member", r.URL.Path)
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"rate limited"}`))
	})
	cfg := system_setting.DiscordRegisterGateConfig{BanGroups: []system_setting.DiscordGateGroup{{Rules: []system_setting.DiscordGateRule{{GuildID: "banned-guild"}}}}}
	system_setting.NormalizeDiscordRegisterGate(&cfg)

	outcome := evaluateDiscordBanPatrol(context.Background(), "access-token", "identify guilds.members.read", cfg)
	assert.Equal(t, DiscordPatrolOutcomeTransient, outcome.Result)
	assert.Equal(t, "rate_limited", outcome.Reason)
	assert.Equal(t, 3*time.Second, outcome.RetryAfter)
}

func TestEvaluateDiscordBanPatrolGuildListGuildOnlyBanAvoidsMemberEndpoint(t *testing.T) {
	withDiscordMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/users/@me/guilds", r.URL.Path)
		_, _ = w.Write([]byte(`[{"id":"banned-guild"}]`))
	})
	cfg := system_setting.DiscordRegisterGateConfig{BanGroups: []system_setting.DiscordGateGroup{{Rules: []system_setting.DiscordGateRule{{GuildID: "banned-guild"}}}}}
	system_setting.NormalizeDiscordRegisterGate(&cfg)

	outcome := evaluateDiscordBanPatrol(context.Background(), "access-token", "identify guilds guilds.members.read", cfg)
	assert.Equal(t, DiscordPatrolOutcomeBanMatched, outcome.Result)
}

func TestEvaluateDiscordBanPatrolRoleBanRequiresMemberEndpoint(t *testing.T) {
	withDiscordMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/@me/guilds":
			_, _ = w.Write([]byte(`[{"id":"banned-guild"}]`))
		case "/users/@me/guilds/banned-guild/member":
			_, _ = w.Write([]byte(discordTestMemberJSON(time.Now(), "ban-role")))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	cfg := system_setting.DiscordRegisterGateConfig{BanGroups: []system_setting.DiscordGateGroup{{Rules: []system_setting.DiscordGateRule{{GuildID: "banned-guild", RoleIDs: []string{"ban-role"}}}}}}
	system_setting.NormalizeDiscordRegisterGate(&cfg)

	outcome := evaluateDiscordBanPatrol(context.Background(), "access-token", "identify guilds guilds.members.read", cfg)
	assert.Equal(t, DiscordPatrolOutcomeBanMatched, outcome.Result)
}

func TestEvaluateDiscordBanPatrolIgnoresAllowGroups(t *testing.T) {
	withDiscordMemberServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/users/@me/guilds/banned-guild/member", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	})
	cfg := system_setting.DiscordRegisterGateConfig{
		Groups:    []system_setting.DiscordGateGroup{{Rules: []system_setting.DiscordGateRule{{GuildID: "allowed-guild", RoleIDs: []string{"missing-role"}}}}},
		BanGroups: []system_setting.DiscordGateGroup{{Rules: []system_setting.DiscordGateRule{{GuildID: "banned-guild"}}}},
	}
	system_setting.NormalizeDiscordRegisterGate(&cfg)

	outcome := evaluateDiscordBanPatrol(context.Background(), "access-token", "identify guilds.members.read", cfg)
	assert.Equal(t, DiscordPatrolOutcomePass, outcome.Result)
}
