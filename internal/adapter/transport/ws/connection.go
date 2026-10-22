package ws

import (
	"encoding/binary"
	"sync"
	"sync/atomic"
	"time"

	adaptertransport "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	fwebsocket "github.com/fasthttp/websocket"
	"github.com/gofiber/contrib/v3/websocket"
)

const (
	wsPingInterval  = 2 * time.Second
	wsPingWriteWait = 5 * time.Second
)

type WsConnection struct {
	conn                  *fwebsocket.Conn
	closed                chan struct{}
	closeOnce             sync.Once
	pingMs                atomic.Int64
	lastSeen              atomic.Int64
	maxIncomingFrameBytes int
	idleTimeout           time.Duration
	writeDeadline         time.Duration
}

func NewWsConnection(conn *websocket.Conn, cfg adaptertransport.TransportConfig) *WsConnection {
	inner := conn.Conn
	maxBytes := cfg.MaxIncomingFrameBytes
	if maxBytes > 0 {
		inner.SetReadLimit(int64(maxBytes))
	}

	c := &WsConnection{
		conn:                  inner,
		closed:                make(chan struct{}),
		maxIncomingFrameBytes: maxBytes,
		idleTimeout:           cfg.PeerIdleTimeout,
		writeDeadline:         cfg.WSWriteDeadline,
	}
	c.pingMs.Store(-1)
	c.lastSeen.Store(time.Now().UnixNano())

	inner.SetPongHandler(func(message string) error {
		now := time.Now().UnixNano()
		c.lastSeen.Store(now)
		payload := []byte(message)
		if len(payload) < 8 {
			return nil
		}
		sentAt := int64(binary.BigEndian.Uint64(payload[:8]))
		rtt := (now - sentAt) / int64(time.Millisecond)
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
	if c.writeDeadline > 0 {
		// Зависший write не должен блокировать peer.outbound навсегда.
		// NB: ping-loop WriteControl может перезаписать deadline на более
		// короткий — приемлемо: преждевременный timeout = logged drop.
		if err := c.conn.SetWriteDeadline(time.Now().Add(c.writeDeadline)); err != nil {
			return err
		}
	}
	return c.conn.WriteMessage(fwebsocket.BinaryMessage, payload)
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
	c.lastSeen.Store(time.Now().UnixNano())
	return frame, nil
}

func (c *WsConnection) Ping() int64 {
	return c.pingMs.Load()
}

// IsAlive — false: соединение закрыто или живость (входящие кадры + pong) не подтверждалась дольше idleTimeout.
// idleTimeout <= 0 — проверка только closed (механизм выключен).
func (c *WsConnection) IsAlive() bool {
	select {
	case <-c.closed:
		return false
	default:
	}
	if c.idleTimeout <= 0 {
		return true
	}
	return time.Since(time.Unix(0, c.lastSeen.Load())) <= c.idleTimeout
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
			if err := c.conn.WriteControl(fwebsocket.PingMessage, payload, time.Now().Add(wsPingWriteWait)); err != nil {
				// conn не отвечает на запись — помечаем закрытым, иначе zombie остаётся IsAlive.
				_ = c.Close()
				return
			}
		}
	}
}

// Wait blocks until the read loop exits (client disconnect or Close).
func (c *WsConnection) Wait() {
	<-c.closed
}
