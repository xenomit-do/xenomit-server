package spatial

import "testing"

func TestQueryAndMove(t *testing.T) {
	g := New(500)
	g.Upsert(1, 100, 100)
	g.Upsert(2, 800, 100)
	g.Upsert(3, 100, 900)
	near := g.QueryRadius(100, 100, 200)
	if len(near) != 1 || near[0] != 1 {
		t.Fatalf("near %v", near)
	}
	g.Upsert(2, 120, 120)
	near = g.QueryRadius(100, 100, 50)
	found := map[int32]bool{}
	for _, id := range near {
		found[id] = true
	}
	if !found[1] || !found[2] || found[3] {
		t.Fatalf("after move %v", near)
	}
	g.Remove(2)
	near = g.QueryRadius(120, 120, 10)
	if len(near) != 0 {
		t.Fatalf("removed still present %v", near)
	}
}
