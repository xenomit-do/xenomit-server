package world

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/xenomit/server/pkg/bus"
	"github.com/xenomit/server/pkg/events"
	"github.com/xenomit/server/pkg/model"
	"github.com/xenomit/server/pkg/observability"
)

// Presence is one online pilot.
type Presence struct {
	UserID  int64  `json:"userId"`
	MapID   int    `json:"mapId"`
	Faction int    `json:"faction"`
	Session string `json:"sessionId"`
}

// Persistence is the player-service write path the shards call.
type Persistence interface {
	SaveState(model.Snapshot)
	ApplyTx(model.Tx) error
}

// Deps wires a controller to the process.
type Deps struct {
	Bus       bus.Bus
	Log       *events.Log
	Save      Persistence
	Obs       *observability.Obs
	GrantItem func(userID int64, item string) error
	Refresh   func(userID int64) (model.Snapshot, error)
}

// Controller owns every map shard and the cross-shard handoff.
type Controller struct {
	bus       bus.Bus
	log       *events.Log
	save      Persistence
	obs       *observability.Obs
	grantItem func(userID int64, item string) error
	refresh   func(userID int64) (model.Snapshot, error)
	shards    map[int]*Shard
	mu        chan struct{}
	where     map[int64]int
	sessions  map[int64]string
	faction   map[int64]int
	handoff   map[string]model.Snapshot
}

func New(deps Deps) *Controller {
	c := &Controller{
		bus: deps.Bus, log: deps.Log, save: deps.Save, obs: deps.Obs,
		grantItem: deps.GrantItem, refresh: deps.Refresh,
		shards:   map[int]*Shard{},
		mu:       make(chan struct{}, 1),
		where:    map[int64]int{},
		sessions: map[int64]string{},
		faction:  map[int64]int{},
		handoff:  map[string]model.Snapshot{},
	}
	c.mu <- struct{}{}
	for _, def := range DefaultMaps() {
		sh := newShard(def)
		sh.emitFn = c.emit
		sh.bindFn = c.bind
		sh.saveFn = c.saveState
		sh.onJump = c.scheduleJump
		sh.forgetFn = c.forget
		sh.log = deps.Log
		sh.obs = deps.Obs
		if deps.Save != nil {
			sh.reward = deps.Save.ApplyTx
		}
		sh.grantItem = deps.GrantItem
		sh.refresh = deps.Refresh
		sh.inbox = make(chan command, 512)
		c.shards[def.ID] = sh
	}
	if deps.Bus != nil {
		deps.Bus.Subscribe("in.>", func(subject string, data []byte) {
			var ing model.Ingress
			if err := json.Unmarshal(data, &ing); err != nil {
				return
			}
			if ing.MapID == 0 {
				ing.MapID = mapIDFromSubject(subject)
			}
			c.onIngress(ing)
		})
	}
	return c
}

func mapIDFromSubject(subject string) int {
	// in.<mapId>
	if len(subject) < 4 {
		return 0
	}
	id, _ := strconv.Atoi(subject[3:])
	return id
}

// Start runs every shard until ctx is cancelled.
func (c *Controller) Start(ctx context.Context) {
	for _, sh := range c.shards {
		sh := sh
		go sh.loop(ctx)
	}
	go c.checkpoint(ctx)
}

func (c *Controller) checkpoint(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, sh := range c.shards {
				sh.call(func(s *Shard) {
					for _, id := range s.store.IDs() {
						b, ok := s.store.Get(id)
						if ok && b.Kind == kindPlayer && s.saveFn != nil {
							s.saveFn(s.snapshot(b))
						}
					}
				})
			}
		}
	}
}

func (c *Controller) lock()   { <-c.mu }
func (c *Controller) unlock() { c.mu <- struct{}{} }

func (c *Controller) emit(session string, frame []byte) {
	if c.bus == nil || session == "" || frame == nil {
		return
	}
	c.bus.Publish("out."+session, frame)
	if c.obs != nil {
		c.obs.PacketsOut.Add(1)
	}
}

