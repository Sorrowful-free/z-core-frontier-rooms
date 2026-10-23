package state_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	testpeer "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/peer"
)

// (shadow-наблюдатель не подключается: тест ассертит точный состав
// state.Peers — «unchanged {1,2}»; лишний peer ломал бы premise.)
func TestOnLeave_FailedPatchSend_StateAndMasterUntouched(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(50), 4)
	master := testpeer.New(1, "master")
	member := testpeer.New(2, "member")
	if err := h.room.Join(master); err != nil {
		t.Fatalf("Join master: %v", err)
	}
	if err := h.room.Join(member); err != nil {
		t.Fatalf("Join member: %v", err)
	}
	master.Clear()
	member.Clear()

	member.SetDeliverErr(errDeliverFailed)

	if err := h.room.Leave(master); err == nil {
		t.Fatal("Leave: want error from failed leave patch broadcast")
	}
	if h.room.HasPeer(master.GetID()) {
		t.Fatal("master must be removed from room peers even when OnLeave fails")
	}

	member.SetDeliverErr(nil)
	if err := h.policy.OnTickFullState(); err != nil {
		t.Fatalf("OnTickFullState: %v", err)
	}

	full := member.EventsByOpcode(state.OpCodeFullState)
	if len(full) != 1 {
		t.Fatalf("member full states = %d, want 1", len(full))
	}
	got, err := h.roomCodec.Decode(full[0].Frame.Payload)
	if err != nil {
		t.Fatalf("Decode full state: %v", err)
	}
	if len(got.Peers) != 2 {
		t.Fatalf("state peers = %#v, want unchanged {1,2} after failed OnLeave", got.Peers)
	}
	if !got.Peers[domain.PeerID(1)].IsMaster {
		t.Fatal("master flag must stay on peer 1 after failed OnLeave")
	}
}

func TestOnMessage_InputFromUnknownPeer_ReturnsErrPeerNotFound(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(51), 4)
	master := testpeer.New(1, "master")
	mustJoin(t, h.room, master)
	shadow := mustJoinShadow(t, h)
	master.Clear()

	fullPayload, err := h.inputCodec.Encode(&state.InputState{
		Values: state.NewMapState[state.ValueId, state.ValueState](),
	})
	if err != nil {
		t.Fatalf("Encode input: %v", err)
	}
	err = h.policy.OnMessage(events.RoomEvent{
		PeerID: domain.PeerID(99),
		Frame:  domain.Frame{OpCode: state.OpCodeFullInput, Payload: fullPayload},
	})
	if !errors.Is(err, domain.ErrPeerNotFound) {
		t.Fatalf("full input err = %v, want ErrPeerNotFound", err)
	}

	patchPayload, err := h.inputCodec.EncodePatch(&state.InputStatePatch{
		Values: *state.NewMapStatePatch[state.ValueId, state.ValueState, state.ValueState](),
	})
	if err != nil {
		t.Fatalf("EncodePatch input: %v", err)
	}
	err = h.policy.OnMessage(events.RoomEvent{
		PeerID: domain.PeerID(99),
		Frame:  domain.Frame{OpCode: state.OpCodePatchInput, Payload: patchPayload},
	})
	if !errors.Is(err, domain.ErrPeerNotFound) {
		t.Fatalf("patch input err = %v, want ErrPeerNotFound", err)
	}

	if err := h.policy.OnTickFullState(); err != nil {
		t.Fatalf("OnTickFullState: %v", err)
	}
	full := master.EventsByOpcode(state.OpCodeFullState)
	if len(full) != 1 {
		t.Fatalf("master full states = %d, want 1", len(full))
	}
	got, err := h.roomCodec.Decode(full[0].Frame.Payload)
	if err != nil {
		t.Fatalf("Decode full state: %v", err)
	}
	if len(got.Inputs) != 0 {
		t.Fatalf("inputs = %#v, want empty after ghost input rejected", got.Inputs)
	}

	assertWeakInvariants(t, shadow.view)
	settleTick(t, h, shadow.view)
	resyncShadow(t, h)
	assertStrongInvariants(t, shadow.view, h.room)
}

// (shadow-наблюдатель не подключается: capacity=1 занят конкурентным join'ом,
// наблюдателю нет слота.)
func TestJoin_ConcurrentLastSlot_LoserRolledBackFromState(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(52), 1)
	p1 := testpeer.New(1, "a")
	p2 := testpeer.New(2, "b")

	type joinResult struct {
		peer *testpeer.Peer
		err  error
	}
	results := make(chan joinResult, 2)
	var wg sync.WaitGroup
	for _, p := range []*testpeer.Peer{p1, p2} {
		wg.Add(1)
		go func(p *testpeer.Peer) {
			defer wg.Done()
			results <- joinResult{peer: p, err: h.room.Join(p)}
		}(p)
	}
	wg.Wait()
	close(results)

	var winner *testpeer.Peer
	for res := range results {
		switch {
		case res.err == nil:
			if winner != nil {
				t.Fatal("both joins succeeded, want exactly one")
			}
			winner = res.peer
		case !errors.Is(res.err, domain.ErrRoomFull):
			t.Fatalf("Join peer %d: unexpected err %v", res.peer.GetID(), res.err)
		}
	}
	if winner == nil {
		t.Fatal("no join succeeded, want exactly one winner")
	}
	if !h.room.HasPeer(winner.GetID()) {
		t.Fatalf("winner peer %d not in room", winner.GetID())
	}

	winner.Clear()
	if err := h.policy.OnTickFullState(); err != nil {
		t.Fatalf("OnTickFullState: %v", err)
	}
	full := winner.EventsByOpcode(state.OpCodeFullState)
	if len(full) != 1 {
		t.Fatalf("winner full states = %d, want 1", len(full))
	}
	got, err := h.roomCodec.Decode(full[0].Frame.Payload)
	if err != nil {
		t.Fatalf("Decode full state: %v", err)
	}
	if len(got.Peers) != 1 {
		t.Fatalf("state peers = %#v, want only winner %d (loser not rolled back)", got.Peers, winner.GetID())
	}
	if !got.Peers[winner.GetID()].IsMaster {
		t.Fatalf("winner %d must be sole master after loser rollback", winner.GetID())
	}
}
