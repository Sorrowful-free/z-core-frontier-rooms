package ws

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	porttransport "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport/ws"
	"github.com/gofiber/contrib/v3/websocket"
)

type WsConnectionFactory struct {
	logger logging.Logger
	cfg    transport.TransportConfig
}

func NewWsConnectionFactory(logger logging.Logger, cfg transport.TransportConfig) *WsConnectionFactory {
	return &WsConnectionFactory{
		logger: logger,
		cfg:    cfg,
	}
}

func (f *WsConnectionFactory) CreateConnection(conn *websocket.Conn) (porttransport.Connection, ws.WsConnectionWaiter) {
	connection := NewWsConnection(conn, f.cfg)
	return connection, connection
}
