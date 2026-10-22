package transport_test

import (
	"testing"
	"time"

	adaptertransport "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport"
)

func TestTransportConfig_Validate(t *testing.T) {
	t.Parallel()

	valid := adaptertransport.TransportConfig{
		MaxIncomingFrameBytes: adaptertransport.DefaultMaxIncomingFrameBytes,
		RoomIncomingQueueSize: adaptertransport.DefaultRoomIncomingQueueSize,
		PeerOutboundQueueSize: adaptertransport.DefaultPeerOutboundQueueSize,
		EnetIncomingQueueSize: adaptertransport.DefaultEnetIncomingQueueSize,
		PeerIdleTimeout:       adaptertransport.DefaultPeerIdleTimeout,
		PeerIdleSweepInterval: adaptertransport.DefaultPeerIdleSweepInterval,
		WSWriteDeadline:       adaptertransport.DefaultWSWriteDeadline,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid config: %v", err)
	}

	if err := (adaptertransport.TransportConfig{}).Validate(); err != nil {
		t.Fatalf("zero config (all disabled/unbuffered): %v", err)
	}

	tests := []struct {
		name string
		mut  func(*adaptertransport.TransportConfig)
	}{
		{"max frame", func(c *adaptertransport.TransportConfig) { c.MaxIncomingFrameBytes = -1 }},
		{"room queue", func(c *adaptertransport.TransportConfig) { c.RoomIncomingQueueSize = -1 }},
		{"peer queue", func(c *adaptertransport.TransportConfig) { c.PeerOutboundQueueSize = -1 }},
		{"enet queue", func(c *adaptertransport.TransportConfig) { c.EnetIncomingQueueSize = -1 }},
		{"idle timeout", func(c *adaptertransport.TransportConfig) { c.PeerIdleTimeout = -time.Second }},
		{"sweep interval", func(c *adaptertransport.TransportConfig) { c.PeerIdleSweepInterval = -time.Second }},
		{"ws write deadline", func(c *adaptertransport.TransportConfig) { c.WSWriteDeadline = -time.Second }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid
			tt.mut(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatalf("%s: expected error for negative field", tt.name)
			}
		})
	}
}
