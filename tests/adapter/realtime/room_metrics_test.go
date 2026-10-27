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

// newMetricsRoom - комната с продовым wiring метрик: StateRoomPolicyFactory
// (master-машина получает metrics полем) + RoomFactory (lifecycle-эмиты).
func newMetricsRoom(t *testing.T, m *metricsadapter.Metrics) realtime.Room {
	t.Helper()
	logger := stdlib.New("test")
	policyFactory := statepolicy.NewStateRoomPolicyFactory(logger, m)
	factory := adapterrealtime.NewRoomFactory(logger, policyFactory, 0, m)
	room, err := factory.CreateRoom(context.Background(), domain.RoomID(1), 8, nil, domain.RoomPolicyKindState)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if err := room.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = room.Stop() })
	return room
}

// P19b фикс 1: первый элекшн комнаты - то же назначение master, что и
// перевыборы, и обязан считаться в master_elections_total.
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
		t.Fatalf("master_elections after second join = %v, want 1 (join не-master'а - не элекшн)", got)
	}

	if err := room.Leave(host); err != nil {
		t.Fatalf("leave host: %v", err)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_master_elections_total", nil); got != 2 {
		t.Fatalf("master_elections after master leave = %v, want 2 (перевыборы)", got)
	}
}

// P19b фикс 2: повторный leave не должен дважды декрементить active_peers -
// метрика привязана к факту изъятия peer'а из map на room-loop'е, а не к
// вызову Leave. Оба конкурентных caller'а могут проскочить pre-check до
// сериализации на loop'е - гарда внутри lifecycle оставляет один decrement.
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
			_ = room.Leave(peer) // второй вызов ошибочен - игнорируем
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

// P31: teardown комнаты с N peers — Room.Stop вычитает оставшихся из
// active_peers разом (-len(r.peers)): kill-пути комнаты (closeRoom,
// DELETE, empty-death, shutdown) убивают peers мимо Leave и их +1 на
// join иначе завышали бы gauge навсегда. Peers, ушедшие через Leave,
// уже получили свой -1 и в r.peers не лежат — двойного вычета нет.
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

	// Один уходит обычным Leave: -1 уже эмитнут на lifecycle — Stop не
	// должен вычесть его повторно.
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
