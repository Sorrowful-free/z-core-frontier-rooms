package domain

import "time"

// ReservationSlot — состояние слота peer в reservation (без деталей adapter).
type ReservationSlot int

const (
	ReservationSlotNone ReservationSlot = iota
	ReservationSlotReserved
	ReservationSlotAdmitted
)

// AdmittedSlot — admitted-слот peer в reservation (для orphan sweep и resume).
type AdmittedSlot struct {
	PeerID PeerID
	// NickName — ник из слота: единственный источник ника при resume (P20).
	NickName string
	// AdmittedAt — lastSeen: момент admit / последнего disconnect или
	// reconnect (MarkDisconnected/Reconnect его обновляют, P20).
	AdmittedAt time.Time
}
