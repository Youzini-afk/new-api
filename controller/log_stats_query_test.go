package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gin-gonic/gin"
)

func newQueryContext(t *testing.T, target string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, target, nil)
	return ctx, recorder
}

func TestConversationRequestPathForKind(t *testing.T) {
	cases := []struct {
		name    string
		kind    string
		want    string
		wantErr bool
	}{
		{name: "explicit chat_completions", kind: "chat_completions", want: "/v1/chat/completions"},
		{name: "explicit responses", kind: "responses", want: "/v1/responses"},
		{name: "explicit messages", kind: "messages", want: "/v1/messages"},
		{name: "empty defaults to chat_completions", kind: "", want: "/v1/chat/completions"},
		{name: "whitespace defaults to chat_completions", kind: "   ", want: "/v1/chat/completions"},
		{name: "invalid kind", kind: "chat", wantErr: true},
		{name: "invalid kind unknown", kind: "unknown", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := conversationRequestPathForKind(tc.kind)
			if tc.wantErr {
				require.Error(t, err)
				assert.Empty(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
			// normalizeConversationKind 与 path 解析保持一致
			if tc.kind == "" || strings.TrimSpace(tc.kind) == "" {
				assert.Equal(t, "chat_completions", normalizeConversationKind(tc.kind))
			}
		})
	}
}

func TestParseLogStatsTimeRange_DefaultsWhenAllAbsent(t *testing.T) {
	ctx, _ := newQueryContext(t, "/api/ip_stats/conversation/rank")
	startTs, endTs, rangeDays, err := parseLogStatsTimeRange(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, rangeDays)
	assert.Greater(t, endTs, int64(0))
	// end=now, start=end-86400
	assert.Equal(t, endTs-startTs, int64(86400))
}

func TestParseLogStatsTimeRange_RangeDaysFromQuery(t *testing.T) {
	ctx, _ := newQueryContext(t, "/api/ip_stats/conversation/rank?range_days=3")
	startTs, endTs, rangeDays, err := parseLogStatsTimeRange(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 3, rangeDays)
	assert.Equal(t, endTs-startTs, int64(3*86400))
}

func TestParseLogStatsTimeRange_InvalidRangeDaysZero(t *testing.T) {
	ctx, _ := newQueryContext(t, "/api/ip_stats/conversation/rank?range_days=0")
	_, _, _, err := parseLogStatsTimeRange(ctx, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "range_days")
}

func TestParseLogStatsTimeRange_InvalidRangeDaysNonInteger(t *testing.T) {
	ctx, _ := newQueryContext(t, "/api/ip_stats/conversation/rank?range_days=abc")
	_, _, _, err := parseLogStatsTimeRange(ctx, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "range_days")
}

func TestParseLogStatsTimeRange_NegativeRangeDays(t *testing.T) {
	ctx, _ := newQueryContext(t, "/api/ip_stats/conversation/rank?range_days=-5")
	_, _, _, err := parseLogStatsTimeRange(ctx, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "range_days")
}

func TestParseLogStatsTimeRange_BothTimestampsProvided(t *testing.T) {
	ctx, _ := newQueryContext(t, "/api/ip_stats/conversation/rank?start_timestamp=1000&end_timestamp=5000&range_days=2")
	startTs, endTs, rangeDays, err := parseLogStatsTimeRange(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, int64(1000), startTs)
	assert.Equal(t, int64(5000), endTs)
	// range_days 仍按查询参数回显
	assert.Equal(t, 2, rangeDays)
}

func TestParseLogStatsTimeRange_OnlyStartProvided(t *testing.T) {
	ctx, _ := newQueryContext(t, "/api/ip_stats/conversation/rank?start_timestamp=1000&range_days=2")
	startTs, endTs, rangeDays, err := parseLogStatsTimeRange(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, int64(1000), startTs)
	assert.Equal(t, int64(1000)+int64(2*86400), endTs)
	assert.Equal(t, 2, rangeDays)
}

func TestParseLogStatsTimeRange_OnlyEndProvided(t *testing.T) {
	ctx, _ := newQueryContext(t, "/api/ip_stats/conversation/rank?end_timestamp=5000&range_days=2")
	startTs, endTs, rangeDays, err := parseLogStatsTimeRange(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, int64(5000), endTs)
	assert.Equal(t, int64(5000)-int64(2*86400), startTs)
	assert.Equal(t, 2, rangeDays)
}

func TestParseLogStatsTimeRange_DefaultRangeDaysWhenQueryAbsent(t *testing.T) {
	ctx, _ := newQueryContext(t, "/api/ip_stats/conversation/rank?end_timestamp=5000")
	_, _, rangeDays, err := parseLogStatsTimeRange(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, 7, rangeDays)
}

func TestParseLogStatsTimeRange_MalformedTimestampsTreatedAsAbsent(t *testing.T) {
	// 时间戳解析失败按 0 处理，与现有 log 控制器一致；窗口合法性交给 model 校验。
	ctx, _ := newQueryContext(t, "/api/ip_stats/conversation/rank?start_timestamp=notanumber&range_days=2")
	startTs, endTs, _, err := parseLogStatsTimeRange(ctx, 1)
	require.NoError(t, err)
	// start 解析失败 -> 视为未提供 -> 走"only end"分支但 end 也未提供 -> 走默认分支
	assert.Equal(t, endTs-startTs, int64(2*86400))
}

func TestParsePositiveIntQuery_DefaultWhenAbsent(t *testing.T) {
	ctx, _ := newQueryContext(t, "/api/ip_stats/conversation/rank")
	got, err := parsePositiveIntQuery(ctx, "limit", 100)
	require.NoError(t, err)
	assert.Equal(t, 100, got)
}

func TestParsePositiveIntQuery_Value(t *testing.T) {
	ctx, _ := newQueryContext(t, "/api/ip_stats/conversation/rank?limit=50")
	got, err := parsePositiveIntQuery(ctx, "limit", 100)
	require.NoError(t, err)
	assert.Equal(t, 50, got)
}

func TestParsePositiveIntQuery_NonInteger(t *testing.T) {
	ctx, _ := newQueryContext(t, "/api/ip_stats/conversation/rank?limit=abc")
	_, err := parsePositiveIntQuery(ctx, "limit", 100)
	require.Error(t, err)
}

func TestParsePositiveIntQuery_NonPositive(t *testing.T) {
	ctx, _ := newQueryContext(t, "/api/ip_stats/conversation/rank?limit=0")
	_, err := parsePositiveIntQuery(ctx, "limit", 100)
	require.Error(t, err)
}

func TestParseLimitQuery_DefaultWhenAbsent(t *testing.T) {
	ctx, _ := newQueryContext(t, "/api/ip_stats/conversation/rank")
	assert.Equal(t, 100, parseLimitQuery(ctx, 100))
}

func TestParseLimitQuery_Value(t *testing.T) {
	ctx, _ := newQueryContext(t, "/api/ip_stats/conversation/rank?limit=50")
	assert.Equal(t, 50, parseLimitQuery(ctx, 100))
}

func TestParseLimitQuery_MalformedFallsBackToDefault(t *testing.T) {
	// limit 解析失败回退到默认值；交给 model 层 clamp，不在 controller 报错。
	ctx, _ := newQueryContext(t, "/api/ip_stats/conversation/rank?limit=abc")
	assert.Equal(t, 100, parseLimitQuery(ctx, 100))
}

func TestParseLimitQuery_ZeroPassedThroughForModelClamp(t *testing.T) {
	// limit<=0 不在 controller 报错，交给 model.clampRankLimit 处理（0 -> 100）。
	ctx, _ := newQueryContext(t, "/api/ip_stats/conversation/rank?limit=0")
	assert.Equal(t, 0, parseLimitQuery(ctx, 100))
}

func TestParseLeaderboardMetric(t *testing.T) {
	cases := []struct {
		name    string
		metric  string
		want    string
		wantErr bool
	}{
		{name: "empty defaults to calls", metric: "", want: "calls"},
		{name: "calls", metric: "calls", want: "calls"},
		{name: "quota", metric: "quota", want: "quota"},
		{name: "rph", metric: "rph", want: "rph"},
		{name: "invalid", metric: "invalid", wantErr: true},
		{name: "empty after trim defaults to calls", metric: "  ", want: "calls"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseLeaderboardMetric(tc.metric)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
		})
	}
}
