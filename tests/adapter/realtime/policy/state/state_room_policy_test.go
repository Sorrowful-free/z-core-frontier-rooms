package state_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/stdlib"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/codec"
	statepolicy "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/policy/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	portpolicy "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime/policy"
	testpeer "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/peer"
)

func TestOnJoin_FirstPeerGetsFullStateWithRoomMetadata(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(42), 8)
	master := testpeer.New(1, "master")

	if err := h.room.Join(master); err != nil {
		t.Fatalf("Join master: %v", err)
	}

	events := master.Snapshot()
	if len(events) != 1 {
		t.Fatalf("master events = %d, want 1 full state", len(events))
	}
	if events[0].Frame.OpCode != state.OpCodeFullState {
		t.Fatalf("opcode = %#x, want full state", events[0].Frame.OpCode)
	}

	got, err := h.roomCodec.Decode(events[0].Frame.Payload)
	if err != nil {
		t.Fatalf("Decode full: %v", err)
	}
	if got.ID != domain.RoomID(42) {
		t.Fatalf("room id = %d, want 42", got.ID)
	}
	if got.Capacity != 8 {
		t.Fatalf("capacity = %d, want 8", got.Capacity)
	}
	peer := got.Peers[domain.PeerID(1)]
	if !peer.IsMaster || peer.NickName != "master" {
		t.Fatalf("master peer state: %#v", peer)
	}
}

// Tick-coalesced emission (P34): the join delta to the others goes out not
// immediately but on the next patch tick — a universal broadcast to all;
// the joiner gets an echo of Added[self] on top of the full state it
// already received (soft apply — upsert).
func TestOnJoin_JoinDeltaReachesAllPeersOnNextTick(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(1), 4)
	master := testpeer.New(1, "master")
	client := testpeer.New(2, "client")

	mustJoin(t, h.room, master)
	// Converge: the master's join delta goes out on the first tick, prevState catches up.
	if err := h.policy.OnTickPatchState(); err != nil {
		t.Fatalf("OnTickPatchState converge: %v", err)
	}
	master.Clear()

	if err := h.room.Join(client); err != nil {
		t.Fatalf("Join client: %v", err)
	}

	// No immediate emission to the others: only the full state to the joiner.
	if got := master.Snapshot(); len(got) != 0 {
		t.Fatalf("master events after join = %v, want none until tick", testpeer.Opcodes(got))
	}
	clientEvents := client.Snapshot()
	if len(clientEvents) != 1 || clientEvents[0].Frame.OpCode != state.OpCodeFullState {
		t.Fatalf("client should receive single full state, got %#v", testpeer.Opcodes(clientEvents))
	}

	if err := h.policy.OnTickPatchState(); err != nil {
		t.Fatalf("OnTickPatchState: %v", err)
	}

	for _, p := range []*testpeer.Peer{master, client} {
		patches := testpeer.FilterByOpcode(p.Snapshot(), state.OpCodePatchState)
		if len(patches) != 1 {
			t.Fatalf("peer %d patches = %d, want 1 tick patch", p.GetID(), len(patches))
		}
		patch, err := h.roomCodec.DecodePatch(patches[0].Frame.Payload)
		if err != nil {
			t.Fatalf("DecodePatch peer %d: %v", p.GetID(), err)
		}
		if _, ok := patch.Peers.Added[domain.PeerID(2)]; !ok {
			t.Fatalf("peer %d patch peers.added = %#v, want joiner", p.GetID(), patch.Peers.Added)
		}
	}
}

