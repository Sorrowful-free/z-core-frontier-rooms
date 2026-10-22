package state_test

import (
	"errors"
	"fmt"
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
	"go.uber.org/mock/gomock"
)

var errElectionFailed = errors.New("election failed")

// memberGetPeerFails — сценарий падений GetPeer(member): решает по номеру
// вызова (1-based), падать ли этому вызову. Порядок вызовов на member внутри
// одного patch-тика: scan в refreshPeerPings (1), election (2), assign (3);
// дальше — recovery-тики / lazy re-elect по RPC.
type memberGetPeerFails func(call int) bool

// failGetPeerOnCalls — падать на перечисленных вызовах GetPeer(member).
func failGetPeerOnCalls(calls ...int) memberGetPeerFails {
	bad := make(map[int]struct{}, len(calls))
	for _, c := range calls {
		bad[c] = struct{}{}
	}
	return func(call int) bool {
		_, fail := bad[call]
		return fail
	}
}

// failGetPeerAfter — первые okCalls вызовов проходят, все последующие падают
// (latch: election не восстанавливается никогда).
func failGetPeerAfter(okCalls int) memberGetPeerFails {
	return func(call int) bool { return call > okCalls }
}

// nilMasterRoom — политика + mock room в состоянии «post-failed-election»:
// в state остаётся живой member, но p.master == nil (election на прошлом
// тике упал). sent — все PeerEvent, прошедшие через room.Send.
type nilMasterFixture struct {
	policy *statepolicy.StateRoomPolicy
	member *capturePeer
	sent   *[]events.PeerEvent
}

// setupNilMasterRoom вгоняет политику в nil-master состояние: master(1)
// становится stale (ушёл из room, остался в state), а election на member(2)
// внутри refreshPeerPings падает по сценарию failMember.
func setupNilMasterRoom(t *testing.T, failMember memberGetPeerFails) *nilMasterFixture {
	t.Helper()

	ctrl := gomock.NewController(t)
	room := mocks.NewMockRoom(ctrl)

	var sent []events.PeerEvent
	room.EXPECT().GetID().Return(domain.RoomID(90)).AnyTimes()
	room.EXPECT().GetCapacity().Return(int8(4)).AnyTimes()
	room.EXPECT().Send(gomock.Any()).DoAndReturn(func(ev events.PeerEvent) error {
		sent = append(sent, ev)
		return nil
	}).AnyTimes()

	master := newCapturePeer(1, "master")
	member := newCapturePeer(2, "member")

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
	// state: {1: master, 2}; p.master = master.

	room.EXPECT().GetPeer(master.id).
		Return(nil, fmt.Errorf("%w: %d", domain.ErrPeerNotFound, master.id)).
		AnyTimes()
	memberCalls := 0
	room.EXPECT().GetPeer(member.id).DoAndReturn(func(domain.PeerID) (portrealtime.Peer, error) {
		memberCalls++
		if failMember(memberCalls) {
			return nil, errElectionFailed
		}
		return member, nil
	}).AnyTimes()

	// Тик: master вычищен как stale, election на member падает → p.master = nil,
	// в state остаётся только member.
	if err := policy.OnTickPatchState(); err != nil {
		t.Fatalf("OnTickPatchState (nil-master setup): %v", err)
	}

	return &nilMasterFixture{policy: policy, member: member, sent: &sent}
}

// Комната без master (election упал на прошлом тике) обязана переизбрать
// master на следующем patch-тике, пока есть живые peers.
func TestOnTickPatchState_NilMasterRoom_ReelectsOnNextTick(t *testing.T) {
	t.Parallel()

	// Election на первом тике падает (вызов 2 — elect после scan), дальше member снова доступен.
	nm := setupNilMasterRoom(t, failGetPeerOnCalls(2))

	if err := nm.policy.OnTickPatchState(); err != nil {
		t.Fatalf("OnTickPatchState recovery: %v", err)
	}

	patches := filterOpcode(*nm.sent, state.OpCodePatchState)
	if len(patches) == 0 {
		t.Fatal("recovery tick must broadcast master reassignment patch")
	}
	patch, err := codec.NewRoomStateCodec().DecodePatch(patches[len(patches)-1].Frame.Payload)
	if err != nil {
		t.Fatalf("DecodePatch: %v", err)
	}
	updated, ok := patch.Peers.Updated[nm.member.id]
	if !ok || updated.IsMaster == nil || !*updated.IsMaster {
		t.Fatalf("peers.updated[%d] = %#v ok=%v, want IsMaster=true", nm.member.id, updated, ok)
	}
}

