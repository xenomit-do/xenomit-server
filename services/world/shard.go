package world

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/xenomit/server/pkg/ecs"
	"github.com/xenomit/server/pkg/events"
	"github.com/xenomit/server/pkg/model"
	"github.com/xenomit/server/pkg/movement"
	"github.com/xenomit/server/pkg/observability"
	"github.com/xenomit/server/pkg/protocol/legacy"
	"github.com/xenomit/server/pkg/protocol/v10"
	"github.com/xenomit/server/pkg/spatial"
)

const (
	kindPlayer = iota + 1
	kindNPC
	kindPortal
	kindBox
	kindMine
	kindStation
	kindBall

	viewShips  int32 = 2000
	viewAssets int32 = 1400
	laserRange int32 = 700
	mineRange  int32 = 250
	boxRange   int32 = 400
	gateRange  int32 = 800
)

// Body is the single archetype stored in the shard's component store.
type Body struct {
	NetID          int32
	Kind           int
	Name           string
	ShipType       string
	ShipID         int
	Faction        int32
	ClanID         int32
	ClanTag        string
	Rank           int32
	UserID         int64
	SessionID      string
	HP             int32
	MaxHP          int32
	Shield         int32
	MaxShield      int32
	Nano           int32
	MaxNano        int32
	Damage         int32
	Speed          int32
	Credits        int64
	Uridium        int64
	Honor          float64
	XP             float64
	Premium        bool
	Level          int16
	Motion         movement.State
	Selected       int32
	Attacking      bool
	LastShot       time.Time
	LastCombat     time.Time
	LastActive     time.Time
	AI             int
	Aggressive     bool
	SpawnX         int32
	SpawnY         int32
	Dead           bool
	RespawnAt      time.Time
	View           map[int32]struct{}
	VisibleBy      map[int32]struct{}
	InSecure       bool
	PortalNear     int32
	ToMap          int
	ToX, ToY       int32
	Graphic        int32
	AssetType      int16
	Jumping        bool
	JumpAt         time.Time
	JumpPortal     int32
	LogoutAt       time.Time
	DisconnectedAt time.Time
	Cloaked        bool
	HasLoot        bool
	LootCredits    int64
	LootUridium    int64
	LootHonor      float64
	LootXP         float64
	LootItem       string
	NoRespawn      bool
}

type command struct {
	kind string
	ing  model.Ingress
	fn   func(*Shard)
}

// Shard is one map actor: a single goroutine drains inbox and steps the world.
type Shard struct {
	MapID     int
	def       MapDef
	ecs       *ecs.World
	store     *ecs.Store[Body]
	byNet     map[int32]ecs.Entity
	byUser    map[int64]ecs.Entity
	grid      *spatial.Grid
	inbox     chan command
	clock     func() time.Time
	emitFn    func(session string, frame []byte)
	bindFn    func(userID int64, mapID int, session string, faction int)
	saveFn    func(model.Snapshot)
	reward    func(model.Tx) error
	grantItem func(userID int64, item string) error
	refresh   func(userID int64) (model.Snapshot, error)
	onJump    func(userID int64, toMap int, x, y int32)
	forgetFn  func(userID int64)
	log       *events.Log
	obs       *observability.Obs
	nextNeg   int32
	nextObj   int32
	seed      uint32
	visAcc    time.Duration
	secAcc    time.Duration
	rad       struct {
		on     bool
		cx, cy int32
		r      int32
	}
	peace                     time.Time
	ballMMO, ballEIC, ballVRU int
}

func newShard(def MapDef) *Shard {
	s := &Shard{
		MapID:   def.ID,
		def:     def,
		ecs:     ecs.NewWorld(),
		store:   ecs.NewStore[Body](),
		byNet:   map[int32]ecs.Entity{},
		byUser:  map[int64]ecs.Entity{},
		grid:    spatial.New(500),
		clock:   time.Now,
		nextNeg: -1,
		nextObj: -10000,
		seed:    uint32(def.ID + 1),
	}
	s.populate()
	return s
}

func (s *Shard) now() time.Time {
	if s.clock != nil {
		return s.clock()
	}
	return time.Now()
}

func (s *Shard) loop(ctx context.Context) {
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case c := <-s.inbox:
			s.dispatch(c)
		case <-tick.C:
			start := time.Now()
			s.Step(50 * time.Millisecond)
			if s.obs != nil {
				s.obs.ObserveTick(time.Since(start))
			}
		}
	}
}

// call runs fn on the shard goroutine and waits. A nil inbox runs it inline (tests).
func (s *Shard) call(fn func(*Shard)) {
	if s.inbox == nil {
		fn(s)
		return
	}
	done := make(chan struct{})
	s.inbox <- command{fn: func(sh *Shard) { fn(sh); close(done) }}
	<-done
}

func (s *Shard) dispatch(c command) {
	if c.fn != nil {
		c.fn(s)
		return
	}
	switch c.kind {
	case "join":
		s.join(c.ing)
	case "leave":
		s.leave(c.ing.UserID, c.ing.SessionID)
	case "kick":
		s.removePlayer(c.ing.UserID)
	case "packet":
		s.packet(c.ing.UserID, c.ing.Packet)
	}
}

