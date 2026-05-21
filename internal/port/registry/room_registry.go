package registry

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

type RoomRegistry interface {
	GetRoom(id domain.RoomID) (realtime.Room, error)
	CreateRoom(id domain.RoomID) (realtime.Room, error)
	DeleteRoom(id domain.RoomID) error
}
