// Package events is the durable-event log (Kafka in production, memory here).
// Hot-path packets never go through this log. Kills, purchases, chat, and
// admin actions do.
package events

import (
	"encoding/json"
	"sync"
	"time"
)

// Event is one domain fact.
type Event struct {
	Topic   string          `json:"topic"`
	Key     string          `json:"key"`
	Payload json.RawMessage `json:"payload"`
	At      time.Time       `json:"at"`
}

// Handler is invoked asynchronously so a subscriber can call back into a shard
// without deadlocking the publisher.
type Handler func(Event)

// Log is an append-only topic log.
type Log struct {
	mu   sync.Mutex
	recs []Event
	subs map[string][]Handler
}

func NewLog() *Log {
	return &Log{subs: map[string][]Handler{}}
}

// Append stores the event and fans it out. payload is JSON-encoded.
func (l *Log) Append(topic, key string, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	ev := Event{Topic: topic, Key: key, Payload: raw, At: time.Now().UTC()}
	l.mu.Lock()
	l.recs = append(l.recs, ev)
	var hs []Handler
	hs = append(hs, l.subs[topic]...)
	hs = append(hs, l.subs[">"]...)
	l.mu.Unlock()
	for _, h := range hs {
		h := h
		ev := ev
		go h(ev)
	}
}

// Subscribe registers a handler for one topic. Use ">" for every topic.
func (l *Log) Subscribe(topic string, h Handler) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.subs[topic] = append(l.subs[topic], h)
}

// Records returns a copy of the log, oldest first.
func (l *Log) Records() []Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Event, len(l.recs))
	copy(out, l.recs)
	return out
}
