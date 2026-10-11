package adminops

import (
	"testing"

	"github.com/xenomit/server/pkg/events"
	"github.com/xenomit/server/services/player"
	"github.com/xenomit/server/services/world"
)

func TestGiveCommand(t *testing.T) {
	accounts := player.New()
	accounts.Seed()
	if _, err := accounts.Register("bob", "bob", "Bob", "bob@localhost"); err != nil {
		t.Fatal(err)
	}
	ops := New(world.New(world.Deps{Save: accounts}), accounts, events.NewLog())
	msg, ok := ops.Command(1, "/give Bob 500 12")
	if !ok {
		t.Fatal("command not handled")
	}
	acc, found := accounts.ByName("Bob")
	if !found || acc.Credits != 10500 || acc.Uridium != 12 {
		t.Fatalf("balance credits=%d uri=%d (%s)", acc.Credits, acc.Uridium, msg)
	}
	if _, ok := ops.Command(1, "/nope"); ok {
		t.Fatal("unknown command was handled")
	}
}
