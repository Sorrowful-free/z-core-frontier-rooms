package errors_test

import (
	"testing"

	deliveryerrors "github.com/Sorrowful-free/z-core-frontier-rooms/internal/delivery/errors"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

func TestJoinRejectOpCode_ReservationErrors(t *testing.T) {
	t.Parallel()

	if got := deliveryerrors.JoinRejectOpCode(domain.ErrReservationExpired); got != domain.OpReservationExpired {
		t.Fatalf("JoinRejectOpCode(reservation) = %#x, want %#x", got, domain.OpReservationExpired)
	}
	if got := deliveryerrors.JoinRejectOpCode(domain.ErrTicketSlotHeld); got != domain.OpReservationSlotHeld {
		t.Fatalf("JoinRejectOpCode(slot held) = %#x, want %#x", got, domain.OpReservationSlotHeld)
	}
}