// Step advances the simulation by dt. Tests call it directly.
func (s *Shard) Step(dt time.Duration) {
	now := s.now()
	s.integrate(now)
	s.zones(now)
	s.combat(now)
	s.visAcc += dt
	if s.visAcc >= 250*time.Millisecond {
		s.visAcc = 0
		s.visibility()
	}
	s.secAcc += dt
	if s.secAcc >= time.Second {
		s.secAcc -= time.Second
		s.ai(now)
		s.regen(now)
		s.upkeep(now)
	}
}

func (s *Shard) populate() {
	for _, n := range s.def.NPCs {
		s.spawnNPC(n)
	}
	for _, p := range s.def.Portals {
		s.nextObj--
		s.add(Body{
			NetID: s.nextObj, Kind: kindPortal, Name: "Portal",
			Faction: p.Faction, Graphic: p.Graphic, ToMap: p.ToMap, ToX: p.ToX, ToY: p.ToY,
			Motion: movement.State{X: p.X, Y: p.Y},
		})
	}
	for _, st := range s.def.Stations {
		s.nextObj--
		s.add(Body{
			NetID: s.nextObj, Kind: kindStation, Name: st.Name, AssetType: st.Type,
			Motion: movement.State{X: st.X, Y: st.Y},
		})
	}
	for i := 0; i < s.def.Boxes; i++ {
		s.nextObj--
		s.add(Body{
			NetID: s.nextObj, Kind: kindBox, Name: "Bonus box", AssetType: v10.AssetBonusBox,
			Motion: movement.State{X: 1200 + int32(i)*120, Y: 1100},
			SpawnX: 1200 + int32(i)*120, SpawnY: 1100,
		})
	}
	for i := 0; i < s.def.Mines; i++ {
		s.nextObj--
		s.add(Body{
			NetID: s.nextObj, Kind: kindMine, Name: "Mine", Damage: 800,
			Motion: movement.State{X: 12000 + int32(i)*400, Y: 9000},
		})
	}
}

func (s *Shard) spawnNPC(n NPCSpawn) int32 {
	s.nextNeg--
	hp, sh := n.HP, n.Shield
	if hp == 0 {
		hp = 800
	}
	s.add(Body{
		NetID: s.nextNeg, Kind: kindNPC, Name: n.Name, ShipType: strconv.Itoa(n.ShipID), ShipID: n.ShipID,
		HP: hp, MaxHP: hp, Shield: sh, MaxShield: sh, Speed: n.Speed, Damage: n.Damage,
		Aggressive: n.Aggressive, Motion: movement.State{X: n.X, Y: n.Y}, SpawnX: n.X, SpawnY: n.Y,
	})
	return s.nextNeg
}

func (s *Shard) add(b Body) {
	if b.View == nil {
		b.View = map[int32]struct{}{}
	}
	if b.VisibleBy == nil {
		b.VisibleBy = map[int32]struct{}{}
	}
	e := s.ecs.Create()
	s.store.Set(e, b)
	s.byNet[b.NetID] = e
	if b.UserID != 0 {
		s.byUser[b.UserID] = e
	}
	s.grid.Upsert(b.NetID, b.Motion.X, b.Motion.Y)
}

func (s *Shard) put(b Body) {
	e, ok := s.byNet[b.NetID]
	if !ok {
		return
	}
	s.store.Set(e, b)
	if b.Dead {
		s.grid.Remove(b.NetID)
		return
	}
	s.grid.Upsert(b.NetID, b.Motion.X, b.Motion.Y)
}

func (s *Shard) userBody(id int64) (Body, bool) {
	e, ok := s.byUser[id]
	if !ok {
		return Body{}, false
	}
	return s.store.Get(e)
}

func (s *Shard) net(id int32) (Body, bool) {
	e, ok := s.byNet[id]
	if !ok {
		return Body{}, false
	}
	return s.store.Get(e)
}

func (s *Shard) emit(session string, frame []byte) {
	if session == "" || frame == nil || s.emitFn == nil {
		return
	}
	s.emitFn(session, frame)
}

func (s *Shard) text(session, msg string) {
	frame, err := v10.EncodeLegacy(msg)
	if err != nil {
		return
	}
	s.emit(session, frame)
}

