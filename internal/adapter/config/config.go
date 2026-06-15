package config

import (
	admissionadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/admission"
	httpauthadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/httpauth"
	httplimitsadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/httplimits"
	reservationadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/reservation"
)

// Config — корневая конфигурация процесса; поля — типы из соответствующих adapter-пакетов.
type Config struct {
	Admission   admissionadapter.AdmissionConfig
	HTTPAuth    httpauthadapter.HTTPAuthConfig
	HTTPLimits  httplimitsadapter.HTTPLimitsConfig
	Reservation reservationadapter.ReservationConfig
}
