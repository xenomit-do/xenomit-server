// Package model holds snapshots shared by player, world, and webapi.
package model

// Ship is a row of the ship catalog (server_ships).
type Ship struct {
	ID     int
	LootID string
	Name   string
	HP     int32
	Shield int32
	Speed  int32
	Cargo  int32
	Damage int32
	NPC    bool
}

// Ships is the catalog the simulation boots with. Ids follow the client:
// 1 Phoenix, 10 Goliath, 84 Streuner.
var Ships = map[int]Ship{
	1:  {ID: 1, LootID: "ship1", Name: "Phoenix", HP: 4000, Shield: 1000, Speed: 320, Cargo: 100, Damage: 65},
	10: {ID: 10, LootID: "ship10", Name: "Goliath", HP: 25600, Shield: 10000, Speed: 300, Cargo: 1500, Damage: 80},
	84: {ID: 84, LootID: "ship84", Name: "Streuner", HP: 800, Shield: 200, Speed: 280, Damage: 20, NPC: true},
}

func ShipByID(id int) Ship {
	if s, ok := Ships[id]; ok {
		return s
	}
	return Ships[1]
}

// Snapshot is the player document a shard needs at login and on transfer.
type Snapshot struct {
	UserID     int64   `json:"userId"`
	PilotName  string  `json:"pilotName"`
	ShipID     int     `json:"shipId"`
	LootID     string  `json:"lootId"`
	FactionID  int     `json:"factionId"`
	ClanID     int     `json:"clanId"`
	ClanTag    string  `json:"clanTag"`
	RankID     int     `json:"rankId"`
	MapID      int     `json:"mapId"`
	X          int32   `json:"x"`
	Y          int32   `json:"y"`
	Speed      int32   `json:"speed"`
	HP         int32   `json:"hp"`
	MaxHP      int32   `json:"maxHp"`
	Shield     int32   `json:"shield"`
	MaxShield  int32   `json:"maxShield"`
	Nano       int32   `json:"nano"`
	MaxNano    int32   `json:"maxNano"`
	Damage     int32   `json:"damage"`
	Credits    int64   `json:"credits"`
	Uridium    int64   `json:"uridium"`
	Honor      float64 `json:"honor"`
	Experience float64 `json:"experience"`
	Premium    bool    `json:"premium"`
	Level      int16   `json:"level"`
	Admin      bool    `json:"admin"`
	SessionID  string  `json:"sessionId"`
}

// Tx is one ledger mutation. ID is the idempotency key.
type Tx struct {
	ID         string  `json:"id"`
	UserID     int64   `json:"userId"`
	Reason     string  `json:"reason"`
	Credits    int64   `json:"credits"`
	Uridium    int64   `json:"uridium"`
	Honor      float64 `json:"honor"`
	Experience float64 `json:"experience"`
}

// HomeMap is the company base map: MMO 1, EIC 5, VRU 9.
func HomeMap(faction int) int {
	switch faction {
	case 2:
		return 5
	case 3:
		return 9
	default:
		return 1
	}
}

// LevelFromXP is a simple curve used until the full rank tables are ported.
func LevelFromXP(xp float64) int16 {
	level := int16(1 + int(xp/10000))
	if level > 32 {
		return 32
	}
	if level < 1 {
		return 1
	}
	return level
}
