package domain

import "time"

// ReservationSlot — state of a peer slot in reservation (without adapter details).
type ReservationSlot int

const (
	ReservationSlotNone ReservationSlot = iota
	ReservationSlotReserved
	ReservationSlotAdmitted
)

// AdmittedSlot — admitted peer slot in reservation (for orphan sweep and resume).
type AdmittedSlot struct {
	PeerID PeerID
	// NickName — nickname from the slot: the only nickname source on resume (P20).
	NickName string
	// LastSeen — single grace timestamp of the slot: the moment of admit,
	// the last disconnect-mark (MarkDisconnected) or resume-refresh (Reconnect) —
	// all three write the same field (P20); the grace deadline is computed from it.
	LastSeen time.Time
}

// RoomSlotDetail — read-only detail of a reservation slot in any state
// (P31, sweep of never-joined rooms): while ListAdmittedPeers returns
// only admitted slots, RoomSlotDetail also covers reserve slots
// (pending ticket) for the "all reserves expired or there are no slots" predicate.
type RoomSlotDetail struct {
	PeerID PeerID
	// State — current slot state (ReservationSlotReserved /
	// ReservationSlotAdmitted; never None — missing slots are not
	// enumerated).
	State ReservationSlot
	// ExpiresAt — deadline of a reserved slot (ticket TTL); zero for admitted
	// (an admitted slot no longer has a ticket deadline).
	ExpiresAt time.Time
	// LastSeen — grace timestamp of an admitted slot (same semantics as
	// AdmittedSlot.LastSeen); zero for a reserved slot.
	LastSeen time.Time
}
