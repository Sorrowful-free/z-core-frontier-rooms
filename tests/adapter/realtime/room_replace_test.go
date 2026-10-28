package realtime_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/stdlib"
	adapterrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/codec"
	statepolicy "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/policy/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	portpolicy "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime/policy"
	testpeer "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/peer"
)

func TestReplace_SendsFullStateToNewPeerNotOld(t *testing.T) {
	t.Parallel()

	logger := stdlib.New("replace-test")
	policy := statepolicy.NewStateRoomPolicy(
		logger,
		codec.NewRoomStateCodec(),
		codec.NewEntitiesStateCodec(),
		codec.NewInputStateCodec(),
		codec.NewRpcStateCodec(),
		time.Hour,
		time.Hour,
	)
	room := adapterrealtime.NewRoom(context.Background(), domain.RoomID(30), policy, 4, nil, 0, logger)

	if err := room.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = room.Stop() })

	const peerID = domain.PeerID(7)
	oldPeer := testpeer.New(peerID, "old")
	if err := room.Join(oldPeer); err != nil {
		t.Fatalf("Join old: %v", err)
	}
	oldPeer.Clear()

	newPeer := testpeer.New(peerID, "new")
	if err := room.Replace(newPeer); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	newFull := testpeer.FilterByOpcode(newPeer.Snapshot(), state.OpCodeFullState)
	if len(newFull) != 1 {
		t.Fatalf("new peer full states = %d, want 1", len(newFull))
	}
	if len(testpeer.FilterByOpcode(oldPeer.Snapshot(), state.OpCodeFullState)) != 0 {
		t.Fatal("old peer must not receive full state on replace")
	}
}

func TestReplace_FailedJoinRestoresPeersMapAndPolicyState(t *testing.T) {
	t.Parallel()

	logger := stdlib.New("replace-rollback-test")
	policy := &trackingReplacePolicy{rejectNick: "reject"}
	room := adapterrealtime.NewRoom(context.Background(), domain.RoomID(32), policy, 4, nil, 0, logger)

	if err := room.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = room.Stop() })

	const peerID = domain.PeerID(5)
	oldPeer := testpeer.New(peerID, "keeper")
	if err := room.Join(oldPeer); err != nil {
		t.Fatalf("Join old: %v", err)
	}

	newPeer := testpeer.New(peerID, "reject")
	err := room.Replace(newPeer)
	if !errors.Is(err, domain.ErrReplaceFailed) {
		t.Fatalf("Replace err = %v, want ErrReplaceFailed", err)
	}

	got, getErr := room.GetPeer(peerID)
	if getErr != nil {
		t.Fatalf("GetPeer: %v", getErr)
	}
	if got != oldPeer {
		t.Fatal("room peers map must roll back to old peer after failed replace")
	}
	if policy.inRoom[peerID] != "keeper" {
		t.Fatalf("policy state = %q, want keeper", policy.inRoom[peerID])
	}
	// Regression: old is restored in the slot and must stay alive —
	// stopping old on a successful rollback would kill the live
	// connection.
	if !oldPeer.IsAlive() {
		t.Fatal("old peer must stay alive after successful rollback restore")
	}
}

func TestReplace_SecondPeerRoom_NewGetsFullState(t *testing.T) {
	t.Parallel()

	logger := stdlib.New("replace-test")
	policy := statepolicy.NewStateRoomPolicy(
		logger,
		codec.NewRoomStateCodec(),
		codec.NewEntitiesStateCodec(),
		codec.NewInputStateCodec(),
		codec.NewRpcStateCodec(),
		time.Hour,
		time.Hour,
	)
	room := adapterrealtime.NewRoom(context.Background(), domain.RoomID(31), policy, 4, nil, 0, logger)

	if err := room.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = room.Stop() })

	other := testpeer.New(2, "other")
	oldPeer := testpeer.New(1, "old")
	if err := room.Join(oldPeer); err != nil {
		t.Fatalf("Join old: %v", err)
	}
	if err := room.Join(other); err != nil {
		t.Fatalf("Join other: %v", err)
	}
	oldPeer.Clear()
	other.Clear()

	newPeer := testpeer.New(1, "new")
	if err := room.Replace(newPeer); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	if len(testpeer.FilterByOpcode(newPeer.Snapshot(), state.OpCodeFullState)) != 1 {
		t.Fatal("reconnected peer should receive full state")
	}
	if len(testpeer.FilterByOpcode(oldPeer.Snapshot(), state.OpCodeFullState)) != 0 {
		t.Fatal("old connection must not receive replace full state")
	}
}

var (
	errJoinRejected  = errors.New("join rejected")
	errLeaveRejected = errors.New("leave rejected")
)

type trackingReplacePolicy struct {
	inRoom     map[domain.PeerID]string
	rejectNick string
	// failJoins — reject all OnJoin (the double-failure Replace scenario:
	// both join(new) and the rollback join(old) fail). Written before the
	// dispatch send, read on the room loop — the lifecycle channel keeps
	// the ordering.
	failJoins bool
	// failLeaves — reject all OnLeave (the first-stage leave in Replace
	// fails after the peer has already been removed from r.peers).
	failLeaves bool
}

func (p *trackingReplacePolicy) OnStart(realtime.Room) error {
	p.inRoom = make(map[domain.PeerID]string)
	return nil
}

func (p *trackingReplacePolicy) OnStop(realtime.Room) error { return nil }

