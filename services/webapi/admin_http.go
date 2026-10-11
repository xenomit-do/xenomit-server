package webapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/xenomit/server/pkg/model"
	"github.com/xenomit/server/services/admin"
)

var errOps = errors.New("admin ops unavailable")

func (s *Server) adminHelp(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.admin(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"auth": "Authorization: Bearer <token> of an admin account, or X-Admin-Token when ADMIN_TOKEN is set",
		"ops":  adminops.Guide(),
	})
}

func (s *Server) adminAccounts(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.admin(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.Accounts.List())
}

func (s *Server) adminAudit(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.ops(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.Ops.Recent(100))
}

func (s *Server) adminMap(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.admin(w, r); !ok {
		return
	}
	id, _ := strconv.Atoi(r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]any{"mapId": id, "entities": s.World.Inspect(id)})
}

func (s *Server) adminWhere(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.admin(w, r); !ok {
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	info, ok := s.World.Where(id)
	writeJSON(w, http.StatusOK, map[string]any{"online": ok, "ship": info})
}

func (s *Server) adminUnban(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.ops(w, r)
	if !ok {
		return
	}
	var body struct{ UserID int64 }
	if !readJSON(w, r, &body) {
		return
	}
	if err := s.Ops.Unban(actor, body.UserID); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"unbanned": body.UserID})
}

func (s *Server) adminGive(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.ops(w, r)
	if !ok {
		return
	}
	var body adminops.GiveRequest
	if !readJSON(w, r, &body) {
		return
	}
	res, err := s.Ops.Give(actor, body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) adminSpawn(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.ops(w, r)
	if !ok {
		return
	}
	var body model.SpawnSpec
	if !readJSON(w, r, &body) {
		return
	}
	ids, err := s.Ops.Spawn(actor, body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	mapID := body.MapID
	if body.NearUserID != 0 {
		if info, ok := s.World.Where(body.NearUserID); ok {
			mapID = info.MapID
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"mapId": mapID, "netIds": ids})
}

func (s *Server) adminDespawn(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.ops(w, r)
	if !ok {
		return
	}
	var body struct {
		MapID int
		NetID int32
	}
	if !readJSON(w, r, &body) {
		return
	}
	if err := s.Ops.Despawn(actor, body.MapID, body.NetID); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": body.NetID})
}

func (s *Server) adminClear(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.ops(w, r)
	if !ok {
		return
	}
	var body struct {
		MapID int
		Kind  string
	}
	if !readJSON(w, r, &body) {
		return
	}
	n, err := s.Ops.Clear(actor, body.MapID, body.Kind)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": n})
}

func (s *Server) adminHeal(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.ops(w, r)
	if !ok {
		return
	}
	var body struct{ UserID int64 }
	if !readJSON(w, r, &body) {
		return
	}
	if err := s.Ops.Heal(actor, body.UserID); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "healed"})
}

func (s *Server) adminKill(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.ops(w, r)
	if !ok {
		return
	}
	var body adminops.KillRequest
	if !readJSON(w, r, &body) {
		return
	}
	if err := s.Ops.Kill(actor, body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "destroyed"})
}

func (s *Server) adminVitals(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.ops(w, r)
	if !ok {
		return
	}
	var body model.Vitals
	if !readJSON(w, r, &body) {
		return
	}
	msg, err := s.Ops.Vitals(actor, body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": msg})
}

func (s *Server) adminTeleport(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.ops(w, r)
	if !ok {
		return
	}
	var body struct {
		UserID int64
		MapID  int
		X, Y   int32
	}
	if !readJSON(w, r, &body) {
		return
	}
	if err := s.Ops.Teleport(actor, body.UserID, body.MapID, body.X, body.Y); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "moved"})
}

func (s *Server) adminSummon(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.ops(w, r)
	if !ok {
		return
	}
	var body struct{ UserID, ToUserID int64 }
	if !readJSON(w, r, &body) {
		return
	}
	if err := s.Ops.Summon(actor, body.UserID, body.ToUserID); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "summoned"})
}

func (s *Server) adminMessage(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.ops(w, r)
	if !ok {
		return
	}
	var body struct {
		UserID  int64
		Message string
	}
	if !readJSON(w, r, &body) {
		return
	}
	if err := s.Ops.Message(actor, body.UserID, body.Message); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}

func (s *Server) adminPeace(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.ops(w, r)
	if !ok {
		return
	}
	var body struct {
		MapID   int
		Seconds int
	}
	if !readJSON(w, r, &body) {
		return
	}
	if err := s.Ops.Peace(actor, body.MapID, body.Seconds); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mapId": body.MapID, "seconds": body.Seconds})
}

func (s *Server) adminRadiation(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.ops(w, r)
	if !ok {
		return
	}
	var body struct {
		MapID  int
		X, Y   int32
		Radius int32
	}
	if !readJSON(w, r, &body) {
		return
	}
	if err := s.Ops.Radiation(actor, body.MapID, body.X, body.Y, body.Radius); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mapId": body.MapID, "radius": body.Radius})
}

func (s *Server) ops(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, ok := s.admin(w, r)
	if !ok {
		return 0, false
	}
	if s.Ops == nil {
		writeErr(w, http.StatusServiceUnavailable, errOps)
		return 0, false
	}
	return id, true
}
