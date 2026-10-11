package social

import (
	"fmt"
	"sync"

	"github.com/xenomit/server/pkg/events"
)

// Clan is one corporation.
type Clan struct {
	ID        int              `json:"id"`
	Name      string           `json:"name"`
	Tag       string           `json:"tag"`
	Leader    int64            `json:"leader"`
	Members   map[int64]string `json:"members"`
	Diplomacy map[int]string   `json:"diplomacy"`
}

// Group is a temporary squad.
type Group struct {
	ID      int             `json:"id"`
	Leader  int64           `json:"leader"`
	Members map[int64]bool  `json:"members"`
	Invites map[int64]int64 `json:"invites"`
}

// Service owns clans, groups, and diplomacy.
type Service struct {
	mu     sync.Mutex
	clans  map[int]*Clan
	groups map[int]*Group
	byUser map[int64]int
	nextC  int
	nextG  int
	log    *events.Log
}

func New(log *events.Log) *Service {
	return &Service{
		clans:  map[int]*Clan{},
		groups: map[int]*Group{},
		byUser: map[int64]int{},
		log:    log,
	}
}

func (s *Service) CreateClan(leader int64, name, tag string) (*Clan, error) {
	if name == "" || tag == "" {
		return nil, fmt.Errorf("name and tag are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byUser[leader]; ok {
		return nil, fmt.Errorf("already in a clan")
	}
	s.nextC++
	c := &Clan{
		ID: s.nextC, Name: name, Tag: tag, Leader: leader,
		Members:   map[int64]string{leader: "leader"},
		Diplomacy: map[int]string{},
	}
	s.clans[c.ID] = c
	s.byUser[leader] = c.ID
	s.emit("clan.created", c.ID)
	return c, nil
}

func (s *Service) Join(userID int64, clanID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.clans[clanID]
	if !ok {
		return fmt.Errorf("clan %d not found", clanID)
	}
	if _, ok := s.byUser[userID]; ok {
		return fmt.Errorf("already in a clan")
	}
	c.Members[userID] = "member"
	s.byUser[userID] = clanID
	s.emit("clan.changed", clanID)
	return nil
}

func (s *Service) Leave(userID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.byUser[userID]
	if !ok {
		return fmt.Errorf("not in a clan")
	}
	c := s.clans[id]
	delete(c.Members, userID)
	delete(s.byUser, userID)
	if len(c.Members) == 0 {
		delete(s.clans, id)
	} else if c.Leader == userID {
		for m := range c.Members {
			c.Leader = m
			c.Members[m] = "leader"
			break
		}
	}
	s.emit("clan.changed", id)
	return nil
}

func (s *Service) Diplomacy(clanID, other int, status string) error {
	switch status {
	case "war", "nap", "alliance", "none":
	default:
		return fmt.Errorf("unknown diplomacy %s", status)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.clans[clanID]
	if !ok {
		return fmt.Errorf("clan %d not found", clanID)
	}
	if status == "none" {
		delete(c.Diplomacy, other)
	} else {
		c.Diplomacy[other] = status
	}
	s.emit("clan.diplomacy", clanID)
	return nil
}

func (s *Service) ClanOf(userID int64) (Clan, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.byUser[userID]
	if !ok {
		return Clan{}, false
	}
	c := s.clans[id]
	cp := *c
	return cp, true
}

func (s *Service) InviteGroup(leader, target int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.groupOfLocked(leader)
	if g == nil {
		s.nextG++
		g = &Group{ID: s.nextG, Leader: leader, Members: map[int64]bool{leader: true}, Invites: map[int64]int64{}}
		s.groups[g.ID] = g
	}
	if g.Leader != leader {
		return 0, fmt.Errorf("only the leader can invite")
	}
	g.Invites[target] = leader
	return g.ID, nil
}

func (s *Service) AcceptGroup(userID int64, groupID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.groups[groupID]
	if !ok {
		return fmt.Errorf("group not found")
	}
	if _, ok := g.Invites[userID]; !ok {
		return fmt.Errorf("no invite")
	}
	delete(g.Invites, userID)
	g.Members[userID] = true
	return nil
}

func (s *Service) LeaveGroup(userID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, g := range s.groups {
		if !g.Members[userID] {
			continue
		}
		delete(g.Members, userID)
		if len(g.Members) == 0 {
			delete(s.groups, id)
			continue
		}
		if g.Leader == userID {
			for m := range g.Members {
				g.Leader = m
				break
			}
		}
	}
}

func (s *Service) groupOfLocked(userID int64) *Group {
	for _, g := range s.groups {
		if g.Members[userID] {
			return g
		}
	}
	return nil
}

func (s *Service) emit(topic string, clanID int) {
	if s.log != nil {
		s.log.Append(topic, fmt.Sprint(clanID), map[string]any{"clanId": clanID})
	}
}
