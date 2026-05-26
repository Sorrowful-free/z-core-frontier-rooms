package domain

// ReservationSlot — состояние слота peer в reservation (без деталей adapter).
type ReservationSlot int

const (
	ReservationSlotNone ReservationSlot = iota
	ReservationSlotReserved
	ReservationSlotAdmitted
)
