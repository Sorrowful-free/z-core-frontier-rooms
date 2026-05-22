package ws

import (
	"github.com/gofiber/contrib/v3/websocket"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
)

type WsConnectionFactory interface {
	CreateConnection(conn *websocket.Conn) (transport.Connection, WsConnectionWaiter)
}
