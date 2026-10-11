package adminops

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/xenomit/server/pkg/model"
)

// Command runs an in-game admin slash command.
// handled is false when the line is not one of these ops.
func (s *Service) Command(actor int64, line string) (string, bool) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", false
	}
	switch strings.ToLower(fields[0]) {
	case "/help", "/admin":
		return helpLine(), true
	case "/give":
		msg, err := s.cmdGive(actor, fields)
		return cmdResult(msg, err), true
	case "/spawn":
		msg, err := s.cmdSpawn(actor, fields)
		return cmdResult(msg, err), true
	case "/despawn":
		msg, err := s.cmdDespawn(actor, fields)
		return cmdResult(msg, err), true
	case "/clear":
		msg, err := s.cmdClear(actor, fields)
		return cmdResult(msg, err), true
	case "/heal":
		msg, err := s.cmdHeal(actor, fields)
		return cmdResult(msg, err), true
	case "/kill":
		msg, err := s.cmdKill(actor, fields)
		return cmdResult(msg, err), true
	case "/god":
		msg, err := s.cmdGod(actor)
		return cmdResult(msg, err), true
	case "/speed":
		msg, err := s.cmdSpeed(actor, fields)
		return cmdResult(msg, err), true
	case "/cloak":
		msg, err := s.cmdCloak(actor)
		return cmdResult(msg, err), true
	case "/summon":
		msg, err := s.cmdSummon(actor, fields)
		return cmdResult(msg, err), true
	case "/tp":
		msg, err := s.cmdTP(actor, fields)
		return cmdResult(msg, err), true
	case "/where":
		msg, err := s.cmdWhere(actor, fields)
		return cmdResult(msg, err), true
	case "/tell":
		msg, err := s.cmdTell(actor, fields, line)
		return cmdResult(msg, err), true
	case "/premium":
		msg, err := s.cmdPremium(actor, fields)
		return cmdResult(msg, err), true
	case "/unban":
		msg, err := s.cmdUnban(actor, fields)
		return cmdResult(msg, err), true
	case "/peace":
		msg, err := s.cmdPeace(actor, fields)
		return cmdResult(msg, err), true
	case "/radiation":
		msg, err := s.cmdRadiation(actor, fields)
		return cmdResult(msg, err), true
	default:
		return "", false
	}
}

func cmdResult(msg string, err error) string {
	if err != nil {
		return err.Error()
	}
	return msg
}

func helpLine() string {
	return "Admin: /give /spawn /despawn /clear /heal /kill /god /speed /cloak /summon /tp /where /tell /premium /unban /peace /radiation. HTTP guide: GET /admin/help"
}

func (s *Service) cmdGive(actor int64, fields []string) (string, error) {
	if len(fields) < 3 {
		return "", fmt.Errorf("usage: /give <pilot|id> <credits> [uridium] | uri|honor|xp|item <value>")
	}
	acc, err := s.pilot(fields[1])
	if err != nil {
		return "", err
	}
	req := GiveRequest{UserID: acc.UserID}
	switch strings.ToLower(fields[2]) {
	case "uri", "uridium":
		if len(fields) < 4 {
			return "", fmt.Errorf("usage: /give <pilot> uri <amount>")
		}
		req.Uridium, err = strconv.ParseInt(fields[3], 10, 64)
	case "honor":
		if len(fields) < 4 {
			return "", fmt.Errorf("usage: /give <pilot> honor <amount>")
		}
		req.Honor, err = strconv.ParseFloat(fields[3], 64)
	case "xp", "exp":
		if len(fields) < 4 {
			return "", fmt.Errorf("usage: /give <pilot> xp <amount>")
		}
		req.Experience, err = strconv.ParseFloat(fields[3], 64)
	case "item":
		if len(fields) < 4 {
			return "", fmt.Errorf("usage: /give <pilot> item <itemId>")
		}
		req.Item = fields[3]
	case "credits":
		if len(fields) < 4 {
			return "", fmt.Errorf("usage: /give <pilot> credits <amount>")
		}
		req.Credits, err = strconv.ParseInt(fields[3], 10, 64)
	default:
		req.Credits, err = strconv.ParseInt(fields[2], 10, 64)
		if err == nil && len(fields) > 3 {
			req.Uridium, err = strconv.ParseInt(fields[3], 10, 64)
		}
	}
	if err != nil {
		return "", fmt.Errorf("bad amount")
	}
	res, err := s.Give(actor, req)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s now has %d credits, %d uridium.", res.PilotName, res.Credits, res.Uridium), nil
}

