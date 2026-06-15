package ws

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"

	adaptertransport "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/gofiber/contrib/v3/websocket"
)

const (
	wsPingInterval = 2 * time.Second
	wsPingWriteWait = 5 * time.Second
)

type WsConnection struct {
	conn                  *websocket.Conn
	closed                chan struct{}
	closeOnce             sync.Once
	pingMs                atomic.Int64
	maxIncomingFrameBytes int
}

func NewWsConnection(conn *websocket.Conn, cfg adaptertransport.TransportConfig) *WsConnection {
	maxBytes := cfg.MaxIncomingFrameBytes
	if maxBytes > 0 {
		conn.SetReadLimit(int64(maxBytes))
	}

	c := &WsConnection{
		conn:                  conn,
		closed:                make(chan struct{}),
		maxIncomingFrameBytes: maxBytes,
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

	frame, err := domain.DecodeIncomingFrame(payload, c.maxIncomingFrameBytes)
	if err != nil {
		c.Close()
		return domain.Frame{}, err
	}
	return frame, nil
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
