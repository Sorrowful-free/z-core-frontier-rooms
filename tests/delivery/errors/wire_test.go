package errors_test

import (
	"testing"

	deliveryerrors "github.com/Sorrowful-free/z-core-frontier-rooms/internal/delivery/errors"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

func TestIsJoinRejectOpCode(t *testing.T) {
	t.Parallel()

	// Reject-диапазоны: 0x40–0x5F (join) и 0x70–0x7F (reservation) —
	// оба закрывают соединение. Границы + named OpCode для читаемости.
	cases := []struct {
		name string
		op   domain.OpCode
		want bool
	}{
		{"below join band", domain.OpCode(0x3F), false},
		{"join band min", domain.OpCode(0x40), true},
		{"join band max", domain.OpCode(0x5F), true},
		{"in-room band", domain.OpCode(0x60), false},
		{"in-room band max", domain.OpCode(0x6F), false},
		{"reservation band min", domain.OpCode(0x70), true},
		{"reservation band max", domain.OpCode(0x7F), true},
		{"above reservation band", domain.OpCode(0x80), false},
		{"OpInvalidToken", domain.OpInvalidToken, true},
		{"OpInternal", domain.OpInternal, true},
		{"OpInRoomInternal", domain.OpInRoomInternal, false},
		{"OpReservationExpired", domain.OpReservationExpired, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deliveryerrors.IsJoinRejectOpCode(tc.op); got != tc.want {
				t.Fatalf("IsJoinRejectOpCode(%#x) = %v, want %v", tc.op, got, tc.want)
			}
		})
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
