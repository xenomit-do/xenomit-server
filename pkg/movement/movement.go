// Package movement is dead-reckoning. The server stores a start, a destination,
// and a duration; the position at time t is interpolated. Clients receive
// MoveCommand once, when the move starts.
package movement

import (
	"math"
	"time"
)

// State is one entity's current move.
type State struct {
	X, Y         int32
	FromX, FromY int32
	DestX, DestY int32
	Moving       bool
	Start        time.Time
	Duration     time.Duration
}

// Begin starts a move toward (destX, destY). speed is units per second.
// The returned duration in milliseconds is the timeToTarget field of MoveCommand.
// ok is false when speed is zero or the destination is the current cell.
func Begin(s *State, destX, destY, speed int32, now time.Time) (timeMs int32, ok bool) {
	if speed <= 0 {
		s.Moving = false
		return 0, false
	}
	dx := float64(destX - s.X)
	dy := float64(destY - s.Y)
	dist := math.Hypot(dx, dy)
	if dist < 1 {
		s.Moving = false
		return 0, false
	}
	ms := dist / float64(speed) * 1000
	s.FromX, s.FromY = s.X, s.Y
	s.DestX, s.DestY = destX, destY
	s.Start = now
	s.Duration = time.Duration(ms * float64(time.Millisecond))
	if s.Duration < time.Millisecond {
		s.Duration = time.Millisecond
	}
	s.Moving = true
	return int32(math.Round(ms)), true
}

// Actual writes the position at now back into s and returns it.
// When the move completes, Moving is cleared and the destination is snapped.
func Actual(s *State, now time.Time) (int32, int32) {
	if !s.Moving {
		return s.X, s.Y
	}
	elapsed := now.Sub(s.Start)
	if elapsed >= s.Duration || s.Duration <= 0 {
		s.Moving = false
		s.X, s.Y = s.DestX, s.DestY
		return s.X, s.Y
	}
	t := float64(elapsed) / float64(s.Duration)
	dx := float64(s.DestX - s.FromX)
	dy := float64(s.DestY - s.FromY)
	s.X = int32(math.Round(float64(s.FromX) + dx*t))
	s.Y = int32(math.Round(float64(s.FromY) + dy*t))
	return s.X, s.Y
}

// Clamp keeps a point inside the map rectangle.
func Clamp(x, y, w, h int32) (int32, int32) {
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	if w > 0 && x > w {
		x = w
	}
	if h > 0 && y > h {
		y = h
	}
	return x, y
}
