package enet

import (
	"fmt"
	"time"

	adaptertransport "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport"
)

// DefaultAdmitTimeout — deadline EventConnect → admit: peer, не приславший ticket
// за это время, отключается (слот PeerLimit не занят бесконечно).
const DefaultAdmitTimeout = 10 * time.Second

// EnetConfig — параметры ENet host (отдельный UDP-слушатель, не Fiber).
// Файл без build-тегов: конфиг нужен и stub-сборкам без cgo.
type EnetConfig struct {
	ListenPort            uint16
	PeerLimit             uint64
	ChannelLimit          uint64
	ServiceTimeoutMs      uint32
	MaxIncomingFrameBytes int
	EnetIncomingQueueSize int
	// AdmitTimeout — макс. время от EventConnect до admit; просроченные peer'ы отключаются.
	AdmitTimeout time.Duration
}

func DefaultEnetConfig() EnetConfig {
	return EnetConfig{
		ListenPort:            7777,
		PeerLimit:             64,
		ChannelLimit:          2,
		ServiceTimeoutMs:      10,
		MaxIncomingFrameBytes: adaptertransport.DefaultMaxIncomingFrameBytes,
		EnetIncomingQueueSize: adaptertransport.DefaultEnetIncomingQueueSize,
		AdmitTimeout:          DefaultAdmitTimeout,
	}
}

// Validate проверяет допустимость значений (config.mdc: валидация у модуля).
func (c EnetConfig) Validate() error {
	if c.ListenPort == 0 {
		return fmt.Errorf("enet config: listen port must be > 0")
	}
	if c.PeerLimit == 0 || c.PeerLimit > 4095 {
		return fmt.Errorf("enet config: peer limit must be in range 1..4095")
	}
	if c.ChannelLimit == 0 || c.ChannelLimit > 255 {
		return fmt.Errorf("enet config: channel limit must be in range 1..255")
	}
	if c.ServiceTimeoutMs <= 0 {
		return fmt.Errorf("enet config: service timeout must be > 0")
	}
	if c.MaxIncomingFrameBytes < 0 {
		return fmt.Errorf("enet config: max incoming frame bytes must be >= 0")
	}
	if c.EnetIncomingQueueSize < 0 {
		return fmt.Errorf("enet config: enet incoming queue size must be >= 0")
	}
	if c.AdmitTimeout <= 0 {
		return fmt.Errorf("enet config: admit timeout must be > 0")
	}
	return nil
}
