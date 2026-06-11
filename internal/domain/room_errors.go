package domain

import "errors"

// Sentinel-ошибки внутри комнаты (после admit); маппинг — delivery/errors/join_reject.go (InRoomErrorOpCode).

var (
	ErrInRoomInternal        = errors.New("in-room: internal")
	ErrInRoomInvalidPayload  = errors.New("in-room: invalid payload")
	ErrNotMaster             = errors.New("in-room: not master")
	ErrInvalidRpcTarget      = errors.New("in-room: invalid rpc target")
	ErrRpcTargetPeerNotFound = errors.New("in-room: rpc target peer not found")
	ErrNoMaster              = errors.New("in-room: no master")
	ErrInRoomUnknownOpcode   = errors.New("in-room: unknown opcode")
	ErrRoomNotFound         = errors.New("room not found")
	ErrRoomAlreadyExists    = errors.New("room already exists")
	ErrRoomsLimitReached    = errors.New("rooms limit reached")
)
