package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newChannelTrafficMemoryStateForTest() *channelTrafficMemoryState {
	return &channelTrafficMemoryState{
		active: make(map[string]struct{}),
		notify: make(chan struct{}),
	}
}

func TestChannelTrafficMemoryStateEnforcesFIFOConcurrencyQueue(t *testing.T) {
	state := newChannelTrafficMemoryStateForTest()
	config := dto.ChannelTrafficControl{
		Enabled:             true,
		MaxConcurrency:      1,
		QueueSize:           2,
		QueueTimeoutSeconds: 30,
	}
	now := time.Unix(1_700_000_000, 0)

	first := state.tryAcquire("first", config, now)
	second := state.tryAcquire("second", config, now)
	third := state.tryAcquire("third", config, now)
	overflow := state.tryAcquire("overflow", config, now)

	assert.Equal(t, channelTrafficAcquired, first.status)
	assert.Equal(t, channelTrafficQueued, second.status)
	assert.Equal(t, 1, second.position)
	assert.Equal(t, channelTrafficQueued, third.status)
	assert.Equal(t, 2, third.position)
	assert.Equal(t, channelTrafficFull, overflow.status)

	state.release("first")
	second = state.tryAcquire("second", config, now)
	third = state.tryAcquire("third", config, now)
	assert.Equal(t, channelTrafficAcquired, second.status)
	assert.Equal(t, channelTrafficQueued, third.status)
	assert.Equal(t, 1, third.position)

	state.release("second")
	third = state.tryAcquire("third", config, now)
	assert.Equal(t, channelTrafficAcquired, third.status)
}

func TestChannelTrafficMemoryStateEnforcesSlidingRPM(t *testing.T) {
	state := newChannelTrafficMemoryStateForTest()
	config := dto.ChannelTrafficControl{
		Enabled:             true,
		RPM:                 2,
		QueueSize:           2,
		QueueTimeoutSeconds: 120,
	}
	now := time.Unix(1_700_000_000, 500_000_000)

	assert.Equal(t, channelTrafficAcquired, state.tryAcquire("first", config, now).status)
	assert.Equal(t, channelTrafficAcquired, state.tryAcquire("second", config, now).status)
	blocked := state.tryAcquire("third", config, now.Add(59*time.Second))
	assert.Equal(t, channelTrafficQueued, blocked.status)
	assert.Positive(t, blocked.retryAfter)

	admitted := state.tryAcquire("third", config, now.Add(61*time.Second))
	assert.Equal(t, channelTrafficAcquired, admitted.status)
	assert.Equal(t, 1, admitted.rpm)
}

func TestChannelTrafficResponseBodyReleasesLeaseOnce(t *testing.T) {
	var releases atomic.Int32
	lease := newChannelTrafficLease(
		context.Background(),
		ChannelTrafficAdmissionInfo{ChannelID: 7},
		func() { releases.Add(1) },
		nil,
	)
	body := HoldChannelTrafficUntilResponseClosed(io.NopCloser(strings.NewReader("ok")), lease)

	data, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, "ok", string(data))
	require.NoError(t, body.Close())
	lease.Release()
	assert.Equal(t, int32(1), releases.Load())
}

func TestDoChannelTrafficHTTPRequestHoldsConcurrencyUntilBodyEOF(t *testing.T) {
	oldRedisEnabled := common.RedisEnabled
	oldMemory := channelTrafficMemory
	common.RedisEnabled = false
	channelTrafficMemory = &channelTrafficMemoryManager{states: make(map[int]*channelTrafficMemoryState)}
	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
		channelTrafficMemory = oldMemory
	})

	config := &dto.ChannelTrafficControl{
		Enabled:        true,
		MaxConcurrency: 1,
	}
	response, err := DoChannelTrafficHTTPRequest(context.Background(), 17, config, func() (*http.Response, error) {
		return &http.Response{Body: io.NopCloser(strings.NewReader("ok"))}, nil
	})
	require.NoError(t, err)

	blocked, err := AcquireChannelTraffic(context.Background(), 17, config)
	assert.Nil(t, blocked)
	var apiErr *types.NewAPIError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, types.ErrorCodeChannelTrafficQueueFull, apiErr.GetErrorCode())

	data, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, "ok", string(data))

	admitted, err := AcquireChannelTraffic(context.Background(), 17, config)
	require.NoError(t, err)
	require.NotNil(t, admitted)
	admitted.Release()
}

func TestChannelTrafficMemoryAcquireHonorsCanceledContext(t *testing.T) {
	manager := &channelTrafficMemoryManager{states: make(map[int]*channelTrafficMemoryState)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	lease, err := manager.acquire(ctx, 21, dto.ChannelTrafficControl{
		Enabled:        true,
		MaxConcurrency: 1,
	}, "memory")
	assert.Nil(t, lease)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestChannelTrafficRedisKeysUseOneClusterSlot(t *testing.T) {
	for _, key := range channelTrafficRedisKeys(42) {
		assert.Contains(t, key, "{42}")
	}
}

func TestChannelTrafficErrorsNeverDisableChannel(t *testing.T) {
	err := newChannelTrafficAdmissionError(9, types.ErrorCodeChannelTrafficQueueTimeout)
	assert.True(t, types.IsChannelTrafficControlError(err))
	assert.False(t, ShouldDisableChannel(err))
}
