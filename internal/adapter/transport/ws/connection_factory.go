package ws

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport/ws"
	"github.com/gofiber/contrib/v3/websocket"
)

type WsConnectionFactory struct {
	logger logging.Logger
}

func NewWsConnectionFactory(logger logging.Logger) *WsConnectionFactory {
	return &WsConnectionFactory{
		logger: logger,
	}
}

func (f *WsConnectionFactory) CreateConnection(conn *websocket.Conn) (transport.Connection, ws.WsConnectionWaiter) {
	connection := NewWsConnection(conn)
	return connection, connection
}
