package transport

import (
	"fmt"
	"time"
)

const (
	// DefaultMaxIncomingFrameBytes — 256 KiB; WS SetReadLimit и ENet packet check.
	DefaultMaxIncomingFrameBytes = 256 * 1024

	DefaultRoomIncomingQueueSize = 128
	DefaultPeerOutboundQueueSize = 128
	DefaultEnetIncomingQueueSize = 256

	// DefaultPeerIdleTimeout — окно живости IsAlive для WS.
	DefaultPeerIdleTimeout = 30 * time.Second
	// DefaultPeerIdleSweepInterval — период фонового sweep idle peer'ов.
	DefaultPeerIdleSweepInterval = 10 * time.Second

	// DefaultWSWriteDeadline — дедлайн записи WS-кадра (Send).
	DefaultWSWriteDeadline = 10 * time.Second

	// DefaultPeerCloseDrainTimeout — потолок drain'а peer.outbound при
	// Stop: закоммиченные до Stop кадры получают попытку Send до
	// conn.Close (P30). Дубль-партнёр realtime.defaultPeerCloseDrainTimeout
	// (дефолт прямого NewPeer в тестах) — держать равными.
	DefaultPeerCloseDrainTimeout = 1 * time.Second
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
	// PeerIdleTimeout — окно живости для IsAlive; <= 0 — механизм полностью отключён.
	PeerIdleTimeout time.Duration
	// PeerIdleSweepInterval — период фонового sweep idle peer'ов; <= 0 — не запускать.
	PeerIdleSweepInterval time.Duration
	// WSWriteDeadline — дедлайн записи WS-кадра (SetWriteDeadline перед WriteMessage); 0 — без дедлайна.
	WSWriteDeadline time.Duration
	// PeerCloseDrainTimeout — потолок drain'а peer.outbound при peer.Stop (P30); 0 — drain отключён (Stop не ждёт write-loop).
	PeerCloseDrainTimeout time.Duration
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
