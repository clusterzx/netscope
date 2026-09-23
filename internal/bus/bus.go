// Package bus is the in-process event bus. It fans out messages to subscribers
// (SSE clients, metrics, internal consumers). Delivery is best effort: a subscriber whose
// buffer is full misses messages instead of blocking publishers. Consumers that must not
// lose data (rule engine, processors) are fed through dedicated queues instead.
package bus

import (
	"sync"
	"sync/atomic"
	"time"
)

// Topics.
const (
	TopicRun          = "run"          // run queued/started/progress/finished
	TopicRunLog       = "run.log"      // log line of a run
	TopicDevice       = "device"       // device created/updated/online/offline
	TopicEvent        = "event"        // event created/acknowledged
	TopicPlugin       = "plugin"       // plugin config changed
	TopicNotification = "notification" // notification sent/failed
	TopicHealth       = "health"       // health check state changed
	TopicSystem       = "system"       // system messages (backup, nvd sync ...)
	TopicLog          = "log"          // application log line (log viewer)
)

// Message is one bus message.
type Message struct {
	ID    uint64    `json:"id"`
	Topic string    `json:"topic"`
	Type  string    `json:"type"`
	At    time.Time `json:"at"`
	Data  any       `json:"data"`
}

// Subscription receives messages.
type Subscription struct {
	C      <-chan Message
	ch     chan Message
	topics map[string]bool
	bus    *Bus
	once   sync.Once
	drops  atomic.Uint64
}

// Close unsubscribes.
func (s *Subscription) Close() {
	s.once.Do(func() {
		s.bus.mu.Lock()
		delete(s.bus.subs, s)
		s.bus.mu.Unlock()
		close(s.ch)
	})
}

// Dropped returns the number of messages lost because the buffer was full.
func (s *Subscription) Dropped() uint64 { return s.drops.Load() }

// Bus is a topic based fan-out.
type Bus struct {
	mu   sync.RWMutex
	subs map[*Subscription]struct{}
	seq  atomic.Uint64
}

// New creates a bus.
func New() *Bus { return &Bus{subs: map[*Subscription]struct{}{}} }

// Subscribe returns a subscription for the given topics (none = all topics).
func (b *Bus) Subscribe(buffer int, topics ...string) *Subscription {
	if buffer < 1 {
		buffer = 64
	}
	ch := make(chan Message, buffer)
	s := &Subscription{C: ch, ch: ch, bus: b}
	if len(topics) > 0 {
		s.topics = map[string]bool{}
		for _, t := range topics {
			s.topics[t] = true
		}
	}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	return s
}

// Publish sends a message to all matching subscribers without blocking.
func (b *Bus) Publish(topic, typ string, data any) {
	msg := Message{ID: b.seq.Add(1), Topic: topic, Type: typ, At: time.Now(), Data: data}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for s := range b.subs {
		if s.topics != nil && !s.topics[topic] {
			continue
		}
		select {
		case s.ch <- msg:
		default:
			s.drops.Add(1)
		}
	}
}

// Subscribers returns the number of active subscriptions.
func (b *Bus) Subscribers() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}
