package ws

import (
	"encoding/binary"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/gofiber/contrib/v3/websocket"
)

const (
	wsPingInterval = 2 * time.Second
	wsPingWriteWait = 5 * time.Second
)

type WsConnection struct {
	conn      *websocket.Conn
	closed    chan struct{}
	closeOnce sync.Once
	pingMs    atomic.Int64
}

func NewWsConnection(conn *websocket.Conn) *WsConnection {
	c := &WsConnection{
		conn:   conn,
		closed: make(chan struct{}),
	}
	c.pingMs.Store(-1)

	conn.SetPongHandler(func(message string) error {
		payload := []byte(message)
		if len(payload) < 8 {
			return nil
		}
		sentAt := int64(binary.BigEndian.Uint64(payload[:8]))
		rtt := (time.Now().UnixNano() - sentAt) / int64(time.Millisecond)
		if rtt >= 0 {
			c.pingMs.Store(rtt)
		}
		return nil
	})

	go c.runPingLoop()
	return c
}

func (c *WsConnection) Close() error {
	var err error
	c.closeOnce.Do(func() {
		close(c.closed)
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
		c.Close()
		return domain.Frame{}, err
	}

	if len(payload) == 0 {
		return domain.Frame{}, errors.New("ws: empty payload")
	}
	return domain.Frame{OpCode: domain.OpCode(payload[0]), Delivery: domain.DeliveryDefault, Payload: domain.Payload(payload[1:])}, nil
}

func (c *WsConnection) Ping() int64 {
	return c.pingMs.Load()
}

func (c *WsConnection) runPingLoop() {
	ticker := time.NewTicker(wsPingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.closed:
			return
		case <-ticker.C:
			payload := make([]byte, 8)
			binary.BigEndian.PutUint64(payload, uint64(time.Now().UnixNano()))
			if err := c.conn.WriteControl(websocket.PingMessage, payload, time.Now().Add(wsPingWriteWait)); err != nil {
				return
			}
		}
	}
}

// Wait blocks until the read loop exits (client disconnect or Close).
func (c *WsConnection) Wait() {
	<-c.closed
}
