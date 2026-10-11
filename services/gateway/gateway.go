// Package gateway terminates the Flash client: game TCP, chat TCP, and the policy file.
package gateway

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xenomit/server/pkg/bus"
	"github.com/xenomit/server/pkg/model"
	"github.com/xenomit/server/pkg/observability"
	"github.com/xenomit/server/pkg/protocol/codec"
	"github.com/xenomit/server/pkg/protocol/policy"
	"github.com/xenomit/server/pkg/protocol/v10"
	"github.com/xenomit/server/services/chat"
)

// Sessions validates the session id issued by webapi.
type Sessions interface {
	Validate(userID int64, sessionID string) (model.Snapshot, error)
}

// Gateway is the edge. It does not own map state.
type Gateway struct {
	Bus        bus.Bus
	Sessions   Sessions
	Obs        *observability.Obs
	GameAddr   string
	ChatAddr   string
	PolicyAddr string

	mu     sync.Mutex
	wmu    sync.Mutex
	conns  map[string]net.Conn
	byUser map[int64]string
}

func New(b bus.Bus, sessions Sessions, obs *observability.Obs) *Gateway {
	g := &Gateway{
		Bus: b, Sessions: sessions, Obs: obs,
		GameAddr: ":8080", ChatAddr: ":9338", PolicyAddr: ":843",
		conns: map[string]net.Conn{}, byUser: map[int64]string{},
	}
	if b != nil {
		b.Subscribe("out.>", func(subject string, data []byte) {
			parts := strings.Split(subject, ".")
			if len(parts) == 2 && parts[0] == "out" {
				g.write(parts[1], data)
			}
		})
		b.Subscribe("chat.out.>", func(subject string, data []byte) {
			parts := strings.Split(subject, ".")
			if len(parts) == 3 {
				g.write("c:"+parts[2], data)
			}
		})
		b.Subscribe("gateway.kick", func(_ string, data []byte) {
			g.close(string(data))
		})
	}
	return g
}

// Start binds listeners. Game bind failure is returned; policy bind failure is logged.
func (g *Gateway) Start(ctx context.Context) (game, chatAddr, policyAddr string, err error) {
	gl, err := net.Listen("tcp", g.GameAddr)
	if err != nil {
		return "", "", "", err
	}
	cl, err := net.Listen("tcp", g.ChatAddr)
	if err != nil {
		gl.Close()
		return "", "", "", err
	}
	var pl net.Listener
	if g.PolicyAddr != "" {
		pl, err = net.Listen("tcp", g.PolicyAddr)
		if err != nil {
			slog.Warn("policy listener disabled", "addr", g.PolicyAddr, "err", err)
			err = nil
		}
	}
	go g.accept(ctx, gl, g.serveGame)
	go g.accept(ctx, cl, g.serveChat)
	if pl != nil {
		go g.accept(ctx, pl, g.servePolicy)
		policyAddr = pl.Addr().String()
	}
	go func() {
		<-ctx.Done()
		gl.Close()
		cl.Close()
		if pl != nil {
			pl.Close()
		}
	}()
	return gl.Addr().String(), cl.Addr().String(), policyAddr, nil
}

func (g *Gateway) accept(ctx context.Context, ln net.Listener, serve func(net.Conn)) {
	_ = ctx
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go serve(conn)
	}
}

func (g *Gateway) servePolicy(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 64)
	_, _ = conn.Read(buf)
	g.wmu.Lock()
	_, _ = conn.Write([]byte(policy.Response))
	g.wmu.Unlock()
}

func (g *Gateway) serveGame(conn net.Conn) {
	defer conn.Close()
	var session string
	var userID int64
	defer func() {
		if session == "" {
			return
		}
		g.mu.Lock()
		if g.conns[session] == conn {
			delete(g.conns, session)
			if g.byUser[userID] == session {
				delete(g.byUser, userID)
			}
		}
		g.mu.Unlock()
		g.publish(model.Ingress{Type: "leave", SessionID: session, UserID: userID})
	}()
	buf := make([]byte, 0, 1024)
	tmp := make([]byte, 4096)
	window := time.Now()
	count := 0
	for {
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Minute))
		n, err := conn.Read(tmp)
		if err != nil {
			return
		}
		buf = append(buf, tmp[:n]...)
		if len(buf) > codec.MaxFrame*2 {
			return
		}
		for len(buf) > 0 {
			if buf[0] == '<' {
				if !strings.HasPrefix(string(buf), policy.Request) {
					if len(buf) < len(policy.Request) {
						break
					}
					return
				}
				g.wmu.Lock()
				_, _ = conn.Write([]byte(policy.Response))
				g.wmu.Unlock()
				buf = buf[len(policy.Request):]
				continue
			}
			if len(buf) >= 2 && buf[0] == '0' && buf[1] == '|' {
				end := strings.IndexAny(string(buf), "\n\x00")
				if end < 0 {
					break
				}
				line := string(buf[:end])
				buf = buf[end+1:]
				if !g.handleV4(conn, &session, &userID, line) {
					return
				}
				continue
			}
			if len(buf) < 2 {
				break
			}
			ln := int(binary.BigEndian.Uint16(buf[:2]))
			if ln < 2 || ln > codec.MaxFrame {
				return
			}
			if len(buf) < 2+ln {
				break
			}
			frame := append([]byte(nil), buf[:2+ln]...)
			buf = buf[2+ln:]
			now := time.Now()
			if now.Sub(window) > time.Second {
				window = now
				count = 0
			}
			count++
			if count > 60 {
				return
			}
			if g.Obs != nil {
				g.Obs.PacketsIn.Add(1)
			}
			if !g.handleFrame(conn, &session, &userID, frame) {
				return
			}
		}
	}
}

