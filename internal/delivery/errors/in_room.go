package errors

import (
	stderrors "errors"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
)

// InRoomErrorOpCode maps in-room errors to wire OpCode (0x60–0x6F, empty payload).
func InRoomErrorOpCode(err error) domain.OpCode {
	switch {
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
