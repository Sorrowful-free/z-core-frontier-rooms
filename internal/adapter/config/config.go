package config

import (
	admissionadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/admission"
	httpauthadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/httpauth"
	httplimitsadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/httplimits"
	zaplogadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/zap"
	reservationadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/reservation"
	serveradapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/server"
	adaptertransport "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport"
)

// Config — корневая конфигурация процесса; поля — типы из соответствующих adapter-пакетов.
type Config struct {
	Logging     zaplogadapter.RootConfig
	Server      serveradapter.ServerConfig
	Admission   admissionadapter.AdmissionConfig
	HTTPAuth    httpauthadapter.HTTPAuthConfig
	HTTPLimits  httplimitsadapter.HTTPLimitsConfig
	Reservation reservationadapter.ReservationConfig
	Transport   adaptertransport.TransportConfig
}
