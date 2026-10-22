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
// внутри refreshPeerPings падает.
// failMemberGetPeer(call) решает по номеру вызова GetPeer(member), падать ли
// ему: порядок вызовов на member — scan тика (1), election (2), дальше —
// recovery/RPC-пути.
func setupNilMasterRoom(t *testing.T, failMemberGetPeer func(call int) bool) *nilMasterFixture {
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
		if failMemberGetPeer(memberCalls) {
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

	// Election на первом тике падает, дальше member снова доступен.
	nm := setupNilMasterRoom(t, func(call int) bool { return call == 2 })

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

	nm := setupNilMasterRoom(t, func(call int) bool { return call == 2 })
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

	nm := setupNilMasterRoom(t, func(call int) bool { return call >= 2 })

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
