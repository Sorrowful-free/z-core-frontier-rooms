package realtime

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

type RelayRoomHandlerFactory struct {
	logger logging.Logger
}

func NewRelayRoomHandlerFactory(logger logging.Logger) *RelayRoomHandlerFactory {
	return &RelayRoomHandlerFactory{
		logger: logger,
	}
}

func (f *RelayRoomHandlerFactory) CreateRoomHandler() realtime.RoomHandler {
	return NewRelayRoomHandler(f.logger)
}
