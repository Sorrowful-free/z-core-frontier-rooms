package ws

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/gofiber/contrib/v3/websocket"
)

type WsConnection struct {
	conn     *websocket.Conn
	incoming chan domain.Frame
}

func NewWsConnection(conn *websocket.Conn) *WsConnection {
	return &WsConnection{
		conn:     conn,
		incoming: make(chan domain.Frame),
	}
}

func (c *WsConnection) Close() error {
	return c.conn.Close()
}

func (c *WsConnection) GetIncoming() chan<- domain.Frame {
	return c.incoming
}

func (c *WsConnection) Send(frame domain.Frame) error {
	return c.conn.WriteMessage(websocket.BinaryMessage, frame.Payload)
}
