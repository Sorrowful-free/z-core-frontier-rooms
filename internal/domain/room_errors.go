package domain

import "errors"

// Sentinel-ошибки внутри комнаты (после admit); маппинг — delivery/errors/in_room.go.

var (
	ErrInRoomInternal        = errors.New("in-room: internal")
	ErrInRoomInvalidPayload  = errors.New("in-room: invalid payload")
	ErrNotMaster             = errors.New("in-room: not master")
	ErrInvalidRpcTarget      = errors.New("in-room: invalid rpc target")
	ErrRpcTargetPeerNotFound = errors.New("in-room: rpc target peer not found")
	ErrNoMaster              = errors.New("in-room: no master")
	ErrRoomNotFound         = errors.New("room not found")
	ErrRoomAlreadyExists    = errors.New("room already exists")
)
