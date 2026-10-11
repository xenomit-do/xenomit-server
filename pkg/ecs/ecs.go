// Package ecs is a small component store. One goroutine owns a World;
// that is the single-writer rule from the architecture plan.
package ecs

// Entity is an opaque id local to one World.
type Entity uint32

// World allocates entity ids and holds component stores.
type World struct {
	next Entity
	live map[Entity]struct{}
}

func NewWorld() *World {
	return &World{live: map[Entity]struct{}{}}
}

func (w *World) Create() Entity {
	w.next++
	w.live[w.next] = struct{}{}
	return w.next
}

func (w *World) Alive(id Entity) bool {
	_, ok := w.live[id]
	return ok
}

func (w *World) Destroy(id Entity) { delete(w.live, id) }

func (w *World) Count() int { return len(w.live) }

// Store is a typed component map. Values are copied on Get/Set.
type Store[T any] struct {
	data map[Entity]T
}

func NewStore[T any]() *Store[T] {
	return &Store[T]{data: map[Entity]T{}}
}

func (s *Store[T]) Set(id Entity, v T) { s.data[id] = v }

func (s *Store[T]) Get(id Entity) (T, bool) {
	v, ok := s.data[id]
	return v, ok
}

func (s *Store[T]) Delete(id Entity) { delete(s.data, id) }

func (s *Store[T]) Len() int { return len(s.data) }

// IDs returns a snapshot so callers can delete while iterating.
func (s *Store[T]) IDs() []Entity {
	out := make([]Entity, 0, len(s.data))
	for id := range s.data {
		out = append(out, id)
	}
	return out
}
