package enet

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
)

// EnetConnectionFactory создаёт transport.Connection для ENet-peer'а.
// Peer — tagged alias (peer_cgo.go / peer_stub.go): под enet+cgo это libenet.Peer,
// в остальных сборках — any, поэтому интерфейс один на все build-теги.
type EnetConnectionFactory interface {
	CreateConnection(peer Peer, incoming chan domain.Frame) transport.Connection
}
