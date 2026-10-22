package enet_test

import (
	"testing"
	"time"

	enetadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/enet"
)

func TestEnetConfig_DefaultValidate(t *testing.T) {
	t.Parallel()

	cfg := enetadapter.DefaultEnetConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("DefaultEnetConfig().Validate() = %v, want nil", err)
	}
	if cfg.AdmitTimeout != enetadapter.DefaultAdmitTimeout {
		t.Fatalf("AdmitTimeout = %v, want %v", cfg.AdmitTimeout, enetadapter.DefaultAdmitTimeout)
	}
}

func TestEnetConfig_Validate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		mutate  func(*enetadapter.EnetConfig)
		wantErr bool
	}{
		{"valid", func(*enetadapter.EnetConfig) {}, false},
		{"listen port zero", func(c *enetadapter.EnetConfig) { c.ListenPort = 0 }, true},
		{"peer limit zero", func(c *enetadapter.EnetConfig) { c.PeerLimit = 0 }, true},
		{"channel limit zero", func(c *enetadapter.EnetConfig) { c.ChannelLimit = 0 }, true},
		{"frame bytes zero ok", func(c *enetadapter.EnetConfig) { c.MaxIncomingFrameBytes = 0 }, false},
		{"frame bytes negative", func(c *enetadapter.EnetConfig) { c.MaxIncomingFrameBytes = -1 }, true},
		{"queue zero ok", func(c *enetadapter.EnetConfig) { c.EnetIncomingQueueSize = 0 }, false},
		{"queue negative", func(c *enetadapter.EnetConfig) { c.EnetIncomingQueueSize = -1 }, true},
		{"admit timeout zero", func(c *enetadapter.EnetConfig) { c.AdmitTimeout = 0 }, true},
		{"admit timeout negative", func(c *enetadapter.EnetConfig) { c.AdmitTimeout = -time.Second }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := enetadapter.DefaultEnetConfig()
			tc.mutate(&cfg)
			err := cfg.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("Validate() = nil, want error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}
