package errors

import (
	stderrors "errors"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
)

// ReservationRejectOpCode maps reservation errors to wire OpCode (0x70–0x7F, empty payload).
func ReservationRejectOpCode(err error) domain.OpCode {
	if op, ok := mapReservationRejectOpCode(err); ok {
		return op
	}
	return domain.OpReservationInternal
}

// SendReservationReject sends one reject frame before the transport is closed.
func SendReservationReject(conn transport.Connection, err error) {
	sendErrorFrame(conn, ReservationRejectOpCode(err))
}

// IsReservationRejectOpCode reports whether op belongs to the reservation reject range (0x70–0x7F).
func IsReservationRejectOpCode(op domain.OpCode) bool {
	return op >= 0x70 && op <= 0x7F
}

func mapReservationRejectOpCode(err error) (domain.OpCode, bool) {
	switch {
	case stderrors.Is(err, domain.ErrReservationNotFound):
		return domain.OpReservationNotFound, true
	case stderrors.Is(err, domain.ErrReservationFull):
		return domain.OpReservationFull, true
	case stderrors.Is(err, domain.ErrTicketSlotHeld),
		stderrors.Is(err, domain.ErrReservationAlreadyExists):
		return domain.OpReservationSlotHeld, true
	case stderrors.Is(err, domain.ErrReservationNotReserved):
		return domain.OpReservationNotReserved, true
	case stderrors.Is(err, domain.ErrReservationExpired):
		return domain.OpReservationExpired, true
	case stderrors.Is(err, domain.ErrReservationAlreadyAdmitted):
		return domain.OpReservationAlreadyAdmitted, true
	case stderrors.Is(err, domain.ErrPeerAlreadyInRoom):
		return domain.OpPeerAlreadyInRoom, true
	case stderrors.Is(err, domain.ErrReservationInternal):
		return domain.OpReservationInternal, true
	default:
		return 0, false
	}
}
