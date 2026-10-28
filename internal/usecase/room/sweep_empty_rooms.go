package room

import (
	"context"
	"errors"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/reservation"
)

// Value of the `sweep` label for SweepRan (bounded in the adapter).
const sweepKindEmptyRooms = "empty_rooms"

// SweepEmptyRoomsUseCase — death of never-joined rooms (P31): a room was
// created (POST /rooms + host ticket) but nobody entered — the reserve slot
// is only cleaned lazily on the next Reserve, while the room itself would
// hang in the registry forever. Transition checks (closeRoomIfEmpty on peer
// departure) do not cover this class — there never were any peers, so a
// time-based sweep is needed.
//
// Kill predicate: peers == 0 ∧ no admitted slots ∧ all reserve slots
// expired (expiresAt < now) or there are no slots. A live reserve
// (unexpired ticket claim) keeps the room alive — the room has no
// createdAt and doesn't need one: the newest ticket's deadline is the
// threshold. A zero ExpiresAt never expires (same contract as
// sweepExpiredLocked in adapter/reservation).
//
// The kill goes through closeRoomIfEmpty: re-checking admitted before
// DeleteRoom narrows the race window with a parallel admit (an admit
// between the slot listing and the kill returns len>0 → no-op). A fresh
// Reserve between the listing and the kill is not caught by the check —
// the joiner gets a reject instead of a room (accepted race, symmetric to
// the transition path).
type SweepEmptyRoomsUseCase struct {
	roomRegistry registry.RoomRegistry
	reservation  reservation.Reservation
	logger       logging.Logger
	metrics      portmetrics.Metrics
}

func NewSweepEmptyRoomsUseCase(
	roomRegistry registry.RoomRegistry,
	reservation reservation.Reservation,
	logger logging.Logger,
	m portmetrics.Metrics,
) *SweepEmptyRoomsUseCase {
	return &SweepEmptyRoomsUseCase{
		roomRegistry: roomRegistry,
		reservation:  reservation,
		logger:       logger,
		metrics:      portmetrics.OrNoop(m),
	}
}

// RunOnce iterates over rooms and kills empty ones with no live slots.
// Returns the number of rooms killed in this pass.
func (uc *SweepEmptyRoomsUseCase) RunOnce(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	// A sweep pass is counted separately from processed records (P19b).
	uc.metrics.SweepRan(sweepKindEmptyRooms)

	rooms, err := uc.roomRegistry.GetList(ctx)
	if err != nil {
		return 0, err
	}

	now := time.Now()
	killed := 0

	for _, room := range rooms {
		if err := ctx.Err(); err != nil {
			return killed, err
		}
		if len(room.GetPeers()) > 0 {
			continue
		}
		roomID := room.GetID()

		slots, err := uc.reservation.ListRoomSlots(ctx, roomID)
		if err != nil {
			// The room was already unregistered from reservation by a
			// parallel teardown — no slots, the kill predicate holds. Other
			// errors — state unknown, skip until the next pass.
			if !errors.Is(err, domain.ErrReservationNotFound) {
				uc.logger.Error("empty rooms sweep: list slots failed", "error", err, "roomID", roomID)
				continue
			}
			slots = nil
		}

		if hasLiveReservationSlot(slots, now) {
			continue
		}

		uc.logger.Warn("empty rooms sweep: room has no live slots", "roomID", roomID)
		if closeRoomIfEmpty(ctx, room, uc.roomRegistry, uc.reservation, uc.logger) {
			killed++
		}
	}

	return killed, nil
}

// hasLiveReservationSlot — true if at least one slot still holds the room:
// admitted (live or in grace — reconnect is still possible) or a reserve
// with an unexpired ticket deadline. ExpiresAt.IsZero() never expires, same
// as in the adapter's sweepExpiredLocked.
func hasLiveReservationSlot(slots []domain.RoomSlotDetail, now time.Time) bool {
	for _, slot := range slots {
		switch slot.State {
		case domain.ReservationSlotAdmitted:
			return true
		case domain.ReservationSlotReserved:
			if slot.ExpiresAt.IsZero() || !slot.ExpiresAt.Before(now) {
				return true
			}
		}
	}
	return false
}

// StartEmptyRoomsSweep starts the background sweep; interval <= 0 — no-op
// (nil handle). The cadence is the same RESERVATION_ORPHAN_SWEEP_INTERVAL as
// the orphan sweep: 0 disables both.
func StartEmptyRoomsSweep(
	ctx context.Context,
	uc *SweepEmptyRoomsUseCase,
	interval time.Duration,
	logger logging.Logger,
) *SweepHandle {
	return startSweep(ctx, interval, logger, "empty rooms sweep", "killed", uc.RunOnce)
}
