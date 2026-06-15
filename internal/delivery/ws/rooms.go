package ws

import (
	"context"

	adaptertransport "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport"
	deliveryerrors "github.com/Sorrowful-free/z-core-frontier-rooms/internal/delivery/errors"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport/ws"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
	"github.com/gofiber/contrib/v3/websocket"
	"github.com/gofiber/fiber/v3"
)

type RoomsHandler struct {
	ctx               context.Context
	joinRoomUseCase   *room.JoinRoomUseCase
	leaveRoomUseCase  *room.LeaveRoomUseCase
	connectionFactory ws.WsConnectionFactory
	transportCfg      adaptertransport.TransportConfig
	logger            logging.Logger
}

func NewRoomsHandler(
	ctx context.Context,
	joinRoomUseCase *room.JoinRoomUseCase,
	leaveRoomUseCase *room.LeaveRoomUseCase,
	connectionFactory ws.WsConnectionFactory,
	transportCfg adaptertransport.TransportConfig,
	logger logging.Logger,
) *RoomsHandler {
	return &RoomsHandler{
		ctx:               ctx,
		joinRoomUseCase:   joinRoomUseCase,
		leaveRoomUseCase:  leaveRoomUseCase,
		connectionFactory: connectionFactory,
		transportCfg:      transportCfg,
		logger:            logger,
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
	if h.transportCfg.MaxIncomingFrameBytes > 0 && len(token) > h.transportCfg.MaxIncomingFrameBytes {
		h.logger.Warn("websocket connect: token too large", "bytes", len(token))
		_ = c.Close()
		return
	}

	connection, waiter := h.connectionFactory.CreateConnection(c)
	defer func() { _ = connection.Close() }()

	summary, peerID, err := h.joinRoomUseCase.JoinRoom(h.ctx, connection, token)
	if err != nil {
		deliveryerrors.SendJoinReject(connection, err)
		h.logger.Error("websocket join room failed", "error", err, "op", deliveryerrors.JoinRejectOpCode(err))
		return
	}

	waiter.Wait()

	if err := h.leaveRoomUseCase.LeaveRoom(h.ctx, summary.ID, peerID); err != nil {
		h.logger.Error("websocket leave room failed", "error", err, "roomID", summary.ID, "peerID", peerID)
	}
}
