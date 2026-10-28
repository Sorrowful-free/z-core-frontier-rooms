package domain

import "time"

// ClaimsKind — credential type in claims: a one-time join ticket or a
// reconnect token for resuming under the same peerID (P20).
type ClaimsKind byte

const (
	ClaimsKindJoin ClaimsKind = iota
	ClaimsKindReconnect
)

type Claims struct {
	// Kind — join (join ticket) or reconnect (reconnect token, P20).
	Kind     ClaimsKind
	RoomID   RoomID
	PeerID   PeerID
	NickName string
	// HostID - strict binding admit (P18): the assigned ENet host.
	// HostIDUnbound - ticket is not bound (WS-only deploy); ENet admit
	// rejects it with ErrWrongHost.
	HostID    HostID
	IssuedAt  time.Time
	ExpiresAt time.Time
}
