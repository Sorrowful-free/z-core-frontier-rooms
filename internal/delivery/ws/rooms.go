package ws

import (
	"context"

	wsconn "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/ws"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
	"github.com/gofiber/contrib/v3/websocket"
	"github.com/gofiber/fiber/v3"
)

type RoomsHandler struct {
	ctx              context.Context
	joinRoomUseCase  *room.JoinRoomUseCase
	leaveRoomUseCase *room.LeaveRoomUseCase
	logger           logging.Logger
}

func NewRoomsHandler(ctx context.Context, joinRoomUseCase *room.JoinRoomUseCase, leaveRoomUseCase *room.LeaveRoomUseCase, logger logging.Logger) *RoomsHandler {
	return &RoomsHandler{
		ctx:              ctx,
		joinRoomUseCase:  joinRoomUseCase,
		leaveRoomUseCase: leaveRoomUseCase,
		logger:           logger,
	}
}

func (h *RoomsHandler) RegisterRoutes(app *fiber.App) {
	app.Use("/ws", func(c fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})

	app.Get("/ws", websocket.New(h.handleConnect))
}

func (h *RoomsHandler) handleConnect(c *websocket.Conn) {
	token := []byte(c.Query("token"))
	if len(token) == 0 {
		h.logger.Warn("websocket connect: missing token")
		_ = c.Close()
		return
	}

	connection := wsconn.NewWsConnection(c)
	defer func() { _ = connection.Close() }()

	summary, peerID, err := h.joinRoomUseCase.JoinRoom(h.ctx, connection, token)
	if err != nil {
		h.logger.Error("websocket join room failed", "error", err)
		return
	}

	connection.Wait()

	if err := h.leaveRoomUseCase.LeaveRoom(h.ctx, summary.ID, peerID); err != nil {
		h.logger.Error("websocket leave room failed", "error", err, "roomID", summary.ID, "peerID", peerID)
	}
}
