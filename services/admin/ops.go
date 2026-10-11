// Package adminops is the live-ops surface shared by HTTP and chat.
package adminops

import (
	"fmt"
	"strconv"
	"time"

	"github.com/xenomit/server/pkg/events"
	"github.com/xenomit/server/pkg/model"
	"github.com/xenomit/server/services/player"
	"github.com/xenomit/server/services/world"
)

// Service performs admin operations and writes an admin.audit event for each one.
type Service struct {
	World    *world.Controller
	Accounts *player.Service
	Log      *events.Log
}

func New(w *world.Controller, accounts *player.Service, log *events.Log) *Service {
	return &Service{World: w, Accounts: accounts, Log: log}
}

// GiveRequest adds or removes currency, items, premium, or the admin flag.
// Negative credits and uridium are taken back when the balance allows it.
type GiveRequest struct {
	UserID     int64   `json:"userId"`
	Name       string  `json:"name"`
	Credits    int64   `json:"credits"`
	Uridium    int64   `json:"uridium"`
	Honor      float64 `json:"honor"`
	Experience float64 `json:"experience"`
	Item       string  `json:"item"`
	Premium    *bool   `json:"premium"`
	Admin      *bool   `json:"admin"`
}

// GiveResult is the account after the grant.
type GiveResult struct {
	UserID     int64   `json:"userId"`
	PilotName  string  `json:"pilotName"`
	Credits    int64   `json:"credits"`
	Uridium    int64   `json:"uridium"`
	Honor      float64 `json:"honor"`
	Experience float64 `json:"experience"`
	Premium    bool    `json:"premium"`
	Admin      bool    `json:"admin"`
	Online     bool    `json:"online"`
	Item       string  `json:"item,omitempty"`
}

func (s *Service) Give(actor int64, req GiveRequest) (GiveResult, error) {
	acc, err := s.resolve(req.UserID, req.Name)
	if err != nil {
		return GiveResult{}, err
	}
	if req.Credits > 1_000_000_000_000 || req.Credits < -1_000_000_000_000 || req.Uridium > 1_000_000_000_000 || req.Uridium < -1_000_000_000_000 {
		return GiveResult{}, fmt.Errorf("amount is out of range")
	}
	if req.Credits != 0 || req.Uridium != 0 || req.Honor != 0 || req.Experience != 0 {
		tx := model.Tx{
			ID:         fmt.Sprintf("admin-give-%d-%d", acc.UserID, time.Now().UnixNano()),
			UserID:     acc.UserID,
			Reason:     "admin",
			Credits:    req.Credits,
			Uridium:    req.Uridium,
			Honor:      req.Honor,
			Experience: req.Experience,
		}
		if err := s.Accounts.ApplyTx(tx); err != nil {
			return GiveResult{}, err
		}
		if s.Log != nil {
			s.Log.Append("economy.tx", tx.ID, tx)
		}
	}
	if req.Item != "" {
		if err := s.Accounts.AddItem(acc.UserID, req.Item); err != nil {
			return GiveResult{}, err
		}
	}
	if req.Premium != nil {
		if err := s.Accounts.SetPremium(acc.UserID, *req.Premium); err != nil {
			return GiveResult{}, err
		}
	}
	if req.Admin != nil {
		if err := s.Accounts.SetAdmin(acc.UserID, *req.Admin); err != nil {
			return GiveResult{}, err
		}
	}
	snap, err := s.Accounts.Snapshot(acc.UserID)
	if err != nil {
		return GiveResult{}, err
	}
	note := fmt.Sprintf("Admin grant: %d credits, %d uridium.", req.Credits, req.Uridium)
	if req.Item != "" {
		note += " Item " + req.Item + "."
	}
	online := s.World != nil && s.World.SyncPilot(acc.UserID, snap, note)
	s.Audit(actor, "give", req)
	got, _ := s.Accounts.Get(acc.UserID)
	return GiveResult{
		UserID: got.UserID, PilotName: got.PilotName, Credits: got.Credits, Uridium: got.Uridium,
		Honor: got.Honor, Experience: got.XP, Premium: got.Premium, Admin: got.Admin,
		Online: online, Item: req.Item,
	}, nil
}

func (s *Service) Spawn(actor int64, spec model.SpawnSpec) ([]int32, error) {
	if s.World == nil {
		return nil, fmt.Errorf("world is not running")
	}
	ids, err := s.World.Spawn(spec)
	if err != nil {
		return nil, err
	}
	s.Audit(actor, "spawn", map[string]any{"spec": spec, "netIds": ids})
	return ids, nil
}

func (s *Service) Despawn(actor int64, mapID int, netID int32) error {
	if err := s.World.Despawn(mapID, netID); err != nil {
		return err
	}
	s.Audit(actor, "despawn", map[string]any{"mapId": mapID, "netId": netID})
	return nil
}

func (s *Service) Clear(actor int64, mapID int, kind string) (int, error) {
	n, err := s.World.Clear(mapID, kind)
	if err != nil {
		return 0, err
	}
	s.Audit(actor, "clear", map[string]any{"mapId": mapID, "kind": kind, "removed": n})
	return n, nil
}

func (s *Service) Heal(actor, userID int64) error {
	if err := s.World.Heal(userID); err != nil {
		return err
	}
	s.Audit(actor, "heal", userID)
	return nil
}

// KillRequest destroys a pilot or a spawned entity.
type KillRequest struct {
	MapID        int   `json:"mapId"`
	NetID        int32 `json:"netId"`
	UserID       int64 `json:"userId"`
	KillerUserID int64 `json:"killerUserId"`
}

