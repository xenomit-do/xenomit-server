// Package v10 is the DarkOrbit 10.0 binary command catalog used by the
// vertical slice: login, movement, ships, lasers, portals, and assets.
// Field order and rotations are taken from the 10.0 emulator's readCommand
// and write methods. Nested modules are embedded without a length prefix.
package v10

import (
	"github.com/xenomit/server/pkg/protocol/codec"
)

const (
	LoginRequestID     int16 = 10996
	LegacyModuleID     int16 = 4224
	MoveRequestID      int16 = 17063
	ShipSelectID       int16 = 18091
	CollectBoxID       int16 = 6532
	AttackLaserID      int16 = 31106
	AttackAbortLaserID int16 = 1528
	AttackRocketID     int16 = 22801
	PortalJumpID       int16 = 9253
	LogoutCancelID     int16 = 10343
	MoveCommandID      int16 = 29819
	ShipRemoveID       int16 = 19797
	SetSpeedID         int16 = 4082
	AttackLaserRunID   int16 = 28127
	AttackHitID        int16 = 3758
	ShipCreateID       int16 = 7270
	ShipInitID         int16 = 7511
	AssetCreateID      int16 = 13718
	AssetRemoveID      int16 = 3397
	CreatePortalID     int16 = 20288
	RemovePortalID     int16 = 31113
	ActivatePortalID   int16 = 2310
	ClanRelationID     int16 = 27378
	Class11dID         int16 = 14030
	VisualModifierID   int16 = 12647
	AttackTypeID       int16 = 32205
	AssetTypeID        int16 = 15758
)

const (
	RelationNone    int16 = 0
	RelationAlly    int16 = 1
	RelationNAP     int16 = 2
	RelationWar     int16 = 3
	AttackLaser     int16 = 1
	AttackMine      int16 = 2
	AttackRadiation int16 = 3
	AttackRocket    int16 = 0
	AssetBonusBox   int16 = 2
	AssetRepair     int16 = 53
	AssetCompany    int16 = 46
)

// Login is LoginRequest (10996).
type Login struct {
	InstanceID int32
	UserID     int32
	FactionID  int16
	SessionID  string
	Version    string
}

// EncodeLogin builds the packet a client sends. Rotations are the inverse of DecodeLogin.
func EncodeLogin(l Login) ([]byte, error) {
	w := codec.NewWriter(LoginRequestID)
	w.WriteInt32(codec.Mix(l.InstanceID, 24, 8))
	w.WriteInt32(codec.Mix(l.UserID, 24, 8))
	w.WriteInt16(l.FactionID)
	w.WriteUTF(l.SessionID)
	w.WriteUTF(l.Version)
	return w.Frame()
}

func DecodeLogin(frame []byte) (Login, error) {
	r, err := codec.NewReader(frame)
	if err != nil {
		return Login{}, err
	}
	if r.ID != LoginRequestID {
		return Login{}, codec.ErrFrame
	}
	var l Login
	inst, err := r.ReadInt32()
	if err != nil {
		return Login{}, err
	}
	l.InstanceID = codec.Mix(inst, 8, 24)
	uid, err := r.ReadInt32()
	if err != nil {
		return Login{}, err
	}
	l.UserID = codec.Mix(uid, 8, 24)
	l.FactionID, err = r.ReadInt16()
	if err != nil {
		return Login{}, err
	}
	l.SessionID, err = r.ReadUTF()
	if err != nil {
		return Login{}, err
	}
	l.Version, err = r.ReadUTF()
	return l, err
}

// Move is MoveRequest (17063).
type Move struct {
	TargetX, TargetY int32
	PosX, PosY       int32
}

func EncodeMoveRequest(m Move) ([]byte, error) {
	w := codec.NewWriter(MoveRequestID)
	w.WriteInt32(codec.Mix(m.TargetY, 24, 8))
	w.WriteInt32(codec.Mix(m.PosX, 17, 15))
	w.WriteInt16(0)
	w.WriteInt32(codec.Mix(m.TargetX, 4, 28))
	w.WriteInt32(codec.Mix(m.PosY, 27, 5))
	return w.Frame()
}

