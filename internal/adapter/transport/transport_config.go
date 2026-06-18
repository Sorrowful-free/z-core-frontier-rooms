package transport

import "fmt"

const (
	// DefaultMaxIncomingFrameBytes — 256 KiB; WS SetReadLimit и ENet packet check.
	DefaultMaxIncomingFrameBytes = 256 * 1024

	DefaultRoomIncomingQueueSize = 128
	DefaultPeerOutboundQueueSize = 128
	DefaultEnetIncomingQueueSize = 256
)

// TransportConfig — лимиты data plane (WS + ENet).
type TransportConfig struct {
	// MaxIncomingFrameBytes — макс. размер входящего кадра (OpCode + payload) и ENet admit-пакета; 0 — без лимита.
	MaxIncomingFrameBytes int
	// RoomIncomingQueueSize — буфер room.incoming; 0 — unbuffered; при переполнении — drop кадра.
	RoomIncomingQueueSize int
	// PeerOutboundQueueSize — буфер peer.outbound; 0 — unbuffered; при переполнении — drop кадра.
	PeerOutboundQueueSize int
	// EnetIncomingQueueSize — буфер ENet session → peer; при переполнении — drop (host loop не блокируется).
	EnetIncomingQueueSize int
}

// Validate проверяет допустимость значений.
func (c TransportConfig) Validate() error {
	if c.MaxIncomingFrameBytes < 0 {
		return fmt.Errorf("transport config: max incoming frame bytes must be >= 0")
	}
	if c.RoomIncomingQueueSize < 0 {
		return fmt.Errorf("transport config: room incoming queue size must be >= 0")
	}
	if c.PeerOutboundQueueSize < 0 {
		return fmt.Errorf("transport config: peer outbound queue size must be >= 0")
	}
	if c.EnetIncomingQueueSize < 0 {
		return fmt.Errorf("transport config: enet incoming queue size must be >= 0")
	}
	return nil
}
