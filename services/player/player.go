package player

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/xenomit/server/pkg/model"
)

var (
	ErrAuth    = errors.New("invalid credentials")
	ErrBanned  = errors.New("banned")
	ErrSession = errors.New("invalid session")
	ErrExists  = errors.New("account exists")
	ErrMissing = errors.New("account not found")
)

// Account is the system-of-record row. The shard keeps a copy while the player is online.
type Account struct {
	UserID    int64
	Username  string
	PilotName string
	Email     string
	passHash  string
	FactionID int
	ShipID    int
	SessionID string
	ClanID    int
	ClanTag   string
	RankID    int
	Admin     bool
	Banned    bool
	BanReason string
	Credits   int64
	Uridium   int64
	Honor     float64
	XP        float64
	Premium   bool
	X, Y      int32
	MapID     int
	Config    int
	Items     map[string]int
	DamageMod int32
	ChatPerm  int
}

// Service is the player-service write path. Other services call it in-process;
// the protobuf SessionService is the same contract over gRPC.
type Service struct {
	mu        sync.Mutex
	users     map[int64]*Account
	names     map[string]int64
	token     map[string]int64
	bySession map[string]int64
	txns      map[string]struct{}
	next      int64
}

func New() *Service {
	return &Service{
		users:     map[int64]*Account{},
		names:     map[string]int64{},
		token:     map[string]int64{},
		bySession: map[string]int64{},
		txns:      map[string]struct{}{},
	}
}

// Seed creates the local pilot/pilot account on company 1.
func (s *Service) Seed() {
	acc, err := s.Register("pilot", "pilot", "Pilot", "pilot@localhost")
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	acc.Admin = true
	acc.FactionID = 1
	acc.ShipID = 10
	acc.Premium = true
	acc.Credits = 100000
	acc.Uridium = 20000
	acc.MapID = 1
	acc.X, acc.Y = 1000, 1000
	acc.ChatPerm = 1
	s.applyShipLocked(acc)
}

func (s *Service) Register(username, password, pilot, email string) (*Account, error) {
	username = strings.TrimSpace(username)
	pilot = strings.TrimSpace(pilot)
	if username == "" || password == "" || pilot == "" {
		return nil, fmt.Errorf("username, password, and pilot name are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := strings.ToLower(username)
	if _, ok := s.names[key]; ok {
		return nil, ErrExists
	}
	if _, ok := s.names[strings.ToLower(pilot)]; ok {
		return nil, ErrExists
	}
	s.next++
	acc := &Account{
		UserID:    s.next,
		Username:  username,
		PilotName: pilot,
		Email:     email,
		passHash:  hashPassword(password),
		ShipID:    1,
		RankID:    1,
		Config:    1,
		Items:     map[string]int{},
		Credits:   10000,
	}
	s.applyShipLocked(acc)
	s.users[acc.UserID] = acc
	s.names[key] = acc.UserID
	s.names[strings.ToLower(pilot)] = acc.UserID
	return acc, nil
}

func (s *Service) applyShipLocked(acc *Account) {
	ship := model.ShipByID(acc.ShipID)
	acc.DamageMod = ship.Damage
}

// Login checks the password and returns a bearer token.
func (s *Service) Login(username, password string) (token string, userID int64, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.names[strings.ToLower(strings.TrimSpace(username))]
	if !ok {
		return "", 0, ErrAuth
	}
	acc := s.users[id]
	if acc.Banned {
		return "", 0, ErrBanned
	}
	if acc.passHash != hashPassword(password) {
		return "", 0, ErrAuth
	}
	token, err = randomToken(16)
	if err != nil {
		return "", 0, err
	}
	s.token[token] = acc.UserID
	return token, acc.UserID, nil
}

func (s *Service) Bearer(token string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.token[token]
	if !ok {
		return 0, ErrAuth
	}
	return id, nil
}

// IssueSession rotates the session id the game client presents on :8080.
func (s *Service) IssueSession(userID int64) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	acc, ok := s.users[userID]
	if !ok {
		return "", ErrMissing
	}
	if acc.Banned {
		return "", ErrBanned
	}
	sid, err := randomToken(16)
	if err != nil {
		return "", err
	}
	if acc.SessionID != "" {
		delete(s.bySession, acc.SessionID)
	}
	acc.SessionID = sid
	s.bySession[sid] = acc.UserID
	return sid, nil
}

// BySession resolves the dosid cookie the map page and loading screen use.
func (s *Service) BySession(sessionID string) (model.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.bySession[sessionID]
	if !ok || sessionID == "" {
		return model.Snapshot{}, ErrSession
	}
	acc := s.users[id]
	if acc == nil || acc.SessionID != sessionID {
		return model.Snapshot{}, ErrSession
	}
	if acc.Banned {
		return model.Snapshot{}, ErrBanned
	}
	return s.snapshotLocked(acc), nil
}

func (s *Service) Validate(userID int64, sessionID string) (model.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	acc, ok := s.users[userID]
	if !ok || acc.SessionID == "" || acc.SessionID != sessionID {
		return model.Snapshot{}, ErrSession
	}
	if acc.Banned {
		return model.Snapshot{}, ErrBanned
	}
	return s.snapshotLocked(acc), nil
}

func (s *Service) Snapshot(userID int64) (model.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	acc, ok := s.users[userID]
	if !ok {
		return model.Snapshot{}, ErrMissing
	}
	return s.snapshotLocked(acc), nil
}

func (s *Service) snapshotLocked(acc *Account) model.Snapshot {
	ship := model.ShipByID(acc.ShipID)
	hp, shield := ship.HP, ship.Shield
	return model.Snapshot{
		UserID: acc.UserID, PilotName: acc.PilotName, ShipID: acc.ShipID, LootID: ship.LootID,
		FactionID: acc.FactionID, ClanID: acc.ClanID, ClanTag: acc.ClanTag, RankID: acc.RankID,
		MapID: acc.MapID, X: acc.X, Y: acc.Y,
		Speed: ship.Speed, HP: hp, MaxHP: hp, Shield: shield, MaxShield: shield,
		Damage:  ship.Damage + bonusDamage(acc),
		Credits: acc.Credits, Uridium: acc.Uridium, Honor: acc.Honor, Experience: acc.XP,
		Premium: acc.Premium, Level: model.LevelFromXP(acc.XP), Admin: acc.Admin,
		SessionID: acc.SessionID,
	}
}

func bonusDamage(acc *Account) int32 {
	var d int32
	for id, n := range acc.Items {
		if id == "lf-3" {
			d += 40 * int32(n)
		}
	}
	return d
}

// ApplyTx commits a ledger mutation once per idempotency key.
func (s *Service) ApplyTx(tx model.Tx) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if tx.ID != "" {
		if _, ok := s.txns[tx.ID]; ok {
			return nil
		}
	}
	acc, ok := s.users[tx.UserID]
	if !ok {
		return ErrMissing
	}
	if acc.Credits+tx.Credits < 0 || acc.Uridium+tx.Uridium < 0 {
		return fmt.Errorf("insufficient funds")
	}
	acc.Credits += tx.Credits
	acc.Uridium += tx.Uridium
	acc.Honor += tx.Honor
	acc.XP += tx.Experience
	if tx.ID != "" {
		s.txns[tx.ID] = struct{}{}
	}
	return nil
}

