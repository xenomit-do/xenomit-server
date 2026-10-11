// Package spatial is a uniform grid that replaces the O(n²) spacemap scan.
package spatial

import "math"

// Grid indexes entity ids by cell.
type Grid struct {
	cell  int32
	cells map[int64]map[int32]struct{}
	pos   map[int32][2]int32
}

func New(cell int32) *Grid {
	if cell <= 0 {
		cell = 500
	}
	return &Grid{
		cell:  cell,
		cells: map[int64]map[int32]struct{}{},
		pos:   map[int32][2]int32{},
	}
}

func (g *Grid) Upsert(id, x, y int32) {
	if old, ok := g.pos[id]; ok {
		g.removeCell(id, old[0], old[1])
	}
	g.pos[id] = [2]int32{x, y}
	k := g.key(x, y)
	bucket := g.cells[k]
	if bucket == nil {
		bucket = map[int32]struct{}{}
		g.cells[k] = bucket
	}
	bucket[id] = struct{}{}
}

func (g *Grid) Remove(id int32) {
	old, ok := g.pos[id]
	if !ok {
		return
	}
	g.removeCell(id, old[0], old[1])
	delete(g.pos, id)
}

func (g *Grid) removeCell(id, x, y int32) {
	k := g.key(x, y)
	bucket := g.cells[k]
	delete(bucket, id)
	if len(bucket) == 0 {
		delete(g.cells, k)
	}
}

func (g *Grid) key(x, y int32) int64 {
	cx := floorDiv(x, g.cell)
	cy := floorDiv(y, g.cell)
	return int64(cx)<<32 | int64(uint32(cy))
}

// QueryRadius returns ids whose stored position is within r of (x, y), excluding self.
func (g *Grid) QueryRadius(x, y, r int32) []int32 {
	if r < 0 {
		return nil
	}
	minX := floorDiv(x-r, g.cell)
	maxX := floorDiv(x+r, g.cell)
	minY := floorDiv(y-r, g.cell)
	maxY := floorDiv(y+r, g.cell)
	rr := float64(r)
	var out []int32
	for cx := minX; cx <= maxX; cx++ {
		for cy := minY; cy <= maxY; cy++ {
			k := int64(cx)<<32 | int64(uint32(cy))
			for id := range g.cells[k] {
				p := g.pos[id]
				if math.Hypot(float64(p[0]-x), float64(p[1]-y)) <= rr {
					out = append(out, id)
				}
			}
		}
	}
	return out
}

func floorDiv(a, b int32) int32 {
	if a >= 0 {
		return a / b
	}
	return -((-a + b - 1) / b)
}
