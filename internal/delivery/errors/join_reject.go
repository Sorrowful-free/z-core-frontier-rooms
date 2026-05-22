package errors

import (
	stderrors "errors"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
)

// JoinRejectOpCode maps join/admit errors to wire OpCode (0x40–0x5F, empty payload).
func JoinRejectOpCode(err error) domain.OpCode {
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