// No per-message emission: OnMessage only mutates the state, the delta
// reaches everyone on a patch tick — including the master-sender (echo).
func TestOnMessage_FullEntities_PatchReachesAllPeersOnTick(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(3), 8)
	master := testpeer.New(1, "master")
	client := testpeer.New(2, "client")

	mustJoin(t, h.room, master, client)
	mustConverge(t, h)
	master.Clear()
	client.Clear()

	entities := state.NewMapState[state.EntityID, state.EntityState]()
	entities[state.EntityID(10)] = state.EntityState{
		Owner:      domain.PeerID(1),
		Components: state.NewMapState[state.ComponentID, state.ComponentState](),
	}
	payload, err := h.entitiesCodec.Encode(entities)
	if err != nil {
		t.Fatalf("Encode entities: %v", err)
	}

	if err := h.policy.OnMessage(events.RoomEvent{
		PeerID: domain.PeerID(1),
		Frame:  domain.Frame{OpCode: state.OpCodeFullEntities, Payload: payload},
	}); err != nil {
		t.Fatalf("OnMessage full entities: %v", err)
	}

	if len(master.Snapshot()) != 0 || len(client.Snapshot()) != 0 {
		t.Fatalf("no wire traffic expected before tick, master=%d client=%d",
			len(master.Snapshot()), len(client.Snapshot()))
	}

	if err := h.policy.OnTickPatchState(); err != nil {
		t.Fatalf("OnTickPatchState: %v", err)
	}
	for _, p := range []*testpeer.Peer{master, client} {
		patches := testpeer.FilterByOpcode(p.Snapshot(), state.OpCodePatchState)
		if len(patches) != 1 {
			t.Fatalf("peer %d patches = %d, want 1", p.GetID(), len(patches))
		}
		patch, err := h.roomCodec.DecodePatch(patches[0].Frame.Payload)
		if err != nil {
			t.Fatalf("DecodePatch peer %d: %v", p.GetID(), err)
		}
		if _, ok := patch.Entities.Added[state.EntityID(10)]; !ok {
			t.Fatalf("peer %d entities.added = %#v", p.GetID(), patch.Entities.Added)
		}
	}
}

func TestOnMessage_FullEntities_RejectsNonMaster(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(4), 4)
	master := testpeer.New(1, "master")
	client := testpeer.New(2, "client")
	mustJoin(t, h.room, master, client)

	payload, err := h.entitiesCodec.Encode(state.NewMapState[state.EntityID, state.EntityState]())
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	err = h.policy.OnMessage(events.RoomEvent{
		PeerID: domain.PeerID(2),
		Frame:  domain.Frame{OpCode: state.OpCodeFullEntities, Payload: payload},
	})
	if !errors.Is(err, domain.ErrNotMaster) {
		t.Fatalf("err = %v, want ErrNotMaster", err)
	}
}

func TestOnMessage_PatchEntities_PatchReachesAllPeersOnTick(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(14), 8)
	master := testpeer.New(1, "master")
	client := testpeer.New(2, "client")
	mustJoin(t, h.room, master, client)
	mustConverge(t, h)
	master.Clear()
	client.Clear()

	components := state.NewMapState[state.ComponentID, state.ComponentState]()
	components[state.ComponentID(1)] = state.ComponentState{
		Values: state.NewMapState[state.ValueId, state.ValueState](),
	}
	components[state.ComponentID(1)].Values[state.ValueId(1)] = state.ValueState([]byte{0xAB})

	patch := state.NewMapStatePatch[state.EntityID, state.EntityState, state.EntityStatePatch]()
	patch.Added[state.EntityID(10)] = state.EntityState{
		Owner:      domain.PeerID(1),
		Components: components,
	}
	payload, err := h.entitiesCodec.EncodePatch(patch)
	if err != nil {
		t.Fatalf("EncodePatch entities: %v", err)
	}

	if err := h.policy.OnMessage(events.RoomEvent{
		PeerID: domain.PeerID(1),
		Frame:  domain.Frame{OpCode: state.OpCodePatchEntities, Payload: payload},
	}); err != nil {
		t.Fatalf("OnMessage patch entities: %v", err)
	}

	if err := h.policy.OnTickPatchState(); err != nil {
		t.Fatalf("OnTickPatchState: %v", err)
	}
	for _, p := range []*testpeer.Peer{master, client} {
		patches := testpeer.FilterByOpcode(p.Snapshot(), state.OpCodePatchState)
		if len(patches) != 1 {
			t.Fatalf("peer %d patches = %d, want 1", p.GetID(), len(patches))
		}
		got, err := h.roomCodec.DecodePatch(patches[0].Frame.Payload)
		if err != nil {
			t.Fatalf("DecodePatch peer %d: %v", p.GetID(), err)
		}
		if _, ok := got.Entities.Added[state.EntityID(10)]; !ok {
			t.Fatalf("peer %d entities.added = %#v", p.GetID(), got.Entities.Added)
		}
	}
}

