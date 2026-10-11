package movement

import (
	"testing"
	"time"
)

func TestDeadReckon(t *testing.T) {
	var s State
	s.X, s.Y = 0, 0
	start := time.Unix(0, 0)
	ms, ok := Begin(&s, 300, 0, 300, start)
	if !ok || ms != 1000 {
		t.Fatalf("ms=%d ok=%v", ms, ok)
	}
	x, y := Actual(&s, start.Add(500*time.Millisecond))
	if x != 150 || y != 0 {
		t.Fatalf("midpoint %d,%d", x, y)
	}
	x, y = Actual(&s, start.Add(2*time.Second))
	if x != 300 || y != 0 || s.Moving {
		t.Fatalf("end %d,%d moving=%v", x, y, s.Moving)
	}
}

func TestZeroSpeed(t *testing.T) {
	var s State
	_, ok := Begin(&s, 10, 10, 0, time.Now())
	if ok {
		t.Fatal("expected no move")
	}
}
