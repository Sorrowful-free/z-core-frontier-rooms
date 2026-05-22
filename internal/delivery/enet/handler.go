package enet

import (
	"context"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
)

// RoomsHandler — inbound ENet: host loop, ticket в первом пакете, JoinRoom / LeaveRoom use cases.
type RoomsHandler struct {
	ctx              context.Context
	joinRoomUseCase  *room.JoinRoomUseCase
	leaveRoomUseCase *room.LeaveRoomUseCase
	logger           logging.Logger
	cfg              Config
}

func NewRoomsHandler(ctx context.Context, joinRoomUseCase *room.JoinRoomUseCase, leaveRoomUseCase *room.LeaveRoomUseCase, cfg Config, logger logging.Logger) *RoomsHandler {
	return &RoomsHandler{
		ctx:              ctx,
		joinRoomUseCase:  joinRoomUseCase,
		leaveRoomUseCase: leaveRoomUseCase,
		logger:           logger,
		cfg:              cfg,
	}
}

// Run запускает ENet host loop до отмены ctx. Без тега enet+cgo — см. run_stub.go.
func (h *RoomsHandler) Listen() {
	go func() {
		if err := h.run(h.ctx); err != nil {
			h.logger.Error("enet listen failed", "error", err)
		}
	}()
}
