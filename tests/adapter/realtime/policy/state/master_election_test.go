package state_test

import (
	"slices"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	testpeer "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/peer"
)

func TestOnLeave_MasterFailover_PicksLowestInitializedPing(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(20), 8)
	master := testpeer.New(1, "master", testpeer.WithPing(10))
	mid := testpeer.New(2, "mid", testpeer.WithPing(50))
	best := testpeer.New(3, "best", testpeer.WithPing(20))
	mustJoin(t, h.room, master, mid, best)
	shadow := mustJoinShadow(t, h)
	mid.Clear()
	best.Clear()

	if err := h.room.Leave(master); err != nil {
		t.Fatalf("Leave master: %v", err)
	}
	assertWeakInvariants(t, shadow.view)

	assertPeerIsMasterInPatch(t, h, mid, domain.PeerID(3))
	assertPeerIsMasterInPatch(t, h, best, domain.PeerID(3))

	settleTick(t, h, shadow.view)
	resyncShadow(t, h)
	assertStrongInvariants(t, shadow.view, h.room)
}

func TestOnLeave_MasterFailover_EqualPingPicksMinPeerID(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(21), 8)
	master := testpeer.New(1, "master", testpeer.WithPing(5))
	peerA := testpeer.New(4, "a", testpeer.WithPing(30))
	peerB := testpeer.New(2, "b", testpeer.WithPing(30))
	mustJoin(t, h.room, master, peerA, peerB)
	shadow := mustJoinShadow(t, h)
	peerA.Clear()
	peerB.Clear()

	if err := h.room.Leave(master); err != nil {
		t.Fatalf("Leave master: %v", err)
	}
	assertWeakInvariants(t, shadow.view)

	assertPeerIsMasterInPatch(t, h, peerA, domain.PeerID(2))
	assertPeerIsMasterInPatch(t, h, peerB, domain.PeerID(2))

	settleTick(t, h, shadow.view)
	resyncShadow(t, h)
	assertStrongInvariants(t, shadow.view, h.room)
}

func TestOnLeave_MasterFailover_AllPingsUnknown_FallsBackToMinPeerID(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(22), 8)
	master := testpeer.New(1, "master", testpeer.WithPing(pingUnknown))
	peerHigh := testpeer.New(5, "high", testpeer.WithPing(pingUnknown))
	peerLow := testpeer.New(2, "low", testpeer.WithPing(pingUnknown))
	mustJoin(t, h.room, master, peerHigh, peerLow)
	shadow := mustJoinShadow(t, h)
	peerHigh.Clear()
	peerLow.Clear()

	if err := h.room.Leave(master); err != nil {
		t.Fatalf("Leave master: %v", err)
	}
	assertWeakInvariants(t, shadow.view)

	assertPeerIsMasterInPatch(t, h, peerHigh, domain.PeerID(2))
	assertPeerIsMasterInPatch(t, h, peerLow, domain.PeerID(2))

	settleTick(t, h, shadow.view)
	resyncShadow(t, h)
	assertStrongInvariants(t, shadow.view, h.room)
}

func TestOnLeave_MasterFailover_MixedPingsIgnoresUnknown(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(23), 8)
	master := testpeer.New(1, "master", testpeer.WithPing(1))
	unknown := testpeer.New(2, "unknown", testpeer.WithPing(pingUnknown))
	initialized := testpeer.New(3, "init", testpeer.WithPing(100))
	mustJoin(t, h.room, master, unknown, initialized)
	shadow := mustJoinShadow(t, h)
	unknown.Clear()
	initialized.Clear()

	if err := h.room.Leave(master); err != nil {
		t.Fatalf("Leave master: %v", err)
	}
	assertWeakInvariants(t, shadow.view)

	// peer 3 has единственный инициализированный ping, хотя ID больше
	assertPeerIsMasterInPatch(t, h, unknown, domain.PeerID(3))
	assertPeerIsMasterInPatch(t, h, initialized, domain.PeerID(3))

	settleTick(t, h, shadow.view)
	resyncShadow(t, h)
	assertStrongInvariants(t, shadow.view, h.room)
}

func TestOnLeave_NonMaster_KeepsCurrentMaster(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(24), 8)
	master := testpeer.New(1, "master", testpeer.WithPing(10))
	client := testpeer.New(2, "client", testpeer.WithPing(5))
	mustJoin(t, h.room, master, client)
	shadow := mustJoinShadow(t, h)
	master.Clear()
	client.Clear()

	if err := h.room.Leave(client); err != nil {
		t.Fatalf("Leave client: %v", err)
	}
	assertWeakInvariants(t, shadow.view)

	patches := master.EventsByOpcode(state.OpCodePatchState)
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

	settleTick(t, h, shadow.view)
	resyncShadow(t, h)
	assertStrongInvariants(t, shadow.view, h.room)
}

func TestOnLeave_RemovesPeerInputFromRoomState(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(25), 4)
	master := testpeer.New(1, "master")
	client := testpeer.New(2, "client")
	mustJoin(t, h.room, master, client)
	shadow := mustJoinShadow(t, h)
	master.Clear()
	client.Clear()

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
	master.Clear()

	if err := h.room.Leave(client); err != nil {
		t.Fatalf("Leave client: %v", err)
	}

	patches := master.EventsByOpcode(state.OpCodePatchState)
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
	assertWeakInvariants(t, shadow.view)

	settleTick(t, h, shadow.view)
	resyncShadow(t, h)
	assertStrongInvariants(t, shadow.view, h.room)
}

func assertPeerIsMasterInPatch(t *testing.T, h *harness, peer *testpeer.Peer, wantMaster domain.PeerID) {
	t.Helper()

	patches := peer.EventsByOpcode(state.OpCodePatchState)
	if len(patches) != 1 {
		t.Fatalf("peer %d patches = %d, want 1", peer.GetID(), len(patches))
	}
	patch, err := h.roomCodec.DecodePatch(patches[0].Frame.Payload)
	if err != nil {
		t.Fatalf("DecodePatch: %v", err)
	}
	if updated, ok := patch.Peers.Updated[wantMaster]; ok && updated.IsMaster != nil && *updated.IsMaster {
		return
	}
	t.Fatalf("peer %d patch peers.updated = %#v, want master %d", peer.GetID(), patch.Peers.Updated, wantMaster)
}
