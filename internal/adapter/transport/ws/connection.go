package ws

import (
	"sync"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/gofiber/contrib/v3/websocket"
)

type WsConnection struct {
	conn      *websocket.Conn
	closed    chan struct{}
	closeOnce sync.Once
}

func NewWsConnection(conn *websocket.Conn) *WsConnection {
	c := &WsConnection{
		conn:   conn,
		closed: make(chan struct{}),
	}
	return c
}

func (c *WsConnection) Close() error {
	var err error
	c.closeOnce.Do(func() {
		c.closeInternal()
		err = c.conn.Close()
	})
	return err
}

func (c *WsConnection) Send(frame domain.Frame) error {
	payload := append([]byte{byte(frame.OpCode)}, frame.Payload...)
	return c.conn.WriteMessage(websocket.BinaryMessage, payload)
}

func (c *WsConnection) Receive() (domain.Frame, error) {
	_, payload, err := c.conn.ReadMessage()
	if err != nil {
		c.closeInternal()
		return domain.Frame{}, err
	}
	return domain.Frame{OpCode: domain.OpCode(payload[0]), Delivery: domain.DeliveryDefault, Payload: domain.Payload(payload[1:])}, nil
}

// Wait blocks until the read loop exits (client disconnect or Close).
func (c *WsConnection) Wait() {
	<-c.closed
}

func (c *WsConnection) closeInternal() {
	close(c.closed)
}
