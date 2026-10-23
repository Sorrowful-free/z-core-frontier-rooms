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

func TestOnJoin_FailedJoinBroadcast_LeavesNoPhantomPeer(t *testing.T) {
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
	err := h.room.Join(joiner)
	if !errors.Is(err, domain.ErrJoinDenied) {
		t.Fatalf("Join err = %v, want ErrJoinDenied", err)
	}
	if h.room.HasPeer(joiner.GetID()) {
		t.Fatal("failed joiner must be rolled back from room peers")
	}

	flaky.SetDeliverErr(nil)
	if err := h.policy.OnTickPatchState(); err != nil {
		t.Fatalf("OnTickPatchState after failed join: %v (phantom peer leaked into state)", err)
	}

	master.Clear()
	if err := h.policy.OnTickFullState(); err != nil {
		t.Fatalf("OnTickFullState after failed join: %v", err)
	}
	got := decodeOnlyFullState(t, h, master)
	if len(got.Peers) != 2 {
		t.Fatalf("peers = %#v, want {1,2} (phantom peer leaked)", got.Peers)
	}
	if _, ok := got.Peers[domain.PeerID(3)]; ok {
		t.Fatal("phantom joiner present in authoritative state")
	}
	if !got.Peers[domain.PeerID(1)].IsMaster {
		t.Fatal("master flag must stay on peer 1")
	}

	joiner.Clear()
	if err := h.room.Join(joiner); err != nil {
		t.Fatalf("retry Join: %v", err)
	}
	retry := decodeOnlyFullState(t, h, joiner)
	if len(retry.Peers) != 3 {
		t.Fatalf("peers after retry = %#v, want {1,2,3}", retry.Peers)
	}
	if !retry.Peers[domain.PeerID(1)].IsMaster {
		t.Fatal("master flag must stay on peer 1 after retry")
	}
	if retry.Peers[domain.PeerID(3)].IsMaster {
		t.Fatal("retried joiner must not be master")
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
