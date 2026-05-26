package errors_test

import (
	"errors"
	"fmt"
	"testing"

	deliveryerrors "github.com/Sorrowful-free/z-core-frontier-rooms/internal/delivery/errors"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

func TestReservationRejectOpCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want domain.OpCode
	}{
		{"not found", domain.ErrReservationNotFound, domain.OpReservationNotFound},
		{"full", domain.ErrReservationFull, domain.OpReservationFull},
		{"slot held", domain.ErrTicketSlotHeld, domain.OpReservationSlotHeld},
		{"already exists", domain.ErrReservationAlreadyExists, domain.OpReservationSlotHeld},
		{"not reserved", domain.ErrReservationNotReserved, domain.OpReservationNotReserved},
		{"expired", domain.ErrReservationExpired, domain.OpReservationExpired},
		{"already admitted", domain.ErrReservationAlreadyAdmitted, domain.OpReservationAlreadyAdmitted},
		{"peer in room", domain.ErrPeerAlreadyInRoom, domain.OpPeerAlreadyInRoom},
		{
			"wrapped expired",
			fmt.Errorf("admit: %w: room 1 peer 2", domain.ErrReservationExpired),
			domain.OpReservationExpired,
		},
		{"unknown", errors.New("other"), domain.OpReservationInternal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := deliveryerrors.ReservationRejectOpCode(tt.err); got != tt.want {
				t.Fatalf("ReservationRejectOpCode() = %#x, want %#x", got, tt.want)
			}
		})
	}
}

func TestJoinRejectOpCode_ReservationErrors(t *testing.T) {
	t.Parallel()

	if got := deliveryerrors.JoinRejectOpCode(domain.ErrReservationExpired); got != domain.OpReservationExpired {
		t.Fatalf("JoinRejectOpCode(reservation) = %#x, want %#x", got, domain.OpReservationExpired)
	}
	if got := deliveryerrors.JoinRejectOpCode(domain.ErrTicketSlotHeld); got != domain.OpReservationSlotHeld {
		t.Fatalf("JoinRejectOpCode(slot held) = %#x, want %#x", got, domain.OpReservationSlotHeld)
	}
}
