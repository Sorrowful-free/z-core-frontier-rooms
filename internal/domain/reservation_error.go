package domain

import "errors"

// Sentinel-ошибки внутри комнаты (после admit); маппинг — delivery/errors/in_room.go.

var (
	ErrReservationInternal        = errors.New("reservation: internal")
	ErrReservationNotFound        = errors.New("reservation not found")
	ErrReservationAlreadyExists   = errors.New("reservation already exists")
	ErrReservationFull            = errors.New("reservation full")
	ErrReservationAlreadyAdmitted = errors.New("reservation already admitted")
	ErrReservationNotReserved     = errors.New("reservation not reserved")
	ErrReservationExpired         = errors.New("reservation expired")
)
