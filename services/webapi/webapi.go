package webapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xenomit/server/pkg/model"
	"github.com/xenomit/server/pkg/observability"
	"github.com/xenomit/server/services/admin"
	"github.com/xenomit/server/services/economy"
	"github.com/xenomit/server/services/events"
	"github.com/xenomit/server/services/player"
	"github.com/xenomit/server/services/ranking"
	"github.com/xenomit/server/services/social"
	"github.com/xenomit/server/services/world"
)

// Server is the CMS replacement: registration, session issue, hangar, shop, admin.
type Server struct {
	Accounts   *player.Service
	Economy    *economy.Service
	Social     *social.Service
	Rank       *ranking.Service
	World      *world.Controller
	Events     *eventsvc.Manager
	Ops        *adminops.Service
	Obs        *observability.Obs
	AdminToken string
	// ClientRoot is the spacemap directory (preloader.swf, main.swf, and assets).
	ClientRoot string
	// GamePort is the TCP game listener port, used in maps.php.
	GamePort string
	// ChatPort is the TCP chat listener port, shown on the launcher page.
	ChatPort string
	// GamePublic overrides maps.php, for example "127.0.0.1:8080".
	GamePublic string
	mux        *http.ServeMux
}

func New(s *Server) *Server {
	s.mux = http.NewServeMux()
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) Listen(addr string) *http.Server {
	if addr == "" {
		addr = ":8088"
	}
	return &http.Server{Addr: addr, Handler: s.mux, ReadHeaderTimeout: 5 * time.Second}
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /{$}", s.launcher)
	s.mux.HandleFunc("GET /indexInternal.es", s.mapPage)
	s.mux.HandleFunc("GET /crossdomain.xml", s.crossdomain)
	s.mux.HandleFunc("GET /spacemap/xml/maps.php", s.mapsPHP)
	s.mux.HandleFunc("POST /flashAPI/loadingScreen.php", s.loadingScreen)
	s.mux.HandleFunc("GET /spacemap/", s.spacemap)
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})
	s.mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, _ *http.Request) {
		if s.Obs != nil {
			s.Obs.WriteProm(w)
		}
	})
	s.mux.HandleFunc("POST /register", s.register)
	s.mux.HandleFunc("POST /login", s.login)
	s.mux.HandleFunc("POST /session", s.session)
	s.mux.HandleFunc("POST /company", s.company)
	s.mux.HandleFunc("GET /hangar", s.hangar)
	s.mux.HandleFunc("POST /hangar/ship", s.hangarShip)
	s.mux.HandleFunc("GET /shop", s.shop)
	s.mux.HandleFunc("POST /shop/buy", s.buy)
	s.mux.HandleFunc("GET /ranking", s.ranking)
	s.mux.HandleFunc("GET /stats/online", s.onlineStats)
	s.mux.HandleFunc("GET /players/{id}/online", s.playerOnline)
	s.mux.HandleFunc("POST /clans", s.createClan)
	s.mux.HandleFunc("POST /clans/join", s.joinClan)
	s.mux.HandleFunc("POST /clans/diplomacy", s.diplomacy)
	s.mux.HandleFunc("GET /admin/help", s.adminHelp)
	s.mux.HandleFunc("GET /admin/accounts", s.adminAccounts)
	s.mux.HandleFunc("GET /admin/audit", s.adminAudit)
	s.mux.HandleFunc("GET /admin/maps/{id}", s.adminMap)
	s.mux.HandleFunc("GET /admin/where/{id}", s.adminWhere)
	s.mux.HandleFunc("POST /admin/kick", s.adminKick)
	s.mux.HandleFunc("POST /admin/ban", s.adminBan)
	s.mux.HandleFunc("POST /admin/unban", s.adminUnban)
	s.mux.HandleFunc("POST /admin/broadcast", s.adminBroadcast)
	s.mux.HandleFunc("POST /admin/events/start", s.adminEvent)
	s.mux.HandleFunc("POST /admin/give", s.adminGive)
	s.mux.HandleFunc("POST /admin/spawn", s.adminSpawn)
	s.mux.HandleFunc("POST /admin/despawn", s.adminDespawn)
	s.mux.HandleFunc("POST /admin/clear", s.adminClear)
	s.mux.HandleFunc("POST /admin/heal", s.adminHeal)
	s.mux.HandleFunc("POST /admin/kill", s.adminKill)
	s.mux.HandleFunc("POST /admin/vitals", s.adminVitals)
	s.mux.HandleFunc("POST /admin/teleport", s.adminTeleport)
	s.mux.HandleFunc("POST /admin/summon", s.adminSummon)
	s.mux.HandleFunc("POST /admin/message", s.adminMessage)
	s.mux.HandleFunc("POST /admin/peace", s.adminPeace)
	s.mux.HandleFunc("POST /admin/radiation", s.adminRadiation)
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username, Password, PilotName, Email string
	}
	if !readJSON(w, r, &body) {
		return
	}
	acc, err := s.Accounts.Register(body.Username, body.Password, body.PilotName, body.Email)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"userId": acc.UserID, "pilotName": acc.PilotName})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var body struct{ Username, Password string }
	if !readJSON(w, r, &body) {
		return
	}
	token, id, err := s.Accounts.Login(body.Username, body.Password)
	if err != nil {
		status := http.StatusUnauthorized
		if errors.Is(err, player.ErrBanned) {
			status = http.StatusForbidden
		}
		writeErr(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "userId": id})
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	id, ok := s.user(w, r)
	if !ok {
		return
	}
	sid, err := s.Accounts.IssueSession(id)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: "dosid", Value: sid, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	snap, _ := s.Accounts.Snapshot(id)
	writeJSON(w, http.StatusOK, map[string]any{"sessionId": sid, "userId": id, "mapId": snap.MapID})
}

