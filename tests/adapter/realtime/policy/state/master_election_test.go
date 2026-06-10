package state_test

import (
	"slices"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

func TestOnLeave_MasterFailover_PicksLowestInitializedPing(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(20), 8)
	master := newCapturePeerWithPing(1, "master", 10)
	mid := newCapturePeerWithPing(2, "mid", 50)
	best := newCapturePeerWithPing(3, "best", 20)
	mustJoin(t, h.room, master, mid, best)
	mid.clear()
	best.clear()

	if err := h.room.Leave(master); err != nil {
		t.Fatalf("Leave master: %v", err)
	}

	assertPeerIsMasterInPatch(t, h, mid, domain.PeerID(3))
	assertPeerIsMasterInPatch(t, h, best, domain.PeerID(3))
}

func TestOnLeave_MasterFailover_EqualPingPicksMinPeerID(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(21), 8)
	master := newCapturePeerWithPing(1, "master", 5)
	peerA := newCapturePeerWithPing(4, "a", 30)
	peerB := newCapturePeerWithPing(2, "b", 30)
	mustJoin(t, h.room, master, peerA, peerB)
	peerA.clear()
	peerB.clear()

	if err := h.room.Leave(master); err != nil {
		t.Fatalf("Leave master: %v", err)
	}

	assertPeerIsMasterInPatch(t, h, peerA, domain.PeerID(2))
	assertPeerIsMasterInPatch(t, h, peerB, domain.PeerID(2))
}

func TestOnLeave_MasterFailover_AllPingsUnknown_FallsBackToMinPeerID(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(22), 8)
	master := newCapturePeerWithPing(1, "master", pingUnknown)
	peerHigh := newCapturePeerWithPing(5, "high", pingUnknown)
	peerLow := newCapturePeerWithPing(2, "low", pingUnknown)
	mustJoin(t, h.room, master, peerHigh, peerLow)
	peerHigh.clear()
	peerLow.clear()

	if err := h.room.Leave(master); err != nil {
		t.Fatalf("Leave master: %v", err)
	}

	assertPeerIsMasterInPatch(t, h, peerHigh, domain.PeerID(2))
	assertPeerIsMasterInPatch(t, h, peerLow, domain.PeerID(2))
}

func TestOnLeave_MasterFailover_MixedPingsIgnoresUnknown(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(23), 8)
	master := newCapturePeerWithPing(1, "master", 1)
	unknown := newCapturePeerWithPing(2, "unknown", pingUnknown)
	initialized := newCapturePeerWithPing(3, "init", 100)
	mustJoin(t, h.room, master, unknown, initialized)
	unknown.clear()
	initialized.clear()

	if err := h.room.Leave(master); err != nil {
		t.Fatalf("Leave master: %v", err)
	}

	// peer 3 has единственный инициализированный ping, хотя ID больше
	assertPeerIsMasterInPatch(t, h, unknown, domain.PeerID(3))
	assertPeerIsMasterInPatch(t, h, initialized, domain.PeerID(3))
}

func TestOnLeave_NonMaster_KeepsCurrentMaster(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(24), 8)
	master := newCapturePeerWithPing(1, "master", 10)
	client := newCapturePeerWithPing(2, "client", 5)
	mustJoin(t, h.room, master, client)
	master.clear()
	client.clear()

	if err := h.room.Leave(client); err != nil {
		t.Fatalf("Leave client: %v", err)
	}

	patches := filterOpcode(master.snapshot(), state.OpCodePatchState)
	if len(patches) != 1 {
		t.Fatalf("master patches = %d, want 1", len(patches))
	}
	patch, err := h.roomCodec.DecodePatch(patches[0].Frame.Payload)
	if err != nil {
		t.Fatalf("DecodePatch: %v", err)
	}
	if !slices.Contains(patch.Peers.Removed, domain.PeerID(2)) {
		t.Fatalf("patch peers.removed = %#v", patch.Peers.Removed)
	}
	for id, upd := range patch.Peers.Updated {
		if upd.IsMaster != nil && *upd.IsMaster {
			t.Fatalf("unexpected new master %d in patch", id)
		}
	}
}

func TestOnLeave_RemovesPeerInputFromRoomState(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(25), 4)
	master := newCapturePeer(1, "master")
	client := newCapturePeer(2, "client")
	mustJoin(t, h.room, master, client)
	master.clear()
	client.clear()

	inputPatch := &state.InputStatePatch{
		Values: *state.NewMapStatePatch[state.ValueId, state.ValueState, state.ValueState](),
	}
	inputPatch.Values.Added[state.ValueId(1)] = state.ValueState([]byte{0xCD})
	payload, err := h.inputCodec.EncodePatch(inputPatch)
	if err != nil {
		t.Fatalf("EncodePatch input: %v", err)
	}
	if err := h.policy.OnMessage(events.RoomEvent{
		PeerID: domain.PeerID(2),
		Frame:  domain.Frame{OpCode: state.OpCodePatchInput, Payload: payload},
	}); err != nil {
		t.Fatalf("OnMessage patch input: %v", err)
	}
	master.clear()

	if err := h.room.Leave(client); err != nil {
		t.Fatalf("Leave client: %v", err)
	}

	patches := filterOpcode(master.snapshot(), state.OpCodePatchState)
	if len(patches) != 1 {
		t.Fatalf("master patches = %d, want 1", len(patches))
	}
	patch, err := h.roomCodec.DecodePatch(patches[0].Frame.Payload)
	if err != nil {
		t.Fatalf("DecodePatch: %v", err)
	}
	if !slices.Contains(patch.Peers.Removed, domain.PeerID(2)) {
		t.Fatalf("patch peers.removed = %#v", patch.Peers.Removed)
	}
	if !slices.Contains(patch.Inputs.Removed, domain.PeerID(2)) {
		t.Fatalf("patch inputs.removed = %#v", patch.Inputs.Removed)
	}
}

func assertPeerIsMasterInPatch(t *testing.T, h *harness, peer *capturePeer, wantMaster domain.PeerID) {
	t.Helper()

	patches := filterOpcode(peer.snapshot(), state.OpCodePatchState)
	if len(patches) != 1 {
		t.Fatalf("peer %d patches = %d, want 1", peer.id, len(patches))
	}
	patch, err := h.roomCodec.DecodePatch(patches[0].Frame.Payload)
	if err != nil {
		t.Fatalf("DecodePatch: %v", err)
	}
	if updated, ok := patch.Peers.Updated[wantMaster]; ok && updated.IsMaster != nil && *updated.IsMaster {
		return
	}
	t.Fatalf("peer %d patch peers.updated = %#v, want master %d", peer.id, patch.Peers.Updated, wantMaster)
}