func (s *Shard) join(ing model.Ingress) {
	if ing.Snapshot == nil {
		return
	}
	snap := *ing.Snapshot
	if b, ok := s.userBody(ing.UserID); ok {
		b.SessionID = ing.SessionID
		b.DisconnectedAt = time.Time{}
		b.LastActive = s.now()
		s.put(b)
		s.bind(b)
		s.announce(b)
		s.visibility()
		return
	}
	ship := model.ShipByID(snap.ShipID)
	if snap.X == 0 && snap.Y == 0 {
		snap.X, snap.Y = s.def.HomeX, s.def.HomeY
	}
	hp, shield := snap.MaxHP, snap.MaxShield
	if hp == 0 {
		hp = ship.HP
	}
	if shield == 0 && snap.MaxShield == 0 {
		shield = ship.Shield
	}
	speed := snap.Speed
	if speed == 0 {
		speed = ship.Speed
	}
	dmg := snap.Damage
	if dmg == 0 {
		dmg = ship.Damage
	}
	b := Body{
		NetID: int32(snap.UserID), Kind: kindPlayer, Name: snap.PilotName, ShipType: ship.LootID, ShipID: snap.ShipID,
		Faction: int32(snap.FactionID), ClanID: int32(snap.ClanID), ClanTag: snap.ClanTag, Rank: int32(snap.RankID),
		UserID: snap.UserID, SessionID: ing.SessionID,
		HP: hp, MaxHP: hp, Shield: shield, MaxShield: shield, Speed: speed, Damage: dmg,
		Credits: snap.Credits, Uridium: snap.Uridium, Honor: snap.Honor, XP: snap.Experience,
		Premium: snap.Premium, Level: snap.Level, LastActive: s.now(),
		Motion: movement.State{X: snap.X, Y: snap.Y},
	}
	s.add(b)
	s.bind(b)
	s.announce(b)
	s.visibility()
	if s.log != nil {
		s.log.Append("session.login", strconv.FormatInt(snap.UserID, 10), map[string]any{
			"userId": snap.UserID, "mapId": s.MapID, "name": snap.PilotName,
		})
	}
	if s.obs != nil {
		s.obs.Logins.Add(1)
		s.obs.Sessions.Add(1)
	}
}

func (s *Shard) bind(b Body) {
	if s.bindFn != nil && b.UserID != 0 {
		s.bindFn(b.UserID, s.MapID, b.SessionID, int(b.Faction))
	}
}

func (s *Shard) announce(b Body) {
	frame, err := v10.EncodeShipInit(v10.ShipInit{
		UserID: b.NetID, Name: b.Name, ShipType: b.ShipType, ClanTag: b.ClanTag,
		Speed: b.Speed, Shield: b.Shield, MaxShield: b.MaxShield,
		HP: b.HP, MaxHP: b.MaxHP, Nano: b.Nano, MaxNano: b.MaxNano,
		X: b.Motion.X, Y: b.Motion.Y, MapID: int32(s.MapID),
		FactionID: b.Faction, ClanID: b.ClanID, Expansion: 3, Rank: b.Rank,
		Premium: b.Premium, XP: b.XP, Honor: b.Honor, Level: b.Level,
		Credits: b.Credits, Uridium: b.Uridium,
		Unknown2: true, Unknown3: true, Cloaked: b.Cloaked,
	})
	if err == nil {
		s.emit(b.SessionID, frame)
	}
	s.text(b.SessionID, "0|S|CFG|1")
	s.text(b.SessionID, "0|n|ssi|0|0|0")
	s.text(b.SessionID, "0|A|BK|0")
	if sp, err := v10.EncodeSetSpeed(b.Speed, b.Speed); err == nil {
		s.emit(b.SessionID, sp)
	}
	for _, id := range s.store.IDs() {
		o, ok := s.store.Get(id)
		if !ok || o.Dead {
			continue
		}
		s.showStatic(b.SessionID, o)
	}
}

func (s *Shard) showStatic(session string, o Body) {
	switch o.Kind {
	case kindPortal:
		frame, err := v10.EncodeCreatePortal(o.NetID, o.Faction, o.Graphic, o.Motion.X, o.Motion.Y, true, true)
		if err == nil {
			s.emit(session, frame)
		}
	case kindStation, kindBox:
		frame, err := v10.EncodeAssetCreate(o.AssetType, o.Name, o.NetID, 0, o.Motion.X, o.Motion.Y, o.Faction)
		if err == nil {
			s.emit(session, frame)
		}
	}
}

func (s *Shard) leave(userID int64, session string) {
	b, ok := s.userBody(userID)
	if !ok {
		return
	}
	if session != "" && b.SessionID != session {
		return
	}
	b.DisconnectedAt = s.now()
	b.Attacking = false
	s.put(b)
}

func (s *Shard) removePlayer(userID int64) Body {
	b, ok := s.userBody(userID)
	if !ok {
		return Body{}
	}
	s.hide(b.NetID, kindPlayer)
	s.removeNet(b.NetID)
	if s.saveFn != nil {
		s.saveFn(s.snapshot(b))
	}
	if s.forgetFn != nil {
		s.forgetFn(b.UserID)
	}
	if s.obs != nil {
		s.obs.Sessions.Add(-1)
	}
	return b
}

func (s *Shard) snapshot(b Body) model.Snapshot {
	return model.Snapshot{
		UserID: b.UserID, PilotName: b.Name, ShipID: b.ShipID, MapID: s.MapID,
		X: b.Motion.X, Y: b.Motion.Y, FactionID: int(b.Faction),
	}
}

func (s *Shard) removeNet(netID int32) {
	e, ok := s.byNet[netID]
	if !ok {
		return
	}
	b, _ := s.store.Get(e)
	s.store.Delete(e)
	s.ecs.Destroy(e)
	delete(s.byNet, netID)
	if b.UserID != 0 {
		delete(s.byUser, b.UserID)
	}
	s.grid.Remove(netID)
}

