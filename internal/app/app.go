// Package app wires the xenomit process: gateway, world shards, and the HTTP API.
package app

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/xenomit/server/pkg/bus"
	"github.com/xenomit/server/pkg/events"
	"github.com/xenomit/server/pkg/model"
	"github.com/xenomit/server/pkg/observability"
	"github.com/xenomit/server/services/admin"
	"github.com/xenomit/server/services/chat"
	"github.com/xenomit/server/services/economy"
	"github.com/xenomit/server/services/events"
	"github.com/xenomit/server/services/gateway"
	"github.com/xenomit/server/services/player"
	"github.com/xenomit/server/services/ranking"
	"github.com/xenomit/server/services/social"
	"github.com/xenomit/server/services/webapi"
	"github.com/xenomit/server/services/world"
)

// Config is the process configuration. Empty addresses use the public ports.
type Config struct {
	GameAddr   string
	ChatAddr   string
	PolicyAddr string
	HTTPAddr   string
	AdminToken string
	// ClientRoot is the directory served at /spacemap/ (preloader.swf, main.swf).
	ClientRoot string
	// GamePublic is the host:port written into maps.php. Empty uses the request host.
	GamePublic string
}

// Server is a running xenomit process.
type Server struct {
	Game   string
	Chat   string
	Policy string
	HTTP   string
	http   *http.Server
}

// Start boots every service. Cancel ctx to stop listeners.
func Start(ctx context.Context, cfg Config) (*Server, error) {
	if cfg.GameAddr == "" {
		cfg.GameAddr = ":8080"
	}
	if cfg.ChatAddr == "" {
		cfg.ChatAddr = ":9338"
	}
	if cfg.PolicyAddr == "" {
		cfg.PolicyAddr = ":843"
	}
	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = ":8088"
	}
	b := bus.New()
	log := events.NewLog()
	obs := observability.New()
	accounts := player.New()
	accounts.Seed()
	econ := economy.New(accounts, log)
	socialSvc := social.New(log)
	rank := ranking.New(log)
	ctl := world.New(world.Deps{
		Bus: b, Log: log, Save: accounts, Obs: obs,
		GrantItem: accounts.AddItem, Refresh: accounts.Snapshot,
	})
	ev := eventsvc.New(worldAPI{ctl}, econ, log)
	ops := adminops.New(ctl, accounts, log)
	ctl.Start(ctx)
	chat.New(b, chatAccounts{accounts}, controls{w: ctl, e: ev, a: accounts, ops: ops}, log)
	gw := gateway.New(b, accounts, obs)
	gw.GameAddr, gw.ChatAddr, gw.PolicyAddr = cfg.GameAddr, cfg.ChatAddr, cfg.PolicyAddr
	game, chatAddr, policy, err := gw.Start(ctx)
	if err != nil {
		return nil, err
	}
	api := webapi.New(&webapi.Server{
		Accounts: accounts, Economy: econ, Social: socialSvc, Rank: rank,
		World: ctl, Events: ev, Ops: ops, Obs: obs, AdminToken: cfg.AdminToken,
		ClientRoot: cfg.ClientRoot, GamePort: portOf(game), ChatPort: portOf(chatAddr),
		GamePublic: cfg.GamePublic,
	})
	ln, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return nil, err
	}
	httpSrv := &http.Server{Handler: api.Handler()}
	go httpSrv.Serve(ln)
	go func() {
		<-ctx.Done()
		_ = httpSrv.Close()
	}()
	return &Server{Game: game, Chat: chatAddr, Policy: policy, HTTP: ln.Addr().String(), http: httpSrv}, nil
}

type worldAPI struct{ c *world.Controller }

func (w worldAPI) Transfer(userID int64, mapID int, x, y int32) error {
	return w.c.Transfer(userID, mapID, x, y)
}
func (w worldAPI) BroadcastAll(text string)       { w.c.BroadcastAll(text) }
func (w worldAPI) BroadcastLegacy(message string) { w.c.BroadcastLegacy(message) }
func (w worldAPI) Alive(mapID int) []int64        { return w.c.Alive(mapID) }
func (w worldAPI) SetRadiation(mapID int, cx, cy, radius int32) {
	w.c.SetRadiation(mapID, cx, cy, radius)
}
func (w worldAPI) ClearRadiation(mapID int)                      { w.c.ClearRadiation(mapID) }
func (w worldAPI) ShrinkRadiation(mapID int, by int32) int32     { return w.c.ShrinkRadiation(mapID, by) }
func (w worldAPI) SetPeace(mapID int, until time.Time)           { w.c.SetPeace(mapID, until) }
func (w worldAPI) SpawnBall(mapID int, x, y int32)               { w.c.SpawnBall(mapID, x, y) }
func (w worldAPI) TickSpaceball(mapID int) (int, int, int, bool) { return w.c.TickSpaceball(mapID) }
func (w worldAPI) Online() []eventsvc.Presence {
	src := w.c.Online()
	out := make([]eventsvc.Presence, len(src))
	for i, p := range src {
		out[i] = eventsvc.Presence{UserID: p.UserID, MapID: p.MapID, Faction: p.Faction}
	}
	return out
}

type chatAccounts struct{ *player.Service }

func (a chatAccounts) Validate(userID int64, sessionID string) (model.Snapshot, error) {
	return a.Service.Validate(userID, sessionID)
}
func (a chatAccounts) ByName(name string) (chat.User, bool) {
	acc, ok := a.Service.ByName(name)
	if !ok {
		return chat.User{}, false
	}
	return toUser(acc), true
}
func (a chatAccounts) Get(userID int64) (chat.User, bool) {
	acc, ok := a.Service.Get(userID)
	if !ok {
		return chat.User{}, false
	}
	return toUser(acc), true
}
func portOf(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return ""
	}
	return port
}

func toUser(acc player.Account) chat.User {
	return chat.User{UserID: acc.UserID, PilotName: acc.PilotName, Admin: acc.Admin, ChatPerm: acc.ChatPerm}
}

type controls struct {
	w   *world.Controller
	e   *eventsvc.Manager
	a   *player.Service
	ops *adminops.Service
}

func (c controls) Kick(userID int64) error { return c.w.Kick(userID) }
func (c controls) Ban(userID int64, reason string) error {
	if err := c.a.Ban(userID, reason, true); err != nil {
		return err
	}
	return c.w.Kick(userID)
}
func (c controls) Broadcast(text string)              { c.w.BroadcastAll(text) }
func (c controls) StartEvent(name string) error       { return c.e.Start(name) }
func (c controls) Jump(userID int64, mapID int) error { return c.w.Jump(userID, mapID) }
func (c controls) StartDuel(a, b int64) error         { return c.e.StartDuel(a, b) }
func (c controls) Command(actor int64, line string) (string, bool) {
	if c.ops == nil {
		return "", false
	}
	return c.ops.Command(actor, line)
}