func (s *Service) cmdSpawn(actor int64, fields []string) (string, error) {
	if len(fields) < 2 {
		return "", fmt.Errorf("usage: /spawn npc [shipId] [count] | box [credits] [uridium] [item] | mine [damage] | portal <mapId> | station | ball")
	}
	spec := model.SpawnSpec{NearUserID: actor, Count: 1}
	switch strings.ToLower(fields[1]) {
	case "npc", "ship", "streuner":
		spec.Kind = "npc"
		if len(fields) > 2 {
			spec.ShipID, _ = strconv.Atoi(fields[2])
		}
		if len(fields) > 3 {
			spec.Count, _ = strconv.Atoi(fields[3])
		}
	case "box", "bonusbox":
		spec.Kind = "box"
		loot := model.Loot{Credits: 100, Uridium: 5}
		if len(fields) > 2 {
			loot.Credits, _ = strconv.ParseInt(fields[2], 10, 64)
		}
		if len(fields) > 3 {
			loot.Uridium, _ = strconv.ParseInt(fields[3], 10, 64)
		}
		if len(fields) > 4 {
			loot.Item = fields[4]
		}
		spec.Loot = &loot
	case "mine":
		spec.Kind = "mine"
		if len(fields) > 2 {
			d, _ := strconv.Atoi(fields[2])
			spec.Damage = int32(d)
		}
	case "portal", "gate":
		spec.Kind = "portal"
		if len(fields) < 3 {
			return "", fmt.Errorf("usage: /spawn portal <mapId>")
		}
		spec.ToMap, _ = strconv.Atoi(fields[2])
	case "station":
		spec.Kind = "station"
	case "ball", "spaceball":
		spec.Kind = "ball"
	default:
		return "", fmt.Errorf("unknown spawn %s", fields[1])
	}
	ids, err := s.Spawn(actor, spec)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("spawned %s %v", spec.Kind, ids), nil
}

func (s *Service) cmdDespawn(actor int64, fields []string) (string, error) {
	if len(fields) < 2 {
		return "", fmt.Errorf("usage: /despawn <netId>")
	}
	here, err := s.here(actor)
	if err != nil {
		return "", err
	}
	id, err := strconv.ParseInt(fields[1], 10, 32)
	if err != nil {
		return "", fmt.Errorf("bad netId")
	}
	if err := s.Despawn(actor, here.MapID, int32(id)); err != nil {
		return "", err
	}
	return fmt.Sprintf("removed %d", id), nil
}

func (s *Service) cmdClear(actor int64, fields []string) (string, error) {
	if len(fields) < 2 {
		return "", fmt.Errorf("usage: /clear npc|box|mine|portal|station|ball")
	}
	here, err := s.here(actor)
	if err != nil {
		return "", err
	}
	n, err := s.Clear(actor, here.MapID, fields[1])
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("removed %d %s", n, fields[1]), nil
}

func (s *Service) cmdHeal(actor int64, fields []string) (string, error) {
	id := actor
	if len(fields) > 1 {
		acc, err := s.pilot(fields[1])
		if err != nil {
			return "", err
		}
		id = acc.UserID
	}
	if err := s.Heal(actor, id); err != nil {
		return "", err
	}
	return "repaired", nil
}