func (s *Shard) packet(userID int64, frame []byte) {
	b, ok := s.userBody(userID)
	if !ok || b.Dead {
		return
	}
	b.LastActive = s.now()
	s.put(b)
	id, err := v10.CommandID(frame)
	if err != nil {
		return
	}
	switch id {
	case v10.MoveRequestID:
		m, err := v10.DecodeMoveRequest(frame)
		if err == nil {
			s.beginMove(userID, m.TargetX, m.TargetY)
		}
	case v10.ShipSelectID:
		sel, err := v10.DecodeSelect(frame)
		if err == nil {
			s.selectTarget(userID, sel.TargetID)
		}
	case v10.AttackLaserID:
		s.setAttack(userID, true)
	case v10.AttackAbortLaserID:
		s.setAttack(userID, false)
	case v10.AttackRocketID:
		s.rocket(userID)
	case v10.PortalJumpID:
		s.usePortal(userID)
	case v10.CollectBoxID:
		s.collect(userID)
	case v10.LogoutCancelID:
		s.cancelLogout(userID)
	case v10.LegacyModuleID:
		msg, err := v10.DecodeLegacy(frame)
		if err == nil {
			s.legacy(userID, msg)
		}
	}
}

func (s *Shard) beginMove(userID int64, x, y int32) {
	b, ok := s.userBody(userID)
	if !ok || b.Dead || b.Jumping {
		return
	}
	x, y = movement.Clamp(x, y, s.def.W, s.def.H)
	ms, moved := movement.Begin(&b.Motion, x, y, b.Speed, s.now())
	s.put(b)
	if !moved {
		return
	}
	frame, err := v10.EncodeMoveCommand(b.NetID, x, y, ms)
	if err != nil {
		return
	}
	s.emit(b.SessionID, frame)
	s.fan(b, frame)
}

func (s *Shard) fan(b Body, frame []byte) {
	for id := range b.VisibleBy {
		other, ok := s.net(id)
		if ok && other.SessionID != "" && other.SessionID != b.SessionID {
			s.emit(other.SessionID, frame)
		}
	}
}

func (s *Shard) selectTarget(userID int64, target int32) {
	b, ok := s.userBody(userID)
	if !ok {
		return
	}
	if target != 0 {
		if _, exists := s.byNet[target]; !exists {
			return
		}
	}
	b.Selected = target
	if target == 0 {
		b.Attacking = false
	}
	s.put(b)
}

func (s *Shard) setAttack(userID int64, on bool) {
	b, ok := s.userBody(userID)
	if !ok || b.Dead {
		return
	}
	if on && (b.Selected == 0 || b.InSecure || s.peaceful()) {
		return
	}
	b.Attacking = on
	s.put(b)
}

func (s *Shard) peaceful() bool {
	return !s.peace.IsZero() && s.now().Before(s.peace)
}

func (s *Shard) legacy(userID int64, msg string) {
	parts := legacy.Split(msg)
	if len(parts) >= 3 && parts[1] == "S" && parts[2] == "CFG" {
		b, ok := s.userBody(userID)
		if !ok {
			return
		}
		s.text(b.SessionID, msg)
		return
	}
	if len(parts) >= 3 && parts[1] == "A" && parts[2] == "LOT" {
		b, ok := s.userBody(userID)
		if !ok {
			return
		}
		wait := 10 * time.Second
		if b.Premium {
			wait = 5 * time.Second
		}
		b.LogoutAt = s.now().Add(wait)
		s.put(b)
		s.text(b.SessionID, legacy.Std("Logging out..."))
	}
}

func (s *Shard) cancelLogout(userID int64) {
	b, ok := s.userBody(userID)
	if !ok {
		return
	}
	b.LogoutAt = time.Time{}
	s.put(b)
}

func (s *Shard) integrate(now time.Time) {
	for _, id := range s.store.IDs() {
		b, ok := s.store.Get(id)
		if !ok || b.Dead || !b.Motion.Moving {
			continue
		}
		movement.Actual(&b.Motion, now)
		s.put(b)
	}
}

