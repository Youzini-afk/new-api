package service

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChannelFallbackHelpers_OverrideLifecycle covers the consume-once
// semantics of the override key and the applied-flag loop guard. These are the
// service-level invariants the controller relies on to avoid fallback loops
// and to make the next getChannel call return the fallback channel.
func TestChannelFallbackHelpers_OverrideLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)

	// Before scheduling: no override, not applied.
	_, ok := ConsumeChannelFallbackOverride(ctx)
	require.False(t, ok)
	require.False(t, IsChannelFallbackApplied(ctx))

	MarkChannelFallbackScheduled(ctx, 42, 7, ChannelFallbackReasonError)

	// After scheduling: override is set, applied flag is set.
	overrideID, ok := ConsumeChannelFallbackOverride(ctx)
	require.True(t, ok)
	require.Equal(t, 42, overrideID)
	require.True(t, IsChannelFallbackApplied(ctx))

	// Override is consume-once: second read returns nothing. Applied flag
	// stays true so a second fallback in the same request is still rejected.
	_, ok = ConsumeChannelFallbackOverride(ctx)
	require.False(t, ok)
	require.True(t, IsChannelFallbackApplied(ctx))
}

// TestChannelFallbackHelpers_AdminInfoPropagation verifies the admin-info map
// gets the from/to/reason/applied fields populated exactly as scheduled, and
// is a no-op when no fallback was scheduled.
func TestChannelFallbackHelpers_AdminInfoPropagation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("no fallback scheduled: no-op", func(t *testing.T) {
		ctx, _ := gin.CreateTestContext(nil)
		adminInfo := map[string]interface{}{"existing": "value"}
		AppendChannelFallbackAdminInfo(ctx, adminInfo)
		_, hasFrom := adminInfo["channel_fallback_from"]
		assert.False(t, hasFrom)
		assert.Equal(t, "value", adminInfo["existing"])
	})

	t.Run("fallback scheduled: all fields copied", func(t *testing.T) {
		ctx, _ := gin.CreateTestContext(nil)
		MarkChannelFallbackScheduled(ctx, 42, 7, ChannelFallbackReasonEmptyReply)
		// Override has been consumed-by-mark — but the admin metadata stays.
		adminInfo := map[string]interface{}{}
		AppendChannelFallbackAdminInfo(ctx, adminInfo)
		assert.Equal(t, 7, adminInfo["channel_fallback_from"])
		assert.Equal(t, 42, adminInfo["channel_fallback_to"])
		assert.Equal(t, ChannelFallbackReasonEmptyReply, adminInfo["channel_fallback_reason"])
		assert.Equal(t, true, adminInfo["channel_fallback_applied"])
	})
}

// TestRetryParam_ResetRetryNextTry confirms the RetryParam behavior the relay
// loop relies on: when ResetRetryNextTry is called, the next IncreaseRetry is
// a no-op so the fallback attempt does not consume a global retry slot. Two
// subsequent IncreaseRetry calls then advance the counter normally.
func TestRetryParam_ResetRetryNextTry(t *testing.T) {
	p := &RetryParam{Retry: new(int)}

	// Simulate the retry loop's post-fallback sequence: ResetRetryNextTry then
	// IncreaseRetry. The retry counter must stay at 0 — the fallback attempt
	// does not count as an extra global retry step.
	p.ResetRetryNextTry()
	p.IncreaseRetry()
	require.Equal(t, 0, p.GetRetry(), "fallback attempt must not consume a retry slot")

	// Subsequent retries increment normally.
	p.IncreaseRetry()
	require.Equal(t, 1, p.GetRetry())
	p.IncreaseRetry()
	require.Equal(t, 2, p.GetRetry())
}

// TestGenerateTextOtherInfo_IncludesFallbackAdminInfo verifies the success-log
// integration: when a per-channel fallback was scheduled, GenerateTextOtherInfo
// must surface the fallback admin metadata (from/to/reason/applied) in the
// admin_info map alongside the affinity metadata, so a fallback that ultimately
// succeeded is visible in the consume log. processChannelError already appends
// the same fields to error logs; this test pins the success-log path.
func TestGenerateTextOtherInfo_IncludesFallbackAdminInfo(t *testing.T) {
	ctx := newRequestBodyContext(t, http.MethodPost, "/v1/chat/completions", `{"model":"gpt-4"}`)
	relayInfo := newRelayInfoForParamRecordTest(ctx)

	// Schedule a fallback (mimicking prepareNextChannelFallback) so the admin
	// metadata is recorded in the gin context.
	MarkChannelFallbackScheduled(ctx, 42, 7, ChannelFallbackReasonError)

	other := GenerateTextOtherInfo(ctx, relayInfo, 1.0, 1.0, 1.0, 0, 0.0, 0.0, 1.0)
	require.NotNil(t, other)

	adminInfoAny, ok := other["admin_info"]
	require.True(t, ok, "GenerateTextOtherInfo must populate admin_info")
	adminInfo, ok := adminInfoAny.(map[string]interface{})
	require.True(t, ok)

	assert.Equal(t, 7, adminInfo["channel_fallback_from"])
	assert.Equal(t, 42, adminInfo["channel_fallback_to"])
	assert.Equal(t, ChannelFallbackReasonError, adminInfo["channel_fallback_reason"])
	assert.Equal(t, true, adminInfo["channel_fallback_applied"])
}

// TestGenerateTextOtherInfo_NoFallbackAdminInfoWhenUnscheduled verifies the
// success log does NOT carry fallback admin fields when no fallback was
// scheduled, so existing requests' logs stay clean.
func TestGenerateTextOtherInfo_NoFallbackAdminInfoWhenUnscheduled(t *testing.T) {
	ctx := newRequestBodyContext(t, http.MethodPost, "/v1/chat/completions", `{"model":"gpt-4"}`)
	relayInfo := newRelayInfoForParamRecordTest(ctx)

	other := GenerateTextOtherInfo(ctx, relayInfo, 1.0, 1.0, 1.0, 0, 0.0, 0.0, 1.0)
	require.NotNil(t, other)
	adminInfoAny, ok := other["admin_info"]
	require.True(t, ok)
	adminInfo, ok := adminInfoAny.(map[string]interface{})
	require.True(t, ok)
	_, hasFrom := adminInfo["channel_fallback_from"]
	assert.False(t, hasFrom, "no fallback fields when nothing was scheduled")
	_, hasTo := adminInfo["channel_fallback_to"]
	assert.False(t, hasTo)
}