func DecodeMoveRequest(frame []byte) (Move, error) {
	r, err := codec.NewReader(frame)
	if err != nil {
		return Move{}, err
	}
	if r.ID != MoveRequestID {
		return Move{}, codec.ErrFrame
	}
	var m Move
	ty, err := r.ReadInt32()
	if err != nil {
		return Move{}, err
	}
	m.TargetY = codec.Mix(ty, 8, 24)
	px, err := r.ReadInt32()
	if err != nil {
		return Move{}, err
	}
	m.PosX = codec.Mix(px, 15, 17)
	if _, err = r.ReadInt16(); err != nil {
		return Move{}, err
	}
	tx, err := r.ReadInt32()
	if err != nil {
		return Move{}, err
	}
	m.TargetX = codec.Mix(tx, 28, 4)
	py, err := r.ReadInt32()
	if err != nil {
		return Move{}, err
	}
	m.PosY = codec.Mix(py, 5, 27)
	return m, nil
}

// Select is ShipSelectRequest (18091). Only TargetID is used by the sim.
type Select struct {
	TargetID int32
	PosX     int32
	PosY     int32
}

func EncodeSelect(s Select) ([]byte, error) {
	w := codec.NewWriter(ShipSelectID)
	w.WriteInt16(0)
	w.WriteInt32(codec.Mix(0, 27, 5))
	w.WriteInt32(codec.Mix(s.PosX, 20, 12))
	w.WriteInt32(codec.Mix(s.PosY, 14, 18))
	w.WriteInt32(codec.Mix(s.TargetID, 14, 18))
	w.WriteInt32(codec.Mix(0, 7, 25))
	return w.Frame()
}

func DecodeSelect(frame []byte) (Select, error) {
	r, err := codec.NewReader(frame)
	if err != nil {
		return Select{}, err
	}
	if r.ID != ShipSelectID {
		return Select{}, codec.ErrFrame
	}
	if _, err = r.ReadInt16(); err != nil {
		return Select{}, err
	}
	if _, err = r.ReadInt32(); err != nil {
		return Select{}, err
	}
	px, err := r.ReadInt32()
	if err != nil {
		return Select{}, err
	}
	py, err := r.ReadInt32()
	if err != nil {
		return Select{}, err
	}
	tid, err := r.ReadInt32()
	if err != nil {
		return Select{}, err
	}
	if _, err = r.ReadInt32(); err != nil {
		return Select{}, err
	}
	return Select{
		PosX:     codec.Mix(px, 12, 20),
		PosY:     codec.Mix(py, 18, 14),
		TargetID: codec.Mix(tid, 18, 14),
	}, nil
}

func DecodeCollect(frame []byte) (hash string, err error) {
	r, err := codec.NewReader(frame)
	if err != nil {
		return "", err
	}
	for i := 0; i < 4; i++ {
		if _, err = r.ReadInt32(); err != nil {
			return "", err
		}
	}
	return r.ReadUTF()
}

func EncodeEmpty(id int16) ([]byte, error) {
	return codec.NewWriter(id).Frame()
}

func EncodeLegacy(message string) ([]byte, error) {
	w := codec.NewWriter(LegacyModuleID)
	w.WriteUTF(message)
	return w.Frame()
}

func DecodeLegacy(frame []byte) (string, error) {
	r, err := codec.NewReader(frame)
	if err != nil {
		return "", err
	}
	if r.ID != LegacyModuleID {
		return "", codec.ErrFrame
	}
	return r.ReadUTF()
}

func EncodeMoveCommand(userID, x, y, timeMs int32) ([]byte, error) {
	w := codec.NewWriter(MoveCommandID)
	w.WriteInt16(6455)
	w.WriteInt32(codec.Mix(y, 19, 13))
	w.WriteInt32(codec.Mix(userID, 20, 12))
	w.WriteInt32(codec.Mix(x, 28, 4))
	w.WriteInt32(codec.Mix(timeMs, 19, 13))
	return w.Frame()
}

func EncodeShipRemove(userID int32) ([]byte, error) {
	w := codec.NewWriter(ShipRemoveID)
	w.WriteInt32(codec.Mix(userID, 2, 30))
	w.WriteInt16(-9955)
	return w.Frame()
}

func EncodeSetSpeed(speed, display int32) ([]byte, error) {
	w := codec.NewWriter(SetSpeedID)
	w.WriteInt32(codec.Mix(display, 2, 30))
	w.WriteInt32(codec.Mix(speed, 8, 24))
	w.WriteInt16(-5122)
	return w.Frame()
}

func EncodeLaserRun(attacker, target, color int32, diminished, skilled bool) ([]byte, error) {
	w := codec.NewWriter(AttackLaserRunID)
	w.WriteBool(diminished)
	w.WriteInt32(codec.Mix(target, 18, 14))
	w.WriteInt32(codec.Mix(color, 10, 22))
	w.WriteInt32(codec.Mix(attacker, 22, 10))
	w.WriteBool(skilled)
	return w.Frame()
}

