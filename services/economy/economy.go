package economy

import (
	"fmt"
	"time"

	"github.com/xenomit/server/pkg/events"
	"github.com/xenomit/server/pkg/model"
)

// Ledger is the player-service balance port.
type Ledger interface {
	ApplyTx(model.Tx) error
	AddItem(userID int64, itemID string) error
	Snapshot(userID int64) (model.Snapshot, error)
}

// Item is a shop SKU.
type Item struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Uridium int64  `json:"uridium"`
	Credits int64  `json:"credits"`
}

// Catalog is the slice of the shop the economy service sells.
var Catalog = []Item{
	{ID: "lf-3", Name: "LF-3", Uridium: 10000},
}

// Service commits inventory and currency. Every mutation is an idempotent Tx
// on the economy.tx topic before the balance changes.
type Service struct {
	ledger Ledger
	log    *events.Log
}

func New(ledger Ledger, log *events.Log) *Service {
	return &Service{ledger: ledger, log: log}
}

func (s *Service) Apply(tx model.Tx) error {
	if err := s.ledger.ApplyTx(tx); err != nil {
		return err
	}
	if s.log != nil {
		s.log.Append("economy.tx", tx.ID, tx)
	}
	return nil
}

func (s *Service) Buy(userID int64, itemID string) error {
	var item Item
	var found bool
	for _, it := range Catalog {
		if it.ID == itemID {
			item = it
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("unknown item %s", itemID)
	}
	tx := model.Tx{
		ID:      fmt.Sprintf("buy-%d-%s-%d", userID, itemID, time.Now().UnixNano()),
		UserID:  userID,
		Reason:  "buy:" + itemID,
		Credits: -item.Credits,
		Uridium: -item.Uridium,
	}
	if err := s.Apply(tx); err != nil {
		return err
	}
	return s.ledger.AddItem(userID, itemID)
}

func (s *Service) Balance(userID int64) (credits, uridium int64, err error) {
	snap, err := s.ledger.Snapshot(userID)
	if err != nil {
		return 0, 0, err
	}
	return snap.Credits, snap.Uridium, nil
}
