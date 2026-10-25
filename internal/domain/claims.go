package domain

import "time"

// ClaimsKind — тип credential'а в claims: одноразовый join-ticket или
// reconnect-токен v4 для resume под тем же peerID (P20).
type ClaimsKind byte

const (
	ClaimsKindJoin ClaimsKind = iota
	ClaimsKindReconnect
)

type Claims struct {
	// Kind — join (ticket v2/v3) или reconnect (ticket v4, P20).
	Kind     ClaimsKind
	RoomID   RoomID
	PeerID   PeerID
	NickName string
	// HostID - strict binding admit (P18): назначенный ENet host.
	// HostIDAny - ticket не привязан, admit на любом host'е.
	HostID    HostID
	IssuedAt  time.Time
	ExpiresAt time.Time
}
