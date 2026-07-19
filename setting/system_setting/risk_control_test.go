package system_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeRiskControlSettingKeepsGuardRailsConsistent(t *testing.T) {
	setting := NormalizeRiskControlSetting(RiskControlSetting{
		AutoActionMinScore:     98,
		AutoPermanentMinScore:  0,
		MaxAgentCasesPerRun:    0,
		AgentConcurrency:       0,
		AgentRetryCount:        0,
		GatewayUAMarkers:       []string{" Sub2API ", "sub2api", ""},
		ForbiddenClientMarkers: []string{},
	})

	assert.Equal(t, 98, setting.AutoPermanentMinScore)
	assert.Equal(t, 20, setting.MaxAgentCasesPerRun)
	assert.Equal(t, 4, setting.AgentConcurrency)
	assert.Equal(t, 2, setting.AgentRetryCount)
	assert.Equal(t, []string{"sub2api"}, setting.GatewayUAMarkers)
	assert.Empty(t, setting.ForbiddenClientMarkers)
}
