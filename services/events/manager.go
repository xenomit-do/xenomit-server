// Package eventsvc runs scheduled instances: Jackpot Battle, Spaceball, UBA, and duels.
package eventsvc

import (
	"fmt"
	"sync"
	"time"

	"github.com/xenomit/server/pkg/events"
	"github.com/xenomit/server/pkg/model"
)

// Presence is a pilot the world is routing.
type Presence struct {
	UserID  int64
	MapID   int
	Faction int
}

// World is the slice of the world controller these events need.
type World interface {
	Transfer(userID int64, mapID int, x, y int32) error
	BroadcastAll(text string)
	BroadcastLegacy(message string)
	Alive(mapID int) []int64
	Online() []Presence
	SetRadiation(mapID int, cx, cy, radius int32)
	ClearRadiation(mapID int)
	ShrinkRadiation(mapID int, by int32) int32
	SetPeace(mapID int, until time.Time)
	SpawnBall(mapID int, x, y int32)
	TickSpaceball(mapID int) (mmo, eic, vru int, done bool)
}

// Ledger commits rewards.
type Ledger interface {
	Apply(tx model.Tx) error
}

// Manager is the events-service scheduler.
type Manager struct {
	world     World
	ledger    Ledger
	log       *events.Log
	Countdown time.Duration
	mu        sync.Mutex
	running   map[string]bool
	uba       []int64
}

func New(world World, ledger Ledger, log *events.Log) *Manager {
	return &Manager{
		world: world, ledger: ledger, log: log,
		Countdown: 120 * time.Second,
		running:   map[string]bool{},
	}
}

// Start launches a named event. jpb, spaceball, and uba are supported.
func (m *Manager) Start(name string) error {
	switch name {
	case "jpb":
		go m.jackpot()
	case "spaceball":
		go m.spaceball()
	case "uba":
		m.world.BroadcastAll("Ultimate Battle Arena queue is open. Type /uba in chat.")
	default:
		return fmt.Errorf("unknown event %s", name)
	}
	return nil
}

func (m *Manager) begin(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running[name] {
		return false
	}
	m.running[name] = true
	return true
}

func (m *Manager) end(name string) {
	m.mu.Lock()
	delete(m.running, name)
	m.mu.Unlock()
}

func (m *Manager) jackpot() {
	if !m.begin("jpb") {
		return
	}
	defer m.end("jpb")
	seconds := int(m.Countdown / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	for i := seconds; i > 0; i-- {
		m.world.BroadcastAll(fmt.Sprintf("Jackpot Battle starts in %d", i))
		time.Sleep(time.Second)
	}
	for _, p := range m.world.Online() {
		x, y := jpbSpawn(p.Faction)
		_ = m.world.Transfer(p.UserID, 42, x, y)
	}
	m.world.SetRadiation(42, 10400, 6400, 8000)
	var winner int64
	for step := 0; step < 90; step++ {
		time.Sleep(2 * time.Second)
		m.world.ShrinkRadiation(42, 100)
		alive := m.world.Alive(42)
		if len(alive) <= 1 {
			if len(alive) == 1 {
				winner = alive[0]
			}
			break
		}
	}
	m.world.ClearRadiation(42)
	if winner != 0 && m.ledger != nil {
		_ = m.ledger.Apply(model.Tx{
			ID: fmt.Sprintf("jpb-%d", time.Now().UnixNano()), UserID: winner, Reason: "jpb",
			Uridium: 25000, Honor: 25000, Experience: 2_500_000,
		})
		m.world.BroadcastAll("Jackpot Battle has a winner.")
		_ = m.world.Transfer(winner, 1, 1000, 1000)
	}
	if m.log != nil {
		m.log.Append("event.jpb", "jpb", map[string]any{"winner": winner})
	}
}

func jpbSpawn(faction int) (int32, int32) {
	switch faction {
	case 2:
		return 18000, 2000
	case 3:
		return 10000, 10000
	default:
		return 2000, 2000
	}
}

func (m *Manager) spaceball() {
	if !m.begin("spaceball") {
		return
	}
	defer m.end("spaceball")
	m.world.BroadcastAll("Spaceball event started!")
	m.world.SpawnBall(16, 10400, 6400)
	for i := 0; i < 120; i++ {
		time.Sleep(time.Second)
		mmo, eic, vru, done := m.world.TickSpaceball(16)
		m.world.BroadcastLegacy(fmt.Sprintf("0|n|ssi|%d|%d|%d", mmo, eic, vru))
		if done {
			m.world.BroadcastAll("Spaceball event ended.")
			break
		}
	}
}

// StartDuel teleports two pilots to map 101 after a peace countdown.
func (m *Manager) StartDuel(a, b int64) error {
	if err := m.world.Transfer(a, 101, 5000, 5000); err != nil {
		return err
	}
	if err := m.world.Transfer(b, 101, 7000, 5000); err != nil {
		return err
	}
	m.world.SetPeace(101, time.Now().Add(25*time.Second))
	m.world.BroadcastAll("A duel has started.")
	go m.watch(101)
	return nil
}

// EnqueueUBA pairs waiting pilots onto map 121.
func (m *Manager) EnqueueUBA(userID int64) {
	m.mu.Lock()
	m.uba = append(m.uba, userID)
	if len(m.uba) < 2 {
		m.mu.Unlock()
		return
	}
	a, b := m.uba[0], m.uba[1]
	m.uba = m.uba[2:]
	m.mu.Unlock()
	_ = m.world.Transfer(a, 121, 5000, 5000)
	_ = m.world.Transfer(b, 121, 7000, 5000)
	m.world.SetPeace(121, time.Now().Add(10*time.Second))
	go m.watch(121)
}

func (m *Manager) watch(mapID int) {
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(time.Second)
		alive := m.world.Alive(mapID)
		if len(alive) <= 1 {
			for _, id := range alive {
				_ = m.world.Transfer(id, 1, 1000, 1000)
			}
			m.world.BroadcastAll("The match is over.")
			return
		}
	}
}