func clanRelation(kind int16) []byte {
	w := codec.NewWriter(ClanRelationID)
	w.WriteInt16(kind)
	return w.Inner()
}

func attackType(kind int16) []byte {
	w := codec.NewWriter(AttackTypeID)
	w.WriteInt16(kind)
	w.WriteInt16(-6647)
	return w.Inner()
}

func writeAssetType(kind int16) []byte {
	w := codec.NewWriter(AssetTypeID)
	w.WriteInt16(-2023)
	w.WriteInt16(-17738)
	w.WriteInt16(kind)
	return w.Inner()
}

func class11d(mode int16) []byte {
	w := codec.NewWriter(Class11dID)
	w.WriteInt16(-21374)
	w.WriteInt16(mode)
	return w.Inner()
}

// EncodeHit is AttackHitCommand. hp/shield/nano are the target's values after damage.
func EncodeHit(kind int16, attacker, target, hp, shield, nano, damage int32, skilled bool) ([]byte, error) {
	w := codec.NewWriter(AttackHitID)
	w.WriteInt32(codec.Mix(nano, 2, 30))
	w.WriteInt32(codec.Mix(attacker, 19, 13))
	w.WriteBool(skilled)
	w.Raw(attackType(kind))
	w.WriteInt32(codec.Mix(target, 15, 17))
	w.WriteInt32(codec.Mix(damage, 30, 2))
	w.WriteInt32(codec.Mix(shield, 17, 15))
	w.WriteInt32(codec.Mix(hp, 9, 23))
	return w.Frame()
}

// ShipCreate is the appearance packet for every ship except the local player.
type ShipCreate struct {
	UserID        int32
	ShipType      string
	Expansion     int32
	ClanTag       string
	Name          string
	X, Y          int32
	FactionID     int32
	ClanID        int32
	Rank          int32
	Warn          bool
	Relation      int16
	Rings         int32
	UseSystemFont bool
	NPC           bool
	Cloaked       bool
	MotherID      int32
	PositionIndex int32
}

func EncodeShipCreate(s ShipCreate) ([]byte, error) {
	w := codec.NewWriter(ShipCreateID)
	w.WriteUTF(s.Name)
	w.WriteInt32(codec.Mix(s.Rings, 19, 13))
	w.WriteBool(s.UseSystemFont)
	w.WriteInt32(codec.Mix(s.FactionID, 25, 7))
	w.WriteInt32(codec.Mix(s.Expansion, 6, 26))
	w.WriteUTF(s.ShipType)
	w.WriteInt32(codec.Mix(s.Rank, 28, 4))
	w.WriteBool(s.NPC)
	w.WriteInt32(codec.Mix(s.ClanID, 3, 29))
	w.Raw(clanRelation(s.Relation))
	w.WriteBool(s.Warn)
	w.WriteInt32(codec.Mix(s.MotherID, 16, 16))
	w.WriteUTF(s.ClanTag)
	w.WriteInt32(0)
	w.WriteBool(s.Cloaked)
	w.WriteInt32(codec.Mix(s.UserID, 18, 14))
	w.WriteInt32(codec.Mix(s.PositionIndex, 3, 29))
	w.WriteInt32(codec.Mix(s.X, 4, 28))
	w.Raw(class11d(0))
	w.WriteInt32(codec.Mix(s.Y, 9, 23))
	return w.Frame()
}

// ShipInit is ShipInitializationCommand for the local player.
type ShipInit struct {
	UserID                               int32
	Name, ShipType, ClanTag              string
	Speed, Shield, MaxShield             int32
	HP, MaxHP, Cargo, MaxCargo           int32
	Nano, MaxNano, X, Y, MapID           int32
	FactionID, ClanID, Expansion, Rank   int32
	Premium, Cloaked, Unknown2, Unknown3 bool
	XP, Honor                            float64
	Level                                int16
	Credits, Uridium                     int64
	Jackpot                              float32
	Rings                                int32
}

