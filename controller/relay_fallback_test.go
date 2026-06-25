package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newFallbackTestContext builds a minimal gin.Context with a POST request to
// the given path and the given channel other-settings installed in the
// channel_other_setting context slot (mirroring SetupContextForSelectedChannel).
func newFallbackTestContext(t *testing.T, path string, settings dto.ChannelOtherSettings) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, path, nil)
	common.SetContextKey(ctx, constant.ContextKeyChannelOtherSetting, settings)
	return ctx
}

func baseFallbackInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeChatCompletions,
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "gpt-test",
		IsStream:        false,
		UsingGroup:      "default",
		UserGroup:       "default",
	}
}

func baseCurrentChannel(id int) *model.Channel {
	return &model.Channel{Id: id, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled}
}

func TestRelayFallbackModeAllowed_NonStreamAllowlist(t *testing.T) {
	cases := []struct {
		name    string
		format  types.RelayFormat
		mode    int
		stream  bool
		allowed bool
	}{
		{"chat completions non-stream", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions, false, true},
		{"completions non-stream", types.RelayFormatOpenAI, relayconstant.RelayModeCompletions, false, true},
		{"responses non-stream", types.RelayFormatOpenAI, relayconstant.RelayModeResponses, false, true},
		{"responses compact non-stream", types.RelayFormatOpenAI, relayconstant.RelayModeResponsesCompact, false, true},
		{"embeddings non-stream", types.RelayFormatOpenAI, relayconstant.RelayModeEmbeddings, false, true},
		{"rerank non-stream", types.RelayFormatOpenAI, relayconstant.RelayModeRerank, false, true},
		{"gemini non-stream", types.RelayFormatGemini, relayconstant.RelayModeGemini, false, true},
		{"chat completions stream", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions, true, false},
		{"images generations", types.RelayFormatOpenAI, relayconstant.RelayModeImagesGenerations, false, false},
		{"audio speech", types.RelayFormatOpenAI, relayconstant.RelayModeAudioSpeech, false, false},
		{"audio transcription", types.RelayFormatOpenAI, relayconstant.RelayModeAudioTranscription, false, false},
		{"realtime format", types.RelayFormatOpenAIRealtime, relayconstant.RelayModeRealtime, false, false},
		{"unknown mode", types.RelayFormatOpenAI, relayconstant.RelayModeUnknown, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{RelayMode: tc.mode, IsStream: tc.stream}
			assert.Equal(t, tc.allowed, relayFallbackModeAllowed(tc.format, info))
		})
	}
}