func TestOnMessage_PatchEntities_UpdatesExistingEntity(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(15), 8)
	master := testpeer.New(1, "master")
	client := testpeer.New(2, "client")
	mustJoin(t, h.room, master, client)

	seed := state.NewMapState[state.EntityID, state.EntityState]()
	seedComponents := state.NewMapState[state.ComponentID, state.ComponentState]()
	seedComponents[state.ComponentID(1)] = state.ComponentState{
		Values: state.NewMapState[state.ValueId, state.ValueState](),
	}
	seedComponents[state.ComponentID(1)].Values[state.ValueId(1)] = state.ValueState([]byte{0x01})
	seed[state.EntityID(10)] = state.EntityState{
		Owner:      domain.PeerID(1),
		Components: seedComponents,
	}
	seedPayload, err := h.entitiesCodec.Encode(seed)
	if err != nil {
		t.Fatalf("Encode seed entities: %v", err)
	}
	if err := h.policy.OnMessage(events.RoomEvent{
		PeerID: domain.PeerID(1),
		Frame:  domain.Frame{OpCode: state.OpCodeFullEntities, Payload: seedPayload},
	}); err != nil {
		t.Fatalf("OnMessage full entities seed: %v", err)
	}
	// Converge: the seed delta goes out on the tick as Added, otherwise the
	// next tick carries it again — clients will see the Updated patch only
	// after this emission.
	mustConverge(t, h)
	master.Clear()
	client.Clear()

	valuePatch := state.NewMapStatePatch[state.ValueId, state.ValueState, state.ValueState]()
	valuePatch.Updated[state.ValueId(1)] = state.ValueState([]byte{0xFF})

	compPatch := state.NewMapStatePatch[state.ComponentID, state.ComponentState, state.ComponentStatePatch]()
	compPatch.Updated[state.ComponentID(1)] = state.ComponentStatePatch{Values: *valuePatch}

	entityPatch := state.NewMapStatePatch[state.EntityID, state.EntityState, state.EntityStatePatch]()
	entityPatch.Updated[state.EntityID(10)] = state.EntityStatePatch{Components: *compPatch}

	payload, err := h.entitiesCodec.EncodePatch(entityPatch)
	if err != nil {
		t.Fatalf("EncodePatch entities update: %v", err)
	}
	if err := h.policy.OnMessage(events.RoomEvent{
		PeerID: domain.PeerID(1),
		Frame:  domain.Frame{OpCode: state.OpCodePatchEntities, Payload: payload},
	}); err != nil {
		t.Fatalf("OnMessage patch entities update: %v", err)
	}

	if err := h.policy.OnTickPatchState(); err != nil {
		t.Fatalf("OnTickPatchState: %v", err)
	}
	clientPatches := testpeer.FilterByOpcode(client.Snapshot(), state.OpCodePatchState)
	if len(clientPatches) != 1 {
		t.Fatalf("client patches = %d, want 1", len(clientPatches))
	}
	got, err := h.roomCodec.DecodePatch(clientPatches[0].Frame.Payload)
	if err != nil {
		t.Fatalf("DecodePatch: %v", err)
	}
	updated, ok := got.Entities.Updated[state.EntityID(10)]
	if !ok {
		t.Fatalf("entities.updated = %#v", got.Entities.Updated)
	}
	gotVal := updated.Components.Updated[state.ComponentID(1)].Values.Updated[state.ValueId(1)]
	if len(gotVal) != 1 || gotVal[0] != 0xFF {
		t.Fatalf("updated value = %v, want [255]", []byte(gotVal))
	}
}

