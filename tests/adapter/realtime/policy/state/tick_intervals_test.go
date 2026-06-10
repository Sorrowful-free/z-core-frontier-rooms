package state_test

import (
	"testing"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/stdlib"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/codec"
	statepolicy "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/policy/state"
)

func TestNewStateRoomPolicy_ZeroIntervalsUseDefaults(t *testing.T) {
	t.Parallel()

	logger := stdlib.New("tick-intervals")
	policy := statepolicy.NewStateRoomPolicy(
		logger,
		codec.NewRoomStateCodec(&logger),
		codec.NewEntitiesStateCodec(),
		codec.NewInputStateCodec(),
		&codec.RpcStateCodec{},
		0,
		0,
	)

	full, patch := policy.TickIntervals()
	if full != statepolicy.DefaultFullStateInterval {
		t.Fatalf("full interval = %v, want %v", full, statepolicy.DefaultFullStateInterval)
	}
	if patch != statepolicy.DefaultPatchStateInterval {
		t.Fatalf("patch interval = %v, want %v", patch, statepolicy.DefaultPatchStateInterval)
	}
}

func TestNewStateRoomPolicy_CustomIntervalsPreserved(t *testing.T) {
	t.Parallel()

	logger := stdlib.New("tick-intervals")
	const (
		full  = 7 * time.Second
		patch = 33 * time.Millisecond
	)

	policy := statepolicy.NewStateRoomPolicy(
		logger,
		codec.NewRoomStateCodec(&logger),
		codec.NewEntitiesStateCodec(),
		codec.NewInputStateCodec(),
		&codec.RpcStateCodec{},
		full,
		patch,
	)

	gotFull, gotPatch := policy.TickIntervals()
	if gotFull != full || gotPatch != patch {
		t.Fatalf("intervals = (%v, %v), want (%v, %v)", gotFull, gotPatch, full, patch)
	}
}