func (c *Controller) bind(userID int64, mapID int, session string, faction int) {
	c.lock()
	c.where[userID] = mapID
	if session != "" {
		c.sessions[userID] = session
	}
	c.faction[userID] = faction
	c.unlock()
}

func (c *Controller) saveState(snap model.Snapshot) {
	if c.save != nil {
		c.save.SaveState(snap)
	}
}

func (c *Controller) forget(userID int64) {
	c.lock()
	delete(c.where, userID)
	delete(c.sessions, userID)
	c.unlock()
}

func (c *Controller) scheduleJump(userID int64, toMap int, x, y int32) {
	go c.Transfer(userID, toMap, x, y)
}

func (c *Controller) onIngress(ing model.Ingress) {
	c.lock()
	if ing.UserID != 0 {
		if m, ok := c.where[ing.UserID]; ok {
			ing.MapID = m
		}
	}
	c.unlock()
	if ing.Type == "join" && ing.MapID == 0 && ing.Snapshot != nil {
		ing.MapID = ing.Snapshot.MapID
		if ing.MapID == 0 {
			ing.MapID = model.HomeMap(ing.Snapshot.FactionID)
		}
	}
	if ing.MapID == 0 {
		return
	}
	sh := c.shard(ing.MapID)
	sh.inbox <- command{kind: ing.Type, ing: ing}
}

func (c *Controller) shard(id int) *Shard {
	sh, ok := c.shards[id]
	if ok {
		return sh
	}
	def := mapByID(id)
	sh = newShard(def)
	sh.emitFn = c.emit
	sh.bindFn = c.bind
	sh.saveFn = c.saveState
	sh.onJump = c.scheduleJump
	sh.forgetFn = c.forget
	sh.log = c.log
	sh.obs = c.obs
	if c.save != nil {
		sh.reward = c.save.ApplyTx
	}
	sh.grantItem = c.grantItem
	sh.refresh = c.refresh
	sh.inbox = make(chan command, 512)
	c.shards[id] = sh
	go sh.loop(context.Background())
	return sh
}

// Transfer moves a pilot between shards through the handoff:<session> slot.
func (c *Controller) Transfer(userID int64, toMap int, x, y int32) error {
	c.lock()
	from := c.where[userID]
	session := c.sessions[userID]
	c.unlock()
	if from == 0 {
		return nil
	}
	src := c.shards[from]
	if src == nil {
		return nil
	}
	if from == toMap {
		src.call(func(s *Shard) {
			b, ok := s.userBody(userID)
			if !ok {
				return
			}
			b.Motion.Moving = false
			b.Motion.X, b.Motion.Y = x, y
			b.Jumping = false
			s.put(b)
			s.announce(b)
			s.visibility()
		})
		return nil
	}
	var body Body
	src.call(func(s *Shard) {
		body = s.removePlayer(userID)
	})
	if body.UserID == 0 {
		return nil
	}
	snap := model.Snapshot{
		UserID: body.UserID, PilotName: body.Name, ShipID: body.ShipID, LootID: body.ShipType,
		FactionID: int(body.Faction), ClanID: int(body.ClanID), ClanTag: body.ClanTag, RankID: int(body.Rank),
		MapID: toMap, X: x, Y: y, Speed: body.Speed,
		HP: body.MaxHP, MaxHP: body.MaxHP, Shield: body.MaxShield, MaxShield: body.MaxShield,
		Damage: body.Damage, Credits: body.Credits, Uridium: body.Uridium,
		Honor: body.Honor, Experience: body.XP, Premium: body.Premium, Level: body.Level,
		SessionID: session,
	}
	c.lock()
	c.handoff["handoff:"+session] = snap
	c.where[userID] = toMap
	c.unlock()
	dst := c.shard(toMap)
	dst.inbox <- command{kind: "join", ing: model.Ingress{
		Type: "join", SessionID: session, UserID: userID, MapID: toMap, Snapshot: &snap,
	}}
	if c.save != nil {
		c.save.SaveState(snap)
	}
	if c.log != nil {
		c.log.Append("world.transfer", session, map[string]any{"userId": userID, "from": from, "to": toMap})
	}
	return nil
}

