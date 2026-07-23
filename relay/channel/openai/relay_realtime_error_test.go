package openai

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/governance"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func newWebsocketPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	serverConnCh := make(chan *websocket.Conn, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{CheckOrigin: func(_ *http.Request) bool { return true }}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serverConnCh <- conn
		<-release
	}))

	peer, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	serverConn := <-serverConnCh

	t.Cleanup(func() {
		_ = peer.Close()
		_ = serverConn.Close()
		close(release)
		server.Close()
	})
	return serverConn, peer
}

func TestOpenaiRealtimeFirstErrorRemainsRetryable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handlerClient, clientPeer := newWebsocketPair(t)
	upstreamPeer, handlerTarget := newWebsocketPair(t)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	c.Set(common.RequestIdKey, "local-realtime-request")
	info := &relaycommon.RelayInfo{
		ClientWs:    handlerClient,
		TargetWs:    handlerTarget,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "realtime-test"},
	}

	result := make(chan *types.NewAPIError, 1)
	go func() {
		err, _ := OpenaiRealtimeHandler(c, info)
		result <- err
	}()

	rawEvent := `{"type":"error","event_id":"upstream-private-id","error":{"type":"server_error","code":"upstream_failed","message":"raw-realtime-secret https://private.example sk-upstream"}}`
	require.NoError(t, upstreamPeer.WriteMessage(websocket.TextMessage, []byte(rawEvent)))
	var relayErr *types.NewAPIError
	select {
	case relayErr = <-result:
	case <-time.After(5 * time.Second):
		t.Fatal("realtime handler did not stop after upstream error event")
	}

	require.NotNil(t, relayErr)
	require.Contains(t, relayErr.ToOpenAIError().Message, "raw-realtime-secret")
	require.False(t, helper.HasStreamResponseStarted(c))
	require.Nil(t, governance.HandledStreamError(c))
	require.NoError(t, clientPeer.SetReadDeadline(time.Now().Add(100*time.Millisecond)))
	_, _, readErr := clientPeer.ReadMessage()
	require.Error(t, readErr, "a retryable first error must not be committed to the client")
}

func TestOpenaiRealtimeCloseBeforeFirstEventRemainsRetryable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handlerClient, clientPeer := newWebsocketPair(t)
	upstreamPeer, handlerTarget := newWebsocketPair(t)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	info := &relaycommon.RelayInfo{
		ClientWs:    handlerClient,
		TargetWs:    handlerTarget,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "realtime-test"},
	}

	result := make(chan *types.NewAPIError, 1)
	go func() {
		err, _ := OpenaiRealtimeHandler(c, info)
		result <- err
	}()

	require.NoError(t, upstreamPeer.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, "closed before response"),
		time.Now().Add(time.Second),
	))

	select {
	case relayErr := <-result:
		require.NotNil(t, relayErr)
		require.Contains(t, relayErr.ToOpenAIError().Message, "closed before the first response event")
	case <-time.After(5 * time.Second):
		t.Fatal("realtime handler did not return after upstream close")
	}
	require.False(t, helper.HasStreamResponseStarted(c))
	require.Nil(t, governance.HandledStreamError(c))
	require.NoError(t, clientPeer.SetReadDeadline(time.Now().Add(100*time.Millisecond)))
	_, _, readErr := clientPeer.ReadMessage()
	require.Error(t, readErr, "a retryable pre-response close must not be committed to the client")
}

