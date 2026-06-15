package domain

import "time"

// ReservationSlot — состояние слота peer в reservation (без деталей adapter).
type ReservationSlot int

const (
	ReservationSlotNone ReservationSlot = iota
	ReservationSlotReserved
	ReservationSlotAdmitted
)

// AdmittedSlot — admitted-слот peer в reservation (для orphan sweep).
type AdmittedSlot struct {
	PeerID     PeerID
	AdmittedAt time.Time
}