func TestOnMessage_PatchEntities_RejectsNonMaster(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(16), 4)
	master := testpeer.New(1, "master")
	client := testpeer.New(2, "client")
	mustJoin(t, h.room, master, client)

	patch := state.NewMapStatePatch[state.EntityID, state.EntityState, state.EntityStatePatch]()
	patch.Added[state.EntityID(10)] = state.EntityState{
		Owner:      domain.PeerID(2),
		Components: state.NewMapState[state.ComponentID, state.ComponentState](),
	}
	payload, err := h.entitiesCodec.EncodePatch(patch)
	if err != nil {
		t.Fatalf("EncodePatch: %v", err)
	}

	err = h.policy.OnMessage(events.RoomEvent{
		PeerID: domain.PeerID(2),
		Frame:  domain.Frame{OpCode: state.OpCodePatchEntities, Payload: payload},
	})
	if !errors.Is(err, domain.ErrNotMaster) {
		t.Fatalf("err = %v, want ErrNotMaster", err)
	}
}

func TestOnTickFullState_BroadcastsToAllPeers(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(5), 4)
	master := testpeer.New(1, "master")
	client := testpeer.New(2, "client")
	mustJoin(t, h.room, master, client)
	master.Clear()
	client.Clear()

	if err := h.policy.OnTickFullState(); err != nil {
		t.Fatalf("OnTickFullState: %v", err)
	}

	if len(testpeer.FilterByOpcode(master.Snapshot(), state.OpCodeFullState)) != 1 {
		t.Fatal("master should receive periodic full state")
	}
	if len(testpeer.FilterByOpcode(client.Snapshot(), state.OpCodeFullState)) != 1 {
		t.Fatal("client should receive periodic full state")
	}
}

func TestOnTickPatchState_NoChangesNoSend(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(6), 4)
	master := testpeer.New(1, "master")
	client := testpeer.New(2, "client")
	mustJoin(t, h.room, master, client)
	// Converge: the first tick after the join carries the accumulated join delta.
	mustConverge(t, h)
	master.Clear()
	client.Clear()

	if err := h.policy.OnTickPatchState(); err != nil {
		t.Fatalf("OnTickPatchState: %v", err)
	}

	if len(master.Snapshot()) != 0 || len(client.Snapshot()) != 0 {
		t.Fatalf("expected no wire traffic on idle tick, master=%d client=%d",
			len(master.Snapshot()), len(client.Snapshot()))
	}
}

func TestOnMessage_RpcTargetAll_BroadcastsExcludingSender(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(8), 4)
	master := testpeer.New(1, "master")
	client := testpeer.New(2, "client")
	sender := testpeer.New(3, "sender")
	mustJoin(t, h.room, master, client, sender)
	master.Clear()
	client.Clear()
	sender.Clear()

	payload := mustEncodeRPC(t, state.RpcTargetAll, domain.PeerIDInvalid)
	if err := h.policy.OnMessage(events.RoomEvent{
		PeerID: domain.PeerID(3),
		Frame:  domain.Frame{OpCode: state.OpCodeRpc, Payload: payload},
	}); err != nil {
		t.Fatalf("OnMessage rpc all: %v", err)
	}

	if len(testpeer.FilterByOpcode(sender.Snapshot(), state.OpCodeRpc)) != 0 {
		t.Fatal("sender should not receive own rpc")
	}
	if len(testpeer.FilterByOpcode(master.Snapshot(), state.OpCodeRpc)) != 1 {
		t.Fatal("master should receive rpc broadcast")
	}
	if len(testpeer.FilterByOpcode(client.Snapshot(), state.OpCodeRpc)) != 1 {
		t.Fatal("client should receive rpc broadcast")
	}
}

