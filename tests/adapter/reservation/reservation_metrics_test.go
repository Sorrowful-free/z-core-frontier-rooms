package reservation_test

import (
	"context"
	"errors"
	"testing"
	"time"

	metricsadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/metrics"
	adapterreservation "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/reservation"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/prom"
)

// P19b fix 3: an expired slot removed from Admit is a real revoke:
// the reservation_ops{revoke} counter converges to live slots, while the
// active_slots gauge goes to zero.
func TestReservationMetrics_ExpiredSlotRevokeCounted(t *testing.T) {
	t.Parallel()

	m, err := metricsadapter.New(metricsadapter.MetricsConfig{})
	if err != nil {
		t.Fatalf("metrics.New: %v", err)
	}
	res := adapterreservation.NewReservation(discardLogger{}, m)
	ctx := context.Background()

	if err := res.RegisterRoom(ctx, domain.RoomID(1), 4, ""); err != nil {
		t.Fatalf("RegisterRoom: %v", err)
	}
	expired := time.Now().Add(-time.Second)
	if err := res.Reserve(ctx, domain.RoomID(1), domain.PeerID(10), "nick", expired); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_reservation_active_slots", nil); got != 1 {
		t.Fatalf("active_slots after reserve = %v, want 1", got)
	}

	err = res.Admit(ctx, domain.RoomID(1), domain.PeerID(10))
	if !errors.Is(err, domain.ErrReservationExpired) {
		t.Fatalf("Admit err = %v, want ErrReservationExpired", err)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_reservation_ops_total", map[string]string{"op": "revoke"}); got != 1 {
		t.Fatalf("revoke after expired admit = %v, want 1", got)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_reservation_active_slots", nil); got != 0 {
		t.Fatalf("active_slots after expired admit = %v, want 0", got)
	}
}

// Lazy sweep of expired slots on Reserve is also a slot release:
// revoke is counted per removed slot, the gauge — by the total.
func TestReservationMetrics_LazySweepCountsRevokes(t *testing.T) {
	t.Parallel()

	m, err := metricsadapter.New(metricsadapter.MetricsConfig{})
	if err != nil {
		t.Fatalf("metrics.New: %v", err)
	}
	res := adapterreservation.NewReservation(discardLogger{}, m)
	ctx := context.Background()

	if err := res.RegisterRoom(ctx, domain.RoomID(1), 4, ""); err != nil {
		t.Fatalf("RegisterRoom: %v", err)
	}
	expired := time.Now().Add(-time.Second)
	if err := res.Reserve(ctx, domain.RoomID(1), domain.PeerID(10), "a", expired); err != nil {
		t.Fatalf("Reserve a: %v", err)
	}

	// The second Reserve triggers lazy sweep and removes the expired slot.
	if err := res.Reserve(ctx, domain.RoomID(1), domain.PeerID(11), "b", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Reserve b: %v", err)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_reservation_ops_total", map[string]string{"op": "revoke"}); got != 1 {
		t.Fatalf("revoke after lazy sweep = %v, want 1", got)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_reservation_active_slots", nil); got != 1 {
		t.Fatalf("active_slots after lazy sweep = %v, want 1 (только живой слот b)", got)
	}
}
