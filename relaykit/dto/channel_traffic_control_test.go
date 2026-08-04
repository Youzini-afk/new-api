package dto

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelTrafficControlValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  ChannelTrafficControl
		wantErr bool
	}{
		{
			name: "disabled",
			config: ChannelTrafficControl{
				Enabled: false,
			},
		},
		{
			name: "concurrency with queue",
			config: ChannelTrafficControl{
				Enabled:             true,
				MaxConcurrency:      8,
				QueueSize:           100,
				QueueTimeoutSeconds: 30,
			},
		},
		{
			name: "rpm without queue",
			config: ChannelTrafficControl{
				Enabled: true,
				RPM:     60,
			},
		},
		{
			name: "enabled without a limit",
			config: ChannelTrafficControl{
				Enabled: true,
			},
			wantErr: true,
		},
		{
			name: "queue requires timeout",
			config: ChannelTrafficControl{
				Enabled:        true,
				MaxConcurrency: 1,
				QueueSize:      1,
			},
			wantErr: true,
		},
		{
			name: "negative values rejected",
			config: ChannelTrafficControl{
				Enabled:        true,
				MaxConcurrency: -1,
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.config.Validate()
			if test.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestChannelTrafficControlEffectiveRequiresEnabledLimit(t *testing.T) {
	require.False(t, (*ChannelTrafficControl)(nil).Effective())
	require.False(t, (&ChannelTrafficControl{Enabled: true}).Effective())
	require.True(t, (&ChannelTrafficControl{Enabled: true, MaxConcurrency: 1}).Effective())
	require.True(t, (&ChannelTrafficControl{Enabled: true, RPM: 1}).Effective())
}