func (s *Service) cmdKill(actor int64, fields []string) (string, error) {
	if len(fields) < 2 {
		return "", fmt.Errorf("usage: /kill <pilot|netId>")
	}
	req := KillRequest{KillerUserID: actor}
	if strings.HasPrefix(fields[1], "-") {
		here, err := s.here(actor)
		if err != nil {
			return "", err
		}
		id, err := strconv.ParseInt(fields[1], 10, 32)
		if err != nil {
			return "", fmt.Errorf("bad netId")
		}
		req.MapID, req.NetID = here.MapID, int32(id)
	} else if acc, err := s.pilot(fields[1]); err == nil {
		req.UserID = acc.UserID
	} else {
		return "", err
	}
	if err := s.Kill(actor, req); err != nil {
		return "", err
	}
	return "destroyed", nil
}

func (s *Service) cmdGod(actor int64) (string, error) {
	info, err := s.here(actor)
	if err != nil {
		return "", err
	}
	if info.Damage >= 50000 {
		acc, ok := s.Accounts.Get(actor)
		if !ok {
			return "", fmt.Errorf("account missing")
		}
		id := acc.ShipID
		msg, err := s.Vitals(actor, model.Vitals{UserID: actor, ShipID: &id})
		if err != nil {
			return "", err
		}
		if msg == "" {
			msg = "god mode off"
		}
		return "god mode off", nil
	}
	hp := int32(1_000_000)
	shield := int32(1_000_000)
	dmg := int32(50_000)
	speed := int32(450)
	if _, err := s.Vitals(actor, model.Vitals{
		UserID: actor, HP: &hp, MaxHP: &hp, Shield: &shield, MaxShield: &shield, Damage: &dmg, Speed: &speed,
	}); err != nil {
		return "", err
	}
	return "god mode on", nil
}

func (s *Service) cmdSpeed(actor int64, fields []string) (string, error) {
	if len(fields) < 2 {
		return "", fmt.Errorf("usage: /speed <50-2000>")
	}
	n, err := strconv.Atoi(fields[1])
	if err != nil {
		return "", fmt.Errorf("bad speed")
	}
	speed := int32(n)
	if _, err := s.Vitals(actor, model.Vitals{UserID: actor, Speed: &speed}); err != nil {
		return "", err
	}
	return fmt.Sprintf("speed %d", speed), nil
}

func (s *Service) cmdCloak(actor int64) (string, error) {
	info, err := s.here(actor)
	if err != nil {
		return "", err
	}
	on := !info.Cloaked
	if _, err := s.Vitals(actor, model.Vitals{UserID: actor, Cloak: &on}); err != nil {
		return "", err
	}
	if on {
		return "cloaked", nil
	}
	return "visible", nil
}

func (s *Service) cmdSummon(actor int64, fields []string) (string, error) {
	if len(fields) < 2 {
		return "", fmt.Errorf("usage: /summon <pilot>")
	}
	acc, err := s.pilot(fields[1])
	if err != nil {
		return "", err
	}
	if err := s.Summon(actor, acc.UserID, actor); err != nil {
		return "", err
	}
	return "summoned " + acc.PilotName, nil
}

func (s *Service) cmdTP(actor int64, fields []string) (string, error) {
	if len(fields) < 2 {
		return "", fmt.Errorf("usage: /tp <mapId> [x] [y]")
	}
	mapID, err := strconv.Atoi(fields[1])
	if err != nil {
		return "", fmt.Errorf("bad map")
	}
	var x, y int32 = 2000, 2000
	if len(fields) > 2 {
		n, err := strconv.Atoi(fields[2])
		if err != nil {
			return "", fmt.Errorf("bad x")
		}
		x = int32(n)
	}
	if len(fields) > 3 {
		n, err := strconv.Atoi(fields[3])
		if err != nil {
			return "", fmt.Errorf("bad y")
		}
		y = int32(n)
	}
	if err := s.Teleport(actor, actor, mapID, x, y); err != nil {
		return "", err
	}
	return fmt.Sprintf("teleported to map %d (%d,%d)", mapID, x, y), nil
}

func (s *Service) cmdWhere(actor int64, fields []string) (string, error) {
	id := actor
	name := "you"
	if len(fields) > 1 {
		acc, err := s.pilot(fields[1])
		if err != nil {
			return "", err
		}
		id = acc.UserID
		name = acc.PilotName
	}
	info, ok := s.World.Where(id)
	if !ok {
		return "", fmt.Errorf("%s is not in-game", name)
	}
	return fmt.Sprintf("%s map %d at %d,%d hp %d/%d net %d", name, info.MapID, info.X, info.Y, info.HP, info.MaxHP, info.NetID), nil
}

