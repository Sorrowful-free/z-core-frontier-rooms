package realtime

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

type RoomFactory struct {
	logger logging.Logger
}

func NewRoomFactory(logger logging.Logger) *RoomFactory {
	return &RoomFactory{
		logger: logger,
	}
}

func (f *RoomFactory) CreateRoom(id domain.RoomID, handler realtime.RoomHandler) realtime.Room {
	return NewRoom(id, handler, f.logger)
}
