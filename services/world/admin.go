package world

import (
	"fmt"
	"strings"

	"github.com/xenomit/server/pkg/model"
	"github.com/xenomit/server/pkg/movement"
	"github.com/xenomit/server/pkg/protocol/legacy"
	"github.com/xenomit/server/pkg/protocol/v10"
)

// Spawn places entities and returns their net ids. Admin spawns do not respawn
// unless Respawn is explicitly true.
func (c *Controller) Spawn(spec model.SpawnSpec) ([]int32, error) {
	if spec.NearUserID != 0 {
		info, ok := c.Where(spec.NearUserID)
		if !ok {
			return nil, fmt.Errorf("user %d is not in-game", spec.NearUserID)
		}
		spec.MapID = info.MapID
		if spec.X == 0 && spec.Y == 0 {
			spec.X = info.X + 120
			spec.Y = info.Y
		}
	}
	if spec.MapID == 0 {
		return nil, fmt.Errorf("mapId is required")
	}
	sh := c.shard(spec.MapID)
	var ids []int32
	var err error
	sh.call(func(s *Shard) {
		ids, err = s.spawnSpec(spec)
	})
	return ids, err
}

// Despawn removes one non-player entity.
func (c *Controller) Despawn(mapID int, netID int32) error {
	sh := c.shards[mapID]
	if sh == nil {
		return fmt.Errorf("map %d is not loaded", mapID)
	}
	var err error
	sh.call(func(s *Shard) { err = s.despawn(netID) })
	return err
}

// Clear removes every entity of a kind on a map. Players are refused.
func (c *Controller) Clear(mapID int, kind string) (int, error) {
	sh := c.shards[mapID]
	if sh == nil {
		return 0, fmt.Errorf("map %d is not loaded", mapID)
	}
	var n int
	var err error
	sh.call(func(s *Shard) { n, err = s.clearKind(kind) })
	return n, err
}

// Inspect lists everything currently stored on a map.
func (c *Controller) Inspect(mapID int) []model.EntityInfo {
	sh := c.shards[mapID]
	if sh == nil {
		return nil
	}
	var out []model.EntityInfo
	sh.call(func(s *Shard) { out = s.inspect() })
	return out
}

// Where reports a pilot's live ship.
func (c *Controller) Where(userID int64) (model.EntityInfo, bool) {
	p, ok := c.PresenceOf(userID)
	if !ok {
		return model.EntityInfo{}, false
	}
	sh := c.shards[p.MapID]
	if sh == nil {
		return model.EntityInfo{}, false
	}
	var info model.EntityInfo
	var found bool
	sh.call(func(s *Shard) {
		b, ok := s.userBody(userID)
		if !ok {
			return
		}
		info = s.entityInfo(b)
		found = true
	})
	return info, found
}

// Heal fills hull and shield and clears a destruction state.
func (c *Controller) Heal(userID int64) error {
	return c.onPilot(userID, func(s *Shard, b Body) error {
		b.HP, b.Shield = b.MaxHP, b.MaxShield
		b.Dead = false
		b.Attacking = false
		s.put(b)
		s.announce(b)
		s.note(b.SessionID, "Repaired.")
		return nil
	})
}

// Kill destroys one entity. killerUser, when online on that map, receives NPC rewards.
func (c *Controller) Kill(mapID int, netID int32, killerUser int64) error {
	sh := c.shards[mapID]
	if sh == nil {
		return fmt.Errorf("map %d is not loaded", mapID)
	}
	var err error
	sh.call(func(s *Shard) { err = s.smite(netID, killerUser) })
	return err
}

// Vitals applies a live ship edit and pushes a fresh ship initialization.
func (c *Controller) Vitals(v model.Vitals) error {
	return c.onPilot(v.UserID, func(s *Shard, b Body) error {
		if v.ShipID != nil {
			b.ShipID = *v.ShipID
			if v.ShipType != "" {
				b.ShipType = v.ShipType
			}
		}
		if v.MaxHP != nil {
			b.MaxHP = *v.MaxHP
		}
		if v.HP != nil {
			b.HP = *v.HP
		} else if v.MaxHP != nil {
			b.HP = b.MaxHP
		}
		if v.MaxShield != nil {
			b.MaxShield = *v.MaxShield
		}
		if v.Shield != nil {
			b.Shield = *v.Shield
		} else if v.MaxShield != nil {
			b.Shield = b.MaxShield
		}
		if v.Damage != nil {
			b.Damage = *v.Damage
		}
		if v.Speed != nil {
			b.Speed = *v.Speed
		}
		if v.Cloak != nil {
			b.Cloaked = *v.Cloak
		}
		if b.HP > b.MaxHP {
			b.MaxHP = b.HP
		}
		if b.Shield > b.MaxShield {
			b.MaxShield = b.Shield
		}
		if b.HP > 0 {
			b.Dead = false
		}
		s.put(b)
		s.announce(b)
		return nil
	})
}