func (s *Service) cmdTell(actor int64, fields []string, line string) (string, error) {
	if len(fields) < 3 {
		return "", fmt.Errorf("usage: /tell <pilot> <message>")
	}
	acc, err := s.pilot(fields[1])
	if err != nil {
		return "", err
	}
	msg := strings.TrimSpace(line)
	msg = strings.TrimSpace(strings.TrimPrefix(msg, fields[0]))
	msg = strings.TrimSpace(strings.TrimPrefix(msg, fields[1]))
	if err := s.Message(actor, acc.UserID, msg); err != nil {
		return "", err
	}
	return "sent", nil
}

func (s *Service) cmdPremium(actor int64, fields []string) (string, error) {
	if len(fields) < 3 {
		return "", fmt.Errorf("usage: /premium <pilot> on|off")
	}
	acc, err := s.pilot(fields[1])
	if err != nil {
		return "", err
	}
	on := strings.EqualFold(fields[2], "on") || fields[2] == "1"
	if _, err := s.Give(actor, GiveRequest{UserID: acc.UserID, Premium: &on}); err != nil {
		return "", err
	}
	if on {
		return acc.PilotName + " is premium", nil
	}
	return acc.PilotName + " premium removed", nil
}

func (s *Service) cmdUnban(actor int64, fields []string) (string, error) {
	if len(fields) < 2 {
		return "", fmt.Errorf("usage: /unban <userId>")
	}
	id, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return "", fmt.Errorf("bad userId")
	}
	if err := s.Unban(actor, id); err != nil {
		return "", err
	}
	return fmt.Sprintf("unbanned %d", id), nil
}

func (s *Service) cmdPeace(actor int64, fields []string) (string, error) {
	if len(fields) < 2 {
		return "", fmt.Errorf("usage: /peace <seconds> (0 ends it)")
	}
	here, err := s.here(actor)
	if err != nil {
		return "", err
	}
	seconds, err := strconv.Atoi(fields[1])
	if err != nil {
		return "", fmt.Errorf("bad seconds")
	}
	if err := s.Peace(actor, here.MapID, seconds); err != nil {
		return "", err
	}
	return fmt.Sprintf("peace %ds on map %d", seconds, here.MapID), nil
}

func (s *Service) cmdRadiation(actor int64, fields []string) (string, error) {
	here, err := s.here(actor)
	if err != nil {
		return "", err
	}
	if len(fields) < 2 || fields[1] == "off" {
		if err := s.Radiation(actor, here.MapID, 0, 0, 0); err != nil {
			return "", err
		}
		return "radiation cleared", nil
	}
	radius, err := strconv.Atoi(fields[1])
	if err != nil {
		return "", fmt.Errorf("usage: /radiation <radius>|off")
	}
	if err := s.Radiation(actor, here.MapID, here.X, here.Y, int32(radius)); err != nil {
		return "", err
	}
	return fmt.Sprintf("radiation radius %d", radius), nil
}

func (s *Service) here(userID int64) (model.EntityInfo, error) {
	info, ok := s.World.Where(userID)
	if !ok {
		return model.EntityInfo{}, fmt.Errorf("you must be in-game")
	}
	return info, nil
}

func (s *Service) pilot(token string) (playerAccount, error) {
	if id, err := strconv.ParseInt(token, 10, 64); err == nil {
		acc, ok := s.Accounts.Get(id)
		if !ok {
			return playerAccount{}, fmt.Errorf("no user %d", id)
		}
		return playerAccount{UserID: acc.UserID, PilotName: acc.PilotName}, nil
	}
	acc, ok := s.Accounts.ByName(token)
	if !ok {
		return playerAccount{}, fmt.Errorf("no pilot named %s", token)
	}
	return playerAccount{UserID: acc.UserID, PilotName: acc.PilotName}, nil
}

type playerAccount struct {
	UserID    int64
	PilotName string
}
