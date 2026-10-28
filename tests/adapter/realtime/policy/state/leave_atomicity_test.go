package state_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/stdlib"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/codec"
	statepolicy "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/policy/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	portrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/mocks"
	testpeer "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/peer"
	"go.uber.org/mock/gomock"
)

// Откат OnLeave (P34): staged-мутации нет — записи удаляются из p.state
// напрямую, а при падении dispatch (election внутри evPeerLeft) Peers и
// input уходящего восстанавливаются дешёвым ручным restore + master.restore.
// С реальной Room dispatch не падает (GetPeer отвечает только
// ErrPeerNotFound → stale-кандидат пропускается, не фейлит выборы) —
// сценарий строится на mock room.
func TestOnLeave_FailedElection_StateAndMasterRestored(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	room := mocks.NewMockRoom(ctrl)

	var sent []events.PeerEvent
	room.EXPECT().GetID().Return(domain.RoomID(50)).AnyTimes()
	room.EXPECT().GetCapacity().Return(int8(4)).AnyTimes()
	room.EXPECT().Send(gomock.Any()).DoAndReturn(func(ev events.PeerEvent) error {
		sent = append(sent, ev)
		return nil
	}).AnyTimes()

	master := testpeer.New(1, "master")
	member := testpeer.New(2, "member")
	room.EXPECT().GetPeer(master.GetID()).Return(master, nil).AnyTimes()
	// Первый вызов GetPeer(member) — election внутри evPeerLeft — падает,
	// дальше member снова доступен.
	memberCalls := 0
	room.EXPECT().GetPeer(member.GetID()).DoAndReturn(func(domain.PeerID) (portrealtime.Peer, error) {
		memberCalls++
		if memberCalls == 1 {
			return nil, errElectionFailed
		}
		return member, nil
	}).AnyTimes()

	policy := statepolicy.NewStateRoomPolicy(
		stdlib.New("state-policy-test"),
		codec.NewRoomStateCodec(),
		codec.NewEntitiesStateCodec(),
		codec.NewInputStateCodec(),
		codec.NewRpcStateCodec(),
		time.Hour,
		time.Hour,
	)
	if err := policy.OnStart(room); err != nil {
		t.Fatalf("OnStart: %v", err)
	}
	if err := policy.OnJoin(master); err != nil {
		t.Fatalf("OnJoin master: %v", err)
	}
	if err := policy.OnJoin(member); err != nil {
		t.Fatalf("OnJoin member: %v", err)
	}

	// Input master'а — проверка, что запись Inputs уходящего тоже
	// восстанавливается при откате.
	inputPatch := &state.InputStatePatch{
		Values: *state.NewMapStatePatch[state.ValueId, state.ValueState, state.ValueState](),
	}
	inputPatch.Values.Added[state.ValueId(1)] = state.ValueState([]byte{0xCD})
	payload, err := codec.NewInputStateCodec().EncodePatch(inputPatch)
	if err != nil {
		t.Fatalf("EncodePatch input: %v", err)
	}
	if err := policy.OnMessage(events.RoomEvent{
		PeerID: master.GetID(),
		Frame:  domain.Frame{OpCode: state.OpCodePatchInput, Payload: payload},
	}); err != nil {
		t.Fatalf("OnMessage patch input: %v", err)
	}

	err = policy.OnLeave(master)
	if !errors.Is(err, errElectionFailed) {
		t.Fatalf("OnLeave err = %v, want errElectionFailed", err)
	}

	// Откат: state.Peers и Inputs как до leave, master не сменился.
	if err := policy.OnTickFullState(); err != nil {
		t.Fatalf("OnTickFullState: %v", err)
	}
	fulls := testpeer.FilterByOpcode(sent, state.OpCodeFullState)
	if len(fulls) == 0 {
		t.Fatal("want broadcast full state")
	}
	got, err := codec.NewRoomStateCodec().Decode(fulls[len(fulls)-1].Frame.Payload)
	if err != nil {
		t.Fatalf("Decode full state: %v", err)
	}
	if len(got.Peers) != 2 {
		t.Fatalf("state peers = %#v, want unchanged {1,2} after failed OnLeave", got.Peers)
	}
	if !got.Peers[domain.PeerID(1)].IsMaster {
		t.Fatal("master flag must stay on peer 1 after failed OnLeave")
	}
	if _, ok := got.Inputs[domain.PeerID(1)]; !ok {
		t.Fatal("master input must be restored after failed OnLeave")
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