func (g *Gateway) handleFrame(conn net.Conn, session *string, userID *int64, frame []byte) bool {
	id, err := v10.CommandID(frame)
	if err != nil {
		return false
	}
	if *session == "" && id != v10.LoginRequestID && id != v10.LegacyModuleID {
		return true
	}
	if id == v10.LoginRequestID {
		login, err := v10.DecodeLogin(frame)
		if err != nil {
			return false
		}
		return g.login(conn, session, userID, int64(login.UserID), login.SessionID)
	}
	g.publish(model.Ingress{Type: "packet", SessionID: *session, UserID: *userID, Packet: frame})
	return true
}

func (g *Gateway) handleV4(conn net.Conn, session *string, userID *int64, line string) bool {
	parts := strings.Split(strings.TrimRight(line, "\n\x00"), "|")
	if len(parts) >= 4 && parts[0] == "0" && parts[1] == "l" {
		id, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return false
		}
		return g.login(conn, session, userID, id, parts[3])
	}
	if *session == "" {
		return true
	}
	if len(parts) >= 4 && parts[1] == "1" {
		x, _ := strconv.Atoi(parts[2])
		y, _ := strconv.Atoi(parts[3])
		frame, err := v10.EncodeMoveRequest(v10.Move{TargetX: int32(x), TargetY: int32(y)})
		if err != nil {
			return true
		}
		g.publish(model.Ingress{Type: "packet", SessionID: *session, UserID: *userID, Packet: frame})
	}
	return true
}

func (g *Gateway) login(conn net.Conn, session *string, userID *int64, id int64, sid string) bool {
	snap, err := g.Sessions.Validate(id, sid)
	if err != nil {
		if frame, e := v10.EncodeLegacy("0|A|STD|Login failed"); e == nil {
			g.wmu.Lock()
			_, _ = conn.Write(frame)
			g.wmu.Unlock()
		}
		return false
	}
	if snap.MapID == 0 {
		snap.MapID = model.HomeMap(snap.FactionID)
	}
	g.mu.Lock()
	if old, ok := g.byUser[id]; ok && old != sid {
		if c := g.conns[old]; c != nil {
			c.Close()
		}
	}
	g.conns[sid] = conn
	g.byUser[id] = sid
	g.mu.Unlock()
	*session = sid
	*userID = id
	g.publish(model.Ingress{Type: "join", SessionID: sid, UserID: id, MapID: snap.MapID, Snapshot: &snap})
	return true
}

func (g *Gateway) publish(ing model.Ingress) {
	if g.Bus == nil {
		return
	}
	raw, err := json.Marshal(ing)
	if err != nil {
		return
	}
	mapID := ing.MapID
	if mapID == 0 && ing.Snapshot != nil {
		mapID = ing.Snapshot.MapID
	}
	if mapID == 0 {
		mapID = 1
	}
	g.Bus.Publish("in."+strconv.Itoa(mapID), raw)
}

func (g *Gateway) write(key string, data []byte) {
	g.mu.Lock()
	conn := g.conns[key]
	g.mu.Unlock()
	if conn == nil || len(data) == 0 {
		return
	}
	g.wmu.Lock()
	_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, err := conn.Write(data)
	g.wmu.Unlock()
	if err != nil {
		conn.Close()
	}
}

func (g *Gateway) close(session string) {
	g.mu.Lock()
	conn := g.conns[session]
	g.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
}

func (g *Gateway) serveChat(conn net.Conn) {
	defer conn.Close()
	id := strconv.FormatInt(time.Now().UnixNano(), 36)
	key := "c:" + id
	g.mu.Lock()
	g.conns[key] = conn
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		if g.conns[key] == conn {
			delete(g.conns, key)
		}
		g.mu.Unlock()
	}()
	r := bufio.NewReader(conn)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Minute))
		line, err := r.ReadString('#')
		if err != nil {
			if err != io.EOF {
				return
			}
			return
		}
		chat.PublishIn(g.Bus, id, line)
	}
}