// TestRelayFallbackStateDenied_AllDenials exercises every condition that must
// forbid a fallback. Each subtest sets exactly one denial trigger and asserts
// the helper returns true (denied).
func TestRelayFallbackStateDenied_AllDenials(t *testing.T) {
	baseErr := types.NewError(errors.New("boom"), types.ErrorCodeBadResponse)

	t.Run("stream request denied", func(t *testing.T) {
		ctx := newFallbackTestContext(t, "/v1/chat/completions", dto.ChannelOtherSettings{})
		info := baseFallbackInfo()
		info.IsStream = true
		assert.True(t, relayFallbackStateDenied(ctx, info, baseErr))
	})

	t.Run("stream status non-nil denied", func(t *testing.T) {
		ctx := newFallbackTestContext(t, "/v1/chat/completions", dto.ChannelOtherSettings{})
		info := baseFallbackInfo()
		info.StreamStatus = relaycommon.NewStreamStatus()
		assert.True(t, relayFallbackStateDenied(ctx, info, baseErr))
	})

	t.Run("send response count > 0 denied", func(t *testing.T) {
		ctx := newFallbackTestContext(t, "/v1/chat/completions", dto.ChannelOtherSettings{})
		info := baseFallbackInfo()
		info.SendResponseCount = 1
		assert.True(t, relayFallbackStateDenied(ctx, info, baseErr))
	})

	t.Run("received response count > 0 denied", func(t *testing.T) {
		ctx := newFallbackTestContext(t, "/v1/chat/completions", dto.ChannelOtherSettings{})
		info := baseFallbackInfo()
		info.ReceivedResponseCount = 1
		assert.True(t, relayFallbackStateDenied(ctx, info, baseErr))
	})

	t.Run("writer already written denied", func(t *testing.T) {
		ctx := newFallbackTestContext(t, "/v1/chat/completions", dto.ChannelOtherSettings{})
		info := baseFallbackInfo()
		// Force a write through the response writer to flip Written().
		_, _ = ctx.Writer.Write([]byte("x"))
		assert.True(t, ctx.Writer.Written())
		assert.True(t, relayFallbackStateDenied(ctx, info, baseErr))
	})

	t.Run("client context cancelled denied", func(t *testing.T) {
		ctx2 := newFallbackTestContext(t, "/v1/chat/completions", dto.ChannelOtherSettings{})
		info := baseFallbackInfo()
		// Build a request whose context is already cancelled so
		// c.Request.Context().Err() != nil.
		reqCtx, cancel := context.WithCancel(context.Background())
		cancel()
		ctx2.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(reqCtx)
		assert.True(t, relayFallbackStateDenied(ctx2, info, baseErr))
	})

	t.Run("skip-retry error denied", func(t *testing.T) {
		ctx := newFallbackTestContext(t, "/v1/chat/completions", dto.ChannelOtherSettings{})
		info := baseFallbackInfo()
		skipErr := types.NewError(errors.New("bad request"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
		assert.True(t, relayFallbackStateDenied(ctx, info, skipErr))
	})

	t.Run("specific channel id denied", func(t *testing.T) {
		ctx := newFallbackTestContext(t, "/v1/chat/completions", dto.ChannelOtherSettings{})
		ctx.Set("specific_channel_id", "42")
		info := baseFallbackInfo()
		assert.True(t, relayFallbackStateDenied(ctx, info, baseErr))
	})

	t.Run("already applied denied", func(t *testing.T) {
		ctx := newFallbackTestContext(t, "/v1/chat/completions", dto.ChannelOtherSettings{})
		service.MarkChannelFallbackScheduled(ctx, 99, 1, service.ChannelFallbackReasonError)
		info := baseFallbackInfo()
		assert.True(t, relayFallbackStateDenied(ctx, info, baseErr))
	})

	t.Run("clean state not denied", func(t *testing.T) {
		ctx := newFallbackTestContext(t, "/v1/chat/completions", dto.ChannelOtherSettings{})
		info := baseFallbackInfo()
		assert.False(t, relayFallbackStateDenied(ctx, info, baseErr))
	})
}

// TestPrepareNextChannelFallback_ReasonGate verifies that an empty-response error
// requires FallbackOnEmptyReply while any other error requires FallbackOnError.
func TestPrepareNextChannelFallback_ReasonGate(t *testing.T) {
	emptyErr := types.NewError(errors.New("empty"), types.ErrorCodeEmptyResponse)
	otherErr := types.NewError(errors.New("500"), types.ErrorCodeBadResponseStatusCode)

	t.Run("empty response without flag denied", func(t *testing.T) {
		ctx := newFallbackTestContext(t, "/v1/chat/completions", dto.ChannelOtherSettings{
			FallbackChannelID:    99,
			FallbackOnError:      true, // not enough for empty
			FallbackOnEmptyReply: false,
		})
		assert.False(t, prepareNextChannelFallback(ctx, baseCurrentChannel(1), baseFallbackInfo(), emptyErr))
		assert.False(t, service.IsChannelFallbackApplied(ctx))
	})

	t.Run("empty response with flag accepted", func(t *testing.T) {
		ctx := newFallbackTestContext(t, "/v1/chat/completions", dto.ChannelOtherSettings{
			FallbackChannelID:    0, // no real channel, will fail at load
			FallbackOnEmptyReply: true,
		})
		// FallbackChannelID=0 short-circuits before DB lookup; not applied.
		assert.False(t, prepareNextChannelFallback(ctx, baseCurrentChannel(1), baseFallbackInfo(), emptyErr))
		assert.False(t, service.IsChannelFallbackApplied(ctx))
	})

	t.Run("regular error without flag denied", func(t *testing.T) {
		ctx := newFallbackTestContext(t, "/v1/chat/completions", dto.ChannelOtherSettings{
			FallbackChannelID:    99,
			FallbackOnError:      false,
			FallbackOnEmptyReply: true, // not enough for regular error
		})
		assert.False(t, prepareNextChannelFallback(ctx, baseCurrentChannel(1), baseFallbackInfo(), otherErr))
		assert.False(t, service.IsChannelFallbackApplied(ctx))
	})
}

// TestPrepareNextChannelFallback_SettingsShortCircuit verifies the cheap
// rejection paths that don't need a DB: no settings, FallbackChannelID<=0,
// fallback target equals current channel.
func TestPrepareNextChannelFallback_SettingsShortCircuit(t *testing.T) {
	otherErr := types.NewError(errors.New("500"), types.ErrorCodeBadResponseStatusCode)

	t.Run("no other settings in context denied", func(t *testing.T) {
		ctx := newFallbackTestContext(t, "/v1/chat/completions", dto.ChannelOtherSettings{})
		assert.False(t, prepareNextChannelFallback(ctx, baseCurrentChannel(1), baseFallbackInfo(), otherErr))
	})

	t.Run("fallback channel id zero denied", func(t *testing.T) {
		ctx := newFallbackTestContext(t, "/v1/chat/completions", dto.ChannelOtherSettings{
			FallbackOnError: true,
		})
		assert.False(t, prepareNextChannelFallback(ctx, baseCurrentChannel(1), baseFallbackInfo(), otherErr))
	})

	t.Run("fallback target equals current channel denied", func(t *testing.T) {
		ctx := newFallbackTestContext(t, "/v1/chat/completions", dto.ChannelOtherSettings{
			FallbackChannelID: 1,
			FallbackOnError:   true,
		})
		assert.False(t, prepareNextChannelFallback(ctx, baseCurrentChannel(1), baseFallbackInfo(), otherErr))
	})

	t.Run("stream mode denied before settings", func(t *testing.T) {
		ctx := newFallbackTestContext(t, "/v1/chat/completions", dto.ChannelOtherSettings{
			FallbackChannelID: 99,
			FallbackOnError:   true,
		})
		info := baseFallbackInfo()
		info.IsStream = true
		assert.False(t, prepareNextChannelFallback(ctx, baseCurrentChannel(1), info, otherErr))
	})

	t.Run("images mode denied", func(t *testing.T) {
		ctx := newFallbackTestContext(t, "/v1/images/generations", dto.ChannelOtherSettings{
			FallbackChannelID: 99,
			FallbackOnError:   true,
		})
		info := baseFallbackInfo()
		info.RelayMode = relayconstant.RelayModeImagesGenerations
		assert.False(t, prepareNextChannelFallback(ctx, baseCurrentChannel(1), info, otherErr))
	})
}

// TestPrepareNextChannelFallback_ErrorPolicyGate verifies that fallback obeys
// the same error/status-code retry policy as shouldRetry (via
// relayFallbackErrorPolicyAllowed): always-skip status codes (504/524),
// always-skip error codes (bad_response_body), 2xx, and 4xx outside the
// configured retry ranges (e.g. 400) never trigger a fallback even when
// FallbackOnError is set and a valid fallback channel exists. A retryable
// 500/429 still triggers a fallback when settings allow.
//
// This is independent of the global retry-times budget: the policy gate fires
// regardless of common.RetryTimes, so we do not touch RetryTimes here.
func TestPrepareNextChannelFallback_ErrorPolicyGate(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	require.NoError(t, db.Create(&model.Channel{
		Id: 10, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled,
		Group: "default", Models: "gpt-test",
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "default", Model: "gpt-test", ChannelId: 10, Enabled: true,
	}).Error)

	// Denied cases: the policy must reject before scheduling, so applied stays
	// false even though FallbackOnError=true and a valid fallback channel exists.
	deniedCases := []struct {
		name string
		err  *types.NewAPIError
	}{
		{
			"400 bad request outside retry ranges",
			types.NewOpenAIError(errors.New("bad request"), types.ErrorCodeBadResponseStatusCode, http.StatusBadRequest),
		},
		{
			"504 always-skip status code",
			types.NewOpenAIError(errors.New("gateway timeout"), types.ErrorCodeBadResponseStatusCode, http.StatusGatewayTimeout),
		},
		{
			"524 always-skip status code",
			types.NewOpenAIError(errors.New("cloudflare timeout"), types.ErrorCodeBadResponseStatusCode, 524),
		},
		{
			"2xx success status code",
			types.NewOpenAIError(errors.New("ok"), types.ErrorCodeBadResponseStatusCode, http.StatusOK),
		},
		{
			"bad_response_body always-skip error code",
			types.NewError(errors.New("bad body"), types.ErrorCodeBadResponseBody),
		},
	}
	for _, tc := range deniedCases {
		t.Run("denied/"+tc.name, func(t *testing.T) {
			ctx := newFallbackTestContext(t, "/v1/chat/completions", dto.ChannelOtherSettings{
				FallbackChannelID: 10,
				FallbackOnError:   true,
			})
			assert.False(t, prepareNextChannelFallback(ctx, baseCurrentChannel(1), baseFallbackInfo(), tc.err))
			assert.False(t, service.IsChannelFallbackApplied(ctx),
				"policy must reject before scheduling a fallback")
		})
	}

	// Allowed cases: policy permits, fallback is scheduled against the seeded
	// channel 10 and the applied flag is set.
	allowedCases := []struct {
		name string
		err  *types.NewAPIError
	}{
		{
			"500 retryable server error",
			types.NewOpenAIError(errors.New("server error"), types.ErrorCodeBadResponseStatusCode, http.StatusInternalServerError),
		},
		{
			"429 retryable rate limit",
			types.NewOpenAIError(errors.New("rate limited"), types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests),
		},
	}
	for _, tc := range allowedCases {
		t.Run("allowed/"+tc.name, func(t *testing.T) {
			ctx := newFallbackTestContext(t, "/v1/chat/completions", dto.ChannelOtherSettings{
				FallbackChannelID: 10,
				FallbackOnError:   true,
			})
			require.True(t, prepareNextChannelFallback(ctx, baseCurrentChannel(1), baseFallbackInfo(), tc.err))
			require.True(t, service.IsChannelFallbackApplied(ctx))
			overrideID, ok := service.ConsumeChannelFallbackOverride(ctx)
			require.True(t, ok)
			require.Equal(t, 10, overrideID)
		})
	}
}

// TestRelayFallbackErrorPolicyAllowed_MirrorsShouldRetry is a focused unit test
// on the policy helper itself, asserting the parity with shouldRetry's
// error/status-code branches (minus the retry-times and state-guard branches
// that relayFallbackStateDenied already covers).
func TestRelayFallbackErrorPolicyAllowed_MirrorsShouldRetry(t *testing.T) {
	cases := []struct {
		name    string
		err     *types.NewAPIError
		allowed bool
	}{
		{"nil error", nil, false},
		{
			"channel error code always allowed",
			types.NewError(errors.New("channel:no_available_key"), types.ErrorCodeChannelNoAvailableKey),
			true,
		},
		{
			"skip-retry error denied",
			types.NewError(errors.New("invalid"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry()),
			false,
		},
		{
			"2xx denied",
			types.NewOpenAIError(errors.New("ok"), types.ErrorCodeBadResponseStatusCode, http.StatusOK),
			false,
		},
		{
			"status 0 out-of-range allowed",
			types.NewOpenAIError(errors.New("zero"), types.ErrorCodeBadResponseStatusCode, 0),
			true,
		},
		{
			"status 999 out-of-range allowed",
			types.NewOpenAIError(errors.New("weird"), types.ErrorCodeBadResponseStatusCode, 999),
			true,
		},
		{
			"400 denied (outside retry ranges)",
			types.NewOpenAIError(errors.New("bad"), types.ErrorCodeBadResponseStatusCode, http.StatusBadRequest),
			false,
		},
		{
			"408 denied (azure timeout excluded)",
			types.NewOpenAIError(errors.New("azure timeout"), types.ErrorCodeBadResponseStatusCode, http.StatusRequestTimeout),
			false,
		},
		{
			"504 denied (always-skip status)",
			types.NewOpenAIError(errors.New("gw timeout"), types.ErrorCodeBadResponseStatusCode, http.StatusGatewayTimeout),
			false,
		},
		{
			"524 denied (always-skip status)",
			types.NewOpenAIError(errors.New("cf timeout"), types.ErrorCodeBadResponseStatusCode, 524),
			false,
		},
		{
			"bad_response_body denied (always-skip code)",
			types.NewError(errors.New("bad body"), types.ErrorCodeBadResponseBody),
			false,
		},
		{
			"500 allowed (retryable)",
			types.NewOpenAIError(errors.New("server"), types.ErrorCodeBadResponseStatusCode, http.StatusInternalServerError),
			true,
		},
		{
			"429 allowed (retryable)",
			types.NewOpenAIError(errors.New("rate"), types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests),
			true,
		},
		{
			"418 allowed (4xx inside retry range 409-499)",
			types.NewOpenAIError(errors.New("teapot"), types.ErrorCodeBadResponseStatusCode, http.StatusTeapot),
			true,
		},
		{
			"401 allowed (in retry ranges 401-407)",
			types.NewOpenAIError(errors.New("unauthorized"), types.ErrorCodeBadResponseStatusCode, http.StatusUnauthorized),
			true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.allowed, relayFallbackErrorPolicyAllowed(tc.err))
		})
	}
}

// TestCanFallbackChannelServeRequest_TargetValidation covers the runtime target
// validation: same channel, disabled, already-used, missing group/model
// eligibility, and a valid target. Uses an isolated SQLite DB so
// IsChannelEnabledForGroupModel works without the in-memory cache.
func TestCanFallbackChannelServeRequest_TargetValidation(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))

	// Seed two enabled channels + abilities for "default" group / "gpt-test".
	require.NoError(t, db.Create(&model.Channel{
		Id: 10, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled,
		Group: "default", Models: "gpt-test",
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id: 11, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled,
		Group: "default", Models: "gpt-test",
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id: 12, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusManuallyDisabled,
		Group: "default", Models: "gpt-test",
	}).Error)
	// Channel 13 serves a different group.
	require.NoError(t, db.Create(&model.Channel{
		Id: 13, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled,
		Group: "vip", Models: "gpt-test",
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "default", Model: "gpt-test", ChannelId: 10, Enabled: true},
		{Group: "default", Model: "gpt-test", ChannelId: 11, Enabled: true},
		{Group: "default", Model: "gpt-test", ChannelId: 12, Enabled: true},
		{Group: "vip", Model: "gpt-test", ChannelId: 13, Enabled: true},
	}).Error)

	// MemoryCacheEnabled is false in tests, so IsChannelEnabledForGroupModel
	// goes through the DB path directly (no need to seed the in-memory cache).

	info := baseFallbackInfo()

	t.Run("same channel denied", func(t *testing.T) {
		ctx := newFallbackTestContext(t, "/v1/chat/completions", dto.ChannelOtherSettings{})
		fb, err := model.GetChannelById(10, true)
		require.NoError(t, err)
		assert.False(t, canFallbackChannelServeRequest(ctx, fb, 10, info))
	})

	t.Run("disabled channel denied", func(t *testing.T) {
		ctx := newFallbackTestContext(t, "/v1/chat/completions", dto.ChannelOtherSettings{})
		fb, err := model.GetChannelById(12, true)
		require.NoError(t, err)
		assert.False(t, canFallbackChannelServeRequest(ctx, fb, 1, info))
	})

	t.Run("already used channel denied", func(t *testing.T) {
		ctx := newFallbackTestContext(t, "/v1/chat/completions", dto.ChannelOtherSettings{})
		ctx.Set("use_channel", []string{"1", "11"})
		fb, err := model.GetChannelById(11, true)
		require.NoError(t, err)
		assert.False(t, canFallbackChannelServeRequest(ctx, fb, 1, info))
	})

	t.Run("not serving group-model denied", func(t *testing.T) {
		ctx := newFallbackTestContext(t, "/v1/chat/completions", dto.ChannelOtherSettings{})
		fb, err := model.GetChannelById(13, true) // vip-only channel
		require.NoError(t, err)
		assert.False(t, canFallbackChannelServeRequest(ctx, fb, 1, info))
	})

	t.Run("eligible target accepted", func(t *testing.T) {
		ctx := newFallbackTestContext(t, "/v1/chat/completions", dto.ChannelOtherSettings{})
		fb, err := model.GetChannelById(11, true)
		require.NoError(t, err)
		assert.True(t, canFallbackChannelServeRequest(ctx, fb, 1, info))
	})
}

