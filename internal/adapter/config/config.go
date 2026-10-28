package config

import (
	admissionadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/admission"
	httpauthadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/httpauth"
	httplimitsadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/httplimits"
	zaplogadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/zap"
	metricsadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/metrics"
	statepolicy "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/policy/state"
	reservationadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/reservation"
	serveradapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/server"
	adaptertransport "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport"
	enetadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/enet"
)

// Config — root process configuration; fields are types from the corresponding adapter packages.
type Config struct {
	Logging     zaplogadapter.RootConfig
	Server      serveradapter.ServerConfig
	Admission   admissionadapter.AdmissionConfig
	HTTPAuth    httpauthadapter.HTTPAuthConfig
	HTTPLimits  httplimitsadapter.HTTPLimitsConfig
	Reservation reservationadapter.ReservationConfig
	StatePolicy statepolicy.StatePolicyConfig
	Transport   adaptertransport.TransportConfig
	Enet        enetadapter.EnetConfig
	Metrics     metricsadapter.MetricsConfig
}