// RPC→master при отсутствующем master: ленивое переизбрание по живому state
// и доставка избранному peer вместо ErrNoMaster.
func TestOnMessage_RpcTargetMaster_NilMaster_LazyReelectsAndDelivers(t *testing.T) {
	t.Parallel()

	nm := setupNilMasterRoom(t, failGetPeerOnCalls(2))
	*nm.sent = nil

	payload := mustEncodeRPC(t, state.RpcTargetMaster, domain.PeerIDInvalid)
	if err := nm.policy.OnMessage(events.RoomEvent{
		PeerID: nm.member.id,
		Frame:  domain.Frame{OpCode: state.OpCodeRpc, Payload: payload},
	}); err != nil {
		t.Fatalf("OnMessage rpc master: %v", err)
	}

	rpcs := filterOpcode(*nm.sent, state.OpCodeRpc)
	if len(rpcs) != 1 || rpcs[0].PeerID != nm.member.id {
		t.Fatalf("rpc deliveries = %v, want single rpc to elected master %d", opcodes(*nm.sent), nm.member.id)
	}
}

// Комната с peers, но без master и с продолжающим падать election:
// RPC→master обязан вернуть ErrNoMaster (wire — OpInRoomNoMaster).
func TestOnMessage_RpcTargetMaster_NilMasterElectionFails_ReturnsErrNoMaster(t *testing.T) {
	t.Parallel()

	nm := setupNilMasterRoom(t, failGetPeerAfter(1))

	payload := mustEncodeRPC(t, state.RpcTargetMaster, domain.PeerIDInvalid)
	err := nm.policy.OnMessage(events.RoomEvent{
		PeerID: nm.member.id,
		Frame:  domain.Frame{OpCode: state.OpCodeRpc, Payload: payload},
	})
	if !errors.Is(err, domain.ErrNoMaster) {
		t.Fatalf("err = %v, want ErrNoMaster", err)
	}
	if op := domain.InRoomErrorOpCode(err); op != domain.OpInRoomNoMaster {
		t.Fatalf("InRoomErrorOpCode = %#x, want %#x", op, domain.OpInRoomNoMaster)
	}
}

// Пустая комната без master: переизбирать не из кого → ErrNoMaster.
func TestOnMessage_RpcTargetMaster_EmptyRoom_ReturnsErrNoMaster(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(91), 4)

	payload := mustEncodeRPC(t, state.RpcTargetMaster, domain.PeerIDInvalid)
	err := h.policy.OnMessage(events.RoomEvent{
		PeerID: domain.PeerID(1),
		Frame:  domain.Frame{OpCode: state.OpCodeRpc, Payload: payload},
	})
	if !errors.Is(err, domain.ErrNoMaster) {
		t.Fatalf("err = %v, want ErrNoMaster", err)
	}
	if op := domain.InRoomErrorOpCode(err); op != domain.OpInRoomNoMaster {
		t.Fatalf("InRoomErrorOpCode = %#x, want %#x", op, domain.OpInRoomNoMaster)
	}
}

