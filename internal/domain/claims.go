package domain

import "time"

// ClaimsKind — тип credential'а в claims: одноразовый join-ticket или
// reconnect-токен для resume под тем же peerID (P20).
type ClaimsKind byte

const (
	ClaimsKindJoin ClaimsKind = iota
	ClaimsKindReconnect
)

type Claims struct {
	// Kind — join (join-ticket) или reconnect (reconnect-токен, P20).
	Kind     ClaimsKind
	RoomID   RoomID
	PeerID   PeerID
	NickName string
	// HostID - strict binding admit (P18): назначенный ENet host.
	// HostIDUnbound - ticket не привязан (WS-only deploy); на ENet-admit
	// отклоняется ErrWrongHost.
	HostID    HostID
	IssuedAt  time.Time
	ExpiresAt time.Time
}
