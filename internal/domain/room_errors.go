package domain

import "errors"

// Sentinel-ошибки внутри комнаты (после admit).
// Wire OpCode: InRoomErrorOpCode; SendInRoomError — delivery/errors.

var (
	ErrInRoomInternal        = errors.New("in-room: internal")
	ErrInRoomInvalidPayload  = errors.New("in-room: invalid payload")
	ErrNotMaster             = errors.New("in-room: not master")
	ErrInvalidRpcTarget      = errors.New("in-room: invalid rpc target")
	ErrRpcTargetPeerNotFound = errors.New("in-room: rpc target peer not found")
	ErrNoMaster              = errors.New("in-room: no master")
	ErrInRoomUnknownOpcode   = errors.New("in-room: unknown opcode")
	ErrRoomNotFound          = errors.New("room not found")
	ErrRoomAlreadyExists     = errors.New("room already exists")
	ErrRoomsLimitReached     = errors.New("rooms limit reached")
	ErrQueueFull             = errors.New("queue full")
)

// InRoomErrorOpCode maps in-room errors to wire OpCode (0x60–0x6F, empty payload).
func InRoomErrorOpCode(err error) OpCode {
	switch {
	case errors.Is(err, ErrNotMaster):
		return OpInRoomNotMaster
	case errors.Is(err, ErrInRoomInvalidPayload):
		return OpInRoomInvalidPayload
	case errors.Is(err, ErrInvalidRpcTarget):
		return OpInRoomInvalidRpc
	case errors.Is(err, ErrRpcTargetPeerNotFound):
		return OpInRoomRpcPeerNotFound
	case errors.Is(err, ErrNoMaster):
		return OpInRoomNoMaster
	case errors.Is(err, ErrInRoomUnknownOpcode):
		return OpInRoomUnknownOpcode
	case errors.Is(err, ErrInRoomInternal):
		return OpInRoomInternal
	default:
		return OpInRoomInternal
	}
}
