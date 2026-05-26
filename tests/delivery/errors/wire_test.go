package errors_test

import (
	"testing"

	deliveryerrors "github.com/Sorrowful-free/z-core-frontier-rooms/internal/delivery/errors"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

func TestIsJoinRejectOpCode(t *testing.T) {
	t.Parallel()

	if !deliveryerrors.IsJoinRejectOpCode(domain.OpInvalidToken) {
		t.Fatal("0x41 must be join reject")
	}
	if !deliveryerrors.IsJoinRejectOpCode(domain.OpInternal) {
		t.Fatal("0x50 must be join-phase (internal band)")
	}
	if deliveryerrors.IsJoinRejectOpCode(domain.OpInRoomInternal) {
		t.Fatal("0x60 is in-room, not join reject")
	}
	if deliveryerrors.IsJoinRejectOpCode(domain.OpReservationExpired) {
		t.Fatal("0x74 is reservation reject, not join band 0x40–0x5F")
	}
}

func TestIsReservationRejectOpCode(t *testing.T) {
	t.Parallel()

	if !deliveryerrors.IsReservationRejectOpCode(domain.OpReservationFull) {
		t.Fatal("0x71 must be reservation reject")
	}
	if !deliveryerrors.IsReservationRejectOpCode(domain.OpReservationInternal) {
		t.Fatal("0x7F must be reservation reject")
	}
	if deliveryerrors.IsReservationRejectOpCode(domain.OpJoinDenied) {
		t.Fatal("0x44 is join reject, not reservation")
	}
}

func TestIsInRoomErrorOpCode(t *testing.T) {
	t.Parallel()

	if !deliveryerrors.IsInRoomErrorOpCode(domain.OpInRoomInternal) {
		t.Fatal("0x60 must be in-room")
	}
	if deliveryerrors.IsInRoomErrorOpCode(domain.OpJoinDenied) {
		t.Fatal("0x44 is join reject, not in-room")
	}
}