// SyncPilot copies ledger fields onto the live ship and re-sends ship init.
func (c *Controller) SyncPilot(userID int64, snap model.Snapshot, note string) bool {
	p, ok := c.PresenceOf(userID)
	if !ok {
		return false
	}
	sh := c.shards[p.MapID]
	if sh == nil {
		return false
	}
	sh.call(func(s *Shard) {
		b, ok := s.userBody(userID)
		if !ok {
			return
		}
		b.Credits = snap.Credits
		b.Uridium = snap.Uridium
		b.Honor = snap.Honor
		b.XP = snap.Experience
		b.Level = snap.Level
		b.Damage = snap.Damage
		b.Premium = snap.Premium
		s.put(b)
		s.announce(b)
		if note != "" {
			s.note(b.SessionID, note)
		}
	})
	return true
}

// Tell sends one system line to an online pilot.
func (c *Controller) Tell(userID int64, text string) bool {
	err := c.onPilot(userID, func(s *Shard, b Body) error {
		s.note(b.SessionID, text)
		return nil
	})
	return err == nil
}

func (c *Controller) onPilot(userID int64, fn func(*Shard, Body) error) error {
	p, ok := c.PresenceOf(userID)
	if !ok {
		return fmt.Errorf("user %d is not in-game", userID)
	}
	sh := c.shards[p.MapID]
	if sh == nil {
		return fmt.Errorf("user %d is not in-game", userID)
	}
	var err error
	sh.call(func(s *Shard) {
		b, ok := s.userBody(userID)
		if !ok {
			err = fmt.Errorf("user %d is not in-game", userID)
			return
		}
		err = fn(s, b)
	})
	return err
}

func (s *Shard) note(session, text string) {
	s.text(session, legacy.Std(text))
}

func (s *Shard) spawnSpec(spec model.SpawnSpec) ([]int32, error) {
	kind, ok := parseKind(spec.Kind)
	if !ok || kind == kindPlayer {
		return nil, fmt.Errorf("unknown kind %q", spec.Kind)
	}
	n := spec.Count
	if n <= 0 {
		n = 1
	}
	if n > 25 {
		return nil, fmt.Errorf("count must be 1..25")
	}
	respawn := false
	if spec.Respawn != nil {
		respawn = *spec.Respawn
	}
	var ids []int32
	for i := 0; i < n; i++ {
		x, y := movement.Clamp(spec.X+int32(i)*100, spec.Y, s.def.W, s.def.H)
		id, err := s.spawnOne(spec, kind, x, y, !respawn)
		if err != nil {
			return ids, err
		}
		ids = append(ids, id)
	}
	s.visibility()
	return ids, nil
}

func (s *Shard) spawnOne(spec model.SpawnSpec, kind int, x, y int32, noRespawn bool) (int32, error) {
	switch kind {
	case kindNPC:
		shipID := spec.ShipID
		if shipID == 0 {
			shipID = 84
		}
		ship, ok := model.Ships[shipID]
		if !ok {
			return 0, fmt.Errorf("unknown ship %d", shipID)
		}
		hp := spec.HP
		if hp == 0 {
			hp = ship.HP
		}
		shield := spec.Shield
		if shield == 0 && spec.HP == 0 {
			shield = ship.Shield
		}
		speed := spec.Speed
		if speed == 0 {
			speed = ship.Speed
		}
		dmg := spec.Damage
		if dmg == 0 {
			dmg = ship.Damage
		}
		agg := true
		if spec.Aggressive != nil {
			agg = *spec.Aggressive
		}
		id := s.spawnNPC(NPCSpawn{
			ShipID: shipID, Name: cleanName(spec.Name, ship.Name), X: x, Y: y,
			HP: hp, Shield: shield, Speed: speed, Damage: dmg, Aggressive: agg,
		})
		if b, ok := s.net(id); ok {
			b.NoRespawn = noRespawn
			b.Faction = spec.Faction
			s.put(b)
		}
		return id, nil
	case kindBox:
		s.nextObj--
		b := Body{
			NetID: s.nextObj, Kind: kindBox, Name: cleanName(spec.Name, "Bonus box"),
			AssetType: spec.AssetType, NoRespawn: noRespawn,
			Motion: movement.State{X: x, Y: y}, SpawnX: x, SpawnY: y,
		}
		if b.AssetType == 0 {
			b.AssetType = v10.AssetBonusBox
		}
		if spec.Loot != nil {
			b.HasLoot = true
			b.LootCredits = spec.Loot.Credits
			b.LootUridium = spec.Loot.Uridium
			b.LootHonor = spec.Loot.Honor
			b.LootXP = spec.Loot.Experience
			b.LootItem = strings.TrimSpace(spec.Loot.Item)
		}
		s.add(b)
		return b.NetID, nil
	case kindMine:
		dmg := spec.Damage
		if dmg == 0 {
			dmg = 800
		}
		s.nextObj--
		s.add(Body{
			NetID: s.nextObj, Kind: kindMine, Name: cleanName(spec.Name, "Mine"),
			Damage: dmg, NoRespawn: true, Motion: movement.State{X: x, Y: y},
		})
		return s.nextObj, nil
	case kindPortal:
		if spec.ToMap == 0 {
			return 0, fmt.Errorf("portal requires toMap")
		}
		toX, toY := spec.ToX, spec.ToY
		if toX == 0 && toY == 0 {
			toX, toY = 1500, 1500
		}
		graphic := spec.Graphic
		if graphic == 0 {
			graphic = 1
		}
		s.nextObj--
		s.add(Body{
			NetID: s.nextObj, Kind: kindPortal, Name: cleanName(spec.Name, "Portal"),
			Faction: spec.Faction, Graphic: graphic, ToMap: spec.ToMap, ToX: toX, ToY: toY,
			Motion: movement.State{X: x, Y: y},
		})
		return s.nextObj, nil
	case kindStation:
		asset := spec.AssetType
		if asset == 0 {
			asset = v10.AssetRepair
		}
		s.nextObj--
		s.add(Body{
			NetID: s.nextObj, Kind: kindStation, Name: cleanName(spec.Name, "Station"),
			AssetType: asset, Faction: spec.Faction, Motion: movement.State{X: x, Y: y},
		})
		return s.nextObj, nil
	case kindBall:
		s.SpawnBall(x, y)
		return s.nextNeg, nil
	default:
		return 0, fmt.Errorf("unknown kind %q", spec.Kind)
	}
}