// TestPrepareNextChannelFallback_AcceptedSetsAdminInfoAndOverride runs the
// full happy path against an isolated DB and asserts the override id and the
// admin-info metadata get recorded.
func TestPrepareNextChannelFallback_AcceptedSetsAdminInfoAndOverride(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	require.NoError(t, db.Create(&model.Channel{
		Id: 10, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled,
		Group: "default", Models: "gpt-test",
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group: "default", Model: "gpt-test", ChannelId: 10, Enabled: true,
	}).Error)

	ctx := newFallbackTestContext(t, "/v1/chat/completions", dto.ChannelOtherSettings{
		FallbackChannelID: 10,
		FallbackOnError:   true,
	})
	otherErr := types.NewError(errors.New("500"), types.ErrorCodeBadResponseStatusCode)

	ok := prepareNextChannelFallback(ctx, baseCurrentChannel(1), baseFallbackInfo(), otherErr)
	require.True(t, ok)

	// Override id is set and consumable.
	overrideID, ok := service.ConsumeChannelFallbackOverride(ctx)
	require.True(t, ok)
	require.Equal(t, 10, overrideID)

	// Override is consumed-once: second read returns nothing.
	_, ok = service.ConsumeChannelFallbackOverride(ctx)
	require.False(t, ok)

	// Applied flag stays true (it's the loop guard, not consume-once).
	require.True(t, service.IsChannelFallbackApplied(ctx))

	// Admin info map carries from/to/reason/applied keys.
	adminInfo := map[string]interface{}{}
	service.AppendChannelFallbackAdminInfo(ctx, adminInfo)
	assert.Equal(t, 1, adminInfo["channel_fallback_from"])
	assert.Equal(t, 10, adminInfo["channel_fallback_to"])
	assert.Equal(t, service.ChannelFallbackReasonError, adminInfo["channel_fallback_reason"])
	assert.Equal(t, true, adminInfo["channel_fallback_applied"])
}

