package model

// Loot is what a bonus box pays when a pilot collects it.
// A nil Loot on a spawn means the map default (10 credits, 1 uridium).
type Loot struct {
	Credits    int64   `json:"credits"`
	Uridium    int64   `json:"uridium"`
	Honor      float64 `json:"honor"`
	Experience float64 `json:"experience"`
	Item       string  `json:"item,omitempty"`
}

// SpawnSpec is an admin request to place entities on a map.
// NearUserID, when set, uses that pilot's map. X and Y of 0 then mean
// "just beside that pilot".
type SpawnSpec struct {
	MapID      int    `json:"mapId"`
	NearUserID int64  `json:"nearUserId"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	X          int32  `json:"x"`
	Y          int32  `json:"y"`
	Count      int    `json:"count"`
	ShipID     int    `json:"shipId"`
	HP         int32  `json:"hp"`
	Shield     int32  `json:"shield"`
	Speed      int32  `json:"speed"`
	Damage     int32  `json:"damage"`
	Aggressive *bool  `json:"aggressive"`
	AssetType  int16  `json:"assetType"`
	ToMap      int    `json:"toMap"`
	ToX        int32  `json:"toX"`
	ToY        int32  `json:"toY"`
	Graphic    int32  `json:"graphic"`
	Faction    int32  `json:"faction"`
	Respawn    *bool  `json:"respawn"`
	Loot       *Loot  `json:"loot"`
}

// EntityInfo is one object an admin inspect of a map returns.
type EntityInfo struct {
	NetID      int32  `json:"netId"`
	MapID      int    `json:"mapId"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	X          int32  `json:"x"`
	Y          int32  `json:"y"`
	HP         int32  `json:"hp"`
	Shield     int32  `json:"shield"`
	MaxHP      int32  `json:"maxHp"`
	Damage     int32  `json:"damage"`
	Speed      int32  `json:"speed"`
	Dead       bool   `json:"dead"`
	UserID     int64  `json:"userId,omitempty"`
	ShipID     int    `json:"shipId,omitempty"`
	ToMap      int    `json:"toMap,omitempty"`
	Aggressive bool   `json:"aggressive,omitempty"`
	Cloaked    bool   `json:"cloaked"`
	Loot       *Loot  `json:"loot,omitempty"`
}

// Vitals is a live edit of a pilot who is already on a map.
// Nil pointers leave the current value alone.
type Vitals struct {
	UserID    int64  `json:"userId"`
	ShipID    *int   `json:"shipId"`
	ShipType  string `json:"-"`
	HP        *int32 `json:"hp"`
	MaxHP     *int32 `json:"maxHp"`
	Shield    *int32 `json:"shield"`
	MaxShield *int32 `json:"maxShield"`
	Damage    *int32 `json:"damage"`
	Speed     *int32 `json:"speed"`
	Cloak     *bool  `json:"cloak"`
}
