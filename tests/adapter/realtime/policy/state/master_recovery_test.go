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
	testpeer "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/peer"
	"go.uber.org/mock/gomock"
)

var errElectionFailed = errors.New("election failed")

// memberGetPeerFails — сценарий падений GetPeer(member): решает по номеру
// вызова (1-based), падать ли этому вызову. Порядок вызовов на member внутри
// одного patch-тика: scan в pruneStalePeers (1), election (2), assign (3);
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
// shadow — виртуальный wire-наблюдатель, подпитанный sent.
type nilMasterFixture struct {
	policy *statepolicy.StateRoomPolicy
	room   *mocks.MockRoom
	member *testpeer.Peer
	sent   *[]events.PeerEvent
	shadow *shadowView
}

// setupNilMasterRoom вгоняет политику в nil-master состояние: master(1)
// становится stale (ушёл из room, остался в state), а election на member(2)
// внутри pruneStalePeers/dispatch падает по сценарию failMember.
func setupNilMasterRoom(t *testing.T, failMember memberGetPeerFails) *nilMasterFixture {
	t.Helper()

	ctrl := gomock.NewController(t)
	room := mocks.NewMockRoom(ctrl)

	var sent []events.PeerEvent
	// Виртуальный наблюдатель материализует wire-трафик с конца setup —
	// seedится на post-tick состояние (master вычищен, election упал).
	shadow := newShadowView(codec.NewRoomStateCodec())
	feedShadow := false
	room.EXPECT().GetID().Return(domain.RoomID(90)).AnyTimes()
	room.EXPECT().GetCapacity().Return(int8(4)).AnyTimes()
	room.EXPECT().Send(gomock.Any()).DoAndReturn(func(ev events.PeerEvent) error {
		sent = append(sent, ev)
		if feedShadow {
			shadow.feed(ev)
		}
		return nil
	}).AnyTimes()

	master := testpeer.New(1, "master")
	member := testpeer.New(2, "member")

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

	room.EXPECT().GetPeer(master.GetID()).
		Return(nil, fmt.Errorf("%w: %d", domain.ErrPeerNotFound, master.GetID())).
		AnyTimes()
	room.EXPECT().HasPeer(gomock.Any()).DoAndReturn(func(id domain.PeerID) bool {
		return id == member.GetID()
	}).AnyTimes()
	memberCalls := 0
	room.EXPECT().GetPeer(member.GetID()).DoAndReturn(func(domain.PeerID) (portrealtime.Peer, error) {
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

	seed := state.NewRoomState(domain.RoomID(90), 4)
	seed.Peers[member.GetID()] = state.PeerState{NickName: member.GetNickName(), Ping: member.Ping()}
	shadow.seed(seed)
	feedShadow = true

	return &nilMasterFixture{policy: policy, room: room, member: member, sent: &sent, shadow: shadow}
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

	patches := testpeer.FilterByOpcode(*nm.sent, state.OpCodePatchState)
	if len(patches) == 0 {
		t.Fatal("recovery tick must broadcast master reassignment patch")
	}
	patch, err := codec.NewRoomStateCodec().DecodePatch(patches[len(patches)-1].Frame.Payload)
	if err != nil {
		t.Fatalf("DecodePatch: %v", err)
	}
	updated, ok := patch.Peers.Updated[nm.member.GetID()]
	if !ok || updated.IsMaster == nil || !*updated.IsMaster {
		t.Fatalf("peers.updated[%d] = %#v ok=%v, want IsMaster=true", nm.member.GetID(), updated, ok)
	}

	assertWeakInvariants(t, nm.shadow)
	assertStrongInvariants(t, nm.shadow, nm.room)
}

// RPC→master при отсутствующем master: ленивое переизбрание по живому state
// и доставка избранному peer вместо ErrNoMaster.
func TestOnMessage_RpcTargetMaster_NilMaster_LazyReelectsAndDelivers(t *testing.T) {
	t.Parallel()

	nm := setupNilMasterRoom(t, failGetPeerOnCalls(2))
	*nm.sent = nil

	payload := mustEncodeRPC(t, state.RpcTargetMaster, domain.PeerIDInvalid)
	if err := nm.policy.OnMessage(events.RoomEvent{
		PeerID: nm.member.GetID(),
		Frame:  domain.Frame{OpCode: state.OpCodeRpc, Payload: payload},
	}); err != nil {
		t.Fatalf("OnMessage rpc master: %v", err)
	}

	rpcs := testpeer.FilterByOpcode(*nm.sent, state.OpCodeRpc)
	if len(rpcs) != 1 || rpcs[0].PeerID != nm.member.GetID() {
		t.Fatalf("rpc deliveries = %v, want single rpc to elected master %d", testpeer.Opcodes(*nm.sent), nm.member.GetID())
	}

	assertWeakInvariants(t, nm.shadow)
	assertStrongInvariants(t, nm.shadow, nm.room)
}

// Комната с peers, но без master и с продолжающим падать election:
// RPC→master обязан вернуть ErrNoMaster (wire — OpInRoomNoMaster).
func TestOnMessage_RpcTargetMaster_NilMasterElectionFails_ReturnsErrNoMaster(t *testing.T) {
	t.Parallel()

	nm := setupNilMasterRoom(t, failGetPeerAfter(1))

	payload := mustEncodeRPC(t, state.RpcTargetMaster, domain.PeerIDInvalid)
	err := nm.policy.OnMessage(events.RoomEvent{
		PeerID: nm.member.GetID(),
		Frame:  domain.Frame{OpCode: state.OpCodeRpc, Payload: payload},
	})
	if !errors.Is(err, domain.ErrNoMaster) {
		t.Fatalf("err = %v, want ErrNoMaster", err)
	}
	if op := domain.InRoomErrorOpCode(err); op != domain.OpInRoomNoMaster {
		t.Fatalf("InRoomErrorOpCode = %#x, want %#x", op, domain.OpInRoomNoMaster)
	}

	assertWeakInvariants(t, nm.shadow)
	assertStrongInvariants(t, nm.shadow, nm.room)
}

// Пустая комната без master: переизбирать не из кого → ErrNoMaster.
// (shadow-наблюдатель не подключается: он стал бы первым peer и master —
// ломал бы premissу «комната без master».)
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
// Stale-запись в state (peer в state, но не в room) с реальной Room
// не получить — leave вычищает state атомарно с изъятием из r.peers;
// фикстура на mock room.
func TestOnLeave_ElectionSkipsStaleCandidate_ElectsLivePeer(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	room := mocks.NewMockRoom(ctrl)

	var sent []events.PeerEvent
	room.EXPECT().GetID().Return(domain.RoomID(92)).AnyTimes()
	room.EXPECT().GetCapacity().Return(int8(4)).AnyTimes()
	room.EXPECT().Send(gomock.Any()).DoAndReturn(func(ev events.PeerEvent) error {
		sent = append(sent, ev)
		return nil
	}).AnyTimes()

	master := testpeer.New(1, "master")
	stale := testpeer.New(2, "stale")
	survivor := testpeer.New(3, "survivor")
	room.EXPECT().GetPeer(master.GetID()).Return(master, nil).AnyTimes()
	room.EXPECT().GetPeer(stale.GetID()).
		Return(nil, fmt.Errorf("%w: %d", domain.ErrPeerNotFound, stale.GetID())).AnyTimes()
	room.EXPECT().GetPeer(survivor.GetID()).Return(survivor, nil).AnyTimes()

	policy := newMockRoomPolicy(t, room)
	if err := policy.OnJoin(master); err != nil {
		t.Fatalf("OnJoin master: %v", err)
	}
	if err := policy.OnJoin(stale); err != nil {
		t.Fatalf("OnJoin stale: %v", err)
	}
	if err := policy.OnJoin(survivor); err != nil {
		t.Fatalf("OnJoin survivor: %v", err)
	}

	// Master уходит: election видит кандидатов {2 (stale в state), 3 (живой)} —
	// stale пропускается, master достаётся survivor.
	if err := policy.OnLeave(master); err != nil {
		t.Fatalf("OnLeave master: %v (stale candidate must be skipped, not fail election)", err)
	}

	sent = nil
	if err := policy.OnTickFullState(); err != nil {
		t.Fatalf("OnTickFullState: %v", err)
	}
	got := decodeLastFullState(t, sent)
	if !got.Peers[survivor.GetID()].IsMaster {
		t.Fatal("survivor must be elected master (stale candidate skipped)")
	}
}

// Все кандидаты election — stale: пустые выборы — не ошибка, master
// сбрасывается, никакому peer IsMaster-флаг не выставляется. Masterless-
// эпизод не липкий: следующий join + patch-тик вычищает stale и избирает
// joiner'а.
func TestOnLeave_ElectionAllStale_ResetsMasterWithoutFlags(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	room := mocks.NewMockRoom(ctrl)

	var sent []events.PeerEvent
	room.EXPECT().GetID().Return(domain.RoomID(93)).AnyTimes()
	room.EXPECT().GetCapacity().Return(int8(4)).AnyTimes()
	room.EXPECT().Send(gomock.Any()).DoAndReturn(func(ev events.PeerEvent) error {
		sent = append(sent, ev)
		return nil
	}).AnyTimes()

	master := testpeer.New(1, "master")
	stale := testpeer.New(2, "stale")
	joiner := testpeer.New(3, "joiner")
	room.EXPECT().GetPeer(master.GetID()).Return(master, nil).AnyTimes()
	room.EXPECT().GetPeer(stale.GetID()).
		Return(nil, fmt.Errorf("%w: %d", domain.ErrPeerNotFound, stale.GetID())).AnyTimes()
	room.EXPECT().GetPeer(joiner.GetID()).Return(joiner, nil).AnyTimes()

	policy := newMockRoomPolicy(t, room)
	if err := policy.OnJoin(master); err != nil {
		t.Fatalf("OnJoin master: %v", err)
	}
	if err := policy.OnJoin(stale); err != nil {
		t.Fatalf("OnJoin stale: %v", err)
	}

	// Единственный оставшийся кандидат stale → пустые выборы (не ошибка).
	if err := policy.OnLeave(master); err != nil {
		t.Fatalf("OnLeave master: %v (all-stale election must be empty, not error)", err)
	}

	sent = nil
	if err := policy.OnTickFullState(); err != nil {
		t.Fatalf("OnTickFullState: %v", err)
	}
	got := decodeLastFullState(t, sent)
	for id, ps := range got.Peers {
		if ps.IsMaster {
			t.Fatalf("peer %d IsMaster=true after empty election, want no advertised master", id)
		}
	}

	// Joiner входит в masterless-окно (join не переизбирает master в
	// непустой комнате): его full-state — masterless вид.
	if err := policy.OnJoin(joiner); err != nil {
		t.Fatalf("OnJoin joiner: %v", err)
	}
	joinerFull := joiner.EventsByOpcode(state.OpCodeFullState)
	if len(joinerFull) != 1 {
		t.Fatalf("joiner full states = %d, want 1", len(joinerFull))
	}
	got, err := codec.NewRoomStateCodec().Decode(joinerFull[0].Frame.Payload)
	if err != nil {
		t.Fatalf("Decode joiner full state: %v", err)
	}
	for id, ps := range got.Peers {
		if ps.IsMaster {
			t.Fatalf("peer %d IsMaster=true in masterless window, want none", id)
		}
	}

	// Следующий patch-тик вычищает stale и избирает joiner'а.
	sent = nil
	if err := policy.OnTickPatchState(); err != nil {
		t.Fatalf("OnTickPatchState: %v", err)
	}
	patches := testpeer.FilterByOpcode(sent, state.OpCodePatchState)
	if len(patches) != 1 {
		t.Fatalf("patches = %d, want 1", len(patches))
	}
	patch, err := codec.NewRoomStateCodec().DecodePatch(patches[0].Frame.Payload)
	if err != nil {
		t.Fatalf("DecodePatch: %v", err)
	}
	added, ok := patch.Peers.Added[joiner.GetID()]
	if !ok || !added.IsMaster {
		t.Fatalf("peers.added[%d] = %#v ok=%v, want IsMaster=true", joiner.GetID(), added, ok)
	}
}

// newMockRoomPolicy — политика поверх mock room для stale/edge-сценариев,
// недостижимых через реальную Room (leave атомарен с изъятием из r.peers).
func newMockRoomPolicy(t *testing.T, room *mocks.MockRoom) *statepolicy.StateRoomPolicy {
	t.Helper()
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
	return policy
}

func decodeLastFullState(t *testing.T, sent []events.PeerEvent) *state.RoomState {
	t.Helper()
	fulls := testpeer.FilterByOpcode(sent, state.OpCodeFullState)
	if len(fulls) == 0 {
		t.Fatal("want broadcast full state")
	}
	got, err := codec.NewRoomStateCodec().Decode(fulls[len(fulls)-1].Frame.Payload)
	if err != nil {
		t.Fatalf("Decode full state: %v", err)
	}
	return got
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
	fulls := testpeer.FilterByOpcode(*nm.sent, state.OpCodeFullState)
	if len(fulls) == 0 {
		t.Fatal("want broadcast full state")
	}
	got, err := codec.NewRoomStateCodec().Decode(fulls[len(fulls)-1].Frame.Payload)
	if err != nil {
		t.Fatalf("Decode full state: %v", err)
	}
	if len(got.Peers) != 1 {
		t.Fatalf("state peers = %#v, want only member %d", got.Peers, nm.member.GetID())
	}
	if got.Peers[nm.member.GetID()].IsMaster {
		t.Fatal("member IsMaster=true while policy master is nil — flag divergence")
	}

	assertWeakInvariants(t, nm.shadow)
	assertStrongInvariants(t, nm.shadow, nm.room)
}