func (s *Shard) despawn(netID int32) error {
	b, ok := s.net(netID)
	if !ok {
		return fmt.Errorf("entity %d not found", netID)
	}
	if b.Kind == kindPlayer {
		return fmt.Errorf("cannot despawn a pilot")
	}
	s.hide(b.NetID, b.Kind)
	s.removeNet(b.NetID)
	return nil
}

func (s *Shard) clearKind(kind string) (int, error) {
	k, ok := parseKind(kind)
	if !ok || k == kindPlayer {
		return 0, fmt.Errorf("cannot clear %q", kind)
	}
	var ids []int32
	for _, id := range s.store.IDs() {
		b, ok := s.store.Get(id)
		if ok && b.Kind == k {
			ids = append(ids, b.NetID)
		}
	}
	for _, id := range ids {
		_ = s.despawn(id)
	}
	return len(ids), nil
}

func (s *Shard) inspect() []model.EntityInfo {
	out := make([]model.EntityInfo, 0)
	for _, id := range s.store.IDs() {
		b, ok := s.store.Get(id)
		if ok {
			out = append(out, s.entityInfo(b))
		}
	}
	return out
}

func (s *Shard) smite(netID int32, killerUser int64) error {
	target, ok := s.net(netID)
	if !ok || target.Dead {
		return fmt.Errorf("entity %d not found", netID)
	}
	var killer Body
	if killerUser != 0 {
		killer, _ = s.userBody(killerUser)
	}
	target.HP = 0
	target.Shield = 0
	target.Dead = true
	target.Attacking = false
	s.put(target)
	s.onDeath(killer, target)
	return nil
}

func (s *Shard) entityInfo(b Body) model.EntityInfo {
	info := model.EntityInfo{
		NetID: b.NetID, MapID: s.MapID, Kind: kindName(b.Kind), Name: b.Name,
		X: b.Motion.X, Y: b.Motion.Y, HP: b.HP, Shield: b.Shield, MaxHP: b.MaxHP,
		Damage: b.Damage, Speed: b.Speed, Dead: b.Dead, UserID: b.UserID, ShipID: b.ShipID,
		ToMap: b.ToMap, Aggressive: b.Aggressive, Cloaked: b.Cloaked,
	}
	if b.Kind == kindBox {
		if b.HasLoot {
			info.Loot = &model.Loot{
				Credits: b.LootCredits, Uridium: b.LootUridium, Honor: b.LootHonor,
				Experience: b.LootXP, Item: b.LootItem,
			}
		} else {
			info.Loot = &model.Loot{Credits: 10, Uridium: 1}
		}
	}
	return info
}

func parseKind(kind string) (int, bool) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "player", "pilot":
		return kindPlayer, true
	case "npc", "ship", "streuner":
		return kindNPC, true
	case "box", "bonusbox", "bonus":
		return kindBox, true
	case "mine":
		return kindMine, true
	case "portal", "gate", "jumpgate":
		return kindPortal, true
	case "station":
		return kindStation, true
	case "ball", "spaceball":
		return kindBall, true
	default:
		return 0, false
	}
}

func kindName(kind int) string {
	switch kind {
	case kindPlayer:
		return "player"
	case kindNPC:
		return "npc"
	case kindBox:
		return "box"
	case kindMine:
		return "mine"
	case kindPortal:
		return "portal"
	case kindStation:
		return "station"
	case kindBall:
		return "ball"
	default:
		return "unknown"
	}
}

func cleanName(name, fallback string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = fallback
	}
	name = strings.NewReplacer("|", " ", "#", " ", "%", " ").Replace(name)
	r := []rune(name)
	if len(r) > 32 {
		name = string(r[:32])
	}
	return name
}