func TestOnMessage_RpcTargetPeer_UnicastsToTarget(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(9), 4)
	master := testpeer.New(1, "master")
	client := testpeer.New(2, "client")
	mustJoin(t, h.room, master, client)
	master.Clear()
	client.Clear()

	payload := mustEncodeRPC(t, state.RpcTargetPeer, domain.PeerID(1))
	if err := h.policy.OnMessage(events.RoomEvent{
		PeerID: domain.PeerID(2),
		Frame:  domain.Frame{OpCode: state.OpCodeRpc, Payload: payload},
	}); err != nil {
		t.Fatalf("OnMessage rpc peer: %v", err)
	}

	if len(testpeer.FilterByOpcode(master.Snapshot(), state.OpCodeRpc)) != 1 {
		t.Fatal("master should receive targeted rpc")
	}
	if len(testpeer.FilterByOpcode(client.Snapshot(), state.OpCodeRpc)) != 0 {
		t.Fatal("client should not receive targeted rpc to master")
	}
}

func TestOnMessage_RpcTargetMaster_UsesServerMasterNotPayloadPeerID(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(10), 4)
	master := testpeer.New(1, "master")
	client := testpeer.New(2, "client")
	mustJoin(t, h.room, master, client)
	master.Clear()
	client.Clear()

	payload := mustEncodeRPC(t, state.RpcTargetMaster, domain.PeerID(2))
	if err := h.policy.OnMessage(events.RoomEvent{
		PeerID: domain.PeerID(2),
		Frame:  domain.Frame{OpCode: state.OpCodeRpc, Payload: payload},
	}); err != nil {
		t.Fatalf("OnMessage rpc master: %v", err)
	}

	if len(testpeer.FilterByOpcode(master.Snapshot(), state.OpCodeRpc)) != 1 {
		t.Fatal("master should receive rpc to master target")
	}
}

func TestOnMessage_Rpc_InvalidTarget_ReturnsError(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(11), 4)
	master := testpeer.New(1, "master")
	mustJoin(t, h.room, master)

	payload := mustEncodeRPC(t, state.RpcTargetNone, domain.PeerIDInvalid)
	err := h.policy.OnMessage(events.RoomEvent{
		PeerID: domain.PeerID(1),
		Frame:  domain.Frame{OpCode: state.OpCodeRpc, Payload: payload},
	})
	if !errors.Is(err, domain.ErrInvalidRpcTarget) {
		t.Fatalf("err = %v, want ErrInvalidRpcTarget", err)
	}
}

func TestOnMessage_Rpc_TargetPeerNotInRoom_ReturnsError(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(12), 4)
	master := testpeer.New(1, "master")
	mustJoin(t, h.room, master)

	payload := mustEncodeRPC(t, state.RpcTargetPeer, domain.PeerID(99))
	err := h.policy.OnMessage(events.RoomEvent{
		PeerID: domain.PeerID(1),
		Frame:  domain.Frame{OpCode: state.OpCodeRpc, Payload: payload},
	})
	if !errors.Is(err, domain.ErrRpcTargetPeerNotFound) {
		t.Fatalf("err = %v, want ErrRpcTargetPeerNotFound", err)
	}
}

func TestOnMessage_UnknownOpcode_ReturnsError(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(13), 4)
	master := testpeer.New(1, "master")
	mustJoin(t, h.room, master)

	err := h.policy.OnMessage(events.RoomEvent{
		PeerID: domain.PeerID(1),
		Frame:  domain.Frame{OpCode: domain.OpCode(0x99)},
	})
	if !errors.Is(err, domain.ErrInRoomUnknownOpcode) {
		t.Fatalf("err = %v, want ErrInRoomUnknownOpcode", err)
	}
}

