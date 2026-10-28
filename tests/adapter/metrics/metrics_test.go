package metrics_test

import (
	"testing"
	"time"

	metricsadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/metrics"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/prom"
)

func newMetrics(t *testing.T) *metricsadapter.Metrics {
	t.Helper()
	m, err := metricsadapter.New(metricsadapter.MetricsConfig{})
	if err != nil {
		t.Fatalf("metrics.New: %v", err)
	}
	return m
}

func TestMetrics_Counters(t *testing.T) {
	t.Parallel()
	m := newMetrics(t)

	m.SetActiveRooms(3)
	m.AddActivePeers(7)
	m.AddActivePeers(-2)
	m.PeerJoined()
	m.PeerJoined()
	m.PeerLeft()
	m.JoinRejected(domain.OpInvalidToken)
	m.JoinRejected(domain.OpReservationFull)
	m.JoinRejected(domain.OpReservationFull)
	m.RoomIncomingDropped()
	m.PeerOutboundDropped()
	m.EnetIncomingDropped()
	m.MasterElected()
	m.ReservationReserved()
	m.ReservationAdmitted()
	m.ReservationRevoked()
	m.OrphanAdmittedRevoked(4)
	m.IdlePeersEvicted(2)

	if got := prom.Value(t, m.Gatherer(), "rooms_active_rooms", nil); got != 3 {
		t.Fatalf("active_rooms = %v, want 3", got)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_active_peers", nil); got != 5 {
		t.Fatalf("active_peers = %v, want 5", got)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_peer_joins_total", nil); got != 2 {
		t.Fatalf("peer_joins = %v, want 2", got)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_peer_leaves_total", nil); got != 1 {
		t.Fatalf("peer_leaves = %v, want 1", got)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_join_rejects_total", map[string]string{"opcode": "0x41"}); got != 1 {
		t.Fatalf("join_rejects{0x41} = %v, want 1", got)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_join_rejects_total", map[string]string{"opcode": "0x71"}); got != 2 {
		t.Fatalf("join_rejects{0x71} = %v, want 2", got)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_queue_drops_total", map[string]string{"queue": "room_incoming"}); got != 1 {
		t.Fatalf("queue_drops{room_incoming} = %v, want 1", got)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_queue_drops_total", map[string]string{"queue": "peer_outbound"}); got != 1 {
		t.Fatalf("queue_drops{peer_outbound} = %v, want 1", got)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_queue_drops_total", map[string]string{"queue": "enet_incoming"}); got != 1 {
		t.Fatalf("queue_drops{enet_incoming} = %v, want 1", got)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_master_elections_total", nil); got != 1 {
		t.Fatalf("master_elections = %v, want 1", got)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_reservation_ops_total", map[string]string{"op": "reserve"}); got != 1 {
		t.Fatalf("reservation_ops{reserve} = %v, want 1", got)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_reservation_ops_total", map[string]string{"op": "admit"}); got != 1 {
		t.Fatalf("reservation_ops{admit} = %v, want 1", got)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_reservation_ops_total", map[string]string{"op": "revoke"}); got != 1 {
		t.Fatalf("reservation_ops{revoke} = %v, want 1", got)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_sweep_processed_total", map[string]string{"sweep": "orphan_admitted"}); got != 4 {
		t.Fatalf("sweep_processed{orphan_admitted} = %v, want 4", got)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_sweep_processed_total", map[string]string{"sweep": "idle_peers"}); got != 2 {
		t.Fatalf("sweep_processed{idle_peers} = %v, want 2", got)
	}
}

// P19b: new signals — queue depth gauges, tick/send/host metrics,
// reservation gauge, sweep runs, HTTP request histogram.
func TestMetrics_Signals(t *testing.T) {
	t.Parallel()
	m := newMetrics(t)

	m.RoomIncomingDepth(5)
	m.PeerOutboundDepth(3)
	m.RoomTickObserved("full", 50*time.Millisecond)
	m.RoomTickObserved("patch", time.Millisecond)
	m.RoomTickObserved("patch", 2*time.Millisecond)
	m.PeerSendError()
	m.EnetHostPeers(domain.HostID(2), 4)
	m.EnetHostLoopQueueLatency(time.Millisecond)
	m.ReservationActiveSlots(3)
	m.ReservationActiveSlots(-1)
	m.SweepRan("orphan_admitted")
	m.HTTPRequestObserved("/rooms", "GET", 200, 10*time.Millisecond)

	if got := prom.Value(t, m.Gatherer(), "rooms_room_incoming_depth", nil); got != 5 {
		t.Fatalf("room_incoming_depth = %v, want 5", got)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_peer_outbound_depth", nil); got != 3 {
		t.Fatalf("peer_outbound_depth = %v, want 3", got)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_room_tick_seconds", map[string]string{"kind": "full"}); got != 1 {
		t.Fatalf("room_tick{full} count = %v, want 1", got)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_room_tick_seconds", map[string]string{"kind": "patch"}); got != 2 {
		t.Fatalf("room_tick{patch} count = %v, want 2", got)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_peer_send_errors_total", nil); got != 1 {
		t.Fatalf("peer_send_errors = %v, want 1", got)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_enet_host_peers", map[string]string{"host": "2"}); got != 4 {
		t.Fatalf("enet_host_peers{host=2} = %v, want 4", got)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_enet_host_loop_queue_latency_seconds", nil); got != 1 {
		t.Fatalf("host_loop_queue_latency count = %v, want 1", got)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_reservation_active_slots", nil); got != 2 {
		t.Fatalf("reservation_active_slots = %v, want 2", got)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_sweep_runs_total", map[string]string{"sweep": "orphan_admitted"}); got != 1 {
		t.Fatalf("sweep_runs{orphan_admitted} = %v, want 1", got)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_http_request_duration_seconds",
		map[string]string{"route": "/rooms", "method": "GET", "status": "200"}); got != 1 {
		t.Fatalf("http_request_duration count = %v, want 1", got)
	}
}

func TestMetricsConfig_Validate(t *testing.T) {
	t.Parallel()
	if err := (metricsadapter.MetricsConfig{}).Validate(); err != nil {
		t.Fatalf("empty config should be valid (endpoint disabled): %v", err)
	}
	if err := (metricsadapter.MetricsConfig{AdminListenAddr: ":9091"}).Validate(); err != nil {
		t.Fatalf("valid listen addr: %v", err)
	}
	if err := (metricsadapter.MetricsConfig{AdminListenAddr: "not-an-addr"}).Validate(); err == nil {
		t.Fatal("expected error for invalid listen addr")
	}
}

func TestMetrics_NewInvalidConfig(t *testing.T) {
	t.Parallel()
	if _, err := metricsadapter.New(metricsadapter.MetricsConfig{AdminListenAddr: "bad"}); err == nil {
		t.Fatal("expected error for invalid config")
	}
}
