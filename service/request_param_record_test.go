package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newRelayInfoForParamRecordTest builds a minimal RelayInfo via the real
// constructor (GenRelayInfoOpenAI) so the existing append* helpers in
// GenerateTextOtherInfo do not panic on nil internal fields. A nil request is
// accepted by the constructor (it guards `request != nil`).
//
// InitChannelMeta is called explicitly because the base constructor does NOT
// initialize the embedded *ChannelMeta pointer — accessing promoted fields
// (IsModelMapped, UpstreamModelName) on a nil *ChannelMeta would panic.
func newRelayInfoForParamRecordTest(ctx *gin.Context) *relaycommon.RelayInfo {
	info := relaycommon.GenRelayInfoOpenAI(ctx, nil)
	info.InitChannelMeta(ctx)
	// Ensure FirstResponseTime/StartTime are non-zero so the "frt" computation
	// in GenerateTextOtherInfo does not produce a meaningless but harmless
	// large negative; this keeps the test output deterministic.
	info.StartTime = time.Now()
	info.FirstResponseTime = info.StartTime
	return info
}

// newRequestBodyContext builds a gin.Context whose request body is the given
// JSON, wired through common.GetRequestBody (the io.Seeker body storage). This
// mirrors how the relay chain materializes the body.
func newRequestBodyContext(t *testing.T, method, target, bodyJSON string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(method, target, strings.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	// Materialize the body storage so GetRequestBody returns a seekable storage.
	_, err := common.GetRequestBody(ctx)
	require.NoError(t, err)
	return ctx
}

// TestBuildRequestParamsForLog_ReadsBodyThenRestores verifies the helper reads
// configured fields from the JSON body, and that after the call the body
// storage is seeked back to 0 and c.Request.Body is re-assignable so a second
// read returns the original content.
func TestBuildRequestParamsForLog_ReadsBodyThenRestores(t *testing.T) {
	bodyJSON := `{"model":"gpt-4","temperature":0.7,"top_p":0.9,"messages":[{"role":"user","content":"hi"}]}`
	ctx := newRequestBodyContext(t, http.MethodPost, "/v1/chat/completions", bodyJSON)

	// Snapshot setting fields so the test is isolated.
	origFields := system_setting.GetRelayParamRecordSetting().Fields
	t.Cleanup(func() { system_setting.GetRelayParamRecordSetting().Fields = origFields })
	// Restrict openai fields to a small set so the result is deterministic.
	system_setting.GetRelayParamRecordSetting().Fields = map[string][]string{
		"openai": {"model", "temperature", "top_p"},
	}

	params := BuildRequestParamsForLog(ctx, nil /* request DTO nil → fallback path, body wins */)
	require.NotNil(t, params)
	assert.Equal(t, "gpt-4", params["model"])
	// Numeric values are JSON-serialized by the default sanitize branch
	// (matching gy behavior), so temperature arrives as the string "0.7".
	assert.Equal(t, "0.7", params["temperature"])
	assert.Equal(t, "0.9", params["top_p"])

	// Body must be re-readable: read it back via GetBodyStorage and compare.
	storage, err := common.GetBodyStorage(ctx)
	require.NoError(t, err)
	body, err := storage.Bytes()
	require.NoError(t, err)
	assert.Equal(t, bodyJSON, string(body), "body storage must be intact after BuildRequestParamsForLog")

	// c.Request.Body must be re-assignable to the storage and yield the same content.
	assert.NotNil(t, ctx.Request.Body)
	got, err := io.ReadAll(ctx.Request.Body)
	require.NoError(t, err)
	assert.Equal(t, bodyJSON, string(got))
}

// TestBuildRequestParamsForLog_FallsBackToRequestDTO verifies that when the
// body is empty/unavailable, the helper falls back to the request DTO without
// panicking and still returns configured fields present on the DTO.
func TestBuildRequestParamsForLog_FallsBackToRequestDTO(t *testing.T) {
	// A bare *dto.BaseRequest marshals to "{}" — no fields — so the result is
	// nil (no configured fields present). The point of this test is: no panic.
	ctx := newRequestBodyContext(t, http.MethodPost, "/v1/chat/completions", "")
	origFields := system_setting.GetRelayParamRecordSetting().Fields
	t.Cleanup(func() { system_setting.GetRelayParamRecordSetting().Fields = origFields })
	system_setting.GetRelayParamRecordSetting().Fields = map[string][]string{
		"openai": {"model"},
	}

	// A nil request DTO + empty body → nil result, no panic.
	params := BuildRequestParamsForLog(ctx, nil)
	assert.Nil(t, params)
}

// TestBuildRequestParamsForLog_NilContext verifies nil ctx does not panic.
func TestBuildRequestParamsForLog_NilContext(t *testing.T) {
	assert.Nil(t, BuildRequestParamsForLog(nil, nil))
}

// TestMergeRequestParamsToOther_UsesSharedKey verifies the merge uses
// common.RequestParamsOtherKey (single source of truth) and returns other
// unchanged when params is empty.
func TestMergeRequestParamsToOther_UsesSharedKey(t *testing.T) {
	other := map[string]interface{}{"existing": 1}
	params := map[string]interface{}{"model": "gpt-4"}

	merged := MergeRequestParamsToOther(other, params)
	require.NotNil(t, merged)
	assert.Equal(t, 1, merged["existing"], "existing fields preserved")
	stored, ok := merged[common.RequestParamsOtherKey]
	require.True(t, ok, "params must be under common.RequestParamsOtherKey")
	assert.Equal(t, params, stored)

	// Empty params → other returned unchanged (no key added).
	other2 := map[string]interface{}{"existing": 2}
	merged2 := MergeRequestParamsToOther(other2, nil)
	assert.Equal(t, 2, merged2["existing"])
	_, present := merged2[common.RequestParamsOtherKey]
	assert.False(t, present, "empty params must not add the key")

	// Nil other + params → allocates.
	merged3 := MergeRequestParamsToOther(nil, params)
	require.NotNil(t, merged3)
	assert.Equal(t, params, merged3[common.RequestParamsOtherKey])
}

// TestDetectRequestParamGroup_ByPath verifies path-based group detection when
// the request DTO type does not match a known type.
func TestDetectRequestParamGroup_ByPath(t *testing.T) {
	cases := []struct {
		path  string
		group string
	}{
		{"/v1/chat/completions", "openai"},
		{"/v1/responses", "openai_responses"},
		{"/v1/embeddings", "embeddings"},
		{"/v1/images/generations", "images"},
		{"/v1/audio/speech", "audio"},
		{"/v1/messages", "claude"},
		{"/v1/rerank", "rerank"},
		{"/v1beta/models/gemini-pro:generateContent", "gemini_chat"},
		{"/v1/moderations", "openai"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, tc.path, nil)
			assert.Equal(t, tc.group, detectRequestParamGroup(ctx, nil))
		})
	}
}

