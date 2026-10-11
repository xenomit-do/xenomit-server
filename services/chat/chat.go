package chat

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/xenomit/server/pkg/bus"
	"github.com/xenomit/server/pkg/events"
	"github.com/xenomit/server/pkg/model"
)

// Accounts validates chat logins and resolves pilots.
type Accounts interface {
	Validate(userID int64, sessionID string) (model.Snapshot, error)
	ByName(name string) (User, bool)
	Get(userID int64) (User, bool)
}

// User is the chat-facing account view. player.Account is assigned to it
// only for the fields below; the chat service defines its own struct so it
// does not import the player service.
type User struct {
	UserID    int64
	PilotName string
	Admin     bool
	ChatPerm  int
}

// Controls are admin and duel actions fulfilled by the composition root.
type Controls interface {
	Kick(userID int64) error
	Ban(userID int64, reason string) error
	Broadcast(text string)
	StartEvent(name string) error
	Jump(userID int64, mapID int) error
	StartDuel(a, b int64) error
	Command(actor int64, line string) (reply string, handled bool)
}

// Room is a chat channel. ToString matches the Flash room list entry.
type Room struct {
	ID, Tab, Company int
	Name             string
}

func (r Room) String() string {
	return fmt.Sprintf("%d|%s|%d|%d|0|0}", r.ID, r.Name, r.Tab, r.Company)
}

// Service implements the % @ | } # chat semantics behind the gateway.
type Service struct {
	mu       sync.Mutex
	bus      bus.Bus
	accounts Accounts
	controls Controls
	log      *events.Log
	rooms    []Room
	connUser map[string]int64
	userConn map[int64]string
	joined   map[int64]map[int]bool
	invites  map[int]duelInvite
	nextInv  int
	filter   []string
}

type duelInvite struct {
	from, to int64
}

func New(b bus.Bus, accounts Accounts, controls Controls, log *events.Log) *Service {
	s := &Service{
		bus: b, accounts: accounts, controls: controls, log: log,
		rooms: []Room{
			{1, 0, -1, "Global"},
			{2, 1, 1, "MMO"},
			{3, 2, 2, "EIC"},
			{4, 3, 3, "VRU"},
			{5, 5, -1, "Clan Search"},
		},
		connUser: map[string]int64{},
		userConn: map[int64]string{},
		joined:   map[int64]map[int]bool{},
		invites:  map[int]duelInvite{},
		filter:   []string{"http://", "https://"},
	}
	if b != nil {
		b.Subscribe("chat.in", func(_ string, data []byte) {
			connID, text, ok := splitIn(data)
			if ok {
				s.Handle(connID, text)
			}
		})
	}
	return s
}

func splitIn(data []byte) (connID, text string, ok bool) {
	raw := string(data)
	i := strings.IndexByte(raw, '\n')
	if i < 0 {
		return "", "", false
	}
	return raw[:i], raw[i+1:], true
}

// PublishIn is what the gateway sends for one chat socket message.
func PublishIn(b bus.Bus, connID, text string) {
	b.Publish("chat.in", []byte(connID+"\n"+text))
}

func (s *Service) reply(connID, msg string) {
	if !strings.HasSuffix(msg, "#") {
		msg += "#"
	}
	if s.bus != nil {
		s.bus.Publish("chat.out."+connID, []byte(msg))
	}
}

// Handle parses one chat message for a connection.
func (s *Service) Handle(connID, raw string) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimSuffix(raw, "#")
	if raw == "" {
		return
	}
	cmd, _, _ := strings.Cut(raw, "%")
	switch cmd {
	case "bu":
		s.login(connID, raw)
	case "a":
		s.message(connID, raw)
	case "bz":
		s.joinRoom(connID, raw)
	default:
		s.reply(connID, "dq%Unknown command.")
	}
}

func (s *Service) login(connID, raw string) {
	fields := strings.Split(strings.ReplaceAll(raw, "@", "%"), "%")
	if len(fields) < 3 {
		s.reply(connID, "ej%Login failed.")
		return
	}
	userID, err1 := strconv.ParseInt(fields[len(fields)-2], 10, 64)
	session := fields[len(fields)-1]
	if err1 != nil {
		s.reply(connID, "ej%Login failed.")
		return
	}
	snap, err := s.accounts.Validate(userID, session)
	if err != nil {
		s.reply(connID, "ej%Login failed.")
		return
	}
	s.mu.Lock()
	if prev, ok := s.userConn[userID]; ok && prev != connID {
		s.reply(prev, "as%")
	}
	s.connUser[connID] = userID
	s.userConn[userID] = connID
	s.joined[userID] = map[int]bool{1: true}
	s.mu.Unlock()
	s.reply(connID, fmt.Sprintf("bv%%%d#", userID))
	var b strings.Builder
	for _, r := range s.rooms {
		b.WriteString(r.String())
	}
	list := b.String()
	if len(list) > 0 {
		list = list[:len(list)-1]
	}
	s.reply(connID, "by%"+list)
	s.reply(connID, "dq%Use '/duel name' for invite someone to duel.")
	if s.log != nil {
		s.log.Append("chat.login", fmt.Sprint(userID), map[string]any{"userId": userID, "name": snap.PilotName})
	}
}

