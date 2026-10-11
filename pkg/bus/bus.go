// Package bus is an in-process subject bus with NATS-style wildcards.
// Hot-path subjects match the architecture plan: in.<mapId>, out.<sessionId>,
// out.map.<id>, out.all. A NATS core connection can implement the same Bus.
package bus

import (
	"strings"
	"sync"
)

// Handler receives a published payload. Implementations must not retain data.
type Handler func(subject string, data []byte)

// Bus is the publish/subscribe edge between gateway, world, and chat.
type Bus interface {
	Publish(subject string, data []byte)
	Subscribe(pattern string, h Handler) (unsubscribe func())
}

// Memory delivers publishes synchronously to matching subscribers.
// Handlers run without the bus lock held.
type Memory struct {
	mu   sync.Mutex
	next int
	subs map[int]sub
}

type sub struct {
	pattern string
	h       Handler
}

func New() *Memory {
	return &Memory{subs: map[int]sub{}}
}

func (m *Memory) Publish(subject string, data []byte) {
	m.mu.Lock()
	var hs []Handler
	for _, s := range m.subs {
		if match(s.pattern, subject) {
			hs = append(hs, s.h)
		}
	}
	m.mu.Unlock()
	payload := append([]byte(nil), data...)
	for _, h := range hs {
		h(subject, payload)
	}
}

func (m *Memory) Subscribe(pattern string, h Handler) (unsubscribe func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	id := m.next
	m.subs[id] = sub{pattern: pattern, h: h}
	return func() {
		m.mu.Lock()
		delete(m.subs, id)
		m.mu.Unlock()
	}
}

// match supports "*" (one token) and ">" (the rest of the subject).
func match(pattern, subject string) bool {
	if pattern == subject || pattern == ">" {
		return true
	}
	pp := strings.Split(pattern, ".")
	ss := strings.Split(subject, ".")
	for i, p := range pp {
		if p == ">" {
			return true
		}
		if i >= len(ss) {
			return false
		}
		if p == "*" {
			continue
		}
		if p != ss[i] {
			return false
		}
	}
	return len(pp) == len(ss)
}