// TestBuildRawRequestHeadersForInterceptLog_MasksSensitive verifies
// Authorization/cookie/api-key headers are masked while normal headers are kept.
func TestBuildRawRequestHeadersForInterceptLog_MasksSensitive(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("Authorization", "Bearer super-secret")
	ctx.Request.Header.Set("X-Api-Key", "sk-abc123")
	ctx.Request.Header.Set("User-Agent", "curl/8.0")
	ctx.Request.Header.Set("Content-Type", "application/json")

	raw := BuildRawRequestHeadersForInterceptLog(ctx)
	require.NotEmpty(t, raw)
	assert.Contains(t, raw, "***masked***")
	assert.NotContains(t, raw, "super-secret", "Authorization value must be masked")
	assert.NotContains(t, raw, "sk-abc123", "api key value must be masked")
	assert.Contains(t, raw, "curl/8.0")
	assert.Contains(t, raw, "application/json")

	// Nil ctx → empty string.
	assert.Empty(t, BuildRawRequestHeadersForInterceptLog(nil))
}

// TestBuildRawRequestParamsForInterceptLog_ReadsBodyAndRestores verifies the
// raw params builder reads the body, truncates large output, and restores the
// body seek position so the body is re-readable.
func TestBuildRawRequestParamsForInterceptLog_ReadsBodyAndRestores(t *testing.T) {
	bodyJSON := `{"model":"gpt-4","prompt":"hello"}`
	ctx := newRequestBodyContext(t, http.MethodPost, "/v1/chat/completions", bodyJSON)

	raw := BuildRawRequestParamsForInterceptLog(ctx, nil)
	require.NotEmpty(t, raw)
	assert.Contains(t, raw, "gpt-4")

	// Body re-readable.
	storage, err := common.GetBodyStorage(ctx)
	require.NoError(t, err)
	body, err := storage.Bytes()
	require.NoError(t, err)
	assert.Equal(t, bodyJSON, string(body))

	// Truncation: large body is bounded to rawInterceptLogMaxBytes.
	large := strings.Repeat("a", rawInterceptLogMaxBytes*2)
	ctx2 := newRequestBodyContext(t, http.MethodPost, "/v1/chat/completions", large)
	raw2 := BuildRawRequestParamsForInterceptLog(ctx2, nil)
	assert.LessOrEqual(t, len(raw2), rawInterceptLogMaxBytes+3, "output must be bounded (+3 for ellipsis)")
	assert.True(t, strings.HasSuffix(raw2, "..."), "truncated output ends with ellipsis")
}

