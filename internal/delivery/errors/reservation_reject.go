package errors

import (
	stderrors "errors"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

// mapReservationRejectOpCode maps reservation errors to wire OpCode
// (0x70–0x7F, empty payload). Reached only via JoinRejectOpCode on the
// join/admit path — reservation failures on the HTTP issue route return JSON,
// and there is no standalone reservation-reject sender.
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
	case stderrors.Is(err, domain.ErrWrongHost):
		return domain.OpWrongHost, true
	case stderrors.Is(err, domain.ErrReservationInternal):
		return domain.OpReservationInternal, true
	default:
		return 0, false
	}
}
