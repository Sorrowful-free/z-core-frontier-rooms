package enet

import (
	"context"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
)

// RoomsHandler — inbound ENet: host loop, ticket в первом пакете, ConnectUseCase.
type RoomsHandler struct {
	connectUseCase *room.ConnectUseCase
	ctx            context.Context
	logger         logging.Logger
	cfg            Config
}

func NewRoomsHandler(connectUseCase *room.ConnectUseCase, ctx context.Context, cfg Config, logger logging.Logger) *RoomsHandler {
	return &RoomsHandler{
		connectUseCase: connectUseCase,
		ctx:            ctx,
		logger:         logger,
		cfg:            cfg,
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
