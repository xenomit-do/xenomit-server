package world

// MapDef is the boot configuration for one spacemap (server_maps).
type MapDef struct {
	ID           int
	Name         string
	Faction      int
	W, H         int32
	HomeX, HomeY int32
	PVP          bool
	DeathLeaves  bool
	NPCs         []NPCSpawn
	Portals      []PortalDef
	Stations     []StationDef
	Boxes        int
	Mines        int
}

type NPCSpawn struct {
	ShipID     int
	Name       string
	X, Y       int32
	HP, Shield int32
	Speed      int32
	Damage     int32
	Aggressive bool
}

type PortalDef struct {
	X, Y     int32
	ToMap    int
	ToX, ToY int32
	Graphic  int32
	Faction  int32
}

type StationDef struct {
	Name string
	Type int16
	X, Y int32
}

func DefaultMaps() []MapDef {
	streuners := []NPCSpawn{
		{84, "Streuner", 4000, 4000, 800, 200, 280, 20, true},
		{84, "Streuner", 4500, 4200, 800, 200, 280, 20, true},
		{84, "Streuner", 4200, 4700, 800, 200, 280, 20, true},
		{84, "Streuner", 4800, 4500, 800, 200, 280, 20, true},
	}
	return []MapDef{
		{
			ID: 1, Name: "1-1", Faction: 1, W: 20800, H: 12800, HomeX: 1000, HomeY: 1000,
			NPCs:     streuners,
			Portals:  []PortalDef{{X: 6000, Y: 2000, ToMap: 2, ToX: 1500, ToY: 1500, Graphic: 1, Faction: 1}},
			Stations: []StationDef{{Name: "RepairDock", Type: 53, X: 1600, Y: 1600}},
			Boxes:    4, Mines: 2,
		},
		{
			ID: 2, Name: "1-2", Faction: 1, W: 20800, H: 12800, HomeX: 1500, HomeY: 1500,
			NPCs:    []NPCSpawn{{84, "Streuner", 4000, 4000, 800, 200, 280, 20, true}},
			Portals: []PortalDef{{X: 1500, Y: 1500, ToMap: 1, ToX: 5800, ToY: 2000, Graphic: 1, Faction: 1}},
			Boxes:   2,
		},
		{ID: 5, Name: "2-1", Faction: 2, W: 20800, H: 12800, HomeX: 1000, HomeY: 1000,
			Stations: []StationDef{{Name: "RepairDock", Type: 53, X: 1600, Y: 1600}},
			NPCs:     []NPCSpawn{{84, "Streuner", 2400, 1400, 800, 200, 280, 20, true}}},
		{ID: 9, Name: "3-1", Faction: 3, W: 20800, H: 12800, HomeX: 1000, HomeY: 1000,
			Stations: []StationDef{{Name: "RepairDock", Type: 53, X: 1600, Y: 1600}},
			NPCs:     []NPCSpawn{{84, "Streuner", 2400, 1400, 800, 200, 280, 20, true}}},
		{ID: 16, Name: "4-4", W: 20800, H: 12800, HomeX: 10400, HomeY: 6400, PVP: true},
		{ID: 42, Name: "JPB", W: 20800, H: 12800, HomeX: 10400, HomeY: 6400, PVP: true, DeathLeaves: true},
		{ID: 101, Name: "Duel", W: 20800, H: 12800, HomeX: 5000, HomeY: 5000, PVP: true, DeathLeaves: true},
		{ID: 121, Name: "UBA", W: 20800, H: 12800, HomeX: 5000, HomeY: 5000, PVP: true, DeathLeaves: true},
	}
}

func mapByID(id int) MapDef {
	for _, m := range DefaultMaps() {
		if m.ID == id {
			return m
		}
	}
	return MapDef{ID: id, Name: "map", W: 20800, H: 12800, HomeX: 1000, HomeY: 1000}
}