// SaveState writes position and map back from a shard checkpoint.
func (s *Service) SaveState(snap model.Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	acc, ok := s.users[snap.UserID]
	if !ok {
		return
	}
	acc.MapID = snap.MapID
	acc.X, acc.Y = snap.X, snap.Y
}

func (s *Service) SetFaction(userID int64, faction int) error {
	if faction < 1 || faction > 3 {
		return fmt.Errorf("faction must be 1, 2, or 3")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	acc, ok := s.users[userID]
	if !ok {
		return ErrMissing
	}
	acc.FactionID = faction
	acc.MapID = model.HomeMap(faction)
	acc.X, acc.Y = 1000, 1000
	return nil
}

func (s *Service) SetShip(userID int64, shipID int) error {
	if model.ShipByID(shipID).NPC || model.Ships[shipID].ID == 0 {
		return fmt.Errorf("unknown ship %d", shipID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	acc, ok := s.users[userID]
	if !ok {
		return ErrMissing
	}
	acc.ShipID = shipID
	return nil
}

func (s *Service) SetAdmin(userID int64, on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	acc, ok := s.users[userID]
	if !ok {
		return ErrMissing
	}
	acc.Admin = on
	if on {
		acc.ChatPerm = 1
	} else {
		acc.ChatPerm = 0
	}
	return nil
}

func (s *Service) SetPremium(userID int64, on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	acc, ok := s.users[userID]
	if !ok {
		return ErrMissing
	}
	acc.Premium = on
	return nil
}

func (s *Service) List() []Account {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Account, 0, len(s.users))
	for _, acc := range s.users {
		cp := *acc
		if acc.Items != nil {
			cp.Items = make(map[string]int, len(acc.Items))
			for k, n := range acc.Items {
				cp.Items[k] = n
			}
		}
		out = append(out, cp)
	}
	return out
}

func (s *Service) Ban(userID int64, reason string, banned bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	acc, ok := s.users[userID]
	if !ok {
		return ErrMissing
	}
	acc.Banned = banned
	acc.BanReason = reason
	if banned {
		delete(s.bySession, acc.SessionID)
		acc.SessionID = ""
	}
	return nil
}

func (s *Service) SetClan(userID int64, clanID int, tag string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if acc, ok := s.users[userID]; ok {
		acc.ClanID = clanID
		acc.ClanTag = tag
	}
}

func (s *Service) AddItem(userID int64, itemID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	acc, ok := s.users[userID]
	if !ok {
		return ErrMissing
	}
	if acc.Items == nil {
		acc.Items = map[string]int{}
	}
	acc.Items[itemID]++
	return nil
}

func (s *Service) ByName(name string) (Account, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.names[strings.ToLower(name)]
	if !ok {
		return Account{}, false
	}
	return *s.users[id], true
}

func (s *Service) Get(userID int64) (Account, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	acc, ok := s.users[userID]
	if !ok {
		return Account{}, false
	}
	cp := *acc
	return cp, true
}

func hashPassword(pw string) string {
	sum := sha256.Sum256([]byte("xenomit-v1:" + pw))
	return hex.EncodeToString(sum[:])
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
