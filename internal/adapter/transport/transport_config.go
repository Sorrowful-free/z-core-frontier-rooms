package transport

import (
	"fmt"
	"time"
)

const (
	// DefaultMaxIncomingFrameBytes — 256 KiB; WS SetReadLimit and ENet packet check.
	DefaultMaxIncomingFrameBytes = 256 * 1024

	DefaultRoomIncomingQueueSize = 128
	DefaultPeerOutboundQueueSize = 128
	DefaultEnetIncomingQueueSize = 256

	// DefaultPeerIdleTimeout — IsAlive liveness window for WS.
	DefaultPeerIdleTimeout = 30 * time.Second
	// DefaultPeerIdleSweepInterval — background idle peer sweep period.
	DefaultPeerIdleSweepInterval = 10 * time.Second

	// DefaultWSWriteDeadline — write deadline for a WS frame (Send).
	DefaultWSWriteDeadline = 10 * time.Second

	// DefaultPeerCloseDrainTimeout — cap on draining peer.outbound at
	// Stop: frames committed before Stop get a Send attempt before
	// conn.Close (P30). Twin of realtime.defaultPeerCloseDrainTimeout
	// (default of direct NewPeer in tests) — keep them equal.
	DefaultPeerCloseDrainTimeout = 1 * time.Second
)

// TransportConfig — data plane limits (WS + ENet).
type TransportConfig struct {
	// MaxIncomingFrameBytes — max incoming frame size (OpCode + payload) and ENet admit packet; 0 — no limit.
	MaxIncomingFrameBytes int
	// RoomIncomingQueueSize — room.incoming buffer; 0 — unbuffered; on overflow — frame drop.
	RoomIncomingQueueSize int
	// PeerOutboundQueueSize — peer.outbound buffer; 0 — unbuffered; on overflow — frame drop.
	PeerOutboundQueueSize int
	// EnetIncomingQueueSize — ENet session → peer buffer; on overflow — drop (host loop is not blocked).
	EnetIncomingQueueSize int
	// PeerIdleTimeout — liveness window for IsAlive; <= 0 — mechanism fully disabled.
	PeerIdleTimeout time.Duration
	// PeerIdleSweepInterval — background idle peer sweep period; <= 0 — do not start.
	PeerIdleSweepInterval time.Duration
	// WSWriteDeadline — write deadline for a WS frame (SetWriteDeadline before WriteMessage); 0 — no deadline.
	WSWriteDeadline time.Duration
	// PeerCloseDrainTimeout — cap on draining peer.outbound at peer.Stop (P30); 0 — drain disabled (Stop does not wait for the write loop).
	PeerCloseDrainTimeout time.Duration
}

// Validate checks that values are permissible.
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
	if c.PeerIdleTimeout < 0 {
		return fmt.Errorf("transport config: peer idle timeout must be >= 0")
	}
	if c.PeerIdleSweepInterval < 0 {
		return fmt.Errorf("transport config: peer idle sweep interval must be >= 0")
	}
	if c.WSWriteDeadline < 0 {
		return fmt.Errorf("transport config: ws write deadline must be >= 0")
	}
	if c.PeerCloseDrainTimeout < 0 {
		return fmt.Errorf("transport config: peer close drain timeout must be >= 0")
	}
	return nil
}