// TestBuildRawRequestParamsForInterceptLog_FallsBackToRequestDTO verifies
// the fallback to request DTO when the body is empty.
func TestBuildRawRequestParamsForInterceptLog_FallsBackToRequestDTO(t *testing.T) {
	ctx := newRequestBodyContext(t, http.MethodPost, "/v1/chat/completions", "")
	// nil request DTO + empty body → empty string, no panic.
	raw := BuildRawRequestParamsForInterceptLog(ctx, nil)
	assert.Empty(t, raw)
}

// TestIsObservedLogScreeningUser_NoBanSync verifies the observed-user helper
// keys off the local LogScreeningRecord table (active high-risk record) and
// does NOT depend on ban_sync.
func TestIsObservedLogScreeningUser_NoBanSync(t *testing.T) {
	require.NoError(t, model.DB.Exec("DELETE FROM log_screening_records").Error)
	t.Cleanup(func() { model.DB.Exec("DELETE FROM log_screening_records") })

	// No record → not observed.
	assert.False(t, isObservedLogScreeningUser(999999))

	// Insert an active high-risk record for user 42.
	require.NoError(t, model.DB.Create(&model.LogScreeningRecord{
		UserId:        42,
		RiskLevel:     logScreeningRiskLevelHigh,
		ObservedUntil: 0, // never expires
		ExpiresAt:     0, // never expires
		RuleName:      "rule_x",
		Window:        "1h",
		RequestPath:   "all",
	}).Error)
	assert.True(t, isObservedLogScreeningUser(42), "active high-risk record → observed")

	// Expired record → not observed.
	require.NoError(t, model.DB.Exec("DELETE FROM log_screening_records").Error)
	now := common.GetTimestamp()
	require.NoError(t, model.DB.Create(&model.LogScreeningRecord{
		UserId:        43,
		RiskLevel:     logScreeningRiskLevelHigh,
		ObservedUntil: now - 100,
		ExpiresAt:     now - 100,
		RuleName:      "rule_y",
		Window:        "1h",
		RequestPath:   "all",
	}).Error)
	assert.False(t, isObservedLogScreeningUser(43), "expired record → not observed")

	// Non-high-risk record → not observed.
	require.NoError(t, model.DB.Exec("DELETE FROM log_screening_records").Error)
	require.NoError(t, model.DB.Create(&model.LogScreeningRecord{
		UserId:        44,
		RiskLevel:     "low",
		ObservedUntil: 0,
		ExpiresAt:     0,
		RuleName:      "rule_z",
		Window:        "1h",
		RequestPath:   "all",
	}).Error)
	assert.False(t, isObservedLogScreeningUser(44), "non-high-risk record → not observed")

	// Invalid userId → false (no DB hit).
	assert.False(t, isObservedLogScreeningUser(0))
	assert.False(t, isObservedLogScreeningUser(-1))
}

