package ws

import (
	"context"
	"encoding/base64"

	adaptertransport "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport"
	deliveryerrors "github.com/Sorrowful-free/z-core-frontier-rooms/internal/delivery/errors"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport/ws"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
	"github.com/gofiber/contrib/v3/websocket"
	"github.com/gofiber/fiber/v3"
)

type RoomsHandler struct {
	ctx                   context.Context
	admitPeerUseCase      *room.AdmitPeerUseCase
	disconnectPeerUseCase *room.DisconnectPeerUseCase
	connectionFactory     ws.WsConnectionFactory
	transportCfg          adaptertransport.TransportConfig
	logger                logging.Logger
	metrics               portmetrics.Metrics
}

func NewRoomsHandler(
	ctx context.Context,
	admitPeerUseCase *room.AdmitPeerUseCase,
	disconnectPeerUseCase *room.DisconnectPeerUseCase,
	connectionFactory ws.WsConnectionFactory,
	transportCfg adaptertransport.TransportConfig,
	logger logging.Logger,
	m portmetrics.Metrics,
) *RoomsHandler {
	return &RoomsHandler{
		ctx:                   ctx,
		admitPeerUseCase:      admitPeerUseCase,
		disconnectPeerUseCase: disconnectPeerUseCase,
		connectionFactory:     connectionFactory,
		transportCfg:          transportCfg,
		logger:                logger,
		metrics:               portmetrics.OrNoop(m),
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
	connection, waiter := h.connectionFactory.CreateConnection(c)
	defer func() { _ = connection.Close() }()

	tokenQuery := c.Query("token")
	if tokenQuery == "" {
		h.sendJoinReject(connection, domain.ErrEmptyToken)
		h.logger.Warn("websocket connect: missing token")
		return
	}

	token, err := base64.RawURLEncoding.DecodeString(tokenQuery)
	if err != nil {
		h.sendJoinReject(connection, domain.ErrInvalidToken)
		h.logger.Warn("websocket connect: invalid token encoding")
		return
	}
	if h.transportCfg.MaxIncomingFrameBytes > 0 && len(token) > h.transportCfg.MaxIncomingFrameBytes {
		h.logger.Warn("websocket connect: token too large", "bytes", len(token))
		return
	}

	// Единый admit-вход (P20): join-ticket и reconnect-токен разводит фасад
	// по claims.Kind; HostIDUnbound — на WS strict binding не проверяется.
	session, err := h.admitPeerUseCase.Admit(h.ctx, connection, token, domain.HostIDUnbound)
	if err != nil {
		h.sendJoinReject(connection, err)
		h.logger.Error("websocket admit failed", "error", err, "op", deliveryerrors.JoinRejectOpCode(err))
		return
	}

	waiter.Wait()

	if err := h.disconnectPeerUseCase.DisconnectPeer(h.ctx, session); err != nil {
		h.logger.Error("websocket disconnect peer failed", "error", err, "roomID", session.RoomID, "peerID", session.PeerID)
	}
}

// sendJoinReject — reject-кадр + счётчик rejects по wire opcode (P19):
// считается только реально отправленный кадр (P19b), ошибка send —
// в лог, как до появления счётчика.
func (h *RoomsHandler) sendJoinReject(connection transport.Connection, err error) {
	if sendErr := deliveryerrors.SendJoinReject(connection, err); sendErr != nil {
		h.logger.Warn("websocket join reject send failed", "error", sendErr)
		return
	}
	h.metrics.JoinRejected(deliveryerrors.JoinRejectOpCode(err))
}