// BroadcastAll sends a legacy system message to every online session.
func (c *Controller) BroadcastAll(text string) {
	c.BroadcastLegacy(legacyText(text))
}

// BroadcastLegacy sends an already-formatted legacy string, wrapped in command 4224.
func (c *Controller) BroadcastLegacy(message string) {
	frame, err := v10Encode(message)
	if err != nil {
		return
	}
	c.lock()
	sessions := make([]string, 0, len(c.sessions))
	for _, session := range c.sessions {
		sessions = append(sessions, session)
	}
	c.unlock()
	for _, session := range sessions {
		c.emit(session, frame)
	}
}

// Online lists pilots the controller is routing.
func (c *Controller) Online() []Presence {
	c.lock()
	defer c.unlock()
	out := make([]Presence, 0, len(c.where))
	for id, mapID := range c.where {
		out = append(out, Presence{UserID: id, MapID: mapID, Faction: c.faction[id], Session: c.sessions[id]})
	}
	return out
}

// Alive returns user ids on a map whose ships are not destroyed.
func (c *Controller) Alive(mapID int) []int64 {
	sh := c.shards[mapID]
	if sh == nil {
		return nil
	}
	var ids []int64
	sh.call(func(s *Shard) {
		for _, id := range s.store.IDs() {
			b, ok := s.store.Get(id)
			if ok && b.Kind == kindPlayer && !b.Dead {
				ids = append(ids, b.UserID)
			}
		}
	})
	return ids
}

func (c *Controller) SetRadiation(mapID int, cx, cy, radius int32) {
	sh := c.shard(mapID)
	sh.call(func(s *Shard) {
		s.rad.on = true
		s.rad.cx, s.rad.cy, s.rad.r = cx, cy, radius
	})
}

func (c *Controller) ClearRadiation(mapID int) {
	sh := c.shards[mapID]
	if sh == nil {
		return
	}
	sh.call(func(s *Shard) { s.rad.on = false })
}

func (c *Controller) ShrinkRadiation(mapID int, by int32) int32 {
	sh := c.shards[mapID]
	if sh == nil {
		return 0
	}
	var r int32
	sh.call(func(s *Shard) {
		s.rad.r -= by
		if s.rad.r < 200 {
			s.rad.r = 200
		}
		r = s.rad.r
	})
	return r
}

func (c *Controller) SetPeace(mapID int, until time.Time) {
	sh := c.shard(mapID)
	sh.call(func(s *Shard) { s.peace = until })
}

// PresenceOf reports whether a user is on a shard.
func (c *Controller) PresenceOf(userID int64) (Presence, bool) {
	c.lock()
	defer c.unlock()
	mapID, ok := c.where[userID]
	if !ok {
		return Presence{}, false
	}
	return Presence{UserID: userID, MapID: mapID, Faction: c.faction[userID], Session: c.sessions[userID]}, true
}

// Kick removes a pilot immediately.
func (c *Controller) Kick(userID int64) error {
	p, ok := c.PresenceOf(userID)
	if !ok {
		return nil
	}
	if c.bus != nil && p.Session != "" {
		c.bus.Publish("gateway.kick", []byte(p.Session))
	}
	sh := c.shards[p.MapID]
	if sh != nil {
		sh.inbox <- command{kind: "kick", ing: model.Ingress{Type: "kick", UserID: userID, SessionID: p.Session}}
	}
	return nil
}

// Jump is the admin teleport.
func (c *Controller) Jump(userID int64, mapID int) error {
	return c.Transfer(userID, mapID, 2000, 2000)
}

// SpawnBall places the spaceball on a map.
func (c *Controller) SpawnBall(mapID int, x, y int32) {
	sh := c.shard(mapID)
	sh.call(func(s *Shard) { s.SpawnBall(x, y) })
}

// TickSpaceball advances the spaceball score and reports it.
func (c *Controller) TickSpaceball(mapID int) (mmo, eic, vru int, done bool) {
	sh := c.shards[mapID]
	if sh == nil {
		return 0, 0, 0, false
	}
	sh.call(func(s *Shard) {
		mmo, eic, vru, done = s.TickBall()
	})
	return mmo, eic, vru, done
}
