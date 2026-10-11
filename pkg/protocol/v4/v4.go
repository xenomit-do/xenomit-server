// Package v4 is the legacy pipe protocol as a first-class client version.
// On the v4 socket the "0|" string is the whole message. On v10 the same
// strings ride inside command 4224 (see pkg/protocol/v10).
package v4

import (
	"strings"

	"github.com/xenomit/server/pkg/protocol/legacy"
)

// Packet is one v4 message.
type Packet struct {
	Parts []string
}

// Parse splits a v4 payload. Messages may end in newline or NUL.
func Parse(raw string) Packet {
	raw = strings.TrimRight(raw, "\n\r\x00")
	return Packet{Parts: legacy.Split(raw)}
}

func (p Packet) Cmd() string {
	if len(p.Parts) < 2 {
		return ""
	}
	return p.Parts[1]
}

func (p Packet) String() string { return legacy.Join(p.Parts...) }

// Move reports whether this is a v4 move, "0|1|<x>|<y>".
func (p Packet) Move() (x, y string, ok bool) {
	if len(p.Parts) >= 4 && p.Parts[0] == "0" && p.Parts[1] == "1" {
		return p.Parts[2], p.Parts[3], true
	}
	return "", "", false
}

// Login reports whether this is a v4 login, "0|l|<userId>|<sessionId>".
func (p Packet) Login() (userID, sessionID string, ok bool) {
	if len(p.Parts) >= 4 && p.Parts[0] == "0" && p.Parts[1] == "l" {
		return p.Parts[2], p.Parts[3], true
	}
	return "", "", false
}
