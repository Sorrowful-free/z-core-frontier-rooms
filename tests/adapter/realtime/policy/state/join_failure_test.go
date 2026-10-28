package state_test

import (
	"errors"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	testpeer "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/peer"
)

var errDeliverFailed = errors.New("deliver failed")

func TestOnJoin_FailedFullStateSend_LeavesNoPhantomPeer(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(30), 4)

	broken := testpeer.New(1, "broken")
	broken.SetDeliverErr(errDeliverFailed)

	err := h.room.Join(broken)
	if !errors.Is(err, domain.ErrJoinDenied) {
		t.Fatalf("Join err = %v, want ErrJoinDenied", err)
	}
	if h.room.HasPeer(broken.GetID()) {
		t.Fatal("failed peer must be rolled back from room peers")
	}

	if err := h.policy.OnTickPatchState(); err != nil {
		t.Fatalf("OnTickPatchState after failed join: %v (phantom peer leaked into state)", err)
	}

	joiner := testpeer.New(2, "joiner")
	if err := h.room.Join(joiner); err != nil {
		t.Fatalf("retry Join after failed join: %v", err)
	}

	got := decodeOnlyFullState(t, h, joiner)
	if len(got.Peers) != 1 {
		t.Fatalf("peers = %#v, want only joiner (phantom peer leaked)", got.Peers)
	}
	self, ok := got.Peers[domain.PeerID(2)]
	if !ok || !self.IsMaster {
		t.Fatalf("joiner peer state = %#v ok=%v, want master of empty room", self, ok)
	}
}

// P34: join no longer sends a patch to the others — a failed Deliver of a
// bystander cannot break someone else's join. The only send on this path
// is the full state to the joiner itself; a broadcast error will surface
// on the patch tick.
func TestOnJoin_BystanderDeliverFails_JoinStillSucceeds(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(31), 4)
	master := testpeer.New(1, "master")
	flaky := testpeer.New(2, "flaky")
	if err := h.room.Join(master); err != nil {
		t.Fatalf("Join master: %v", err)
	}
	if err := h.room.Join(flaky); err != nil {
		t.Fatalf("Join flaky: %v", err)
	}

	flaky.SetDeliverErr(errDeliverFailed)
	joiner := testpeer.New(3, "joiner")
	if err := h.room.Join(joiner); err != nil {
		t.Fatalf("Join must not depend on bystander deliver: %v", err)
	}

	// The joiner got its full state with all three peers (the write to
	// state is committed — a broken tick does not roll it back).
	got := decodeOnlyFullState(t, h, joiner)
	if len(got.Peers) != 3 {
		t.Fatalf("peers = %#v, want {1,2,3}", got.Peers)
	}
	if !got.Peers[domain.PeerID(1)].IsMaster {
		t.Fatal("master flag must stay on peer 1")
	}

	// The error surfaces on the patch tick via broadcast — but the delta is
	// not lost: prevState commits only after a successful send, and the
	// next tick (after the peer is fixed) delivers it.
	if err := h.policy.OnTickPatchState(); !errors.Is(err, errDeliverFailed) {
		t.Fatalf("OnTickPatchState err = %v, want errDeliverFailed", err)
	}
	flaky.SetDeliverErr(nil)
	flaky.Clear()
	if err := h.policy.OnTickPatchState(); err != nil {
		t.Fatalf("OnTickPatchState after recover: %v", err)
	}
	patches := flaky.EventsByOpcode(state.OpCodePatchState)
	if len(patches) != 1 {
		t.Fatalf("flaky patches = %d, want 1 re-sent tick patch", len(patches))
	}
	patch, err := h.roomCodec.DecodePatch(patches[0].Frame.Payload)
	if err != nil {
		t.Fatalf("DecodePatch: %v", err)
	}
	if _, ok := patch.Peers.Added[joiner.GetID()]; !ok {
		t.Fatalf("re-sent patch peers.added = %#v, want joiner delta preserved", patch.Peers.Added)
	}
}

func decodeOnlyFullState(t *testing.T, h *harness, peer *testpeer.Peer) *state.RoomState {
	t.Helper()

	full := peer.EventsByOpcode(state.OpCodeFullState)
	if len(full) != 1 {
		t.Fatalf("peer %d full states = %d, want 1", peer.GetID(), len(full))
	}
	got, err := h.roomCodec.Decode(full[0].Frame.Payload)
	if err != nil {
		t.Fatalf("Decode full state: %v", err)
	}
	return got
}
