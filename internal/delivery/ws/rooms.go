package ws

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
)

type RoomsHandler struct {
	connectUseCase *room.ConnectUseCase
	logger         logging.Logger
}

func NewRoomsHandler(connectUseCase *room.ConnectUseCase, logger logging.Logger) *RoomsHandler {
	return &RoomsHandler{
		connectUseCase: connectUseCase,
		logger:         logger,
	}
}
