package realtime_test

import (
	"context"
	"testing"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/stdlib"
	adapterrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/codec"
	statepolicy "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/policy/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	portpolicy "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime/policy"
	testpeer "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/peer"
)

func TestDeliver_UnknownOpcode_NotifiesSender(t *testing.T) {
	t.Parallel()

	logger := stdlib.New("test")
	policy := statepolicy.NewStateRoomPolicy(
		logger,
		codec.NewRoomStateCodec(),
		codec.NewEntitiesStateCodec(),
		codec.NewInputStateCodec(),
		codec.NewRpcStateCodec(),
		time.Hour,
		time.Hour,
	)
	room := adapterrealtime.NewRoom(context.Background(), domain.RoomID(2), policy, 8, nil, 0, logger)

	if err := room.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = room.Stop() })

	peer := testpeer.New(domain.PeerID(10), "")
	if err := room.Join(peer); err != nil {
		t.Fatalf("Join: %v", err)
	}
	peer.Clear()

	if err := room.Deliver(events.RoomEvent{
		PeerID: peer.GetID(),
		Frame:  domain.Frame{OpCode: domain.OpCode(0x99)},
	}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	waitForPeerDelivery(t, peer, 1)

	got := peer.Snapshot()
	if got[0].Frame.OpCode != domain.OpInRoomUnknownOpcode {
		t.Fatalf("opcode = %#x, want OpInRoomUnknownOpcode", got[0].Frame.OpCode)
	}
}

func TestDeliver_NotifiesSenderOnPolicyError(t *testing.T) {
	t.Parallel()

	logger := stdlib.New("test")
	policy := &rejectMessagePolicy{err: domain.ErrNotMaster}
	room := adapterrealtime.NewRoom(context.Background(), domain.RoomID(1), policy, 8, nil, 0, logger)

	if err := room.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = room.Stop() })

	peer := testpeer.New(domain.PeerID(10), "")
	if err := room.Join(peer); err != nil {
		t.Fatalf("Join: %v", err)
	}
	peer.Clear()

	if err := room.Deliver(events.RoomEvent{
		PeerID: peer.GetID(),
		Frame:  domain.Frame{OpCode: 0x01},
	}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	waitForPeerDelivery(t, peer, 1)

	got := peer.Snapshot()
	if got[0].Frame.OpCode != domain.OpInRoomNotMaster {
		t.Fatalf("opcode = %#x, want OpInRoomNotMaster", got[0].Frame.OpCode)
	}
	if len(got[0].Frame.Payload) != 0 {
		t.Fatalf("payload = %#v, want empty", got[0].Frame.Payload)
	}
}

type rejectMessagePolicy struct {
	err error
}

func (p *rejectMessagePolicy) OnStart(realtime.Room) error { return nil }
func (p *rejectMessagePolicy) OnStop(realtime.Room) error  { return nil }
func (p *rejectMessagePolicy) OnJoin(realtime.Peer) error  { return nil }
func (p *rejectMessagePolicy) OnLeave(realtime.Peer) error {
	return nil
}
func (p *rejectMessagePolicy) OnMessage(events.RoomEvent) error { return p.err }
func (p *rejectMessagePolicy) TickIntervals() (time.Duration, time.Duration) {
	return 0, 0
}
func (p *rejectMessagePolicy) OnTickFullState() error  { return nil }
func (p *rejectMessagePolicy) OnTickPatchState() error { return nil }

var _ portpolicy.RoomPolicy = (*rejectMessagePolicy)(nil)

func waitForPeerDelivery(t *testing.T, peer *testpeer.Peer, want int) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for len(peer.Snapshot()) < want && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := len(peer.Snapshot()); got != want {
		t.Fatalf("delivered = %d, want %d", got, want)
	}
}
