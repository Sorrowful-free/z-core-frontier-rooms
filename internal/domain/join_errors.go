package domain

import "errors"

// Join/admit sentinel errors; delivery maps them to OpCode (see opcodes.go).

var (
	ErrEmptyToken         = errors.New("empty token")
	ErrInvalidToken       = errors.New("invalid token")
	ErrExpiredToken       = errors.New("expired token")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrPeerNotFound       = errors.New("peer not found")
	ErrJoinDenied         = errors.New("join denied")
	ErrReplaceFailed      = errors.New("replace failed")
	ErrPeerStartFailed    = errors.New("peer start failed")
	ErrRoomFull           = errors.New("room full")
	ErrPeerAlreadyInRoom  = errors.New("peer already in room")
	// ErrTicketSlotHeld — the peer slot is already taken in reservation (reserved or admitted); a repeated Issue is forbidden (policy A).
	ErrTicketSlotHeld = errors.New("ticket slot held")
	// ErrWrongHost - ticket is bound to a different ENet host (claims.hostID != the admitting host); admit is rejected (P18).
	ErrWrongHost = errors.New("ticket bound to another enet host")
	// ErrPeerStale — leave arrived with a peer object already evicted from the room
	// (Replace/resume): stale teardown of the old connection, an expected race (P20).
	ErrPeerStale = errors.New("peer stale")
)