func TestOpenaiRealtimeSanitizesErrorAfterUpstreamOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handlerClient, clientPeer := newWebsocketPair(t)
	upstreamPeer, handlerTarget := newWebsocketPair(t)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	c.Set(common.RequestIdKey, "local-realtime-request")
	info := &relaycommon.RelayInfo{
		ClientWs:    handlerClient,
		TargetWs:    handlerTarget,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "realtime-test"},
	}

	result := make(chan *types.NewAPIError, 1)
	go func() {
		err, _ := OpenaiRealtimeHandler(c, info)
		result <- err
	}()

	require.NoError(t, upstreamPeer.WriteMessage(websocket.TextMessage, []byte(`{"type":"session.created","session":{}}`)))
	require.NoError(t, clientPeer.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, firstMessage, err := clientPeer.ReadMessage()
	require.NoError(t, err)
	require.Contains(t, string(firstMessage), "session.created")

	rawEvent := `{"type":"response.done","response":{"status":"failed","status_details":{"error":{"type":"server_error","code":"upstream_failed","message":"raw-realtime-secret https://private.example sk-upstream"}}}}`
	require.NoError(t, upstreamPeer.WriteMessage(websocket.TextMessage, []byte(rawEvent)))
	_, clientMessage, err := clientPeer.ReadMessage()
	require.NoError(t, err)

	select {
	case relayErr := <-result:
		require.Nil(t, relayErr)
	case <-time.After(5 * time.Second):
		t.Fatal("realtime handler did not stop after upstream error event")
	}

	clientBody := string(clientMessage)
	require.Contains(t, clientBody, `"type":"error"`)
	require.Contains(t, clientBody, "local-realtime-request")
	require.NotContains(t, clientBody, "raw-realtime-secret")
	require.NotContains(t, clientBody, "private.example")
	require.NotContains(t, clientBody, "sk-upstream")
	require.NotNil(t, governance.HandledStreamError(c))
}

func TestOpenaiRealtimeRetryReplaysClientEventsWithSingleReader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handlerClient, clientPeer := newWebsocketPair(t)
	firstUpstreamPeer, firstHandlerTarget := newWebsocketPair(t)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	info := &relaycommon.RelayInfo{
		ClientWs:    handlerClient,
		TargetWs:    firstHandlerTarget,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "realtime-test"},
	}

	firstResult := make(chan *types.NewAPIError, 1)
	go func() {
		err, _ := OpenaiRealtimeHandler(c, info)
		firstResult <- err
	}()

	clientEvent := `{"type":"session.update","session":{"tools":[]}}`
	require.NoError(t, clientPeer.WriteMessage(websocket.TextMessage, []byte(clientEvent)))
	require.NoError(t, firstUpstreamPeer.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, firstForwarded, err := firstUpstreamPeer.ReadMessage()
	require.NoError(t, err)
	require.JSONEq(t, clientEvent, string(firstForwarded))
	require.NoError(t, firstUpstreamPeer.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","error":{"message":"first channel failed"}}`)))
	select {
	case relayErr := <-firstResult:
		require.NotNil(t, relayErr)
	case <-time.After(5 * time.Second):
		t.Fatal("first realtime attempt did not return")
	}

	secondUpstreamPeer, secondHandlerTarget := newWebsocketPair(t)
	info.TargetWs = secondHandlerTarget
	secondResult := make(chan *types.NewAPIError, 1)
	go func() {
		err, _ := OpenaiRealtimeHandler(c, info)
		secondResult <- err
	}()

	require.NoError(t, secondUpstreamPeer.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, replayed, err := secondUpstreamPeer.ReadMessage()
	require.NoError(t, err)
	require.JSONEq(t, clientEvent, string(replayed))
	require.NoError(t, secondUpstreamPeer.WriteMessage(websocket.TextMessage, []byte(`{"type":"session.created","session":{}}`)))
	require.NoError(t, clientPeer.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, clientMessage, err := clientPeer.ReadMessage()
	require.NoError(t, err)
	require.Contains(t, string(clientMessage), "session.created")
	require.NoError(t, secondUpstreamPeer.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"), time.Now().Add(time.Second)))
	select {
	case relayErr := <-secondResult:
		require.Nil(t, relayErr)
	case <-time.After(5 * time.Second):
		t.Fatal("second realtime attempt did not stop")
	}
}
