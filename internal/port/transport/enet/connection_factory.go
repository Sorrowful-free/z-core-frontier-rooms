package enet

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
)

// EnetConnectionFactory creates a transport.Connection for an ENet peer.
// Peer is a tagged alias (peer_cgo.go / peer_stub.go): under enet+cgo it is
// libenet.Peer, in other builds it is any, so the interface is the same for
// all build tags.
type EnetConnectionFactory interface {
	CreateConnection(peer Peer, incoming chan domain.Frame) transport.Connection
}