func (s *Server) company(w http.ResponseWriter, r *http.Request) {
	id, ok := s.user(w, r)
	if !ok {
		return
	}
	var body struct{ FactionID int }
	if !readJSON(w, r, &body) {
		return
	}
	if err := s.Accounts.SetFaction(id, body.FactionID); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"factionId": body.FactionID, "mapId": model.HomeMap(body.FactionID)})
}

func (s *Server) hangar(w http.ResponseWriter, r *http.Request) {
	id, ok := s.user(w, r)
	if !ok {
		return
	}
	snap, err := s.Accounts.Snapshot(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) hangarShip(w http.ResponseWriter, r *http.Request) {
	id, ok := s.user(w, r)
	if !ok {
		return
	}
	var body struct{ ShipID int }
	if !readJSON(w, r, &body) {
		return
	}
	if err := s.Accounts.SetShip(id, body.ShipID); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"shipId": body.ShipID})
}

func (s *Server) shop(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, economy.Catalog)
}

func (s *Server) buy(w http.ResponseWriter, r *http.Request) {
	id, ok := s.user(w, r)
	if !ok {
		return
	}
	var body struct{ ItemID string }
	if !readJSON(w, r, &body) {
		return
	}
	if err := s.Economy.Buy(id, body.ItemID); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	credits, uri, _ := s.Economy.Balance(id)
	writeJSON(w, http.StatusOK, map[string]any{"credits": credits, "uridium": uri})
}

func (s *Server) ranking(w http.ResponseWriter, _ *http.Request) {
	if s.Rank == nil {
		writeJSON(w, http.StatusOK, []ranking.Row{})
		return
	}
	writeJSON(w, http.StatusOK, s.Rank.Top(50))
}

func (s *Server) onlineStats(w http.ResponseWriter, _ *http.Request) {
	list := s.World.Online()
	writeJSON(w, http.StatusOK, map[string]any{"count": len(list), "players": list})
}

