package state_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

func TestOnLeave_FailedPatchSend_StateAndMasterUntouched(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(50), 4)
	master := newCapturePeer(1, "master")
	member := newFlakyCapturePeer(2, "member")
	if err := h.room.Join(master); err != nil {
		t.Fatalf("Join master: %v", err)
	}
	if err := h.room.Join(member); err != nil {
		t.Fatalf("Join member: %v", err)
	}
	master.clear()
	member.clear()

	member.setErr(errDeliverFailed)

	if err := h.room.Leave(master); err == nil {
		t.Fatal("Leave: want error from failed leave patch broadcast")
	}
	if h.room.HasPeer(master.id) {
		t.Fatal("master must be removed from room peers even when OnLeave fails")
	}

	member.setErr(nil)
	if err := h.policy.OnTickFullState(); err != nil {
		t.Fatalf("OnTickFullState: %v", err)
	}

	full := filterOpcode(member.snapshot(), state.OpCodeFullState)
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
	master := newCapturePeer(1, "master")
	mustJoin(t, h.room, master)
	master.clear()

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
	full := filterOpcode(master.snapshot(), state.OpCodeFullState)
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
}

func TestJoin_ConcurrentLastSlot_LoserRolledBackFromState(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(52), 1)
	p1 := newCapturePeer(1, "a")
	p2 := newCapturePeer(2, "b")

	type joinResult struct {
		peer *capturePeer
		err  error
	}
	results := make(chan joinResult, 2)
	var wg sync.WaitGroup
	for _, p := range []*capturePeer{p1, p2} {
		wg.Add(1)
		go func(p *capturePeer) {
			defer wg.Done()
			results <- joinResult{peer: p, err: h.room.Join(p)}
		}(p)
	}
	wg.Wait()
	close(results)

	var winner *capturePeer
	for res := range results {
		switch {
		case res.err == nil:
			if winner != nil {
				t.Fatal("both joins succeeded, want exactly one")
			}
			winner = res.peer
		case !errors.Is(res.err, domain.ErrRoomFull):
			t.Fatalf("Join peer %d: unexpected err %v", res.peer.id, res.err)
		}
	}
	if winner == nil {
		t.Fatal("no join succeeded, want exactly one winner")
	}
	if !h.room.HasPeer(winner.id) {
		t.Fatalf("winner peer %d not in room", winner.id)
	}

	winner.clear()
	if err := h.policy.OnTickFullState(); err != nil {
		t.Fatalf("OnTickFullState: %v", err)
	}
	full := filterOpcode(winner.snapshot(), state.OpCodeFullState)
	if len(full) != 1 {
		t.Fatalf("winner full states = %d, want 1", len(full))
	}
	got, err := h.roomCodec.Decode(full[0].Frame.Payload)
	if err != nil {
		t.Fatalf("Decode full state: %v", err)
	}
	if len(got.Peers) != 1 {
		t.Fatalf("state peers = %#v, want only winner %d (loser not rolled back)", got.Peers, winner.id)
	}
	if !got.Peers[winner.id].IsMaster {
		t.Fatalf("winner %d must be sole master after loser rollback", winner.id)
	}
}

type flakyCapturePeer struct {
	id   domain.PeerID
	nick string

	mu         sync.Mutex
	deliverErr error
	delivered  []events.PeerEvent
}

func newFlakyCapturePeer(id domain.PeerID, nick string) *flakyCapturePeer {
	return &flakyCapturePeer{id: id, nick: nick}
}

func (p *flakyCapturePeer) setErr(err error) {
	p.mu.Lock()
	p.deliverErr = err
	p.mu.Unlock()
}

func (p *flakyCapturePeer) GetID() domain.PeerID { return p.id }
func (p *flakyCapturePeer) GetNickName() string  { return p.nick }
func (p *flakyCapturePeer) Ping() int64          { return 0 }
func (p *flakyCapturePeer) IsAlive() bool        { return true }
func (p *flakyCapturePeer) Start() error         { return nil }
func (p *flakyCapturePeer) Stop() error          { return nil }
func (p *flakyCapturePeer) Deliver(ev events.PeerEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.deliverErr != nil {
		return p.deliverErr
	}
	p.delivered = append(p.delivered, ev)
	return nil
}

func (p *flakyCapturePeer) snapshot() []events.PeerEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]events.PeerEvent, len(p.delivered))
	copy(out, p.delivered)
	return out
}

func (p *flakyCapturePeer) clear() {
	p.mu.Lock()
	p.delivered = nil
	p.mu.Unlock()
}

var _ realtime.Peer = (*flakyCapturePeer)(nil)