// TestGetChannel_ReturnsForcedFallbackAndClearsOverride exercises the
// controller-level integration: after MarkChannelFallbackScheduled sets the
// override, getChannel must return the fallback channel, run
// SetupContextForSelectedChannel so the next attempt sees the fallback's
// settings, and clear the override so a subsequent call reverts to normal
// selection (which fails here because CacheGetRandomSatisfiedChannel needs the
// full channel cache that we don't seed in this isolated test).
func TestGetChannel_ReturnsForcedFallbackAndClearsOverride(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	require.NoError(t, db.Create(&model.Channel{
		Id: 10, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled,
		Group: "default", Models: "gpt-test", Key: "sk-test",
	}).Error)
	// Seed the ability row so IsChannelEnabledForGroupModel admits channel 10
	// for the default group + gpt-test model. MemoryCacheEnabled is false in
	// tests, so the DB path is used directly.
	require.NoError(t, db.Create(&model.Ability{
		Group: "default", Model: "gpt-test", ChannelId: 10, Enabled: true,
	}).Error)

	ctx := newFallbackTestContext(t, "/v1/chat/completions", dto.ChannelOtherSettings{
		FallbackChannelID: 10,
		FallbackOnError:   true,
	})
	otherErr := types.NewError(errors.New("500"), types.ErrorCodeBadResponseStatusCode)
	info := baseFallbackInfo()
	// ChannelMeta must be non-nil so getChannel skips the early stub branch
	// and exercises the override path.
	info.ChannelMeta = &relaycommon.ChannelMeta{ChannelId: 1}

	require.True(t, model.IsChannelEnabledForGroupModel("default", "gpt-test", 10),
		"ability must be visible to IsChannelEnabledForGroupModel")

	// Schedule the fallback via the controller helper; this sets the override.
	require.True(t, prepareNextChannelFallback(ctx, baseCurrentChannel(1), info, otherErr))

	ch, apiErr := getChannel(ctx, info, &service.RetryParam{Retry: new(int)})
	// Note: apiErr is *types.NewAPIError. A nil concrete pointer converts to
	// a non-nil error interface (Go typed-nil gotcha), so require.NoError would
	// spuriously fail. Compare the concrete pointer against nil instead.
	require.True(t, apiErr == nil, "getChannel must not error on override path")
	require.NotNil(t, ch)
	require.Equal(t, 10, ch.Id)

	// The override was consumed-once by getChannel: a second
	// ConsumeChannelFallbackOverride call returns nothing. (We do not call
	// getChannel a second time here because the normal selection path requires
	// a fully-wired channel cache + RetryParam that this isolated test does
	// not seed; the consume-once semantics are already covered by the service
	// helper test TestChannelFallbackHelpers_OverrideLifecycle.)
	_, ok := service.ConsumeChannelFallbackOverride(ctx)
	require.False(t, ok, "override must be consumed by the first getChannel call")

	// Channel context was rewired to the fallback channel.
	require.Equal(t, 10, common.GetContextKeyInt(ctx, constant.ContextKeyChannelId))
	// Admin info reflects the scheduled fallback.
	adminInfo := map[string]interface{}{}
	service.AppendChannelFallbackAdminInfo(ctx, adminInfo)
	require.Equal(t, 1, adminInfo["channel_fallback_from"])
	require.Equal(t, 10, adminInfo["channel_fallback_to"])
}
func TestValidateChannel_FallbackFields(t *testing.T) {
	cases := []struct {
		name      string
		settings  dto.ChannelOtherSettings
		channelID int
		wantErr   bool
	}{
		{"zero values accepted (default off)", dto.ChannelOtherSettings{}, 0, false},
		{"negative retry times rejected", dto.ChannelOtherSettings{RetryTimes: -1}, 0, true},
		{"negative retry interval rejected", dto.ChannelOtherSettings{RetryIntervalSeconds: -0.1}, 0, true},
		{"negative fallback channel id rejected", dto.ChannelOtherSettings{FallbackChannelID: -1}, 0, true},
		{"self fallback rejected on edit", dto.ChannelOtherSettings{FallbackChannelID: 5}, 5, true},
		{"distinct fallback id accepted", dto.ChannelOtherSettings{FallbackChannelID: 5}, 1, false},
		{"zero fallback on existing channel accepted", dto.ChannelOtherSettings{FallbackChannelID: 0}, 5, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch := &model.Channel{
				Id:     tc.channelID,
				Type:   constant.ChannelTypeOpenAI,
				Key:    "k",
				Status: common.ChannelStatusEnabled,
				Group:  "default",
			}
			ch.SetOtherSettings(tc.settings)
			err := validateChannel(ch, false)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