func (s *Server) playerOnline(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	p, ok := s.World.PresenceOf(id)
	writeJSON(w, http.StatusOK, map[string]any{"online": ok, "mapId": p.MapID})
}

func (s *Server) createClan(w http.ResponseWriter, r *http.Request) {
	id, ok := s.user(w, r)
	if !ok {
		return
	}
	var body struct{ Name, Tag string }
	if !readJSON(w, r, &body) {
		return
	}
	c, err := s.Social.CreateClan(id, body.Name, body.Tag)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.Accounts.SetClan(id, c.ID, c.Tag)
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) joinClan(w http.ResponseWriter, r *http.Request) {
	id, ok := s.user(w, r)
	if !ok {
		return
	}
	var body struct{ ClanID int }
	if !readJSON(w, r, &body) {
		return
	}
	if err := s.Social.Join(id, body.ClanID); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if c, ok := s.Social.ClanOf(id); ok {
		s.Accounts.SetClan(id, c.ID, c.Tag)
	}
	writeJSON(w, http.StatusOK, map[string]any{"clanId": body.ClanID})
}

func (s *Server) diplomacy(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.admin(w, r); !ok {
		return
	}
	var body struct {
		ClanID, Other int
		Status        string
	}
	if !readJSON(w, r, &body) {
		return
	}
	if err := s.Social.Diplomacy(body.ClanID, body.Other, body.Status); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": body.Status})
}

func (s *Server) adminKick(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.admin(w, r)
	if !ok {
		return
	}
	var body struct{ UserID int64 }
	if !readJSON(w, r, &body) {
		return
	}
	_ = s.World.Kick(body.UserID)
	s.audit(actor, "kick", body.UserID)
	writeJSON(w, http.StatusOK, map[string]any{"kicked": body.UserID})
}

func (s *Server) adminBan(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.admin(w, r)
	if !ok {
		return
	}
	var body struct {
		UserID int64
		Reason string
	}
	if !readJSON(w, r, &body) {
		return
	}
	if err := s.Accounts.Ban(body.UserID, body.Reason, true); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	_ = s.World.Kick(body.UserID)
	s.audit(actor, "ban", body)
	writeJSON(w, http.StatusOK, map[string]any{"banned": body.UserID})
}

func (s *Server) adminBroadcast(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.admin(w, r)
	if !ok {
		return
	}
	var body struct{ Message string }
	if !readJSON(w, r, &body) {
		return
	}
	s.World.BroadcastAll(body.Message)
	s.audit(actor, "broadcast", body.Message)
	writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}

func (s *Server) adminEvent(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.admin(w, r)
	if !ok {
		return
	}
	var body struct{ Name string }
	if !readJSON(w, r, &body) {
		return
	}
	if err := s.Events.Start(body.Name); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.audit(actor, "event", body.Name)
	writeJSON(w, http.StatusOK, map[string]string{"started": body.Name})
}

func (s *Server) audit(actor int64, action string, detail any) {
	if s.Ops != nil {
		s.Ops.Audit(actor, action, detail)
	}
}

func (s *Server) user(w http.ResponseWriter, r *http.Request) (int64, bool) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		writeErr(w, http.StatusUnauthorized, errors.New("missing token"))
		return 0, false
	}
	id, err := s.Accounts.Bearer(token)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err)
		return 0, false
	}
	return id, true
}

func (s *Server) admin(w http.ResponseWriter, r *http.Request) (int64, bool) {
	if s.AdminToken != "" && r.Header.Get("X-Admin-Token") == s.AdminToken {
		return 0, true
	}
	id, ok := s.user(w, r)
	if !ok {
		return 0, false
	}
	acc, found := s.Accounts.Get(id)
	if !found || !acc.Admin {
		writeErr(w, http.StatusForbidden, errors.New("admin only"))
		return 0, false
	}
	return id, true
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return false
	}
	if err := json.Unmarshal(body, dst); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
