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

// RoomSlotDetail — read-only деталь слота reservation в любом состоянии
// (P31, sweep never-joined комнат): если ListAdmittedPeers возвращает
// только admitted-слоты, RoomSlotDetail покрывает и reserve-слоты
// (ожидающий ticket) для предиката «все reserve истекли или слотов нет».
type RoomSlotDetail struct {
	PeerID PeerID
	// State — текущее состояние слота (ReservationSlotReserved /
	// ReservationSlotAdmitted; никогда None — отсутствующие слоты не
	// перечисляются).
	State ReservationSlot
	// ExpiresAt — дедлайн reserved-слота (TTL тикета); нулевое у admitted
	// (у вошедшего слота дедлайна тикета больше нет).
	ExpiresAt time.Time
	// LastSeen — timestamp grace admitted-слота (та же семантика, что у
	// AdmittedSlot.LastSeen); нулевое у reserved-слота.
	LastSeen time.Time
}
