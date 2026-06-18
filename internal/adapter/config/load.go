package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	admissionadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/admission"
	httpauthadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/httpauth"
	httplimitsadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/httplimits"
	zaplogadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/zap"
	reservationadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/reservation"
	serveradapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/server"
	adaptertransport "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport"
)

const (
	envLogMode  = "LOG_MODE"
	envHTTPAddr = "HTTP_ADDR"

	envAdmissionSecret   = "ADMISSION_SECRET"
	envAdmissionTTL      = "ADMISSION_TTL"
	envAdmissionPassword = "ADMISSION_PASSWORD"

	envHTTPAPIKey       = "HTTP_API_KEY"
	envHTTPAuthDisabled = "HTTP_AUTH_DISABLED"

	envMaxRooms             = "MAX_ROOMS"
	envHTTPCreateRatePerMin = "HTTP_CREATE_RATE_PER_MIN"
	envHTTPIssueRatePerMin  = "HTTP_ISSUE_RATE_PER_MIN"

	envReservationOrphanAdmittedTTL   = "RESERVATION_ORPHAN_ADMITTED_TTL"
	envReservationOrphanSweepInterval = "RESERVATION_ORPHAN_SWEEP_INTERVAL"

	envTransportMaxIncomingFrameBytes = "TRANSPORT_MAX_INCOMING_FRAME_BYTES"
	envTransportRoomIncomingQueue     = "TRANSPORT_ROOM_INCOMING_QUEUE"
	envTransportPeerOutboundQueue     = "TRANSPORT_PEER_OUTBOUND_QUEUE"
	envTransportEnetIncomingQueue     = "TRANSPORT_ENET_INCOMING_QUEUE"
)

// LoadFromEnv читает переменные окружения и собирает Config.
func LoadFromEnv() (*Config, error) {
	logging, err := loadLoggingFromEnv()
	if err != nil {
		return nil, err
	}
	serverCfg, err := loadServerFromEnv()
	if err != nil {
		return nil, err
	}
	admission, err := loadAdmissionFromEnv()
	if err != nil {
		return nil, err
	}
	httpAuth, err := loadHTTPAuthFromEnv()
	if err != nil {
		return nil, err
	}
	httpLimits, err := loadHTTPLimitsFromEnv()
	if err != nil {
		return nil, err
	}
	reservationCfg, err := loadReservationFromEnv()
	if err != nil {
		return nil, err
	}
	transportCfg, err := loadTransportFromEnv()
	if err != nil {
		return nil, err
	}
	return &Config{
		Logging:     logging,
		Server:      serverCfg,
		Admission:   admission,
		HTTPAuth:    httpAuth,
		HTTPLimits:  httpLimits,
		Reservation: reservationCfg,
		Transport:   transportCfg,
	}, nil
}

func loadLoggingFromEnv() (zaplogadapter.RootConfig, error) {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv(envLogMode)))
	if mode == "" {
		mode = zaplogadapter.DefaultLogMode
	}
	cfg := zaplogadapter.RootConfig{Mode: mode}
	if err := cfg.Validate(); err != nil {
		return zaplogadapter.RootConfig{}, err
	}
	return cfg, nil
}

func loadServerFromEnv() (serveradapter.ServerConfig, error) {
	addr := os.Getenv(envHTTPAddr)
	if addr == "" {
		addr = serveradapter.DefaultHTTPAddr
	}
	cfg := serveradapter.ServerConfig{HTTPAddr: addr}
	if err := cfg.Validate(); err != nil {
		return serveradapter.ServerConfig{}, err
	}
	return cfg, nil
}

func loadAdmissionFromEnv() (admissionadapter.AdmissionConfig, error) {
	ttl := admissionadapter.DefaultAdmissionTTL()
	if raw := os.Getenv(envAdmissionTTL); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return admissionadapter.AdmissionConfig{}, fmt.Errorf("%s: %w", envAdmissionTTL, err)
		}
		ttl = parsed
	}

	cfg := admissionadapter.AdmissionConfig{
		Secret:   []byte(os.Getenv(envAdmissionSecret)),
		TTL:      ttl,
		Password: os.Getenv(envAdmissionPassword),
	}
	if err := cfg.Validate(); err != nil {
		return admissionadapter.AdmissionConfig{}, err
	}
	return cfg, nil
}

