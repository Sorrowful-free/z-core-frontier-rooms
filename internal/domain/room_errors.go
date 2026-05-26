package domain

import "errors"

// Sentinel-ошибки внутри комнаты (после admit); маппинг — delivery/errors/in_room.go.

var (
	ErrInRoomInternal    = errors.New("in-room: internal")
	ErrRoomNotFound      = errors.New("room not found")
	ErrRoomAlreadyExists = errors.New("room already exists")
)
