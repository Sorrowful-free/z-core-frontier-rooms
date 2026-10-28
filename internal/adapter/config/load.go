package config

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

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

const (
	envLogMode  = "LOG_MODE"
	envHTTPAddr = "HTTP_ADDR"

	envAdmissionSecret = "ADMISSION_SECRET"
	envAdmissionTTL    = "ADMISSION_TTL"

	envHTTPAPIKey       = "HTTP_API_KEY"
	envHTTPAuthDisabled = "HTTP_AUTH_DISABLED"

	envMaxRooms             = "MAX_ROOMS"
	envHTTPCreateRatePerMin = "HTTP_CREATE_RATE_PER_MIN"
	envHTTPIssueRatePerMin  = "HTTP_ISSUE_RATE_PER_MIN"
	envHTTPDeleteRatePerMin = "HTTP_DELETE_RATE_PER_MIN"
	envHTTPListRatePerMin   = "HTTP_LIST_RATE_PER_MIN"

	envReservationReconnectTTL        = "RESERVATION_RECONNECT_TTL"
	envReservationOrphanSweepInterval = "RESERVATION_ORPHAN_SWEEP_INTERVAL"

	envStatePolicyAllowMasterMigration = "STATE_POLICY_ALLOW_MASTER_MIGRATION"

	envTransportMaxIncomingFrameBytes = "TRANSPORT_MAX_INCOMING_FRAME_BYTES"
	envTransportRoomIncomingQueue     = "TRANSPORT_ROOM_INCOMING_QUEUE"
	envTransportPeerOutboundQueue     = "TRANSPORT_PEER_OUTBOUND_QUEUE"
	envTransportEnetIncomingQueue     = "TRANSPORT_ENET_INCOMING_QUEUE"
	envTransportPeerIdleTimeout       = "TRANSPORT_PEER_IDLE_TIMEOUT"
	envTransportPeerIdleSweepInterval = "TRANSPORT_PEER_IDLE_SWEEP_INTERVAL"
	envTransportWSWriteDeadline       = "TRANSPORT_WS_WRITE_DEADLINE"
	envTransportPeerCloseDrainTimeout = "TRANSPORT_PEER_CLOSE_DRAIN_TIMEOUT"

	envEnetListenPorts      = "ENET_LISTEN_PORTS"
	envEnetPeerLimit        = "ENET_PEER_LIMIT"
	envEnetAdmitTimeout     = "ENET_ADMIT_TIMEOUT"
	envEnetServiceTimeoutMs = "ENET_SERVICE_TIMEOUT_MS"

	envAdminListenAddr = "ADMIN_LISTEN_ADDR"
)

// LoadFromEnv reads environment variables and builds Config.
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
	statePolicyCfg, err := loadStatePolicyFromEnv()
	if err != nil {
		return nil, err
	}
	transportCfg, err := loadTransportFromEnv()
	if err != nil {
		return nil, err
	}
	enetCfg, err := loadEnetFromEnv()
	if err != nil {
		return nil, err
	}
	metricsCfg, err := loadMetricsFromEnv()
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
		StatePolicy: statePolicyCfg,
		Transport:   transportCfg,
		Enet:        enetCfg,
		Metrics:     metricsCfg,
	}, nil
}

