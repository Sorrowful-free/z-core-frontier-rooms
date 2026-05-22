package domain

import "errors"

// Sentinel-ошибки join/admit; delivery мапит их в OpCode (см. opcodes.go).

var (
	ErrEmptyToken         = errors.New("empty token")
	ErrInvalidToken       = errors.New("invalid token")
	ErrExpiredToken       = errors.New("expired token")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrRoomNotFound       = errors.New("room not found")
	ErrPeerNotFound       = errors.New("peer not found")
	ErrJoinDenied         = errors.New("join denied")
	ErrReplaceFailed      = errors.New("replace failed")
	ErrPeerStartFailed    = errors.New("peer start failed")
)
