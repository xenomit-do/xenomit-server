package adminops

// Op is one admin operation, as HTTP and as a chat command.
type Op struct {
	Name  string `json:"name"`
	HTTP  string `json:"http"`
	Chat  string `json:"chat,omitempty"`
	Body  string `json:"body,omitempty"`
	Notes string `json:"notes"`
}

// Guide is the catalog returned by GET /admin/help.
func Guide() []Op {
	return []Op{
		{Name: "give", HTTP: "POST /admin/give", Chat: "/give <pilot|id> <credits> [uridium]", Body: `{"userId":1,"credits":5000,"uridium":100,"honor":10,"experience":1000,"item":"lf-3","premium":true}`, Notes: "Negative credits or uridium are removed when the balance allows it. Item lf-3 adds 40 damage. Online pilots get a fresh ship init."},
		{Name: "spawn", HTTP: "POST /admin/spawn", Chat: "/spawn npc [shipId] [count] | /spawn box [credits] [uridium] [item] | /spawn mine [damage] | /spawn portal <mapId> | /spawn station | /spawn ball", Body: `{"mapId":1,"kind":"box","x":1200,"y":1100,"loot":{"credits":5000,"uridium":50,"honor":10,"experience":100,"item":"lf-3"}}`, Notes: "Kinds: npc, box, mine, portal, station, ball. nearUserId places them on that pilot's map; x and y of 0 means just beside them. NPCs are aggressive unless aggressive is false. Admin spawns do not respawn unless respawn is true. Ship 84 is a Streuner. Portal needs toMap."},
		{Name: "despawn", HTTP: "POST /admin/despawn", Chat: "/despawn <netId>", Body: `{"mapId":1,"netId":-10020}`, Notes: "Refuses pilots. Use kick for those."},
		{Name: "clear", HTTP: "POST /admin/clear", Chat: "/clear npc|box|mine|portal|station|ball", Body: `{"mapId":1,"kind":"box"}`, Notes: "Removes every living entity of that kind on the map."},
		{Name: "inspect", HTTP: "GET /admin/maps/{id}", Notes: "Lists net id, kind, position, hull, and box loot."},
		{Name: "where", HTTP: "GET /admin/where/{userId}", Chat: "/where [pilot]", Notes: "Live position of an online pilot."},
		{Name: "accounts", HTTP: "GET /admin/accounts", Notes: "Every account, including balances and ban state."},
		{Name: "heal", HTTP: "POST /admin/heal", Chat: "/heal [pilot]", Body: `{"userId":1}`, Notes: "Full hull and shield. Clears a destruction."},
		{Name: "kill", HTTP: "POST /admin/kill", Chat: "/kill <pilot|netId>", Body: `{"userId":1,"killerUserId":1}`, Notes: "userId targets a pilot. netId plus mapId targets anything, including a negative NPC id. killerUserId receives the NPC reward when they are on that map."},
		{Name: "vitals", HTTP: "POST /admin/vitals", Chat: "/god, /speed <n>, /cloak", Body: `{"userId":1,"shipId":1,"hp":1000000,"maxHp":1000000,"shield":1000000,"maxShield":1000000,"damage":50000,"speed":450,"cloak":true}`, Notes: "Omitted fields stay as they are. /god toggles a million-hull test ship. Player ships are saved to the hangar; NPC ships are live-only."},
		{Name: "teleport", HTTP: "POST /admin/teleport", Chat: "/tp <mapId> [x] [y]", Body: `{"userId":1,"mapId":2,"x":4000,"y":2000}`, Notes: "The pilot must already be in-game. Same-map moves are instant."},
		{Name: "summon", HTTP: "POST /admin/summon", Chat: "/summon <pilot>", Body: `{"userId":2,"toUserId":1}`, Notes: "Pulls userId to toUserId."},
		{Name: "message", HTTP: "POST /admin/message", Chat: "/tell <pilot> <text>", Body: `{"userId":1,"message":"test spawn is live"}`, Notes: "One system line on the game socket."},
		{Name: "peace", HTTP: "POST /admin/peace", Chat: "/peace <seconds>", Body: `{"mapId":1,"seconds":60}`, Notes: "seconds 0 ends peace. Combat is refused until it expires."},
		{Name: "radiation", HTTP: "POST /admin/radiation", Chat: "/radiation <radius> | /radiation off", Body: `{"mapId":42,"x":10400,"y":6400,"radius":8000}`, Notes: "radius 0 clears it. Damage ticks inside the circle."},
		{Name: "premium", HTTP: "POST /admin/give", Chat: "/premium <pilot> on|off", Body: `{"userId":1,"premium":true}`, Notes: "Sets the account flag and refreshes an online ship."},
		{Name: "admin flag", HTTP: "POST /admin/give", Body: `{"userId":2,"admin":true}`, Notes: "Grants or removes the admin flag and chat permission."},
		{Name: "unban", HTTP: "POST /admin/unban", Chat: "/unban <userId>", Body: `{"userId":2}`, Notes: "Clears a ban. They can log in again."},
		{Name: "audit", HTTP: "GET /admin/audit", Notes: "Recent admin.audit events."},
		{Name: "kick", HTTP: "POST /admin/kick", Chat: "/kick <userId>", Body: `{"userId":2}`},
		{Name: "ban", HTTP: "POST /admin/ban", Chat: "/ban <userId>", Body: `{"userId":2,"reason":"test"}`},
		{Name: "broadcast", HTTP: "POST /admin/broadcast", Chat: "/msg <text>", Body: `{"message":"hello"}`},
		{Name: "event", HTTP: "POST /admin/events/start", Chat: "/start_jpb | /start_spaceball | /start_uba", Body: `{"name":"jpb"}`, Notes: "Names: jpb, spaceball, uba."},
	}
}
