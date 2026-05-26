package domain

import "errors"

// Sentinel-ошибки reservation; маппинг — delivery/errors/reservation_reject.go (OpCode 0x70–0x7F).

var (
	ErrReservationInternal        = errors.New("reservation: internal")
	ErrReservationNotFound        = errors.New("reservation not found")
	ErrReservationAlreadyExists   = errors.New("reservation already exists")
	ErrReservationFull            = errors.New("reservation full")
	ErrReservationAlreadyAdmitted = errors.New("reservation already admitted")
	ErrReservationNotReserved     = errors.New("reservation not reserved")
	ErrReservationExpired         = errors.New("reservation expired")
)
