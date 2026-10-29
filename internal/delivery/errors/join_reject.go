package errors

import (
	stderrors "errors"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
)

// JoinRejectOpCode maps join/admit errors to wire OpCode (0x40–0x5F, 0x70–0x7F, empty payload).
func JoinRejectOpCode(err error) domain.OpCode {
	if op, ok := mapReservationRejectOpCode(err); ok {
		return op
	}
	switch {
	case stderrors.Is(err, domain.ErrEmptyToken):
		return domain.OpEmptyToken
	case stderrors.Is(err, domain.ErrInvalidToken):
		return domain.OpInvalidToken
	case stderrors.Is(err, domain.ErrExpiredToken):
		return domain.OpExpiredToken
	case stderrors.Is(err, domain.ErrRoomNotFound):
		return domain.OpRoomNotFound
	case stderrors.Is(err, domain.ErrRoomFull):
		return domain.OpReservationFull
	case stderrors.Is(err, domain.ErrJoinDenied):
		return domain.OpJoinDenied
	case stderrors.Is(err, domain.ErrReplaceFailed):
		return domain.OpReplaceFailed
	case stderrors.Is(err, domain.ErrPeerNotFound):
		return domain.OpPeerNotFound
	case stderrors.Is(err, domain.ErrPeerStartFailed):
		return domain.OpPeerStartFailed
	default:
		return domain.OpInternal
	}
}

// SendJoinReject sends one reject frame before the transport is closed.
// A send error is returned to the caller: a join-reject counts as sent
// (JoinRejected metric) only on nil — otherwise the counter would count
// attempts rather than delivered frames.
func SendJoinReject(conn transport.Connection, err error) error {
	return sendErrorFrame(conn, JoinRejectOpCode(err))
}

func sendErrorFrame(conn transport.Connection, op domain.OpCode) error {
	return conn.Send(domain.Frame{
		OpCode:   op,
		Delivery: domain.DeliveryReliable,
		Payload:  nil,
	})
}

// InRoomErrorOpCode delegates to domain.InRoomErrorOpCode (adapter/realtime uses domain directly).
func InRoomErrorOpCode(err error) domain.OpCode {
	return domain.InRoomErrorOpCode(err)
}