func loadHTTPAuthFromEnv() (httpauthadapter.HTTPAuthConfig, error) {
	cfg := httpauthadapter.HTTPAuthConfig{
		APIKey:   os.Getenv(envHTTPAPIKey),
		Disabled: strings.EqualFold(os.Getenv(envHTTPAuthDisabled), "true"),
	}
	if err := cfg.Validate(); err != nil {
		return httpauthadapter.HTTPAuthConfig{}, err
	}
	return cfg, nil
}

func loadHTTPLimitsFromEnv() (httplimitsadapter.HTTPLimitsConfig, error) {
	maxRooms, err := intFromEnv(envMaxRooms, httplimitsadapter.DefaultMaxRooms)
	if err != nil {
		return httplimitsadapter.HTTPLimitsConfig{}, err
	}
	createRate, err := intFromEnv(envHTTPCreateRatePerMin, httplimitsadapter.DefaultCreateRoomsPerMinute)
	if err != nil {
		return httplimitsadapter.HTTPLimitsConfig{}, err
	}
	issueRate, err := intFromEnv(envHTTPIssueRatePerMin, httplimitsadapter.DefaultIssueTicketsPerMinute)
	if err != nil {
		return httplimitsadapter.HTTPLimitsConfig{}, err
	}

	cfg := httplimitsadapter.HTTPLimitsConfig{
		MaxRooms:              maxRooms,
		CreateRoomsPerMinute:  createRate,
		IssueTicketsPerMinute: issueRate,
	}
	if err := cfg.Validate(); err != nil {
		return httplimitsadapter.HTTPLimitsConfig{}, err
	}
	return cfg, nil
}

func loadTransportFromEnv() (adaptertransport.TransportConfig, error) {
	maxBytes, err := intFromEnv(envTransportMaxIncomingFrameBytes, adaptertransport.DefaultMaxIncomingFrameBytes)
	if err != nil {
		return adaptertransport.TransportConfig{}, err
	}
	roomQueue, err := intFromEnv(envTransportRoomIncomingQueue, adaptertransport.DefaultRoomIncomingQueueSize)
	if err != nil {
		return adaptertransport.TransportConfig{}, err
	}
	peerQueue, err := intFromEnv(envTransportPeerOutboundQueue, adaptertransport.DefaultPeerOutboundQueueSize)
	if err != nil {
		return adaptertransport.TransportConfig{}, err
	}
	enetQueue, err := intFromEnv(envTransportEnetIncomingQueue, adaptertransport.DefaultEnetIncomingQueueSize)
	if err != nil {
		return adaptertransport.TransportConfig{}, err
	}

	cfg := adaptertransport.TransportConfig{
		MaxIncomingFrameBytes: maxBytes,
		RoomIncomingQueueSize: roomQueue,
		PeerOutboundQueueSize: peerQueue,
		EnetIncomingQueueSize: enetQueue,
	}
	if err := cfg.Validate(); err != nil {
		return adaptertransport.TransportConfig{}, err
	}
	return cfg, nil
}

func loadReservationFromEnv() (reservationadapter.ReservationConfig, error) {
	orphanTTL := reservationadapter.DefaultOrphanAdmittedTTL
	if raw := os.Getenv(envReservationOrphanAdmittedTTL); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return reservationadapter.ReservationConfig{}, fmt.Errorf("%s: %w", envReservationOrphanAdmittedTTL, err)
		}
		orphanTTL = parsed
	}

	sweepInterval := reservationadapter.DefaultOrphanSweepInterval
	if raw := os.Getenv(envReservationOrphanSweepInterval); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return reservationadapter.ReservationConfig{}, fmt.Errorf("%s: %w", envReservationOrphanSweepInterval, err)
		}
		sweepInterval = parsed
	}

	cfg := reservationadapter.ReservationConfig{
		OrphanAdmittedTTL:   orphanTTL,
		OrphanSweepInterval: sweepInterval,
	}
	if err := cfg.Validate(); err != nil {
		return reservationadapter.ReservationConfig{}, err
	}
	return cfg, nil
}

func intFromEnv(name string, defaultValue int) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return value, nil
}
