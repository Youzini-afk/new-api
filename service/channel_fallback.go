package service

import "github.com/gin-gonic/gin"

// Phase 9A/B safe MVP — per-channel explicit fallback backend.
//
// The gin context keys and the admin-info append helper live in the service
// package so both controller error logs (controller.processChannelError) and
// success logs (service.GenerateTextOtherInfo) can reference them without an
// import cycle. The controller schedules the fallback (prepareNextChannelFallback)
// and consumes the override in getChannel; this file only owns the key strings
// and the read-side helpers.
const (
	channelFallbackOverrideKey = "channel_fallback_override_channel_id"
	channelFallbackFromKey     = "channel_fallback_from_channel_id"
	channelFallbackToKey       = "channel_fallback_to_channel_id"
	channelFallbackReasonKey   = "channel_fallback_reason"
	channelFallbackAppliedKey  = "channel_fallback_applied"
)

// ChannelFallbackReasonEmptyReply is the admin-visible reason string recorded
// when a fallback was triggered because the upstream returned an empty response.
const ChannelFallbackReasonEmptyReply = "empty_reply"

// ChannelFallbackReasonError is the admin-visible reason string recorded when a
// fallback was triggered due to a non-empty-reply upstream error.
const ChannelFallbackReasonError = "error"

// MarkChannelFallbackScheduled records a fallback decision in the gin context:
// the override channel id that forces the next getChannel call to return this
// channel, plus admin-visible from/to/reason/applied metadata. Marks
// applied=true so a second fallback in the same request is rejected.
func MarkChannelFallbackScheduled(c *gin.Context, overrideChannelID int, fromChannelID int, reason string) {
	if c == nil {
		return
	}
	c.Set(channelFallbackOverrideKey, overrideChannelID)
	c.Set(channelFallbackFromKey, fromChannelID)
	c.Set(channelFallbackToKey, overrideChannelID)
	c.Set(channelFallbackReasonKey, reason)
	c.Set(channelFallbackAppliedKey, true)
}

// ConsumeChannelFallbackOverride returns the fallback override channel id and
// clears it (consume-once) so subsequent getChannel calls in the same request
// revert to normal selection. Returns (0, false) when no override is set.
func ConsumeChannelFallbackOverride(c *gin.Context) (int, bool) {
	if c == nil {
		return 0, false
	}
	val, ok := c.Get(channelFallbackOverrideKey)
	if !ok {
		return 0, false
	}
	overrideID, ok := val.(int)
	if !ok || overrideID <= 0 {
		return 0, false
	}
	c.Set(channelFallbackOverrideKey, 0)
	return overrideID, true
}

// IsChannelFallbackApplied reports whether a fallback has already been
// scheduled in this request, to guard against fallback loops.
func IsChannelFallbackApplied(c *gin.Context) bool {
	if c == nil {
		return false
	}
	val, ok := c.Get(channelFallbackAppliedKey)
	if !ok {
		return false
	}
	applied, ok := val.(bool)
	return ok && applied
}

// AppendChannelFallbackAdminInfo copies any scheduled fallback admin metadata
// from the gin context into the admin_info map used by error and success logs.
// No-op when no fallback has been scheduled.
func AppendChannelFallbackAdminInfo(c *gin.Context, adminInfo map[string]interface{}) {
	if c == nil || adminInfo == nil {
		return
	}
	if fromID, ok := c.Get(channelFallbackFromKey); ok {
		adminInfo["channel_fallback_from"] = fromID
	}
	if toID, ok := c.Get(channelFallbackToKey); ok {
		adminInfo["channel_fallback_to"] = toID
	}
	if reason, ok := c.Get(channelFallbackReasonKey); ok {
		adminInfo["channel_fallback_reason"] = reason
	}
	if applied, ok := c.Get(channelFallbackAppliedKey); ok {
		adminInfo["channel_fallback_applied"] = applied
	}
}
