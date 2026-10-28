package config_test

import (
	"strings"
	"testing"
	"time"

	appconfig "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/config"
	httplimitsadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/httplimits"
	zaplogadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/zap"
	reservationadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/reservation"
	serveradapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/server"
	adaptertransport "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport"
	enetadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/enet"
)

func validSecret() string {
	return "test-admission-secret-32-chars-min!!"
}

func validHTTPAPIKey() string {
	return "test-http-api-key-16b"
}

func setHTTPAuthEnabled(t *testing.T) {
	t.Helper()
	t.Setenv("HTTP_API_KEY", validHTTPAPIKey())
	t.Setenv("HTTP_AUTH_DISABLED", "")
}

func setHTTPAuthDisabled(t *testing.T) {
	t.Helper()
	t.Setenv("HTTP_API_KEY", "")
	t.Setenv("HTTP_AUTH_DISABLED", "true")
}

func TestLoadFromEnv_AdmissionRequired(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", "")
	t.Setenv("ADMISSION_TTL", "")
	setHTTPAuthDisabled(t)

	_, err := appconfig.LoadFromEnv()
	if err == nil {
		t.Fatal("expected error for missing secret")
	}
	if !strings.Contains(err.Error(), "secret is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadFromEnv_ForbiddenDefaultSecret(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", "dev-secret-change-me")
	t.Setenv("ADMISSION_TTL", "")
	setHTTPAuthDisabled(t)

	_, err := appconfig.LoadFromEnv()
	if err == nil {
		t.Fatal("expected error for forbidden secret")
	}
	if !strings.Contains(err.Error(), "forbidden default secret") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadFromEnv_SecretTooShort(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", "short")
	t.Setenv("ADMISSION_TTL", "")
	setHTTPAuthDisabled(t)

	_, err := appconfig.LoadFromEnv()
	if err == nil {
		t.Fatal("expected error for short secret")
	}
	if !strings.Contains(err.Error(), "at least 32 bytes") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadFromEnv_InvalidTTL(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	t.Setenv("ADMISSION_TTL", "not-a-duration")
	setHTTPAuthDisabled(t)

	_, err := appconfig.LoadFromEnv()
	if err == nil {
		t.Fatal("expected error for invalid ttl")
	}
	if !strings.Contains(err.Error(), "ADMISSION_TTL") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadFromEnv_HTTPAPIKeyRequired(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	t.Setenv("ADMISSION_TTL", "")
	t.Setenv("HTTP_API_KEY", "")
	t.Setenv("HTTP_AUTH_DISABLED", "")

	_, err := appconfig.LoadFromEnv()
	if err == nil {
		t.Fatal("expected error for missing api key")
	}
	if !strings.Contains(err.Error(), "api key is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadFromEnv_HTTPAPIKeyTooShort(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	t.Setenv("HTTP_API_KEY", "short")
	t.Setenv("HTTP_AUTH_DISABLED", "")

	_, err := appconfig.LoadFromEnv()
	if err == nil {
		t.Fatal("expected error for short api key")
	}
	if !strings.Contains(err.Error(), "at least 16 bytes") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadFromEnv_SuccessDefaults(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	t.Setenv("ADMISSION_TTL", "")
	setHTTPAuthEnabled(t)

	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	if string(cfg.Admission.Secret) != validSecret() {
		t.Fatalf("secret = %q", cfg.Admission.Secret)
	}
	if cfg.Admission.TTL != time.Hour {
		t.Fatalf("ttl = %v, want 1h", cfg.Admission.TTL)
	}
	if cfg.HTTPAuth.APIKey != validHTTPAPIKey() {
		t.Fatalf("api key = %q", cfg.HTTPAuth.APIKey)
	}
	if cfg.HTTPAuth.Disabled {
		t.Fatal("http auth should be enabled")
	}
	if cfg.HTTPLimits.MaxRooms != httplimitsadapter.DefaultMaxRooms {
		t.Fatalf("max rooms = %d", cfg.HTTPLimits.MaxRooms)
	}
	if cfg.HTTPLimits.CreateRoomsPerMinute != httplimitsadapter.DefaultCreateRoomsPerMinute {
		t.Fatalf("create rate = %d", cfg.HTTPLimits.CreateRoomsPerMinute)
	}
	if cfg.HTTPLimits.IssueTicketsPerMinute != httplimitsadapter.DefaultIssueTicketsPerMinute {
		t.Fatalf("issue rate = %d", cfg.HTTPLimits.IssueTicketsPerMinute)
	}
	if cfg.Reservation.ReconnectTTL != reservationadapter.DefaultReconnectTTL {
		t.Fatalf("reconnect ttl = %v", cfg.Reservation.ReconnectTTL)
	}
	if cfg.Reservation.OrphanSweepInterval != reservationadapter.DefaultOrphanSweepInterval {
		t.Fatalf("orphan sweep interval = %v", cfg.Reservation.OrphanSweepInterval)
	}
	if cfg.Transport.MaxIncomingFrameBytes != adaptertransport.DefaultMaxIncomingFrameBytes {
		t.Fatalf("max incoming frame bytes = %d", cfg.Transport.MaxIncomingFrameBytes)
	}
	if cfg.Transport.RoomIncomingQueueSize != adaptertransport.DefaultRoomIncomingQueueSize {
		t.Fatalf("room incoming queue = %d", cfg.Transport.RoomIncomingQueueSize)
	}
	if cfg.Transport.PeerOutboundQueueSize != adaptertransport.DefaultPeerOutboundQueueSize {
		t.Fatalf("peer outbound queue = %d", cfg.Transport.PeerOutboundQueueSize)
	}
	if cfg.Transport.EnetIncomingQueueSize != adaptertransport.DefaultEnetIncomingQueueSize {
		t.Fatalf("enet incoming queue = %d", cfg.Transport.EnetIncomingQueueSize)
	}
	if cfg.Transport.WSWriteDeadline != adaptertransport.DefaultWSWriteDeadline {
		t.Fatalf("ws write deadline = %v", cfg.Transport.WSWriteDeadline)
	}
	if cfg.Logging.Mode != zaplogadapter.LogModeProd {
		t.Fatalf("log mode = %q, want prod", cfg.Logging.Mode)
	}
	if cfg.Server.HTTPAddr != serveradapter.DefaultHTTPAddr {
		t.Fatalf("http addr = %q", cfg.Server.HTTPAddr)
	}
}

func TestLoadFromEnv_LoggingAndServerCustom(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	setHTTPAuthEnabled(t)
	t.Setenv("LOG_MODE", "dev")
	t.Setenv("HTTP_ADDR", "127.0.0.1:8080")

	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	if cfg.Logging.Mode != zaplogadapter.LogModeDev {
		t.Fatalf("log mode = %q", cfg.Logging.Mode)
	}
	if cfg.Server.HTTPAddr != "127.0.0.1:8080" {
		t.Fatalf("http addr = %q", cfg.Server.HTTPAddr)
	}
}

func TestLoadFromEnv_InvalidLogMode(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	setHTTPAuthDisabled(t)
	t.Setenv("LOG_MODE", "verbose")

	_, err := appconfig.LoadFromEnv()
	if err == nil {
		t.Fatal("expected error for invalid LOG_MODE")
	}
	if !strings.Contains(err.Error(), "LOG_MODE") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadFromEnv_SuccessCustomTTL(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	t.Setenv("ADMISSION_TTL", "30m")
	setHTTPAuthEnabled(t)

	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	if cfg.Admission.TTL != 30*time.Minute {
		t.Fatalf("ttl = %v", cfg.Admission.TTL)
	}
}

func TestLoadFromEnv_ReservationReconnectTTL(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	setHTTPAuthDisabled(t)
	t.Setenv("RESERVATION_RECONNECT_TTL", "45s")

	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	if cfg.Reservation.ReconnectTTL != 45*time.Second {
		t.Fatalf("reconnect ttl = %v, want 45s", cfg.Reservation.ReconnectTTL)
	}
}

func TestLoadFromEnv_HTTPAuthDisabled(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	setHTTPAuthDisabled(t)

	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	if !cfg.HTTPAuth.Disabled {
		t.Fatal("expected http auth disabled")
	}
}

func TestLoadFromEnv_TransportIdleDefaults(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	setHTTPAuthDisabled(t)
	t.Setenv("TRANSPORT_PEER_IDLE_TIMEOUT", "")
	t.Setenv("TRANSPORT_PEER_IDLE_SWEEP_INTERVAL", "")

	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	if cfg.Transport.PeerIdleTimeout != adaptertransport.DefaultPeerIdleTimeout {
		t.Fatalf("peer idle timeout = %v", cfg.Transport.PeerIdleTimeout)
	}
	if cfg.Transport.PeerIdleSweepInterval != adaptertransport.DefaultPeerIdleSweepInterval {
		t.Fatalf("peer idle sweep interval = %v", cfg.Transport.PeerIdleSweepInterval)
	}
}

func TestLoadFromEnv_TransportIdleCustom(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	setHTTPAuthDisabled(t)
	t.Setenv("TRANSPORT_PEER_IDLE_TIMEOUT", "45s")
	t.Setenv("TRANSPORT_PEER_IDLE_SWEEP_INTERVAL", "3s")

	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	if cfg.Transport.PeerIdleTimeout != 45*time.Second {
		t.Fatalf("peer idle timeout = %v", cfg.Transport.PeerIdleTimeout)
	}
	if cfg.Transport.PeerIdleSweepInterval != 3*time.Second {
		t.Fatalf("peer idle sweep interval = %v", cfg.Transport.PeerIdleSweepInterval)
	}
}

func TestLoadFromEnv_TransportIdleInvalidDuration(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	setHTTPAuthDisabled(t)
	t.Setenv("TRANSPORT_PEER_IDLE_TIMEOUT", "not-a-duration")

	_, err := appconfig.LoadFromEnv()
	if err == nil {
		t.Fatal("expected error for invalid TRANSPORT_PEER_IDLE_TIMEOUT")
	}
	if !strings.Contains(err.Error(), "TRANSPORT_PEER_IDLE_TIMEOUT") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadFromEnv_TransportIdleNegative(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	setHTTPAuthDisabled(t)
	t.Setenv("TRANSPORT_PEER_IDLE_TIMEOUT", "-1s")

	_, err := appconfig.LoadFromEnv()
	if err == nil {
		t.Fatal("expected error for negative TRANSPORT_PEER_IDLE_TIMEOUT")
	}
}

func TestLoadFromEnv_TransportWSWriteDeadline(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	setHTTPAuthDisabled(t)
	t.Setenv("TRANSPORT_WS_WRITE_DEADLINE", "2500ms")

	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	if cfg.Transport.WSWriteDeadline != 2500*time.Millisecond {
		t.Fatalf("ws write deadline = %v, want 2500ms", cfg.Transport.WSWriteDeadline)
	}

	t.Setenv("TRANSPORT_WS_WRITE_DEADLINE", "not-a-duration")
	if _, err := appconfig.LoadFromEnv(); err == nil {
		t.Fatal("expected error for invalid TRANSPORT_WS_WRITE_DEADLINE")
	}
}

// P30: потолок drain'а peer.outbound при Stop — «Send до Close».
func TestLoadFromEnv_TransportPeerCloseDrainTimeout(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	setHTTPAuthDisabled(t)
	t.Setenv("TRANSPORT_PEER_CLOSE_DRAIN_TIMEOUT", "500ms")

	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	if cfg.Transport.PeerCloseDrainTimeout != 500*time.Millisecond {
		t.Fatalf("peer close drain timeout = %v, want 500ms", cfg.Transport.PeerCloseDrainTimeout)
	}

	// 0 — drain выключен (валидно, семантика «механизм off»).
	t.Setenv("TRANSPORT_PEER_CLOSE_DRAIN_TIMEOUT", "0")
	cfg, err = appconfig.LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv (0): %v", err)
	}
	if cfg.Transport.PeerCloseDrainTimeout != 0 {
		t.Fatalf("peer close drain timeout = %v, want 0", cfg.Transport.PeerCloseDrainTimeout)
	}
}

func TestLoadFromEnv_TransportPeerCloseDrainTimeoutInvalid(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	setHTTPAuthDisabled(t)

	t.Setenv("TRANSPORT_PEER_CLOSE_DRAIN_TIMEOUT", "not-a-duration")
	if _, err := appconfig.LoadFromEnv(); err == nil {
		t.Fatal("expected error for invalid TRANSPORT_PEER_CLOSE_DRAIN_TIMEOUT")
	}

	t.Setenv("TRANSPORT_PEER_CLOSE_DRAIN_TIMEOUT", "-1s")
	if _, err := appconfig.LoadFromEnv(); err == nil {
		t.Fatal("expected error for negative TRANSPORT_PEER_CLOSE_DRAIN_TIMEOUT")
	}
}

func TestLoadFromEnv_TransportPeerCloseDrainTimeoutDefault(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	setHTTPAuthDisabled(t)
	t.Setenv("TRANSPORT_PEER_CLOSE_DRAIN_TIMEOUT", "")

	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	if cfg.Transport.PeerCloseDrainTimeout != adaptertransport.DefaultPeerCloseDrainTimeout {
		t.Fatalf("peer close drain timeout = %v, want default %v", cfg.Transport.PeerCloseDrainTimeout, adaptertransport.DefaultPeerCloseDrainTimeout)
	}
}

func TestLoadFromEnv_EnetDefaults(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	setHTTPAuthDisabled(t)
	for _, k := range []string{"ENET_LISTEN_PORTS", "ENET_PEER_LIMIT", "ENET_ADMIT_TIMEOUT"} {
		t.Setenv(k, "")
	}

	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	def := enetadapter.DefaultEnetConfig()
	if len(cfg.Enet.ListenPorts) != 1 || cfg.Enet.ListenPorts[0] != def.ListenPorts[0] {
		t.Fatalf("enet listen ports = %v, want %v", cfg.Enet.ListenPorts, def.ListenPorts)
	}
	if cfg.Enet.PeerLimit != def.PeerLimit {
		t.Fatalf("enet peer limit = %d, want %d", cfg.Enet.PeerLimit, def.PeerLimit)
	}
	if cfg.Enet.AdmitTimeout != def.AdmitTimeout {
		t.Fatalf("enet admit timeout = %v, want %v", cfg.Enet.AdmitTimeout, def.AdmitTimeout)
	}
}

func TestLoadFromEnv_EnetOverrides(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	setHTTPAuthDisabled(t)
	t.Setenv("ENET_LISTEN_PORTS", "8899")
	t.Setenv("ENET_PEER_LIMIT", "16")
	t.Setenv("ENET_ADMIT_TIMEOUT", "3s")

	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	if len(cfg.Enet.ListenPorts) != 1 || cfg.Enet.ListenPorts[0] != 8899 {
		t.Fatalf("enet listen ports = %v, want [8899]", cfg.Enet.ListenPorts)
	}
	if cfg.Enet.PeerLimit != 16 {
		t.Fatalf("enet peer limit = %d, want 16", cfg.Enet.PeerLimit)
	}
	if cfg.Enet.AdmitTimeout != 3*time.Second {
		t.Fatalf("enet admit timeout = %v, want 3s", cfg.Enet.AdmitTimeout)
	}
}

// P33: STATE_POLICY_ALLOW_MASTER_MIGRATION — пусто → true (дефолт),
// явные true/false, мусор → ошибка ParseBool.
func TestLoadFromEnv_StatePolicyDefault(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	setHTTPAuthDisabled(t)
	t.Setenv("STATE_POLICY_ALLOW_MASTER_MIGRATION", "")

	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	if !cfg.StatePolicy.AllowMasterMigration {
		t.Fatal("allow master migration: want true (default)")
	}
}

func TestLoadFromEnv_StatePolicyExplicit(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{"true", true},
		{"false", false},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			t.Setenv("ADMISSION_SECRET", validSecret())
			setHTTPAuthDisabled(t)
			t.Setenv("STATE_POLICY_ALLOW_MASTER_MIGRATION", tc.raw)

			cfg, err := appconfig.LoadFromEnv()
			if err != nil {
				t.Fatalf("LoadFromEnv: %v", err)
			}
			if cfg.StatePolicy.AllowMasterMigration != tc.want {
				t.Fatalf("allow master migration = %v, want %v", cfg.StatePolicy.AllowMasterMigration, tc.want)
			}
		})
	}
}

func TestLoadFromEnv_StatePolicyInvalid(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	setHTTPAuthDisabled(t)
	t.Setenv("STATE_POLICY_ALLOW_MASTER_MIGRATION", "not-a-bool")

	_, err := appconfig.LoadFromEnv()
	if err == nil {
		t.Fatal("expected error for invalid STATE_POLICY_ALLOW_MASTER_MIGRATION")
	}
	if !strings.Contains(err.Error(), "STATE_POLICY_ALLOW_MASTER_MIGRATION") {
		t.Fatalf("err = %v", err)
	}
}

// P18: ENET_LISTEN_PORTS CSV (несколько host'ов).
func TestLoadFromEnv_EnetListenPorts(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	setHTTPAuthDisabled(t)
	t.Setenv("ENET_LISTEN_PORTS", "7777, 7778,7779")

	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	want := []uint16{7777, 7778, 7779}
	if len(cfg.Enet.ListenPorts) != len(want) {
		t.Fatalf("enet listen ports = %v, want %v", cfg.Enet.ListenPorts, want)
	}
	for i, p := range want {
		if cfg.Enet.ListenPorts[i] != p {
			t.Fatalf("enet listen ports = %v, want %v", cfg.Enet.ListenPorts, want)
		}
	}
}

func TestLoadFromEnv_EnetInvalid(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
	}{
		{"ports zero element", map[string]string{"ENET_LISTEN_PORTS": "7777,0"}},
		{"ports overflow element", map[string]string{"ENET_LISTEN_PORTS": "70000"}},
		{"ports not a number", map[string]string{"ENET_LISTEN_PORTS": "7777,abc"}},
		{"ports empty element", map[string]string{"ENET_LISTEN_PORTS": "7777,"}},
		{"ports duplicate", map[string]string{"ENET_LISTEN_PORTS": "7777,7777"}},
		{"peer limit zero", map[string]string{"ENET_PEER_LIMIT": "0"}},
		{"peer limit negative", map[string]string{"ENET_PEER_LIMIT": "-5"}},
		{"admit timeout zero", map[string]string{"ENET_ADMIT_TIMEOUT": "0s"}},
		{"admit timeout negative", map[string]string{"ENET_ADMIT_TIMEOUT": "-1s"}},
		{"admit timeout bad duration", map[string]string{"ENET_ADMIT_TIMEOUT": "soon"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ADMISSION_SECRET", validSecret())
			setHTTPAuthDisabled(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if _, err := appconfig.LoadFromEnv(); err == nil {
				t.Fatalf("LoadFromEnv() = nil error, want failure for %v", tc.env)
			}
		})
	}
}

// P19: ADMIN_LISTEN_ADDR - пустой дефолт = admin listener выключен.
func TestLoadFromEnv_MetricsDefaults(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	setHTTPAuthDisabled(t)
	t.Setenv("ADMIN_LISTEN_ADDR", "")

	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	if cfg.Metrics.AdminListenAddr != "" {
		t.Fatalf("admin listen addr = %q, want empty (disabled)", cfg.Metrics.AdminListenAddr)
	}
}

func TestLoadFromEnv_MetricsAdminAddr(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	setHTTPAuthDisabled(t)
	t.Setenv("ADMIN_LISTEN_ADDR", ":9091")

	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	if cfg.Metrics.AdminListenAddr != ":9091" {
		t.Fatalf("admin listen addr = %q, want :9091", cfg.Metrics.AdminListenAddr)
	}
}

func TestLoadFromEnv_MetricsAdminAddrInvalid(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	setHTTPAuthDisabled(t)
	t.Setenv("ADMIN_LISTEN_ADDR", "not-an-addr")

	if _, err := appconfig.LoadFromEnv(); err == nil {
		t.Fatal("expected error for invalid ADMIN_LISTEN_ADDR")
	}
}