// loadMetricsFromEnv builds metrics.MetricsConfig: ADMIN_LISTEN_ADDR —
// admin listener `GET /metrics`; empty (default) — endpoint disabled.
func loadMetricsFromEnv() (metricsadapter.MetricsConfig, error) {
	cfg := metricsadapter.MetricsConfig{
		AdminListenAddr: strings.TrimSpace(os.Getenv(envAdminListenAddr)),
	}
	if err := cfg.Validate(); err != nil {
		return metricsadapter.MetricsConfig{}, err
	}
	return cfg, nil
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
	ttl, err := durationFromEnv(envAdmissionTTL, admissionadapter.DefaultAdmissionTTL())
	if err != nil {
		return admissionadapter.AdmissionConfig{}, err
	}

	cfg := admissionadapter.AdmissionConfig{
		Secret: []byte(os.Getenv(envAdmissionSecret)),
		TTL:    ttl,
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
	deleteRate, err := intFromEnv(envHTTPDeleteRatePerMin, httplimitsadapter.DefaultDeleteRoomsPerMinute)
	if err != nil {
		return httplimitsadapter.HTTPLimitsConfig{}, err
	}
	listRate, err := intFromEnv(envHTTPListRatePerMin, httplimitsadapter.DefaultListRoomsPerMinute)
	if err != nil {
		return httplimitsadapter.HTTPLimitsConfig{}, err
	}

	cfg := httplimitsadapter.HTTPLimitsConfig{
		MaxRooms:              maxRooms,
		CreateRoomsPerMinute:  createRate,
		IssueTicketsPerMinute: issueRate,
		DeleteRoomsPerMinute:  deleteRate,
		ListRoomsPerMinute:    listRate,
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

	peerIdleTimeout, err := durationFromEnv(envTransportPeerIdleTimeout, adaptertransport.DefaultPeerIdleTimeout)
	if err != nil {
		return adaptertransport.TransportConfig{}, err
	}
	idleSweepInterval, err := durationFromEnv(envTransportPeerIdleSweepInterval, adaptertransport.DefaultPeerIdleSweepInterval)
	if err != nil {
		return adaptertransport.TransportConfig{}, err
	}
	wsWriteDeadline, err := durationFromEnv(envTransportWSWriteDeadline, adaptertransport.DefaultWSWriteDeadline)
	if err != nil {
		return adaptertransport.TransportConfig{}, err
	}
	peerCloseDrainTimeout, err := durationFromEnv(envTransportPeerCloseDrainTimeout, adaptertransport.DefaultPeerCloseDrainTimeout)
	if err != nil {
		return adaptertransport.TransportConfig{}, err
	}

	cfg := adaptertransport.TransportConfig{
		MaxIncomingFrameBytes: maxBytes,
		RoomIncomingQueueSize: roomQueue,
		PeerOutboundQueueSize: peerQueue,
		EnetIncomingQueueSize: enetQueue,
		PeerIdleTimeout:       peerIdleTimeout,
		PeerIdleSweepInterval: idleSweepInterval,
		WSWriteDeadline:       wsWriteDeadline,
		PeerCloseDrainTimeout: peerCloseDrainTimeout,
	}
	if err := cfg.Validate(); err != nil {
		return adaptertransport.TransportConfig{}, err
	}
	return cfg, nil
}

// loadReservationFromEnv builds ReservationConfig (P20): RESERVATION_RECONNECT_TTL —
// grace of the admitted slot from lastSeen.
func loadReservationFromEnv() (reservationadapter.ReservationConfig, error) {
	reconnectTTL, err := durationFromEnv(envReservationReconnectTTL, reservationadapter.DefaultReconnectTTL)
	if err != nil {
		return reservationadapter.ReservationConfig{}, err
	}

	sweepInterval, err := durationFromEnv(envReservationOrphanSweepInterval, reservationadapter.DefaultOrphanSweepInterval)
	if err != nil {
		return reservationadapter.ReservationConfig{}, err
	}

	cfg := reservationadapter.ReservationConfig{
		ReconnectTTL:        reconnectTTL,
		OrphanSweepInterval: sweepInterval,
	}
	if err := cfg.Validate(); err != nil {
		return reservationadapter.ReservationConfig{}, err
	}
	return cfg, nil
}

// loadStatePolicyFromEnv builds state.StatePolicyConfig (P33):
// STATE_POLICY_ALLOW_MASTER_MIGRATION — global master-migration mode of the
// deployment; empty → true (historical behavior), explicit value — ParseBool,
// garbage → error.
func loadStatePolicyFromEnv() (statepolicy.StatePolicyConfig, error) {
	cfg := statepolicy.StatePolicyConfig{AllowMasterMigration: true}
	if raw := os.Getenv(envStatePolicyAllowMasterMigration); raw != "" {
		allow, err := strconv.ParseBool(raw)
		if err != nil {
			return statepolicy.StatePolicyConfig{}, fmt.Errorf("%s: %w", envStatePolicyAllowMasterMigration, err)
		}
		cfg.AllowMasterMigration = allow
	}
	if err := cfg.Validate(); err != nil {
		return statepolicy.StatePolicyConfig{}, err
	}
	return cfg, nil
}

// loadEnetFromEnv builds transport/enet.EnetConfig: ENET_* env vars on top of DefaultEnetConfig.
// ENET_LISTEN_PORTS - CSV of host ports; unset - default [7777].
// Duplicates are caught by Validate().
// cmd/rooms copies frame/queue limits from cfg.Transport and then calls Validate() again.
func loadEnetFromEnv() (enetadapter.EnetConfig, error) {
	cfg := enetadapter.DefaultEnetConfig()

	if raw := os.Getenv(envEnetListenPorts); raw != "" {
		parts := strings.Split(raw, ",")
		ports := make([]uint16, 0, len(parts))
		for _, part := range parts {
			port, err := strconv.Atoi(strings.TrimSpace(part))
			if err != nil {
				return enetadapter.EnetConfig{}, fmt.Errorf("%s: %q: %w", envEnetListenPorts, part, err)
			}
			if port <= 0 || port > math.MaxUint16 {
				return enetadapter.EnetConfig{}, fmt.Errorf("%s: port %d out of range 1..%d", envEnetListenPorts, port, math.MaxUint16)
			}
			ports = append(ports, uint16(port))
		}
		cfg.ListenPorts = ports
	}

	peerLimit, err := intFromEnv(envEnetPeerLimit, int(cfg.PeerLimit))
	if err != nil {
		return enetadapter.EnetConfig{}, err
	}
	if peerLimit <= 0 || peerLimit > 4095 {
		return enetadapter.EnetConfig{}, fmt.Errorf("%s: peer limit %d out of range 1..4095", envEnetPeerLimit, peerLimit)
	}
	cfg.PeerLimit = uint64(peerLimit)

	admitTimeout, err := durationFromEnv(envEnetAdmitTimeout, cfg.AdmitTimeout)
	if err != nil {
		return enetadapter.EnetConfig{}, err
	}
	cfg.AdmitTimeout = admitTimeout

	serviceTimeout, err := intFromEnv(envEnetServiceTimeoutMs, int(cfg.ServiceTimeoutMs))
	if err != nil {
		return enetadapter.EnetConfig{}, err
	}
	if serviceTimeout <= 0 {
		return enetadapter.EnetConfig{}, fmt.Errorf("%s: service timeout %d must be > 0", envEnetServiceTimeoutMs, serviceTimeout)
	}
	cfg.ServiceTimeoutMs = uint32(serviceTimeout)

	if err := cfg.Validate(); err != nil {
		return enetadapter.EnetConfig{}, err
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

func durationFromEnv(name string, defaultValue time.Duration) (time.Duration, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return defaultValue, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return value, nil
}
