package state_test

import (
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

func TestPeerState_MakePatch_apply_roundTrip(t *testing.T) {
	t.Parallel()

	cur := &state.PeerState{ID: domain.PeerID(1), NickName: "a", Ping: 10}
	next := &state.PeerState{ID: domain.PeerID(1), NickName: "b", Ping: 10}

	patch, err := cur.MakePatch(next)
	if err != nil {
		t.Fatalf("MakePatch: %v", err)
	}
	if patch == nil {
		t.Fatal("expected patch")
	}

	if err := cur.ApplyPatch(*patch); err != nil {
		t.Fatalf("ApplyPatch: %v", err)
	}
	if !cur.Equals(next) {
		t.Fatalf("peer after apply = %#v, want %#v", cur, next)
	}
}

func TestRoomState_MakePatch_capacityOnly(t *testing.T) {
	t.Parallel()

	cur := state.NewRoomState(domain.RoomID(1), 4, "secret")
	next := state.NewRoomState(domain.RoomID(1), 8, "secret")

	patch, err := cur.MakePatch(next)
	if err != nil {
		t.Fatalf("MakePatch: %v", err)
	}
	if patch == nil {
		t.Fatal("expected patch")
	}
	if patch.Capacity == nil || *patch.Capacity != 8 {
		t.Fatalf("Capacity patch = %v, want 8", patch.Capacity)
	}
	if len(patch.Peers.Added)+len(patch.Peers.Updated)+len(patch.Peers.Removed) != 0 {
		t.Fatalf("unexpected peer map changes: %#v", patch.Peers)
	}
	if len(patch.Entities.Added)+len(patch.Entities.Updated)+len(patch.Entities.Removed) != 0 {
		t.Fatalf("unexpected entity map changes: %#v", patch.Entities)
	}
}

func TestRoomState_addPeer_roundTrip(t *testing.T) {
	t.Parallel()

	cur := state.NewRoomState(domain.RoomID(1), 4, "")
	next := state.NewRoomState(domain.RoomID(1), 4, "")
	next.Peers.Items[domain.PeerID(2)] = state.PeerState{ID: domain.PeerID(2), NickName: "p2", Ping: 5}

	patch, err := cur.MakePatch(next)
	if err != nil {
		t.Fatalf("MakePatch: %v", err)
	}
	if patch == nil {
		t.Fatal("expected patch")
	}
	if len(patch.Peers.Added) != 1 {
		t.Fatalf("Peers.Added = %#v, want one peer", patch.Peers.Added)
	}

	if err := cur.ApplyPatch(*patch); err != nil {
		t.Fatalf("ApplyPatch: %v", err)
	}

	if _, ok := cur.Peers.Items[domain.PeerID(2)]; !ok {
		t.Fatal("peer 2 not in room after apply")
	}
	got := cur.Peers.Items[domain.PeerID(2)]
	if !got.Equals(&state.PeerState{ID: domain.PeerID(2), NickName: "p2", Ping: 5}) {
		t.Fatalf("peer = %#v", got)
	}
}

func TestRoomState_ApplyPatch_capacity(t *testing.T) {
	t.Parallel()

	cur := state.NewRoomState(domain.RoomID(1), 4, "x")
	cap8 := 8
	patch := state.RoomStatePatch{
		ID:       domain.RoomID(1),
		Capacity: &cap8,
	}

	if err := cur.ApplyPatch(patch); err != nil {
		t.Fatalf("ApplyPatch: %v", err)
	}
	if cur.Capacity != 8 {
		t.Fatalf("Capacity = %d, want 8", cur.Capacity)
	}
}
