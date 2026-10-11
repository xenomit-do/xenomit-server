package v10

import "testing"

func TestLoginRoundTrip(t *testing.T) {
	in := Login{InstanceID: 1, UserID: 42, FactionID: 1, SessionID: "abc", Version: "1.0"}
	frame, err := EncodeLogin(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := DecodeLogin(frame)
	if err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("got %+v", out)
	}
}

func TestMoveRequestRoundTrip(t *testing.T) {
	in := Move{TargetX: 5000, TargetY: 3200, PosX: 1000, PosY: 800}
	frame, err := EncodeMoveRequest(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := DecodeMoveRequest(frame)
	if err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("got %+v", out)
	}
}

func TestSelectRoundTrip(t *testing.T) {
	in := Select{TargetID: -3, PosX: 10, PosY: 20}
	frame, err := EncodeSelect(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := DecodeSelect(frame)
	if err != nil {
		t.Fatal(err)
	}
	if out.TargetID != -3 {
		t.Fatalf("target %d", out.TargetID)
	}
}

func TestMoveCommandLayout(t *testing.T) {
	frame, err := EncodeMoveCommand(1, 100, 200, 1000)
	if err != nil {
		t.Fatal(err)
	}
	id, err := CommandID(frame)
	if err != nil || id != MoveCommandID {
		t.Fatalf("id %d err %v", id, err)
	}
	// y=200 rotated left 19, userId=1 rotated left 20.
	// Lock the rotated integers that follow the 6455 marker.
	r, err := readRaw(frame)
	if err != nil {
		t.Fatal(err)
	}
	marker, _ := r.ReadInt16()
	if marker != 6455 {
		t.Fatalf("marker %d", marker)
	}
	y, _ := r.ReadInt32()
	uid, _ := r.ReadInt32()
	x, _ := r.ReadInt32()
	tm, _ := r.ReadInt32()
	if y != 104857600 || uid != 1048576 || x != 1073741830 || tm != 524288000 {
		t.Fatalf("rotated fields y=%d uid=%d x=%d t=%d", y, uid, x, tm)
	}
}

func TestLegacyAndShipFrames(t *testing.T) {
	leg, err := EncodeLegacy("0|A|STD|hello")
	if err != nil {
		t.Fatal(err)
	}
	msg, err := DecodeLegacy(leg)
	if err != nil || msg != "0|A|STD|hello" {
		t.Fatalf("legacy %q %v", msg, err)
	}
	init, err := EncodeShipInit(ShipInit{UserID: 1, Name: "Pilot", ShipType: "ship10", Speed: 300, MapID: 1, X: 10, Y: 20, Unknown2: true, Unknown3: true})
	if err != nil {
		t.Fatal(err)
	}
	id, err := CommandID(init)
	if err != nil || id != ShipInitID {
		t.Fatalf("ship init id %d", id)
	}
	create, err := EncodeShipCreate(ShipCreate{UserID: -1, Name: "Streuner", ShipType: "84", NPC: true, Relation: RelationWar, X: 1, Y: 2})
	if err != nil {
		t.Fatal(err)
	}
	id, err = CommandID(create)
	if err != nil || id != ShipCreateID {
		t.Fatalf("ship create id %d", id)
	}
}