func (s *Shard) zones(now time.Time) {
	for _, id := range s.store.IDs() {
		b, ok := s.store.Get(id)
		if !ok || b.Kind != kindPlayer || b.Dead {
			continue
		}
		b.InSecure = false
		b.PortalNear = 0
		var nearest float64 = math.MaxFloat64
		for _, oid := range s.store.IDs() {
			o, ok := s.store.Get(oid)
			if !ok || o.Dead {
				continue
			}
			d := hypot(b.Motion.X, b.Motion.Y, o.Motion.X, o.Motion.Y)
			switch o.Kind {
			case kindStation:
				if d <= 1500 {
					b.InSecure = true
				}
				if d <= 700 && b.HP < b.MaxHP {
					b.HP += 40
					if b.HP > b.MaxHP {
						b.HP = b.MaxHP
					}
				}
			case kindPortal:
				if d <= float64(gateRange) && d < nearest {
					nearest = d
					b.PortalNear = o.NetID
				}
			case kindMine:
				if d <= float64(mineRange) {
					s.detonate(o, &b)
					if b.Dead {
						s.onDeath(o, b)
					}
				}
			}
		}
		if b.Jumping && !now.Before(b.JumpAt) {
			portal, ok := s.net(b.JumpPortal)
			b.Jumping = false
			b.JumpPortal = 0
			s.put(b)
			if ok && s.onJump != nil {
				uid, to, x, y := b.UserID, portal.ToMap, portal.ToX, portal.ToY
				go s.onJump(uid, to, x, y)
			}
			continue
		}
		s.put(b)
	}
	if s.rad.on {
		for _, id := range s.store.IDs() {
			b, ok := s.store.Get(id)
			if !ok || b.Kind != kindPlayer || b.Dead {
				continue
			}
			if hypot(b.Motion.X, b.Motion.Y, s.rad.cx, s.rad.cy) > float64(s.rad.r) {
				dmg := b.MaxHP / 20
				if dmg < 1 {
					dmg = 1
				}
				s.applyHit(&b, 0, v10.AttackRadiation, dmg, now)
				s.put(b)
			}
		}
	}
}

func (s *Shard) detonate(mine Body, player *Body) {
	s.hide(mine.NetID, kindMine)
	s.removeNet(mine.NetID)
	s.applyHit(player, mine.NetID, v10.AttackMine, mine.Damage, s.now())
}

func (s *Shard) combat(now time.Time) {
	if s.peaceful() {
		return
	}
	for _, id := range s.store.IDs() {
		b, ok := s.store.Get(id)
		if !ok || b.Dead || !b.Attacking || b.Selected == 0 {
			continue
		}
		if b.InSecure {
			continue
		}
		if !b.LastShot.IsZero() && now.Sub(b.LastShot) < time.Second {
			continue
		}
		target, ok := s.net(b.Selected)
		if !ok || target.Dead {
			b.Attacking = false
			s.put(b)
			continue
		}
		if target.InSecure && target.Kind == kindPlayer {
			continue
		}
		if hypot(b.Motion.X, b.Motion.Y, target.Motion.X, target.Motion.Y) > float64(laserRange) {
			continue
		}
		b.LastShot = now
		s.put(b)
		skilled := b.Kind == kindPlayer
		if run, err := v10.EncodeLaserRun(b.NetID, target.NetID, 1, false, skilled); err == nil {
			s.emit(b.SessionID, run)
			s.fan(b, run)
			if target.SessionID != "" {
				s.emit(target.SessionID, run)
			}
		}
		s.applyHit(&target, b.NetID, v10.AttackLaser, b.Damage, now)
		s.put(target)
		if target.Dead {
			s.onDeath(b, target)
		}
	}
}

func (s *Shard) rocket(userID int64) {
	b, ok := s.userBody(userID)
	if !ok || b.Dead || b.Selected == 0 || b.InSecure || s.peaceful() {
		return
	}
	target, ok := s.net(b.Selected)
	if !ok || target.Dead {
		return
	}
	if hypot(b.Motion.X, b.Motion.Y, target.Motion.X, target.Motion.Y) > 900 {
		return
	}
	s.applyHit(&target, b.NetID, v10.AttackRocket, b.Damage*2, s.now())
	s.put(target)
	if target.Dead {
		s.onDeath(b, target)
	}
}

func (s *Shard) applyHit(target *Body, attacker int32, kind int16, dmg int32, now time.Time) {
	if dmg < 0 {
		dmg = 0
	}
	dealt := dmg
	if target.Shield >= dealt {
		target.Shield -= dealt
	} else {
		rest := dealt - target.Shield
		target.Shield = 0
		target.HP -= rest
		if target.HP < 0 {
			target.HP = 0
		}
	}
	target.LastCombat = now
	if attacker != 0 {
		if a, ok := s.net(attacker); ok {
			a.LastCombat = now
			s.put(a)
		}
	}
	if hit, err := v10.EncodeHit(kind, attacker, target.NetID, target.HP, target.Shield, target.Nano, dealt, false); err == nil {
		s.emit(target.SessionID, hit)
		s.fan(*target, hit)
		if a, ok := s.net(attacker); ok {
			s.emit(a.SessionID, hit)
		}
	}
	if target.HP == 0 {
		target.Dead = true
		target.Attacking = false
	}
}