func (s *Service) Kill(actor int64, req KillRequest) error {
	mapID, netID := req.MapID, req.NetID
	if req.UserID != 0 {
		info, ok := s.World.Where(req.UserID)
		if !ok {
			return fmt.Errorf("user %d is not in-game", req.UserID)
		}
		mapID, netID = info.MapID, info.NetID
	}
	if mapID == 0 || netID == 0 {
		return fmt.Errorf("mapId and netId are required")
	}
	if err := s.World.Kill(mapID, netID, req.KillerUserID); err != nil {
		return err
	}
	s.Audit(actor, "kill", req)
	return nil
}

func (s *Service) Vitals(actor int64, v model.Vitals) (string, error) {
	if v.UserID == 0 {
		return "", fmt.Errorf("userId is required")
	}
	persisted := false
	if v.ShipID != nil {
		ship, ok := model.Ships[*v.ShipID]
		if !ok {
			return "", fmt.Errorf("unknown ship %d", *v.ShipID)
		}
		v.ShipType = ship.LootID
		if !ship.NPC {
			if err := s.Accounts.SetShip(v.UserID, *v.ShipID); err != nil {
				return "", err
			}
			persisted = true
		}
		if v.HP == nil && v.MaxHP == nil {
			hp, shield, speed, dmg := ship.HP, ship.Shield, ship.Speed, ship.Damage
			v.HP, v.MaxHP = &hp, &hp
			v.Shield, v.MaxShield = &shield, &shield
			if v.Speed == nil {
				v.Speed = &speed
			}
			if v.Damage == nil {
				v.Damage = &dmg
			}
		}
	}
	if v.Speed != nil && (*v.Speed < 1 || *v.Speed > 2000) {
		return "", fmt.Errorf("speed must be 1..2000")
	}
	if err := s.World.Vitals(v); err != nil {
		if persisted {
			s.Audit(actor, "vitals", v)
			return "ship saved; pilot is not in-game", nil
		}
		return "", err
	}
	s.Audit(actor, "vitals", v)
	return "updated", nil
}

func (s *Service) Teleport(actor, userID int64, mapID int, x, y int32) error {
	if _, ok := s.World.PresenceOf(userID); !ok {
		return fmt.Errorf("user %d is not in-game", userID)
	}
	if mapID <= 0 {
		return fmt.Errorf("mapId is required")
	}
	if err := s.World.Transfer(userID, mapID, x, y); err != nil {
		return err
	}
	s.Audit(actor, "teleport", map[string]any{"userId": userID, "mapId": mapID, "x": x, "y": y})
	return nil
}

func (s *Service) Summon(actor, userID, toUserID int64) error {
	here, ok := s.World.Where(toUserID)
	if !ok {
		return fmt.Errorf("user %d is not in-game", toUserID)
	}
	return s.Teleport(actor, userID, here.MapID, here.X+80, here.Y)
}

func (s *Service) Message(actor, userID int64, text string) error {
	if text == "" {
		return fmt.Errorf("message is required")
	}
	if !s.World.Tell(userID, text) {
		return fmt.Errorf("user %d is not in-game", userID)
	}
	s.Audit(actor, "message", map[string]any{"userId": userID, "message": text})
	return nil
}

func (s *Service) Peace(actor int64, mapID int, seconds int) error {
	if mapID <= 0 {
		return fmt.Errorf("mapId is required")
	}
	if seconds <= 0 {
		s.World.SetPeace(mapID, time.Time{})
		s.World.BroadcastAll(fmt.Sprintf("Peace ended on map %d.", mapID))
	} else {
		s.World.SetPeace(mapID, time.Now().Add(time.Duration(seconds)*time.Second))
		s.World.BroadcastAll(fmt.Sprintf("Peace on map %d for %d seconds.", mapID, seconds))
	}
	s.Audit(actor, "peace", map[string]any{"mapId": mapID, "seconds": seconds})
	return nil
}

func (s *Service) Radiation(actor int64, mapID int, x, y, radius int32) error {
	if mapID <= 0 {
		return fmt.Errorf("mapId is required")
	}
	if radius <= 0 {
		s.World.ClearRadiation(mapID)
		s.World.BroadcastAll(fmt.Sprintf("Radiation cleared on map %d.", mapID))
	} else {
		s.World.SetRadiation(mapID, x, y, radius)
		s.World.BroadcastAll(fmt.Sprintf("Radiation on map %d at %d,%d radius %d.", mapID, x, y, radius))
	}
	s.Audit(actor, "radiation", map[string]any{"mapId": mapID, "x": x, "y": y, "radius": radius})
	return nil
}

func (s *Service) Unban(actor, userID int64) error {
	if err := s.Accounts.Ban(userID, "", false); err != nil {
		return err
	}
	s.Audit(actor, "unban", userID)
	return nil
}

func (s *Service) Audit(actor int64, action string, detail any) {
	if s.Log == nil {
		return
	}
	s.Log.Append("admin.audit", strconv.FormatInt(actor, 10), map[string]any{
		"actor": actor, "action": action, "detail": detail,
	})
}

func (s *Service) Recent(n int) []events.Event {
	if s.Log == nil || n <= 0 {
		return nil
	}
	var out []events.Event
	for _, ev := range s.Log.Records() {
		if ev.Topic == "admin.audit" {
			out = append(out, ev)
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

func (s *Service) resolve(userID int64, name string) (player.Account, error) {
	if userID != 0 {
		acc, ok := s.Accounts.Get(userID)
		if !ok {
			return player.Account{}, player.ErrMissing
		}
		return acc, nil
	}
	if name == "" {
		return player.Account{}, fmt.Errorf("userId or name is required")
	}
	acc, ok := s.Accounts.ByName(name)
	if !ok {
		return player.Account{}, fmt.Errorf("no pilot named %s", name)
	}
	return acc, nil
}
