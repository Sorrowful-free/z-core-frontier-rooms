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
	// LastSeen — единый timestamp grace слота: момент admit, последнего
	// disconnect-mark (MarkDisconnected) или resume-refresh (Reconnect) —
	// все три пишут одно поле (P20); от него считается grace-дедлайн.
	LastSeen time.Time
}