func (s *Shard) onDeath(killer, victim Body) {
	s.hide(victim.NetID, victim.Kind)
	if victim.Kind == kindNPC {
		if victim.NoRespawn {
			s.removeNet(victim.NetID)
		} else {
			victim.RespawnAt = s.now().Add(5 * time.Second)
			s.put(victim)
		}
	} else if victim.Kind == kindPlayer && !s.def.DeathLeaves {
		victim.RespawnAt = s.now().Add(3 * time.Second)
		s.put(victim)
		s.text(victim.SessionID, legacy.Std("You were destroyed."))
	} else if victim.Kind == kindPlayer {
		s.put(victim)
		s.text(victim.SessionID, legacy.Std("You were destroyed."))
	}
	if killer.Kind == kindPlayer && victim.Kind == kindNPC {
		s.pay(killer.UserID, model.Tx{
			ID:     "kill-" + strconv.FormatInt(killer.UserID, 10) + "-" + strconv.FormatInt(int64(victim.NetID), 10) + "-" + strconv.FormatInt(s.now().UnixNano(), 10),
			UserID: killer.UserID, Reason: "kill", Credits: 100, Uridium: 1, Honor: 10, Experience: 50,
		}, "", "Reward: +100 credits, +1 uridium.")
	}
	if s.log != nil && killer.UserID != 0 {
		s.log.Append("combat.kill", strconv.FormatInt(killer.UserID, 10), map[string]any{
			"killerId": killer.UserID, "name": killer.Name, "honor": 10.0,
			"victimId": victim.UserID, "mapId": s.MapID, "npc": victim.Kind == kindNPC,
		})
	}
	if s.obs != nil {
		s.obs.Kills.Add(1)
	}
}

func (s *Shard) hide(netID int32, kind int) {
	for _, id := range s.store.IDs() {
		b, ok := s.store.Get(id)
		if !ok || b.Kind != kindPlayer {
			continue
		}
		if _, seen := b.View[netID]; !seen && kind != kindPlayer {
			continue
		}
		delete(b.View, netID)
		if b.SessionID == "" {
			continue
		}
		var frame []byte
		var err error
		switch kind {
		case kindPortal:
			frame, err = v10.EncodeRemovePortal(netID)
		case kindBox, kindStation, kindMine:
			frame, err = v10.EncodeAssetRemove(v10.AssetBonusBox, netID)
		default:
			frame, err = v10.EncodeShipRemove(netID)
		}
		if err == nil {
			s.emit(b.SessionID, frame)
		}
	}
	if o, ok := s.net(netID); ok {
		o.VisibleBy = map[int32]struct{}{}
		s.put(o)
	}
}

func (s *Shard) visibility() {
	for _, id := range s.store.IDs() {
		p, ok := s.store.Get(id)
		if !ok || p.Kind != kindPlayer || p.Dead || p.SessionID == "" {
			continue
		}
		next := map[int32]struct{}{}
		radius := viewShips
		for _, nid := range s.grid.QueryRadius(p.Motion.X, p.Motion.Y, radius) {
			o, ok := s.net(nid)
			if !ok || o.Dead || o.NetID == p.NetID {
				continue
			}
			if o.Kind != kindPlayer && o.Kind != kindNPC && o.Kind != kindBall {
				if hypot(p.Motion.X, p.Motion.Y, o.Motion.X, o.Motion.Y) > float64(viewAssets) {
					continue
				}
			}
			next[o.NetID] = struct{}{}
		}
		if p.Selected != 0 {
			if o, ok := s.net(p.Selected); ok && !o.Dead {
				next[p.Selected] = struct{}{}
			}
		}
		for nid := range next {
			if _, had := p.View[nid]; had {
				continue
			}
			o, ok := s.net(nid)
			if !ok {
				continue
			}
			s.appear(p, o)
			o.VisibleBy[p.NetID] = struct{}{}
		}
		for nid := range p.View {
			if _, keep := next[nid]; keep {
				continue
			}
			if nid == p.Selected {
				next[nid] = struct{}{}
				continue
			}
			o, ok := s.net(nid)
			if ok {
				delete(o.VisibleBy, p.NetID)
			}
			s.disappear(p, nid, o.Kind)
		}
		p.View = next
		s.put(p)
	}
}

func (s *Shard) appear(viewer, other Body) {
	switch other.Kind {
	case kindPlayer, kindNPC, kindBall:
		rel := v10.RelationNone
		npc := other.Kind != kindPlayer
		if npc {
			rel = v10.RelationWar
		}
		frame, err := v10.EncodeShipCreate(v10.ShipCreate{
			UserID: other.NetID, ShipType: other.ShipType, Expansion: 3,
			ClanTag: other.ClanTag, Name: other.Name, X: other.Motion.X, Y: other.Motion.Y,
			FactionID: other.Faction, ClanID: other.ClanID, Rank: other.Rank,
			Relation: rel, NPC: npc, MotherID: int32(rel), PositionIndex: int32(rel),
		})
		if err == nil {
			s.emit(viewer.SessionID, frame)
		}
	default:
		s.showStatic(viewer.SessionID, other)
	}
}

func (s *Shard) disappear(viewer Body, netID int32, kind int) {
	var frame []byte
	var err error
	switch kind {
	case kindPortal:
		frame, err = v10.EncodeRemovePortal(netID)
	case kindBox, kindStation, kindMine:
		frame, err = v10.EncodeAssetRemove(v10.AssetBonusBox, netID)
	default:
		frame, err = v10.EncodeShipRemove(netID)
	}
	if err == nil {
		s.emit(viewer.SessionID, frame)
	}
}