func (p *trackingReplacePolicy) OnJoin(peer realtime.Peer) error {
	if p.failJoins || peer.GetNickName() == p.rejectNick {
		return errJoinRejected
	}
	p.inRoom[peer.GetID()] = peer.GetNickName()
	return nil
}

func (p *trackingReplacePolicy) OnLeave(peer realtime.Peer) error {
	if p.failLeaves {
		return errLeaveRejected
	}
	delete(p.inRoom, peer.GetID())
	return nil
}

func (p *trackingReplacePolicy) OnMessage(events.RoomEvent) error { return nil }

func (p *trackingReplacePolicy) TickIntervals() (time.Duration, time.Duration) { return 0, 0 }
func (p *trackingReplacePolicy) OnTickFullState() error                        { return nil }
func (p *trackingReplacePolicy) OnTickPatchState() error                       { return nil }

var _ portpolicy.RoomPolicy = (*trackingReplacePolicy)(nil)

// P20/T8: stale teardown — a leave with the old peer object after Replace
// returns ErrPeerStale and does not touch the new peer.
func TestLeave_AfterReplace_StalePeer(t *testing.T) {
	t.Parallel()

	logger := stdlib.New("stale-leave-test")
	policy := &trackingReplacePolicy{}
	room := adapterrealtime.NewRoom(context.Background(), domain.RoomID(33), policy, 4, nil, 0, logger)

	if err := room.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = room.Stop() })

	const peerID = domain.PeerID(9)
	oldPeer := testpeer.New(peerID, "old")
	if err := room.Join(oldPeer); err != nil {
		t.Fatalf("Join old: %v", err)
	}
	newPeer := testpeer.New(peerID, "new")
	if err := room.Replace(newPeer); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	if err := room.Leave(oldPeer); !errors.Is(err, domain.ErrPeerStale) {
		t.Fatalf("Leave stale err = %v, want ErrPeerStale", err)
	}
	got, getErr := room.GetPeer(peerID)
	if getErr != nil {
		t.Fatalf("GetPeer: %v", getErr)
	}
	if got != newPeer {
		t.Fatal("stale leave must not remove the replaced peer")
	}
	// The current peer object leaves the room normally.
	if err := room.Leave(newPeer); err != nil {
		t.Fatalf("Leave current peer: %v", err)
	}
}

// P20: double Replace failure — join(new) failed and the rollback
// join(old) failed too (a third racing resume of the same reconnect token
// took the slot). The old peer is evicted irreversibly and must be stopped:
// otherwise conn+goroutines live outside r.peers (leak + ghost input).
func TestReplace_DoubleJoinFailureStopsOldPeer(t *testing.T) {
	t.Parallel()

	logger := stdlib.New("replace-double-fail-test")
	policy := &trackingReplacePolicy{}
	room := adapterrealtime.NewRoom(context.Background(), domain.RoomID(34), policy, 4, nil, 0, logger)

	if err := room.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = room.Stop() })

	const peerID = domain.PeerID(11)
	oldPeer := testpeer.New(peerID, "old")
	if err := room.Join(oldPeer); err != nil {
		t.Fatalf("Join old: %v", err)
	}

	// OnJoin now rejects everything: both join(new) and the rollback join(old).
	policy.failJoins = true

	newPeer := testpeer.New(peerID, "new")
	err := room.Replace(newPeer)
	if !errors.Is(err, domain.ErrReplaceFailed) {
		t.Fatalf("Replace err = %v, want ErrReplaceFailed", err)
	}
	if !errors.Is(err, errJoinRejected) {
		t.Fatalf("Replace err = %v, want both join failures joined", err)
	}
	if oldPeer.IsAlive() {
		t.Fatal("old peer must be stopped when replace fails and rollback fails")
	}
	if _, getErr := room.GetPeer(peerID); !errors.Is(getErr, domain.ErrPeerNotFound) {
		t.Fatalf("GetPeer err = %v, want ErrPeerNotFound", getErr)
	}
}

// P20: first-stage Replace failure — leave(old) fails in OnLeave, but the
// peer is already removed from r.peers (leave always evicts, ADR-0004 D4):
// Replace must stop old, otherwise conn+goroutines live outside the room
// (leak + ghost input, the same class as the double-join failure above).
func TestReplace_LeaveDispatchFails_StopsOldPeer(t *testing.T) {
	t.Parallel()

	logger := stdlib.New("replace-leave-fail-test")
	policy := &trackingReplacePolicy{}
	room := adapterrealtime.NewRoom(context.Background(), domain.RoomID(35), policy, 4, nil, 0, logger)

	if err := room.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = room.Stop() })

	const peerID = domain.PeerID(13)
	oldPeer := testpeer.New(peerID, "old")
	if err := room.Join(oldPeer); err != nil {
		t.Fatalf("Join old: %v", err)
	}

	// OnLeave now fails: the first-stage leave in Replace will return an
	// error even though old is already evicted from r.peers.
	policy.failLeaves = true

	newPeer := testpeer.New(peerID, "new")
	err := room.Replace(newPeer)
	if !errors.Is(err, domain.ErrReplaceFailed) {
		t.Fatalf("Replace err = %v, want ErrReplaceFailed", err)
	}
	if !errors.Is(err, errLeaveRejected) {
		t.Fatalf("Replace err = %v, want leave failure joined", err)
	}
	if oldPeer.IsAlive() {
		t.Fatal("old peer must be stopped when leave stage of replace fails")
	}
	if _, getErr := room.GetPeer(peerID); !errors.Is(getErr, domain.ErrPeerNotFound) {
		t.Fatalf("GetPeer err = %v, want ErrPeerNotFound", getErr)
	}
}