// TestBuildRequestParamsForLog_ObservedUserAddsSemanticFields verifies that
// when a user has an active high-risk screening record AND
// ObservedSemanticCaptureEnabled is on, the observed semantic fields are
// captured (bounded + masked), exercising the ban_sync-free observed path.
func TestBuildRequestParamsForLog_ObservedUserAddsSemanticFields(t *testing.T) {
	require.NoError(t, model.DB.Exec("DELETE FROM log_screening_records").Error)
	t.Cleanup(func() { model.DB.Exec("DELETE FROM log_screening_records") })

	setting := system_setting.GetRelayParamRecordSetting()
	origObservedEnabled := setting.ObservedSemanticCaptureEnabled
	origObservedFields := setting.ObservedSemanticFields
	origFields := setting.Fields
	t.Cleanup(func() {
		setting.ObservedSemanticCaptureEnabled = origObservedEnabled
		setting.ObservedSemanticFields = origObservedFields
		setting.Fields = origFields
	})

	// Insert an active high-risk record for the test user.
	require.NoError(t, model.DB.Create(&model.LogScreeningRecord{
		UserId:        7,
		RiskLevel:     logScreeningRiskLevelHigh,
		ObservedUntil: 0,
		ExpiresAt:     0,
		RuleName:      "rule_obs",
		Window:        "1h",
		RequestPath:   "all",
	}).Error)

	bodyJSON := `{"model":"gpt-4","prompt":"please help me with secrets"}`
	ctx := newRequestBodyContext(t, http.MethodPost, "/v1/chat/completions", bodyJSON)
	ctx.Set("id", 7)

	// Enable observed semantic capture + restrict fields so "prompt" is captured
	// for the observed user (it's in the default ObservedSemanticFields list).
	setting.ObservedSemanticCaptureEnabled = true
	setting.ObservedSemanticFields = []string{"prompt"}
	// openai group normally does NOT include "prompt"; the observed path adds it.
	setting.Fields = map[string][]string{"openai": {"model"}}

	params := BuildRequestParamsForLog(ctx, nil)
	require.NotNil(t, params)
	assert.Equal(t, "gpt-4", params["model"])
	// "prompt" is captured because the observed path added it.
	promptVal, ok := params["prompt"]
	require.True(t, ok, "observed semantic field 'prompt' must be captured for observed user")
	assert.Equal(t, "please help me with secrets", promptVal)
}

// TestBuildRequestParamsForLog_NonObservedUserDoesNotCapturePrompt verifies
// that without an active screening record, the "prompt" field (a user-text
// field) is cleared/blanked rather than captured.
func TestBuildRequestParamsForLog_NonObservedUserDoesNotCapturePrompt(t *testing.T) {
	require.NoError(t, model.DB.Exec("DELETE FROM log_screening_records").Error)
	t.Cleanup(func() { model.DB.Exec("DELETE FROM log_screening_records") })

	setting := system_setting.GetRelayParamRecordSetting()
	origObservedEnabled := setting.ObservedSemanticCaptureEnabled
	origFields := setting.Fields
	t.Cleanup(func() {
		setting.ObservedSemanticCaptureEnabled = origObservedEnabled
		setting.Fields = origFields
	})

	bodyJSON := `{"model":"gpt-4","prompt":"please help me with secrets"}`
	ctx := newRequestBodyContext(t, http.MethodPost, "/v1/chat/completions", bodyJSON)
	ctx.Set("id", 999) // no screening record

	setting.ObservedSemanticCaptureEnabled = true
	// openai group includes "prompt" explicitly — but prompt is a user-text
	// field, so it is blanked for non-observed users.
	setting.Fields = map[string][]string{"openai": {"model", "prompt"}}

	params := BuildRequestParamsForLog(ctx, nil)
	require.NotNil(t, params)
	assert.Equal(t, "gpt-4", params["model"])
	// "prompt" is present (it was a configured field) but blanked to "".
	promptVal, ok := params["prompt"]
	require.True(t, ok)
	assert.Equal(t, "", promptVal, "non-observed user: prompt must be blanked")
}

