package enet

import (
	"fmt"
	"time"

	adaptertransport "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport"
)

// DefaultAdmitTimeout — deadline EventConnect → admit: peer, не приславший ticket
// за это время, отключается (слот PeerLimit не занят бесконечно).
const DefaultAdmitTimeout = 10 * time.Second

// EnetConfig - параметры ENet host'ов (отдельные UDP-слушатели, не Fiber).
// Файл без build-тегов: конфиг нужен и stub-сборкам без cgo.
//
// ListenPorts - список портов, по одному host'у на порт (P18, шардирование):
// delivery/enet поднимает HostLoop на каждый порт; hostID в claims - индекс+1
// в этом списке. PeerLimit - per-host (лимит структуры libenet).
//
// ServiceTimeoutMs — бюджет одного вызова host.Service на HostLoop'е (P15):
// это верхняя граница latency очереди команд (send/disconnect ждут возврата
// Service). Дефолт 1ms — ~1000 итераций/сек на host, цена пустого poll'а
// минимальна; поднять можно через ENET_SERVICE_TIMEOUT_MS, если цикл на
// пустом хосте станет заметен.
type EnetConfig struct {
	ListenPorts           []uint16
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
		ListenPorts:           []uint16{7777},
		PeerLimit:             64,
		ChannelLimit:          2,
		ServiceTimeoutMs:      1,
		MaxIncomingFrameBytes: adaptertransport.DefaultMaxIncomingFrameBytes,
		EnetIncomingQueueSize: adaptertransport.DefaultEnetIncomingQueueSize,
		AdmitTimeout:          DefaultAdmitTimeout,
	}
}

// Validate проверяет допустимость значений (config.mdc: валидация у модуля).
func (c EnetConfig) Validate() error {
	if len(c.ListenPorts) == 0 {
		return fmt.Errorf("enet config: at least one listen port required")
	}
	seen := make(map[uint16]bool, len(c.ListenPorts))
	for _, port := range c.ListenPorts {
		if port == 0 {
			return fmt.Errorf("enet config: listen port must be > 0")
		}
		if seen[port] {
			return fmt.Errorf("enet config: duplicate listen port %d", port)
		}
		seen[port] = true
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
