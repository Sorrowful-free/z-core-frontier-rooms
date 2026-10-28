package enet

import (
	"fmt"
	"time"

	adaptertransport "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport"
)

// DefaultAdmitTimeout — EventConnect → admit deadline: a peer that has not
// sent a ticket within this time is disconnected (the PeerLimit slot is not
// held forever).
const DefaultAdmitTimeout = 10 * time.Second

// EnetConfig - parameters of ENet hosts (dedicated UDP listeners, not Fiber).
// File without build tags: stub builds without cgo also need the config.
//
// ListenPorts - list of ports, one host per port (P18, sharding):
// delivery/enet brings up a HostLoop on each port; hostID in claims is
// index+1 into this list. PeerLimit is per-host (a libenet structure limit).
//
// ServiceTimeoutMs — budget of a single host.Service call on the HostLoop
// (P15): it is the upper bound of command queue latency (send/disconnect wait
// for Service to return). Default 1ms — ~1000 iterations/sec per host, the
// cost of an empty poll is minimal; it can be raised via
// ENET_SERVICE_TIMEOUT_MS if the loop on an idle host becomes noticeable.
type EnetConfig struct {
	ListenPorts           []uint16
	PeerLimit             uint64
	ChannelLimit          uint64
	ServiceTimeoutMs      uint32
	MaxIncomingFrameBytes int
	EnetIncomingQueueSize int
	// AdmitTimeout — max time from EventConnect to admit; overdue peers are disconnected.
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

// Validate checks that values are admissible (config.mdc: validation lives with the module).
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