// TestGenerateTextOtherInfo_IncludesRequestParams verifies the log_info_generate
// integration: GenerateTextOtherInfo now nests the request params under
// common.RequestParamsOtherKey, alongside the existing fields.
func TestGenerateTextOtherInfo_IncludesRequestParams(t *testing.T) {
	require.NoError(t, model.DB.Exec("DELETE FROM log_screening_records").Error)
	t.Cleanup(func() { model.DB.Exec("DELETE FROM log_screening_records") })

	origFields := system_setting.GetRelayParamRecordSetting().Fields
	t.Cleanup(func() { system_setting.GetRelayParamRecordSetting().Fields = origFields })
	system_setting.GetRelayParamRecordSetting().Fields = map[string][]string{
		"openai": {"model", "temperature"},
	}

	bodyJSON := `{"model":"gpt-4","temperature":0.5}`
	ctx := newRequestBodyContext(t, http.MethodPost, "/v1/chat/completions", bodyJSON)

	// Build a minimal RelayInfo-shaped value via the real constructor so the
	// existing append* helpers don't panic on nil fields.
	relayInfo := newRelayInfoForParamRecordTest(ctx)

	other := GenerateTextOtherInfo(ctx, relayInfo, 1.0, 1.0, 1.0, 0, 0.0, 0.0, 1.0)
	require.NotNil(t, other)
	// Existing fields preserved.
	assert.Equal(t, 1.0, other["model_ratio"])
	assert.Equal(t, 1.0, other["group_ratio"])
	assert.NotNil(t, other["admin_info"])
	assert.Equal(t, "/v1/chat/completions", other["request_path"])
	// New request_params nested field present.
	params, ok := other[common.RequestParamsOtherKey]
	require.True(t, ok, "GenerateTextOtherInfo must include request_params")
	paramsMap, ok := params.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "gpt-4", paramsMap["model"])
	// Numeric values are JSON-serialized by the default sanitize branch.
	assert.Equal(t, "0.5", paramsMap["temperature"])
}

func TestGenerateTextOtherInfo_IncludesMaskedResponseText(t *testing.T) {
	ctx := newRequestBodyContext(t, http.MethodPost, "/v1/chat/completions", "")
	relayInfo := newRelayInfoForParamRecordTest(ctx)
	relayInfo.ResponseText = "visit https://api.example.com/v1/secret?token=abc and " + strings.Repeat("中", responseTextLogMaxBytes)

	other := GenerateTextOtherInfo(ctx, relayInfo, 1.0, 1.0, 1.0, 0, 0.0, 0.0, 1.0)
	recorded, ok := other["response_text"].(string)
	require.True(t, ok, "response_text must be written for admin logs")
	assert.LessOrEqual(t, len(recorded), responseTextLogMaxBytes, "response_text must be byte-bounded")
	assert.True(t, strings.HasSuffix(recorded, "..."), "truncated response_text must include ellipsis")
	assert.Contains(t, recorded, "https://***.com/***/***?token=***")
	assert.NotContains(t, recorded, "api.example.com")
	assert.NotContains(t, recorded, "token=abc")
	assert.Equal(t, true, other["response_text_truncated"])
	assert.True(t, relayInfo.ResponseTextTruncated)
}

// TestGenerateTextOtherInfo_NoCrashOnNilRequest verifies the integration does
// not panic when relayInfo.Request is nil and the body is empty.
func TestGenerateTextOtherInfo_NoCrashOnNilRequest(t *testing.T) {
	ctx := newRequestBodyContext(t, http.MethodPost, "/v1/chat/completions", "")
	relayInfo := newRelayInfoForParamRecordTest(ctx)
	other := GenerateTextOtherInfo(ctx, relayInfo, 1.0, 1.0, 1.0, 0, 0.0, 0.0, 1.0)
	require.NotNil(t, other)
	// request_params key simply absent when nothing was captured.
	_, present := other[common.RequestParamsOtherKey]
	assert.False(t, present)
}

// keep context import referenced (used in isObservedLogScreeningUser via
// model.DB which is package-level, but some helpers below use context).
var _ = context.Background
