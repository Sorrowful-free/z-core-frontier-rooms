package realtime_test

import (
	"context"
	"sync"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/stdlib"
	metricsadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/metrics"
	adapterrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	statepolicy "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/policy/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	testpeer "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/peer"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/prom"
)

// newMetricsRoom — a room with production metrics wiring: StateRoomPolicyFactory
// (the master machine gets metrics as a field) + RoomFactory (lifecycle emits).
func newMetricsRoom(t *testing.T, m *metricsadapter.Metrics) realtime.Room {
	t.Helper()
	logger := stdlib.New("test")
	policyFactory := statepolicy.NewStateRoomPolicyFactory(logger, statepolicy.StatePolicyConfig{AllowMasterMigration: true}, m)
	factory := adapterrealtime.NewRoomFactory(logger, policyFactory, 0, m)
	room, err := factory.CreateRoom(context.Background(), domain.RoomID(1), 8, nil)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if err := room.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = room.Stop() })
	return room
}

// P19b fix 1: a room's first election is the same master assignment as a
// re-election and must be counted in master_elections_total.
func TestRoomMetrics_FirstElectionCounted(t *testing.T) {
	t.Parallel()

	m, err := metricsadapter.New(metricsadapter.MetricsConfig{})
	if err != nil {
		t.Fatalf("metrics.New: %v", err)
	}
	room := newMetricsRoom(t, m)

	host := testpeer.New(domain.PeerID(1), "host")
	joiner := testpeer.New(domain.PeerID(2), "joiner")

	if err := room.Join(host); err != nil {
		t.Fatalf("join host: %v", err)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_master_elections_total", nil); got != 1 {
		t.Fatalf("master_elections after first join = %v, want 1", got)
	}

	if err := room.Join(joiner); err != nil {
		t.Fatalf("join joiner: %v", err)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_master_elections_total", nil); got != 1 {
		t.Fatalf("master_elections after second join = %v, want 1 (a non-master join is not an election)", got)
	}

	if err := room.Leave(host); err != nil {
		t.Fatalf("leave host: %v", err)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_master_elections_total", nil); got != 2 {
		t.Fatalf("master_elections after master leave = %v, want 2 (re-election)", got)
	}
}

// P19b fix 2: a repeated leave must not decrement active_peers twice —
// the metric is bound to the fact of the peer's removal from the map on the
// room loop, not to the Leave call. Both concurrent callers can slip past
// the pre-check before serializing on the loop — the guard inside the
// lifecycle leaves a single decrement.
func TestRoomMetrics_LeaveDriftGuard(t *testing.T) {
	t.Parallel()

	m, err := metricsadapter.New(metricsadapter.MetricsConfig{})
	if err != nil {
		t.Fatalf("metrics.New: %v", err)
	}
	room := newMetricsRoom(t, m)

	peer := testpeer.New(domain.PeerID(1), "host")
	if err := room.Join(peer); err != nil {
		t.Fatalf("join: %v", err)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_active_peers", nil); got != 1 {
		t.Fatalf("active_peers after join = %v, want 1", got)
	}

	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = room.Leave(peer) // the second call errors — ignored
		}()
	}
	wg.Wait()

	if got := prom.Value(t, m.Gatherer(), "rooms_active_peers", nil); got != 0 {
		t.Fatalf("active_peers after double leave = %v, want 0", got)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_peer_leaves_total", nil); got != 1 {
		t.Fatalf("peer_leaves after double leave = %v, want 1", got)
	}
}

// P31: teardown of a room with N peers — Room.Stop subtracts the remaining
// ones from active_peers in one go (-len(r.peers)): room kill paths
// (closeRoom, DELETE, empty-death, shutdown) kill peers bypassing Leave, and
// their +1 on join would otherwise inflate the gauge forever. Peers that
// left via Leave already got their -1 and are not in r.peers — no double
// subtraction.
func TestRoomMetrics_StopRebalancesActivePeers(t *testing.T) {
	t.Parallel()

	m, err := metricsadapter.New(metricsadapter.MetricsConfig{})
	if err != nil {
		t.Fatalf("metrics.New: %v", err)
	}
	room := newMetricsRoom(t, m)

	const n = 3
	for i := range n {
		if err := room.Join(testpeer.New(domain.PeerID(i+1), "p")); err != nil {
			t.Fatalf("join peer %d: %v", i+1, err)
		}
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_active_peers", nil); got != n {
		t.Fatalf("active_peers after joins = %v, want %d", got, n)
	}

	// One leaves via a normal Leave: the -1 was already emitted on the
	// lifecycle — Stop must not subtract it again.
	peers := room.GetPeers()
	if err := room.Leave(peers[0]); err != nil {
		t.Fatalf("leave: %v", err)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_active_peers", nil); got != n-1 {
		t.Fatalf("active_peers after leave = %v, want %d", got, n-1)
	}

	if err := room.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_active_peers", nil); got != 0 {
		t.Fatalf("active_peers after room Stop = %v, want 0", got)
	}
}