func (s *Service) message(connID, raw string) {
	uid, ok := s.user(connID)
	if !ok {
		return
	}
	fields := strings.Split(strings.ReplaceAll(raw, "@", "%"), "%")
	if len(fields) < 3 {
		return
	}
	text := fields[2]
	for _, bad := range s.filter {
		if strings.Contains(strings.ToLower(text), bad) {
			s.reply(connID, "dq%Message blocked.")
			return
		}
	}
	if strings.HasPrefix(text, "/") {
		s.slash(uid, connID, text)
		return
	}
	acc, _ := s.accounts.Get(uid)
	roomID, _ := strconv.Atoi(fields[1])
	line := fmt.Sprintf("a%%%d%%%s@%s#", roomID, acc.PilotName, text)
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, conn := range s.userConn {
		rooms := s.joined[id]
		if rooms[roomID] || roomID == 0 {
			s.bus.Publish("chat.out."+conn, []byte(line))
		}
	}
	if s.log != nil {
		s.log.Append("chat.messages", fmt.Sprint(uid), map[string]any{"userId": uid, "room": roomID, "text": text})
	}
}

func (s *Service) slash(uid int64, connID, text string) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return
	}
	acc, _ := s.accounts.Get(uid)
	admin := acc.Admin || acc.ChatPerm >= 1
	switch fields[0] {
	case "/w":
		s.whisper(uid, connID, acc.PilotName, fields)
	case "/duel":
		s.duel(uid, connID, acc.PilotName, fields)
	case "/kick":
		if admin && len(fields) > 1 && s.controls != nil {
			id, _ := strconv.ParseInt(fields[1], 10, 64)
			_ = s.controls.Kick(id)
		}
	case "/ban":
		if admin && len(fields) > 1 && s.controls != nil {
			id, _ := strconv.ParseInt(fields[1], 10, 64)
			_ = s.controls.Ban(id, "chat")
		}
	case "/msg":
		if admin && s.controls != nil {
			s.controls.Broadcast(strings.TrimPrefix(text, "/msg "))
		}
	case "/jump":
		if admin && len(fields) > 1 && s.controls != nil {
			mapID, _ := strconv.Atoi(fields[1])
			_ = s.controls.Jump(uid, mapID)
		}
	case "/start_jpb", "/start_spaceball", "/start_uba":
		if admin && s.controls != nil {
			name := strings.TrimPrefix(fields[0], "/start_")
			_ = s.controls.StartEvent(name)
			s.reply(connID, "dq%Event started.")
		}
	default:
		if admin && s.controls != nil {
			if reply, ok := s.controls.Command(uid, text); ok {
				if reply != "" {
					s.reply(connID, "dq%"+reply)
				}
				return
			}
		}
		s.reply(connID, "dq%Unknown command.")
	}
}

func (s *Service) whisper(uid int64, connID, from string, fields []string) {
	if len(fields) < 3 {
		s.reply(connID, "cs%")
		return
	}
	target, ok := s.accounts.ByName(fields[1])
	if !ok || target.UserID == uid {
		s.reply(connID, "ct%")
		return
	}
	msg := strings.Join(fields[2:], " ")
	s.mu.Lock()
	tconn, online := s.userConn[target.UserID]
	s.mu.Unlock()
	if !online {
		s.reply(connID, "ct%")
		return
	}
	s.reply(tconn, fmt.Sprintf("cv%%%s@%s", from, msg))
	s.reply(connID, fmt.Sprintf("cw%%%s@%s", target.PilotName, msg))
}

func (s *Service) duel(uid int64, connID, from string, fields []string) {
	if len(fields) < 2 || s.controls == nil {
		s.reply(connID, "dq%Use '/duel name' for invite someone to duel.")
		return
	}
	target, ok := s.accounts.ByName(fields[1])
	if !ok {
		s.reply(connID, "ct%")
		return
	}
	s.mu.Lock()
	s.nextInv++
	id := s.nextInv
	s.invites[id] = duelInvite{from: uid, to: target.UserID}
	tconn := s.userConn[target.UserID]
	s.mu.Unlock()
	if tconn != "" {
		s.reply(tconn, fmt.Sprintf("cj%%%d@Duel@0@%s", id, from))
	}
	s.reply(connID, "dq%Duel invite sent.")
}

func (s *Service) joinRoom(connID, raw string) {
	uid, ok := s.user(connID)
	if !ok {
		return
	}
	fields := strings.Split(strings.ReplaceAll(raw, "@", "%"), "%")
	if len(fields) < 2 {
		return
	}
	roomID, _ := strconv.Atoi(fields[1])
	s.mu.Lock()
	if inv, ok := s.invites[roomID]; ok && inv.to == uid {
		delete(s.invites, roomID)
		s.mu.Unlock()
		_ = s.controls.StartDuel(inv.from, inv.to)
		s.reply(connID, "dq%Duel started.")
		return
	}
	if s.joined[uid] == nil {
		s.joined[uid] = map[int]bool{}
	}
	s.joined[uid][roomID] = true
	s.mu.Unlock()
}

func (s *Service) user(connID string) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.connUser[connID]
	return id, ok
}

// RoomList is the by% payload, with the trailing brace stripped like the original.
func (s *Service) RoomList() string {
	var b strings.Builder
	for _, r := range s.rooms {
		b.WriteString(r.String())
	}
	list := b.String()
	if len(list) > 0 {
		list = list[:len(list)-1]
	}
	return list
}
