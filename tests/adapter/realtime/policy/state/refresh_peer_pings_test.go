package state_test

import (
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	testpeer "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/peer"
)

// Два peer выходят из room при сломанной доставке patch survivor'у —
// их записи остаются в state.Peers (stale). Один patch-тик обязан
// вычистить оба и переназначить master на оставшийся peer.
func TestOnTickPatchState_PrunesAllStalePeersAndReelectsMaster(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(60), 4)
	master := testpeer.New(1, "master")
	member := testpeer.New(2, "member")
	survivor := testpeer.New(3, "survivor")
	mustJoin(t, h.room, master, member)
	if err := h.room.Join(survivor); err != nil {
		t.Fatalf("Join survivor: %v", err)
	}

	survivor.SetDeliverErr(errDeliverFailed)

	if err := h.room.Leave(master); err == nil {
		t.Fatal("Leave master: want error from failed leave patch broadcast")
	}
	if err := h.room.Leave(member); err == nil {
		t.Fatal("Leave member: want error from failed leave patch broadcast")
	}
	if h.room.HasPeer(master.GetID()) || h.room.HasPeer(member.GetID()) {
		t.Fatal("left peers must be removed from room even when OnLeave fails")
	}

	survivor.SetDeliverErr(nil)
	survivor.Clear()

	if err := h.policy.OnTickPatchState(); err != nil {
		t.Fatalf("OnTickPatchState: %v", err)
	}

	patches := survivor.EventsByOpcode(state.OpCodePatchState)
	if len(patches) != 1 {
		t.Fatalf("survivor patches = %d, want 1", len(patches))
	}
	patch, err := h.roomCodec.DecodePatch(patches[0].Frame.Payload)
	if err != nil {
		t.Fatalf("DecodePatch: %v", err)
	}
	if len(patch.Peers.Removed) != 2 {
		t.Fatalf("peers.removed = %#v, want both stale peers {1,2} in one tick", patch.Peers.Removed)
	}
	updated, ok := patch.Peers.Updated[survivor.GetID()]
	if !ok || updated.IsMaster == nil || !*updated.IsMaster {
		t.Fatalf("peers.updated[%d] = %#v ok=%v, want IsMaster=true", survivor.GetID(), updated, ok)
	}

	survivor.Clear()
	if err := h.policy.OnTickFullState(); err != nil {
		t.Fatalf("OnTickFullState: %v", err)
	}
	full := survivor.EventsByOpcode(state.OpCodeFullState)
	if len(full) != 1 {
		t.Fatalf("survivor full states = %d, want 1", len(full))
	}
	got, err := h.roomCodec.Decode(full[0].Frame.Payload)
	if err != nil {
		t.Fatalf("Decode full state: %v", err)
	}
	if len(got.Peers) != 1 {
		t.Fatalf("state peers = %#v, want only survivor after stale prune", got.Peers)
	}
	if !got.Peers[survivor.GetID()].IsMaster {
		t.Fatal("survivor must be master after stale master pruned")
	}
}
