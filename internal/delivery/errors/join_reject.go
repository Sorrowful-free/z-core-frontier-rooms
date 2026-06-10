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
func SendJoinReject(conn transport.Connection, err error) {
	sendErrorFrame(conn, JoinRejectOpCode(err))
}

func sendErrorFrame(conn transport.Connection, op domain.OpCode) {
	_ = conn.Send(domain.Frame{
		OpCode:   op,
		Delivery: domain.DeliveryReliable,
		Payload:  nil,
	})
}

// IsJoinRejectOpCode reports whether op belongs to the join/admit reject range (0x40–0x5F).
func IsJoinRejectOpCode(op domain.OpCode) bool {
	return op >= 0x40 && op <= 0x5F
}

// IsInRoomErrorOpCode reports whether op belongs to the in-room error range (0x60–0x6F).
func IsInRoomErrorOpCode(op domain.OpCode) bool {
	return op >= 0x60 && op <= 0x6F
}

// InRoomErrorOpCode maps in-room errors to wire OpCode (0x60–0x6F, empty payload).
func InRoomErrorOpCode(err error) domain.OpCode {
	switch {
	case stderrors.Is(err, domain.ErrNotMaster):
		return domain.OpInRoomNotMaster
	case stderrors.Is(err, domain.ErrInRoomInvalidPayload):
		return domain.OpInRoomInvalidPayload
	case stderrors.Is(err, domain.ErrInvalidRpcTarget):
		return domain.OpInRoomInvalidRpc
	case stderrors.Is(err, domain.ErrRpcTargetPeerNotFound):
		return domain.OpInRoomRpcPeerNotFound
	case stderrors.Is(err, domain.ErrNoMaster):
		return domain.OpInRoomNoMaster
	case stderrors.Is(err, domain.ErrInRoomInternal):
		return domain.OpInRoomInternal
	default:
		return domain.OpInRoomInternal
	}
}

// SendInRoomError sends one in-room error frame; connection may stay open.
func SendInRoomError(conn transport.Connection, err error) {
	sendErrorFrame(conn, InRoomErrorOpCode(err))
}
