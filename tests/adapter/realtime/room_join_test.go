package realtime_test

import (
	"context"
	"errors"
	"testing"
	"time"

	adapterrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/stdlib"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	portpolicy "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime/policy"
)

func TestJoinCallsSendFromOnJoinWithoutDeadlock(t *testing.T) {
	t.Parallel()

	logger := stdlib.New("test")
	policy := &sendOnJoinPolicy{}
	room := adapterrealtime.NewRoom(context.Background(), domain.RoomID(1), policy, logger)
	policy.room = room

	if err := room.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = room.Stop() })

	peer := &stubPeer{id: domain.PeerID(10)}

	done := make(chan error, 1)
	go func() { done <- room.Join(peer) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Join: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Join blocked >2s (possible deadlock under handler mutex)")
	}

	if !room.HasPeer(peer.id) {
		t.Fatal("peer not in room after Join")
	}
}

func TestJoinRollbackOnHandlerError(t *testing.T) {
	t.Parallel()

	logger := stdlib.New("test")
	policy := &failOnJoinPolicy{err: domain.ErrJoinDenied}
	room := adapterrealtime.NewRoom(context.Background(), domain.RoomID(1), policy, logger)

	if err := room.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = room.Stop() })

	peer := &stubPeer{id: domain.PeerID(11)}
	err := room.Join(peer)
	if !errors.Is(err, domain.ErrJoinDenied) {
		t.Fatalf("Join err = %v", err)
	}
	if room.HasPeer(peer.id) {
		t.Fatal("peer must be removed from room after failed OnJoin")
	}
}

type sendOnJoinPolicy struct {
	room realtime.Room
}

func (p *sendOnJoinPolicy) OnStart(room realtime.Room) error {
	p.room = room
	return nil
}
func (p *sendOnJoinPolicy) OnStop(realtime.Room) error  { return nil }
func (p *sendOnJoinPolicy) OnLeave(realtime.Peer) error { return nil }
func (p *sendOnJoinPolicy) OnMessage(events.RoomEvent) error {
	return nil
}

func (p *sendOnJoinPolicy) OnJoin(peer realtime.Peer) error {
	return p.room.Send(events.PeerEvent{
		ExcludePeerID: peer.GetID(),
		Frame:         domain.Frame{OpCode: 0x01},
	})
}

type failOnJoinPolicy struct {
	err error
}

func (p *failOnJoinPolicy) OnStart(realtime.Room) error { return nil }
func (p *failOnJoinPolicy) OnStop(realtime.Room) error  { return nil }
func (p *failOnJoinPolicy) OnLeave(realtime.Peer) error { return nil }
func (p *failOnJoinPolicy) OnMessage(events.RoomEvent) error {
	return nil
}
func (p *failOnJoinPolicy) OnJoin(realtime.Peer) error { return p.err }

type stubPeer struct {
	id domain.PeerID
}

func (p *stubPeer) GetID() domain.PeerID    { return p.id }
func (p *stubPeer) GetNickName() string     { return "" }
func (p *stubPeer) GetPing() int64          { return 0 }
func (p *stubPeer) Start() error            { return nil }
func (p *stubPeer) Stop() error             { return nil }
func (p *stubPeer) Deliver(events.PeerEvent) error {
	return nil
}

var _ realtime.Peer = (*stubPeer)(nil)
var _ portpolicy.RoomPolicy = (*sendOnJoinPolicy)(nil)