func (s *Shard) ai(now time.Time) {
	for _, id := range s.store.IDs() {
		b, ok := s.store.Get(id)
		if !ok || b.Kind != kindNPC || b.Dead {
			continue
		}
		if len(b.VisibleBy) == 0 {
			continue
		}
		var target Body
		var have bool
		if b.Aggressive {
			best := math.MaxFloat64
			for _, pid := range s.store.IDs() {
				p, ok := s.store.Get(pid)
				if !ok || p.Kind != kindPlayer || p.Dead || p.InSecure || p.Cloaked {
					continue
				}
				d := hypot(b.Motion.X, b.Motion.Y, p.Motion.X, p.Motion.Y)
				if d < 1000 && d < best {
					best = d
					target = p
					have = true
				}
			}
		}
		if have {
			b.Selected = target.NetID
			b.Attacking = hypot(b.Motion.X, b.Motion.Y, target.Motion.X, target.Motion.Y) <= float64(laserRange)
			ang := float64(s.rand(360)) * math.Pi / 180
			dx := int32(300 * math.Cos(ang))
			dy := int32(300 * math.Sin(ang))
			x, y := movement.Clamp(target.Motion.X+dx, target.Motion.Y+dy, s.def.W, s.def.H)
			ms, moved := movement.Begin(&b.Motion, x, y, b.Speed, now)
			s.put(b)
			if moved {
				if frame, err := v10.EncodeMoveCommand(b.NetID, x, y, ms); err == nil {
					s.fan(b, frame)
				}
			}
			continue
		}
		b.Attacking = false
		b.Selected = 0
		wx := b.SpawnX + int32(s.rand(1500)) - 750
		wy := b.SpawnY + int32(s.rand(1500)) - 750
		wx, wy = movement.Clamp(wx, wy, s.def.W, s.def.H)
		ms, moved := movement.Begin(&b.Motion, wx, wy, b.Speed, now)
		s.put(b)
		if moved {
			if frame, err := v10.EncodeMoveCommand(b.NetID, wx, wy, ms); err == nil {
				s.fan(b, frame)
			}
		}
	}
}

func (s *Shard) rand(n uint32) uint32 {
	s.seed = s.seed*1664525 + 1013904223
	if n == 0 {
		return 0
	}
	return s.seed % n
}

func (s *Shard) regen(now time.Time) {
	for _, id := range s.store.IDs() {
		b, ok := s.store.Get(id)
		if !ok || b.Dead {
			continue
		}
		if b.Shield < b.MaxShield && now.Sub(b.LastCombat) > 5*time.Second {
			b.Shield += b.MaxShield / 20
			if b.Shield > b.MaxShield {
				b.Shield = b.MaxShield
			}
			s.put(b)
		}
	}
}

func (s *Shard) upkeep(now time.Time) {
	for _, id := range s.store.IDs() {
		b, ok := s.store.Get(id)
		if !ok {
			continue
		}
		if (b.Kind == kindNPC || b.Kind == kindBox) && b.Dead && !b.RespawnAt.IsZero() && !now.Before(b.RespawnAt) {
			b.Dead = false
			b.HP, b.Shield = b.MaxHP, b.MaxShield
			b.Motion.X, b.Motion.Y = b.SpawnX, b.SpawnY
			b.Motion.Moving = false
			b.Attacking = false
			b.RespawnAt = time.Time{}
			s.put(b)
			continue
		}
		if b.Kind == kindPlayer && b.Dead && !s.def.DeathLeaves && !b.RespawnAt.IsZero() && !now.Before(b.RespawnAt) {
			b.Dead = false
			b.HP, b.Shield = b.MaxHP, b.MaxShield
			b.Motion.X, b.Motion.Y = s.def.HomeX, s.def.HomeY
			b.Motion.Moving = false
			b.Attacking = false
			b.RespawnAt = time.Time{}
			s.put(b)
			s.announce(b)
			continue
		}
		if b.Kind == kindPlayer && !b.LogoutAt.IsZero() && !now.Before(b.LogoutAt) {
			s.removePlayer(b.UserID)
			continue
		}
		if b.Kind == kindPlayer && !b.DisconnectedAt.IsZero() && now.Sub(b.DisconnectedAt) > 2*time.Minute {
			s.removePlayer(b.UserID)
			continue
		}
		if b.Kind == kindPlayer && !b.Dead && now.Sub(b.LastActive) > 3*time.Minute && now.Sub(b.LastCombat) > 10*time.Second {
			s.text(b.SessionID, legacy.Std("Disconnected for inactivity."))
			s.removePlayer(b.UserID)
		}
	}
}

func (s *Shard) usePortal(userID int64) {
	b, ok := s.userBody(userID)
	if !ok || b.Dead || b.Jumping || b.PortalNear == 0 {
		if ok {
			s.text(b.SessionID, legacy.Std("jumpgate_failed_no_gate"))
		}
		return
	}
	if s.def.PVP && s.now().Sub(b.LastCombat) < 10*time.Second {
		s.text(b.SessionID, "0|A|STM|jumpgate_failed_pvp_map")
		return
	}
	portal, ok := s.net(b.PortalNear)
	if !ok {
		return
	}
	b.Jumping = true
	b.JumpPortal = portal.NetID
	b.JumpAt = s.now().Add(3250 * time.Millisecond)
	s.put(b)
	if frame, err := v10.EncodeActivatePortal(int32(portal.ToMap), portal.NetID); err == nil {
		s.emit(b.SessionID, frame)
	}
}