// Election skip-stale: кандидат, которого уже нет в room (GetPeer →
// ErrPeerNotFound), пропускается — выборы идут среди живых, а не падают.
func TestOnLeave_ElectionSkipsStaleCandidate_ElectsLivePeer(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(92), 4)
	master := newCapturePeer(1, "master")
	stale := newFlakyCapturePeer(2, "stale")
	survivor := newCapturePeer(3, "survivor")
	mustJoin(t, h.room, master, survivor)
	if err := h.room.Join(stale); err != nil {
		t.Fatalf("Join stale: %v", err)
	}

	// stale уходит из room с упавшим broadcast — запись остаётся в state.Peers.
	stale.setErr(errDeliverFailed)
	if err := h.room.Leave(stale); err == nil {
		t.Fatal("Leave stale: want error from failed broadcast")
	}
	stale.setErr(nil)
	survivor.clear()

	// Master уходит: election видит кандидатов {2 (stale в state), 3 (живой)} —
	// stale пропускается, master достаётся survivor.
	if err := h.room.Leave(master); err != nil {
		t.Fatalf("Leave master: %v (stale candidate must be skipped, not fail election)", err)
	}

	assertPeerIsMasterInPatch(t, h, survivor, domain.PeerID(3))
}

// Все кандидаты election — stale: пустые выборы — не ошибка, master
// сбрасывается, никакому peer IsMaster-флаг не выставляется.
func TestOnLeave_ElectionAllStale_ResetsMasterWithoutFlags(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(93), 4)
	master := newCapturePeer(1, "master")
	stale := newFlakyCapturePeer(2, "stale")
	mustJoin(t, h.room, master)
	if err := h.room.Join(stale); err != nil {
		t.Fatalf("Join stale: %v", err)
	}

	stale.setErr(errDeliverFailed)
	if err := h.room.Leave(stale); err == nil {
		t.Fatal("Leave stale: want error from failed broadcast")
	}
	stale.setErr(nil)

	// Единственный оставшийся кандидат stale → пустые выборы (не ошибка).
	if err := h.room.Leave(master); err != nil {
		t.Fatalf("Leave master: %v (all-stale election must be empty, not error)", err)
	}

	// Joiner видит state без рекламируемого master: stale-запись не может
	// держать IsMaster.
	joiner := newCapturePeer(3, "joiner")
	if err := h.room.Join(joiner); err != nil {
		t.Fatalf("Join joiner: %v", err)
	}
	got := decodeOnlyFullState(t, h, joiner)
	for id, ps := range got.Peers {
		if ps.IsMaster {
			t.Fatalf("peer %d IsMaster=true after empty election, want no advertised master", id)
		}
	}

	// Masterless-эпизод не липкий: следующий тик вычищает stale и избирает joiner.
	joiner.clear()
	if err := h.policy.OnTickPatchState(); err != nil {
		t.Fatalf("OnTickPatchState: %v", err)
	}
	assertPeerIsMasterInPatch(t, h, joiner, domain.PeerID(3))
}

// assignMaster validate-first: GetPeer избранного падает уже после победы в
// election — IsMaster-флаги в state.Peers при этом не выставляются
// (state не рекламирует master, которого нет у policy).
func TestOnTickPatchState_AssignMasterFails_NoIsMasterFlagDivergence(t *testing.T) {
	t.Parallel()

	// Setup-тик: scan(1) и elect(2) проходят, assign(3) падает → p.master=nil,
	// но member не должен получить IsMaster=true в state.
	nm := setupNilMasterRoom(t, failGetPeerOnCalls(3))

	if err := nm.policy.OnTickFullState(); err != nil {
		t.Fatalf("OnTickFullState: %v", err)
	}
	fulls := filterOpcode(*nm.sent, state.OpCodeFullState)
	if len(fulls) == 0 {
		t.Fatal("want broadcast full state")
	}
	got, err := codec.NewRoomStateCodec().Decode(fulls[len(fulls)-1].Frame.Payload)
	if err != nil {
		t.Fatalf("Decode full state: %v", err)
	}
	if len(got.Peers) != 1 {
		t.Fatalf("state peers = %#v, want only member %d", got.Peers, nm.member.id)
	}
	if got.Peers[nm.member.id].IsMaster {
		t.Fatal("member IsMaster=true while policy master is nil — flag divergence")
	}
}