// The client's input delta reaches everyone on a patch tick — the sender
// also gets an echo of its own patch (universal broadcast).
func TestOnMessage_PatchInput_PatchReachesAllPeersOnTick(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(7), 4)
	master := testpeer.New(1, "master")
	client := testpeer.New(2, "client")
	mustJoin(t, h.room, master, client)
	mustConverge(t, h)
	master.Clear()
	client.Clear()

	inputPatch := &state.InputStatePatch{
		Values: *state.NewMapStatePatch[state.ValueId, state.ValueState, state.ValueState](),
	}
	inputPatch.Values.Added[state.ValueId(1)] = state.ValueState([]byte{0xAB})
	payload, err := h.inputCodec.EncodePatch(inputPatch)
	if err != nil {
		t.Fatalf("EncodePatch input: %v", err)
	}

	if err := h.policy.OnMessage(events.RoomEvent{
		PeerID: domain.PeerID(2),
		Frame:  domain.Frame{OpCode: state.OpCodePatchInput, Payload: payload},
	}); err != nil {
		t.Fatalf("OnMessage patch input: %v", err)
	}
	if len(master.Snapshot()) != 0 || len(client.Snapshot()) != 0 {
		t.Fatal("no wire traffic expected before tick")
	}

	if err := h.policy.OnTickPatchState(); err != nil {
		t.Fatalf("OnTickPatchState: %v", err)
	}
	for _, p := range []*testpeer.Peer{master, client} {
		patches := testpeer.FilterByOpcode(p.Snapshot(), state.OpCodePatchState)
		if len(patches) != 1 {
			t.Fatalf("peer %d patches = %d, want 1", p.GetID(), len(patches))
		}
		got, err := h.roomCodec.DecodePatch(patches[0].Frame.Payload)
		if err != nil {
			t.Fatalf("DecodePatch peer %d: %v", p.GetID(), err)
		}
		added, ok := got.Inputs.Added[domain.PeerID(2)]
		if !ok {
			t.Fatalf("peer %d inputs.added = %#v, want sender input", p.GetID(), got.Inputs.Added)
		}
		if v := added.Values[state.ValueId(1)]; len(v) != 1 || v[0] != 0xAB {
			t.Fatalf("peer %d input value = %v, want [0xAB]", p.GetID(), []byte(v))
		}
	}
}

// --- harness ---

type harness struct {
	room          *realtime.Room
	policy        portpolicy.RoomPolicy
	roomCodec     *codec.RoomStateCodec
	entitiesCodec *codec.EntitiesStateCodec
	inputCodec    *codec.InputStateCodec
}

func newHarness(t *testing.T, roomID domain.RoomID, capacity int) *harness {
	t.Helper()

	logger := stdlib.New("state-policy-test")
	policy := statepolicy.NewStateRoomPolicy(
		logger,
		codec.NewRoomStateCodec(),
		codec.NewEntitiesStateCodec(),
		codec.NewInputStateCodec(),
		codec.NewRpcStateCodec(),
		time.Hour,
		time.Hour,
	)
	room := realtime.NewRoom(context.Background(), roomID, policy, capacity, nil, 0, logger)
	if err := room.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = room.Stop() })

	return &harness{
		room:          room,
		policy:        policy,
		roomCodec:     codec.NewRoomStateCodec(),
		entitiesCodec: codec.NewEntitiesStateCodec(),
		inputCodec:    codec.NewInputStateCodec(),
	}
}

func mustJoin(t *testing.T, room *realtime.Room, peers ...*testpeer.Peer) {
	t.Helper()
	for _, p := range peers {
		if err := room.Join(p); err != nil {
			t.Fatalf("Join peer %d: %v", p.GetID(), err)
		}
	}
}

// mustConverge — one patch tick: flushes the accumulated delta (join/leave/
// message mutations, since sends now only happen on the tick, P34) and
// commits prevState — afterwards each tick carries only the fresh delta.
func mustConverge(t *testing.T, h *harness) {
	t.Helper()
	if err := h.policy.OnTickPatchState(); err != nil {
		t.Fatalf("OnTickPatchState converge: %v", err)
	}
}

// pingUnknown — an uninitialized ping (RTT not yet measured): in an
// election such a candidate loses to any peer with a known ping.
const pingUnknown = int64(-1)

func mustEncodeRPC(t *testing.T, target state.RpcTarget, peerID domain.PeerID) []byte {
	t.Helper()
	c := codec.NewRpcStateCodec()
	payload, err := c.Encode(&state.RpcState{
		ID:     state.RpcID(1),
		Target: target,
		Values: state.NewMapState[state.ValueId, state.ValueState](),
		PeerID: peerID,
	})
	if err != nil {
		t.Fatalf("Encode rpc: %v", err)
	}
	return payload
}
