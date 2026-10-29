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

// memberGetPeerFails — a GetPeer(member) failure scenario: decides by the
// call number (1-based) whether this call fails. The order of member calls
// inside one patch tick: scan in pruneStalePeers (1), election (2), assign
// (3); after that — recovery ticks / lazy re-elect on RPC.
type memberGetPeerFails func(call int) bool

// failGetPeerOnCalls — fail on the listed GetPeer(member) calls.
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

// failGetPeerAfter — the first okCalls calls succeed, all subsequent ones
// fail (a latch: the election never recovers).
func failGetPeerAfter(okCalls int) memberGetPeerFails {
	return func(call int) bool { return call > okCalls }
}

// nilMasterRoom — a policy + mock room in the "post-failed-election"
// state: the state keeps a live member, but p.master == nil (the election
// failed on the previous tick). sent — all PeerEvent that passed through
// room.Send. shadow — a virtual wire observer fed by sent.
type nilMasterFixture struct {
	policy *statepolicy.StateRoomPolicy
	room   *mocks.MockRoom
	member *testpeer.Peer
	sent   *[]events.PeerEvent
	shadow *shadowView
}

// setupNilMasterRoom drives the policy into the nil-master state:
// master(1) becomes stale (left the room, stayed in state), and the
// election on member(2) inside pruneStalePeers/dispatch fails per the
// failMember scenario.
func setupNilMasterRoom(t *testing.T, failMember memberGetPeerFails) *nilMasterFixture {
	t.Helper()

	ctrl := gomock.NewController(t)
	room := mocks.NewMockRoom(ctrl)

	var sent []events.PeerEvent
	// The virtual observer materializes the wire traffic from the end of
	// setup — it is seeded on the post-tick state (master pruned, election
	// failed).
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

	// Tick: master is pruned as stale, the election on member fails →
	// p.master = nil, only member remains in the state.
	if err := policy.OnTickPatchState(); err != nil {
		t.Fatalf("OnTickPatchState (nil-master setup): %v", err)
	}

	seed := state.NewRoomState(domain.RoomID(90), 4)
	seed.Peers[member.GetID()] = state.PeerState{NickName: member.GetNickName(), Ping: member.Ping()}
	shadow.seed(seed)
	feedShadow = true

	return &nilMasterFixture{policy: policy, room: room, member: member, sent: &sent, shadow: shadow}
}

// A room without a master (the election failed on the previous tick) must
// re-elect a master on the next patch tick as long as live peers remain.
func TestOnTickPatchState_NilMasterRoom_ReelectsOnNextTick(t *testing.T) {
	t.Parallel()

	// The election on the first tick fails (call 2 — elect after scan), afterwards member is reachable again.
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

// RPC→master with no master present: a lazy re-election from the live
// state and delivery to the elected peer instead of ErrNoMaster.
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

// A room with peers but no master and a still-failing election:
// RPC→master must return ErrNoMaster (wire — OpInRoomNoMaster).
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

// An RPC frame in an empty room: the sender is absent from state.Peers,
// so the ghost membership check (P35) rejects it with ErrPeerNotFound
// before master resolution — ErrNoMaster is unreachable from a
// non-member (the populated masterless-room case above keeps that
// surface covered).
func TestOnMessage_RpcTargetMaster_EmptyRoom_ReturnsErrPeerNotFound(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(91), 4)

	payload := mustEncodeRPC(t, state.RpcTargetMaster, domain.PeerIDInvalid)
	err := h.policy.OnMessage(events.RoomEvent{
		PeerID: domain.PeerID(1),
		Frame:  domain.Frame{OpCode: state.OpCodeRpc, Payload: payload},
	})
	if !errors.Is(err, domain.ErrPeerNotFound) {
		t.Fatalf("err = %v, want ErrPeerNotFound", err)
	}
	if op := domain.InRoomErrorOpCode(err); op != domain.OpInRoomInternal {
		t.Fatalf("InRoomErrorOpCode = %#x, want %#x", op, domain.OpInRoomInternal)
	}
}

// Election skip-stale: a candidate that is no longer in the room (GetPeer
// → ErrPeerNotFound) is skipped — the election proceeds among the live
// peers instead of failing. A stale entry in the state (peer in state but
// not in room) cannot be produced with a real Room — leave cleans the
// state atomically with the removal from r.peers; fixture on a mock room.
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

	// The master leaves: the election sees candidates {2 (stale in state),
	// 3 (live)} — stale is skipped, master goes to survivor.
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

// All election candidates are stale: an empty election is not an error,
// the master is reset, no peer gets the IsMaster flag. The masterless
// episode is not sticky: the next join + patch tick prunes the stale
// entries and elects the joiner.
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

	// The only remaining candidate is stale → an empty election (not an error).
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

	// The joiner enters during the masterless window (join does not
	// re-elect a master in a non-empty room): its full state is a
	// masterless view.
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

	// The next patch tick prunes the stale entry and elects the joiner.
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

// newMockRoomPolicy — a policy on top of a mock room for stale/edge
// scenarios that are unreachable through a real Room (leave is atomic with
// the removal from r.peers).
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

// assignMaster validate-first: GetPeer of the electee fails after the
// election was already won — the IsMaster flags in state.Peers must not be
// set in that case (the state must not advertise a master the policy does
// not have).
func TestOnTickPatchState_AssignMasterFails_NoIsMasterFlagDivergence(t *testing.T) {
	t.Parallel()

	// Setup tick: scan(1) and elect(2) succeed, assign(3) fails →
	// p.master=nil, but member must not get IsMaster=true in the state.
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
