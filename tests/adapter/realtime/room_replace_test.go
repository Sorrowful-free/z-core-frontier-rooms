package realtime_test

import (
	"context"
	"sync"
	"testing"
	"time"

	adapterrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/stdlib"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/codec"
	statepolicy "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/policy/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

func TestReplace_SendsFullStateToNewPeerNotOld(t *testing.T) {
	t.Parallel()

	logger := stdlib.New("replace-test")
	policy := statepolicy.NewStateRoomPolicy(
		logger,
		codec.NewRoomStateCodec(&logger),
		codec.NewEntitiesStateCodec(),
		codec.NewInputStateCodec(),
		&codec.RpcStateCodec{},
		time.Hour,
		time.Hour,
	)
	room := adapterrealtime.NewRoom(context.Background(), domain.RoomID(30), policy, 4, logger)

	if err := room.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = room.Stop() })

	const peerID = domain.PeerID(7)
	oldPeer := newReplaceCapturePeer(peerID, "old")
	if err := room.Join(oldPeer); err != nil {
		t.Fatalf("Join old: %v", err)
	}
	oldPeer.clear()

	newPeer := newReplaceCapturePeer(peerID, "new")
	if err := room.Replace(newPeer); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	newFull := filterReplaceOpcode(newPeer.snapshot(), state.OpCodeFullState)
	if len(newFull) != 1 {
		t.Fatalf("new peer full states = %d, want 1", len(newFull))
	}
	if len(filterReplaceOpcode(oldPeer.snapshot(), state.OpCodeFullState)) != 0 {
		t.Fatal("old peer must not receive full state on replace")
	}
}

func TestReplace_SecondPeerRoom_NewGetsFullState(t *testing.T) {
	t.Parallel()

	logger := stdlib.New("replace-test")
	policy := statepolicy.NewStateRoomPolicy(
		logger,
		codec.NewRoomStateCodec(&logger),
		codec.NewEntitiesStateCodec(),
		codec.NewInputStateCodec(),
		&codec.RpcStateCodec{},
		time.Hour,
		time.Hour,
	)
	room := adapterrealtime.NewRoom(context.Background(), domain.RoomID(31), policy, 4, logger)

	if err := room.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = room.Stop() })

	other := newReplaceCapturePeer(2, "other")
	oldPeer := newReplaceCapturePeer(1, "old")
	if err := room.Join(oldPeer); err != nil {
		t.Fatalf("Join old: %v", err)
	}
	if err := room.Join(other); err != nil {
		t.Fatalf("Join other: %v", err)
	}
	oldPeer.clear()
	other.clear()

	newPeer := newReplaceCapturePeer(1, "new")
	if err := room.Replace(newPeer); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	if len(filterReplaceOpcode(newPeer.snapshot(), state.OpCodeFullState)) != 1 {
		t.Fatal("reconnected peer should receive full state")
	}
	if len(filterReplaceOpcode(oldPeer.snapshot(), state.OpCodeFullState)) != 0 {
		t.Fatal("old connection must not receive replace full state")
	}
}

type replaceCapturePeer struct {
	id        domain.PeerID
	nick      string
	mu        sync.Mutex
	delivered []events.PeerEvent
}

func newReplaceCapturePeer(id domain.PeerID, nick string) *replaceCapturePeer {
	return &replaceCapturePeer{id: id, nick: nick}
}

func (p *replaceCapturePeer) GetID() domain.PeerID { return p.id }
func (p *replaceCapturePeer) GetNickName() string  { return p.nick }
func (p *replaceCapturePeer) Ping() int64          { return 0 }
func (p *replaceCapturePeer) Start() error         { return nil }
func (p *replaceCapturePeer) Stop() error          { return nil }
func (p *replaceCapturePeer) Deliver(ev events.PeerEvent) error {
	p.mu.Lock()
	p.delivered = append(p.delivered, ev)
	p.mu.Unlock()
	return nil
}

func (p *replaceCapturePeer) snapshot() []events.PeerEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]events.PeerEvent, len(p.delivered))
	copy(out, p.delivered)
	return out
}

func (p *replaceCapturePeer) clear() {
	p.mu.Lock()
	p.delivered = nil
	p.mu.Unlock()
}

func filterReplaceOpcode(peerEvents []events.PeerEvent, opcode domain.OpCode) []events.PeerEvent {
	var out []events.PeerEvent
	for _, ev := range peerEvents {
		if ev.Frame.OpCode == opcode {
			out = append(out, ev)
		}
	}
	return out
}

var _ realtime.Peer = (*replaceCapturePeer)(nil)
