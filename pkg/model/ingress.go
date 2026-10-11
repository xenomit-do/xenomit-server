package model

// Ingress is the gateway → world envelope published on in.<mapId>.
type Ingress struct {
	Type      string    `json:"type"`
	SessionID string    `json:"sessionId"`
	UserID    int64     `json:"userId"`
	MapID     int       `json:"mapId"`
	Packet    []byte    `json:"packet,omitempty"`
	Snapshot  *Snapshot `json:"snapshot,omitempty"`
}
