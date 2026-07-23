package openai

import (
	"fmt"
	"sync"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const realtimeClientBrokerContextKey = "openai_realtime_client_broker"

// realtimeClientBroker is request-scoped. It is the only goroutine allowed to
// read the downstream websocket, so a first-frame upstream failure can safely
// retry another channel without creating competing client readers. Messages
// are retained until the first upstream event is delivered and replayed to a
// replacement channel attempt in their original order.
type realtimeClientBroker struct {
	ctx  *gin.Context
	conn *websocket.Conn

	mu        sync.Mutex
	history   [][]byte
	active    *realtimeClientSubscription
	committed bool
	err       error
	done      chan struct{}
	doneOnce  sync.Once
}

type realtimeClientSubscription struct {
	messages chan []byte
	done     chan struct{}
	once     sync.Once
}

func getRealtimeClientBroker(c *gin.Context, conn *websocket.Conn) *realtimeClientBroker {
	if existing, ok := c.Get(realtimeClientBrokerContextKey); ok {
		if broker, valid := existing.(*realtimeClientBroker); valid && broker != nil {
			return broker
		}
	}
	broker := &realtimeClientBroker{
		ctx:  c,
		conn: conn,
		done: make(chan struct{}),
	}
	c.Set(realtimeClientBrokerContextKey, broker)
	gopool.Go(broker.readLoop)
	return broker
}

func (b *realtimeClientBroker) readLoop() {
	for {
		_, message, err := b.conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				b.finish(nil)
			} else {
				b.finish(fmt.Errorf("error reading from realtime client: %w", err))
			}
			return
		}

		messageCopy := append([]byte(nil), message...)
		b.mu.Lock()
		if !b.committed {
			b.history = append(b.history, append([]byte(nil), messageCopy...))
		}
		subscription := b.active
		b.mu.Unlock()

		if subscription == nil {
			continue
		}
		select {
		case subscription.messages <- messageCopy:
		case <-subscription.done:
		case <-b.done:
			return
		case <-b.ctx.Done():
			b.finish(b.ctx.Err())
			return
		}
	}
}

func (b *realtimeClientBroker) subscribe() (*realtimeClientSubscription, [][]byte) {
	subscription := &realtimeClientSubscription{
		messages: make(chan []byte, 256),
		done:     make(chan struct{}),
	}
	b.mu.Lock()
	b.active = subscription
	replay := make([][]byte, len(b.history))
	for index := range b.history {
		replay[index] = append([]byte(nil), b.history[index]...)
	}
	b.mu.Unlock()
	return subscription, replay
}

func (b *realtimeClientBroker) unsubscribe(subscription *realtimeClientSubscription) {
	if subscription == nil {
		return
	}
	subscription.once.Do(func() { close(subscription.done) })
	b.mu.Lock()
	if b.active == subscription {
		b.active = nil
	}
	b.mu.Unlock()
}

func (b *realtimeClientBroker) commit() {
	b.mu.Lock()
	b.committed = true
	b.history = nil
	b.mu.Unlock()
}

func (b *realtimeClientBroker) Done() <-chan struct{} {
	return b.done
}

func (b *realtimeClientBroker) Err() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.err
}

func (b *realtimeClientBroker) finish(err error) {
	b.doneOnce.Do(func() {
		b.mu.Lock()
		b.err = err
		b.mu.Unlock()
		close(b.done)
	})
}
