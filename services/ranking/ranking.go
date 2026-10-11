package ranking

import (
	"encoding/json"
	"sort"
	"sync"

	"github.com/xenomit/server/pkg/events"
)

// Row is one leaderboard entry.
type Row struct {
	UserID int64   `json:"userId"`
	Name   string  `json:"name"`
	Honor  float64 `json:"honor"`
	Kills  int     `json:"kills"`
}

type killEvent struct {
	KillerID int64   `json:"killerId"`
	Name     string  `json:"name"`
	Honor    float64 `json:"honor"`
}

// Service folds combat.kill into an in-memory ZSET-style board.
type Service struct {
	mu   sync.Mutex
	rows map[int64]*Row
}

func New(log *events.Log) *Service {
	s := &Service{rows: map[int64]*Row{}}
	if log != nil {
		log.Subscribe("combat.kill", func(ev events.Event) {
			var k killEvent
			if err := decode(ev, &k); err != nil || k.KillerID == 0 {
				return
			}
			s.add(k)
		})
	}
	return s
}

func decode(ev events.Event, dst any) error {
	return json.Unmarshal(ev.Payload, dst)
}

func (s *Service) add(k killEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.rows[k.KillerID]
	if row == nil {
		row = &Row{UserID: k.KillerID, Name: k.Name}
		s.rows[k.KillerID] = row
	}
	if k.Name != "" {
		row.Name = k.Name
	}
	row.Kills++
	row.Honor += k.Honor
}

// Top returns the board, highest honor first.
func (s *Service) Top(n int) []Row {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Row, 0, len(s.rows))
	for _, r := range s.rows {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Honor == out[j].Honor {
			return out[i].Kills > out[j].Kills
		}
		return out[i].Honor > out[j].Honor
	})
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}
