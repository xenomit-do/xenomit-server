package world

import (
	"testing"
	"time"

	"github.com/xenomit/server/pkg/model"
	"github.com/xenomit/server/pkg/protocol/v10"
)

func TestKillStreuner(t *testing.T) {
	s := newShard(mapByID(1))
	now := time.Unix(1_700_000_000, 0)
	s.clock = func() time.Time { return now }
	s.emitFn = func(string, []byte) {}
	var rewards int
	s.reward = func(model.Tx) error {
		rewards++
		return nil
	}
	snap := model.Snapshot{
		UserID: 7, PilotName: "Pilot", ShipID: 10, FactionID: 1, MapID: 1,
		X: 3900, Y: 4000, Speed: 300, HP: 4000, MaxHP: 4000, Damage: 5000,
	}
	s.join(model.Ingress{Type: "join", SessionID: "s", UserID: 7, Snapshot: &snap})
	var npc int32
	for _, id := range s.store.IDs() {
		b, ok := s.store.Get(id)
		if ok && b.Kind == kindNPC {
			npc = b.NetID
			break
		}
	}
	if npc == 0 {
		t.Fatal("no streuner")
	}
	pilot, _ := s.userBody(7)
	nb, _ := s.net(npc)
	pilot.Motion.X = nb.Motion.X + 40
	pilot.Motion.Y = nb.Motion.Y
	s.put(pilot)
	sel, err := v10.EncodeSelect(v10.Select{TargetID: npc})
	if err != nil {
		t.Fatal(err)
	}
	s.packet(7, sel)
	atk, err := v10.EncodeEmpty(v10.AttackLaserID)
	if err != nil {
		t.Fatal(err)
	}
	s.packet(7, atk)
	now = now.Add(time.Second)
	s.Step(time.Second)
	b, ok := s.net(npc)
	if !ok || !b.Dead {
		t.Fatalf("streuner still up hp=%d dead=%v", b.HP, b.Dead)
	}
	if rewards == 0 {
		t.Fatal("expected a kill reward")
	}
}

func TestMoveCommandBroadcast(t *testing.T) {
	s := newShard(mapByID(1))
	now := time.Unix(1_700_000_000, 0)
	s.clock = func() time.Time { return now }
	var got int16
	s.emitFn = func(_ string, frame []byte) {
		id, err := v10.CommandID(frame)
		if err == nil && id == v10.MoveCommandID {
			got = id
		}
	}
	snap := model.Snapshot{UserID: 3, PilotName: "P", ShipID: 1, X: 1000, Y: 1000, Speed: 300, HP: 1000, MaxHP: 1000, Damage: 10}
	s.join(model.Ingress{SessionID: "s", UserID: 3, Snapshot: &snap})
	frame, err := v10.EncodeMoveRequest(v10.Move{TargetX: 1400, TargetY: 1000, PosX: 1000, PosY: 1000})
	if err != nil {
		t.Fatal(err)
	}
	s.packet(3, frame)
	if got != v10.MoveCommandID {
		t.Fatal("move was not broadcast")
	}
	b, _ := s.userBody(3)
	if !b.Motion.Moving {
		t.Fatal("pilot is not moving")
	}
}

func TestAdminBoxLoot(t *testing.T) {
	s := newShard(mapByID(1))
	now := time.Unix(1_700_000_000, 0)
	s.clock = func() time.Time { return now }
	s.emitFn = func(string, []byte) {}
	var got model.Tx
	s.reward = func(tx model.Tx) error {
		got = tx
		return nil
	}
	var item string
	s.grantItem = func(_ int64, id string) error {
		item = id
		return nil
	}
	ids, err := s.spawnSpec(model.SpawnSpec{
		Kind: "box", X: 1000, Y: 1000, Name: "Test box",
		Loot: &model.Loot{Credits: 4242, Uridium: 9, Honor: 4, Experience: 15, Item: "lf-3"},
	})
	if err != nil || len(ids) != 1 {
		t.Fatalf("spawn %v %v", ids, err)
	}
	snap := model.Snapshot{UserID: 7, PilotName: "Pilot", ShipID: 10, FactionID: 1, X: 1000, Y: 1000, Speed: 300, HP: 4000, MaxHP: 4000}
	s.join(model.Ingress{Type: "join", SessionID: "s", UserID: 7, Snapshot: &snap})
	frame, err := v10.EncodeEmpty(v10.CollectBoxID)
	if err != nil {
		t.Fatal(err)
	}
	s.packet(7, frame)
	if _, ok := s.net(ids[0]); ok {
		t.Fatal("custom box should not respawn")
	}
	if got.Credits != 4242 || got.Uridium != 9 || got.Honor != 4 || got.Experience != 15 {
		t.Fatalf("loot tx %+v", got)
	}
	if item != "lf-3" {
		t.Fatalf("item %q", item)
	}
	pilot, _ := s.userBody(7)
	if pilot.Credits != 4242 || pilot.Uridium != 9 {
		t.Fatalf("pilot balance %d %d", pilot.Credits, pilot.Uridium)
	}
}