func (s *Shard) collect(userID int64) {
	b, ok := s.userBody(userID)
	if !ok || b.Dead {
		return
	}
	var box Body
	best := math.MaxFloat64
	for _, id := range s.store.IDs() {
		o, ok := s.store.Get(id)
		if !ok || o.Kind != kindBox || o.Dead {
			continue
		}
		d := hypot(b.Motion.X, b.Motion.Y, o.Motion.X, o.Motion.Y)
		if d <= float64(boxRange) && d < best {
			best = d
			box = o
		}
	}
	if box.NetID == 0 {
		return
	}
	box.Dead = true
	if box.NoRespawn {
		box.RespawnAt = time.Time{}
	} else {
		box.RespawnAt = s.now().Add(15 * time.Second)
	}
	s.put(box)
	s.hide(box.NetID, kindBox)
	if box.NoRespawn {
		s.removeNet(box.NetID)
	}
	credits, uri := int64(10), int64(1)
	var honor, xp float64
	item := ""
	if box.HasLoot {
		credits, uri = box.LootCredits, box.LootUridium
		honor, xp, item = box.LootHonor, box.LootXP, box.LootItem
	}
	s.pay(b.UserID, model.Tx{
		ID:         "box-" + strconv.FormatInt(b.UserID, 10) + "-" + strconv.FormatInt(s.now().UnixNano(), 10),
		UserID:     b.UserID,
		Reason:     "box",
		Credits:    credits,
		Uridium:    uri,
		Honor:      honor,
		Experience: xp,
	}, item, boxNote(box.Name, credits, uri, honor, xp, item))
}

func boxNote(name string, credits, uri int64, honor, xp float64, item string) string {
	if name == "" {
		name = "bonus box"
	}
	note := fmt.Sprintf("Collected %s: %d credits, %d uridium.", name, credits, uri)
	if honor != 0 {
		note += fmt.Sprintf(" %.0f honor.", honor)
	}
	if xp != 0 {
		note += fmt.Sprintf(" %.0f xp.", xp)
	}
	if item != "" {
		note += " Item " + item + "."
	}
	return note
}

func (s *Shard) pay(userID int64, tx model.Tx, item, note string) {
	if s.reward != nil && (tx.Credits != 0 || tx.Uridium != 0 || tx.Honor != 0 || tx.Experience != 0) {
		_ = s.reward(tx)
	}
	if item != "" && s.grantItem != nil {
		_ = s.grantItem(userID, item)
	}
	b, ok := s.userBody(userID)
	if !ok {
		return
	}
	if s.refresh != nil {
		if snap, err := s.refresh(userID); err == nil {
			b.Credits = snap.Credits
			b.Uridium = snap.Uridium
			b.Honor = snap.Honor
			b.XP = snap.Experience
			b.Level = snap.Level
			b.Damage = snap.Damage
			b.Premium = snap.Premium
		}
	} else {
		b.Credits += tx.Credits
		b.Uridium += tx.Uridium
		b.Honor += tx.Honor
		b.XP += tx.Experience
	}
	s.put(b)
	if note != "" {
		s.text(b.SessionID, legacy.Std(note))
	}
}

func hypot(x1, y1, x2, y2 int32) float64 {
	return math.Hypot(float64(x1-x2), float64(y1-y2))
}

// SpawnBall drops the spaceball on this map.
func (s *Shard) SpawnBall(x, y int32) {
	s.nextNeg--
	s.add(Body{
		NetID: s.nextNeg, Kind: kindBall, Name: "Spaceball", ShipType: "1",
		HP: 100000, MaxHP: 100000, Speed: 100, Motion: movement.State{X: x, Y: y},
	})
}

// TickBall awards a point to the faction of the pilot closest to the ball.
func (s *Shard) TickBall() (mmo, eic, vru int, done bool) {
	var ball Body
	var have bool
	for _, id := range s.store.IDs() {
		b, ok := s.store.Get(id)
		if ok && b.Kind == kindBall && !b.Dead {
			ball = b
			have = true
			break
		}
	}
	if !have {
		return s.ballMMO, s.ballEIC, s.ballVRU, false
	}
	best := math.MaxFloat64
	var faction int32
	for _, id := range s.store.IDs() {
		b, ok := s.store.Get(id)
		if !ok || b.Kind != kindPlayer || b.Dead {
			continue
		}
		d := hypot(ball.Motion.X, ball.Motion.Y, b.Motion.X, b.Motion.Y)
		if d < best {
			best = d
			faction = b.Faction
		}
	}
	if best <= 800 {
		switch faction {
		case 2:
			s.ballEIC++
		case 3:
			s.ballVRU++
		default:
			s.ballMMO++
		}
	}
	done = s.ballMMO >= 10 || s.ballEIC >= 10 || s.ballVRU >= 10
	return s.ballMMO, s.ballEIC, s.ballVRU, done
}