func EncodeShipInit(s ShipInit) ([]byte, error) {
	w := codec.NewWriter(ShipInitID)
	w.WriteInt32(codec.Mix(s.FactionID, 12, 20))
	w.WriteUTF(s.ShipType)
	w.WriteBool(s.Unknown3)
	w.WriteInt16(-22408)
	w.WriteInt32(codec.Mix(s.MaxCargo, 16, 16))
	w.WriteInt32(codec.Mix(int32(s.Level), 9, 23))
	w.WriteInt32(codec.Mix(s.MaxNano, 18, 14))
	w.WriteBool(s.Cloaked)
	w.WriteInt32(codec.Mix(s.Speed, 22, 10))
	w.WriteFloat64(float64(s.Uridium))
	w.WriteInt32(codec.Mix(s.MapID, 23, 9))
	w.WriteInt32(codec.Mix(s.Shield, 22, 10))
	w.WriteInt32(codec.Mix(s.Expansion, 8, 24))
	w.WriteFloat64(float64(s.Credits))
	w.WriteInt32(codec.Mix(s.Nano, 19, 13))
	w.WriteUTF(s.Name)
	w.WriteInt32(codec.Mix(s.X, 27, 5))
	w.WriteFloat64(s.XP)
	w.WriteBool(s.Premium)
	w.WriteUTF(s.ClanTag)
	w.WriteInt32(codec.Mix(s.Cargo, 22, 10))
	w.WriteInt32(codec.Mix(s.MaxShield, 6, 26))
	w.WriteInt32(codec.Mix(s.MaxHP, 27, 5))
	w.WriteInt32(codec.Mix(s.UserID, 1, 31))
	w.WriteInt32(codec.Mix(s.Rings, 31, 1))
	w.WriteFloat64(s.Honor)
	w.WriteInt32(codec.Mix(s.ClanID, 24, 8))
	w.WriteInt32(codec.Mix(s.Rank, 3, 29))
	w.WriteFloat32(s.Jackpot)
	w.WriteInt32(codec.Mix(s.HP, 3, 29))
	w.WriteInt32(0)
	w.WriteInt32(codec.Mix(s.Y, 1, 31))
	w.WriteBool(s.Unknown2)
	return w.Frame()
}

func EncodeAssetCreate(assetType int16, name string, assetID, design, x, y, faction int32) ([]byte, error) {
	w := codec.NewWriter(AssetCreateID)
	w.WriteInt32(codec.Mix(x, 22, 10))
	w.WriteInt32(codec.Mix(y, 31, 1))
	w.WriteUTF(name)
	w.WriteInt32(0)
	w.WriteBool(false)
	w.WriteInt32(codec.Mix(0, 15, 17))
	w.WriteInt32(codec.Mix(design, 16, 16))
	w.WriteBool(false)
	w.WriteInt32(codec.Mix(faction, 29, 3))
	w.WriteBool(false)
	w.WriteInt16(24441)
	w.Raw(clanRelation(RelationNone))
	w.WriteUTF("")
	w.Raw(writeAssetType(assetType))
	w.WriteInt32(codec.Mix(0, 22, 10))
	w.WriteBool(false)
	w.WriteInt32(codec.Mix(assetID, 27, 5))
	return w.Frame()
}

func EncodeAssetRemove(assetType int16, id int32) ([]byte, error) {
	w := codec.NewWriter(AssetRemoveID)
	w.Raw(writeAssetType(assetType))
	w.WriteInt16(-17782)
	w.WriteInt32(codec.Mix(id, 15, 17))
	return w.Frame()
}

func EncodeCreatePortal(id, faction, graphic, x, y int32, working, visible bool) ([]byte, error) {
	w := codec.NewWriter(CreatePortalID)
	w.WriteBool(visible)
	w.WriteInt32(codec.Mix(faction, 23, 9))
	w.WriteBool(working)
	w.WriteInt32(codec.Mix(id, 22, 10))
	w.WriteInt32(codec.Mix(graphic, 15, 17))
	w.WriteInt32(codec.Mix(x, 20, 12))
	w.WriteInt32(codec.Mix(y, 4, 28))
	w.WriteInt32(0)
	return w.Frame()
}

func EncodeRemovePortal(id int32) ([]byte, error) {
	w := codec.NewWriter(RemovePortalID)
	w.WriteInt32(codec.Mix(id, 10, 22))
	w.WriteInt16(-12062)
	return w.Frame()
}

func EncodeActivatePortal(mapID, portalID int32) ([]byte, error) {
	w := codec.NewWriter(ActivatePortalID)
	w.WriteInt32(codec.Mix(mapID, 29, 3))
	w.WriteInt32(codec.Mix(portalID, 15, 17))
	w.WriteInt16(20220)
	return w.Frame()
}

// CommandID returns the command id of a framed packet.
func CommandID(frame []byte) (int16, error) {
	r, err := codec.NewReader(frame)
	if err != nil {
		return 0, err
	}
	return r.ID, nil
}
