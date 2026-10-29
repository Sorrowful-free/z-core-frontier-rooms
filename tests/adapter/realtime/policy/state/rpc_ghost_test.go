package state_test

import (
	"errors"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	testpeer "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/peer"
)

// P35: onClientRpc applies the same membership check as the input
// handlers — a valid RPC frame from a peer absent from state.Peers (a
// ghost: never joined, or already removed) is rejected with
// ErrPeerNotFound and must not be relayed to room members.
func TestOnMessage_RpcFromUnknownPeer_ReturnsErrPeerNotFound(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(53), 4)
	master := testpeer.New(1, "master")
	client := testpeer.New(2, "client")
	mustJoin(t, h.room, master, client)
	shadow := mustJoinShadow(t, h)
	master.Clear()
	client.Clear()

	payload := mustEncodeRPC(t, state.RpcTargetAll, domain.PeerIDInvalid)
	err := h.policy.OnMessage(events.RoomEvent{
		PeerID: domain.PeerID(99),
		Frame:  domain.Frame{OpCode: state.OpCodeRpc, Payload: payload},
	})
	if !errors.Is(err, domain.ErrPeerNotFound) {
		t.Fatalf("rpc err = %v, want ErrPeerNotFound", err)
	}
	if got := testpeer.FilterByOpcode(master.Snapshot(), state.OpCodeRpc); len(got) != 0 {
		t.Fatalf("master received %d ghost rpc frames, want 0", len(got))
	}
	if got := testpeer.FilterByOpcode(client.Snapshot(), state.OpCodeRpc); len(got) != 0 {
		t.Fatalf("client received %d ghost rpc frames, want 0", len(got))
	}

	assertWeakInvariants(t, shadow.view)
	settleTick(t, h, shadow.view)
	resyncShadow(t, h)
	assertStrongInvariants(t, shadow.view, h.room)
}

// The queued-frame ghost: the peer was a member, left, and its RPC frame
// lands after the removal — it must be dropped, not relayed.
func TestOnMessage_RpcFromLeftPeer_ReturnsErrPeerNotFound(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(54), 4)
	master := testpeer.New(1, "master")
	leaver := testpeer.New(2, "leaver")
	mustJoin(t, h.room, master, leaver)

	if err := h.room.Leave(leaver); err != nil {
		t.Fatalf("Leave: %v", err)
	}
	master.Clear()

	payload := mustEncodeRPC(t, state.RpcTargetAll, domain.PeerIDInvalid)
	err := h.policy.OnMessage(events.RoomEvent{
		PeerID: leaver.GetID(),
		Frame:  domain.Frame{OpCode: state.OpCodeRpc, Payload: payload},
	})
	if !errors.Is(err, domain.ErrPeerNotFound) {
		t.Fatalf("rpc err = %v, want ErrPeerNotFound", err)
	}
	if got := testpeer.FilterByOpcode(master.Snapshot(), state.OpCodeRpc); len(got) != 0 {
		t.Fatalf("master received %d ghost rpc frames, want 0", len(got))
	}
}

// Malformed payload still answers ErrInRoomInvalidPayload — the decode
// runs before the membership check, same order as the input handlers.
func TestOnMessage_RpcFromUnknownPeer_MalformedPayloadStillInvalidPayload(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(55), 4)
	master := testpeer.New(1, "master")
	mustJoin(t, h.room, master)

	err := h.policy.OnMessage(events.RoomEvent{
		PeerID: domain.PeerID(99),
		Frame:  domain.Frame{OpCode: state.OpCodeRpc, Payload: []byte{0xFF, 0xFF, 0xFF}},
	})
	if !errors.Is(err, domain.ErrInRoomInvalidPayload) {
		t.Fatalf("rpc err = %v, want ErrInRoomInvalidPayload", err)
	}
}
