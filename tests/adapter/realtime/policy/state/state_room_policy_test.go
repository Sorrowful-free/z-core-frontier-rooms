package state_test

import (
	"context"
	"errors"
	"sync"
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
)

func TestOnJoin_FirstPeerGetsFullStateWithRoomMetadata(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(42), 8)
	master := newCapturePeer(1, "master")

	if err := h.room.Join(master); err != nil {
		t.Fatalf("Join master: %v", err)
	}

	events := master.snapshot()
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

func TestOnJoin_SecondPeerGetsFullAndMasterGetsPatch(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(1), 4)
	master := newCapturePeer(1, "master")
	client := newCapturePeer(2, "client")

	if err := h.room.Join(master); err != nil {
		t.Fatalf("Join master: %v", err)
	}
	master.clear()

	if err := h.room.Join(client); err != nil {
		t.Fatalf("Join client: %v", err)
	}

	masterEvents := master.snapshot()
	clientEvents := client.snapshot()

	if len(clientEvents) != 1 || clientEvents[0].Frame.OpCode != state.OpCodeFullState {
		t.Fatalf("client should receive single full state, got %#v", opcodes(clientEvents))
	}

	patches := filterOpcode(masterEvents, state.OpCodePatchState)
	if len(patches) != 1 {
		t.Fatalf("master should receive one patch, got opcodes %v", opcodes(masterEvents))
	}
	patch, err := h.roomCodec.DecodePatch(patches[0].Frame.Payload)
	if err != nil {
		t.Fatalf("DecodePatch: %v", err)
	}
	if _, ok := patch.Peers.Added[domain.PeerID(2)]; !ok {
		t.Fatalf("patch peers.added = %#v", patch.Peers.Added)
	}

	if len(filterOpcode(clientEvents, state.OpCodePatchState)) != 0 {
		t.Fatal("joiner should not receive join patch (already has full)")
	}
}

func TestOnMessage_FullEntities_BroadcastsPatchExcludingMaster(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(3), 8)
	master := newCapturePeer(1, "master")
	client := newCapturePeer(2, "client")

	mustJoin(t, h.room, master, client)
	master.clear()
	client.clear()

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

	clientPatches := filterOpcode(client.snapshot(), state.OpCodePatchState)
	if len(clientPatches) != 1 {
		t.Fatalf("client patches = %d, want 1", len(clientPatches))
	}
	patch, err := h.roomCodec.DecodePatch(clientPatches[0].Frame.Payload)
	if err != nil {
		t.Fatalf("DecodePatch: %v", err)
	}
	if _, ok := patch.Entities.Added[state.EntityID(10)]; !ok {
		t.Fatalf("entities.added = %#v", patch.Entities.Added)
	}

	if len(filterOpcode(master.snapshot(), state.OpCodePatchState)) != 0 {
		t.Fatal("master should not receive entities patch")
	}
}

func TestOnMessage_FullEntities_RejectsNonMaster(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(4), 4)
	master := newCapturePeer(1, "master")
	client := newCapturePeer(2, "client")
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

func TestOnTickFullState_BroadcastsToAllPeers(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(5), 4)
	master := newCapturePeer(1, "master")
	client := newCapturePeer(2, "client")
	mustJoin(t, h.room, master, client)
	master.clear()
	client.clear()

	if err := h.policy.OnTickFullState(); err != nil {
		t.Fatalf("OnTickFullState: %v", err)
	}

	if len(filterOpcode(master.snapshot(), state.OpCodeFullState)) != 1 {
		t.Fatal("master should receive periodic full state")
	}
	if len(filterOpcode(client.snapshot(), state.OpCodeFullState)) != 1 {
		t.Fatal("client should receive periodic full state")
	}
}

func TestOnTickPatchState_NoChangesNoSend(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(6), 4)
	master := newCapturePeer(1, "master")
	client := newCapturePeer(2, "client")
	mustJoin(t, h.room, master, client)
	master.clear()
	client.clear()

	if err := h.policy.OnTickPatchState(); err != nil {
		t.Fatalf("OnTickPatchState: %v", err)
	}

	if len(master.snapshot()) != 0 || len(client.snapshot()) != 0 {
		t.Fatalf("expected no wire traffic on idle tick, master=%d client=%d",
			len(master.snapshot()), len(client.snapshot()))
	}
}

func TestOnMessage_RpcTargetAll_BroadcastsExcludingSender(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(8), 4)
	master := newCapturePeer(1, "master")
	client := newCapturePeer(2, "client")
	sender := newCapturePeer(3, "sender")
	mustJoin(t, h.room, master, client, sender)
	master.clear()
	client.clear()
	sender.clear()

	payload := mustEncodeRPC(t, state.RpcTargetAll, domain.PeerIDInvalid)
	if err := h.policy.OnMessage(events.RoomEvent{
		PeerID: domain.PeerID(3),
		Frame:  domain.Frame{OpCode: state.OpCodeRpc, Payload: payload},
	}); err != nil {
		t.Fatalf("OnMessage rpc all: %v", err)
	}

	if len(filterOpcode(sender.snapshot(), state.OpCodeRpc)) != 0 {
		t.Fatal("sender should not receive own rpc")
	}
	if len(filterOpcode(master.snapshot(), state.OpCodeRpc)) != 1 {
		t.Fatal("master should receive rpc broadcast")
	}
	if len(filterOpcode(client.snapshot(), state.OpCodeRpc)) != 1 {
		t.Fatal("client should receive rpc broadcast")
	}
}

func TestOnMessage_RpcTargetPeer_UnicastsToTarget(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(9), 4)
	master := newCapturePeer(1, "master")
	client := newCapturePeer(2, "client")
	mustJoin(t, h.room, master, client)
	master.clear()
	client.clear()

	payload := mustEncodeRPC(t, state.RpcTargetPeer, domain.PeerID(1))
	if err := h.policy.OnMessage(events.RoomEvent{
		PeerID: domain.PeerID(2),
		Frame:  domain.Frame{OpCode: state.OpCodeRpc, Payload: payload},
	}); err != nil {
		t.Fatalf("OnMessage rpc peer: %v", err)
	}

	if len(filterOpcode(master.snapshot(), state.OpCodeRpc)) != 1 {
		t.Fatal("master should receive targeted rpc")
	}
	if len(filterOpcode(client.snapshot(), state.OpCodeRpc)) != 0 {
		t.Fatal("client should not receive targeted rpc to master")
	}
}

func TestOnMessage_RpcTargetMaster_UsesServerMasterNotPayloadPeerID(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(10), 4)
	master := newCapturePeer(1, "master")
	client := newCapturePeer(2, "client")
	mustJoin(t, h.room, master, client)
	master.clear()
	client.clear()

	payload := mustEncodeRPC(t, state.RpcTargetMaster, domain.PeerID(2))
	if err := h.policy.OnMessage(events.RoomEvent{
		PeerID: domain.PeerID(2),
		Frame:  domain.Frame{OpCode: state.OpCodeRpc, Payload: payload},
	}); err != nil {
		t.Fatalf("OnMessage rpc master: %v", err)
	}

	if len(filterOpcode(master.snapshot(), state.OpCodeRpc)) != 1 {
		t.Fatal("master should receive rpc to master target")
	}
}

func TestOnMessage_Rpc_InvalidTarget_ReturnsError(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(11), 4)
	master := newCapturePeer(1, "master")
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
	master := newCapturePeer(1, "master")
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

func TestOnMessage_PatchInput_BroadcastsToAllIncludingMaster(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(7), 4)
	master := newCapturePeer(1, "master")
	client := newCapturePeer(2, "client")
	mustJoin(t, h.room, master, client)
	master.clear()
	client.clear()

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

	if len(filterOpcode(master.snapshot(), state.OpCodePatchState)) != 1 {
		t.Fatal("master should receive input patch")
	}
	if len(filterOpcode(client.snapshot(), state.OpCodePatchState)) != 1 {
		t.Fatal("client should receive input patch")
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
		codec.NewRoomStateCodec(&logger),
		codec.NewEntitiesStateCodec(),
		codec.NewInputStateCodec(),
		&codec.RpcStateCodec{},
		time.Hour,
		time.Hour,
	)
	room := realtime.NewRoom(context.Background(), roomID, policy, capacity, logger)
	if err := room.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = room.Stop() })

	return &harness{
		room:          room,
		policy:        policy,
		roomCodec:     codec.NewRoomStateCodec(&logger),
		entitiesCodec: codec.NewEntitiesStateCodec(),
		inputCodec:    codec.NewInputStateCodec(),
	}
}

func mustJoin(t *testing.T, room *realtime.Room, peers ...*capturePeer) {
	t.Helper()
	for _, p := range peers {
		if err := room.Join(p); err != nil {
			t.Fatalf("Join peer %d: %v", p.id, err)
		}
	}
}

const pingUnknown = int64(-1)

type capturePeer struct {
	id        domain.PeerID
	nick      string
	ping      int64
	mu        sync.Mutex
	delivered []events.PeerEvent
}

func newCapturePeer(id domain.PeerID, nick string) *capturePeer {
	return newCapturePeerWithPing(id, nick, 0)
}

func newCapturePeerWithPing(id domain.PeerID, nick string, ping int64) *capturePeer {
	return &capturePeer{id: id, nick: nick, ping: ping}
}

func (p *capturePeer) GetID() domain.PeerID { return p.id }
func (p *capturePeer) GetNickName() string  { return p.nick }
func (p *capturePeer) Ping() int64          { return p.ping }
func (p *capturePeer) Start() error            { return nil }
func (p *capturePeer) Stop() error             { return nil }
func (p *capturePeer) Deliver(ev events.PeerEvent) error {
	p.mu.Lock()
	p.delivered = append(p.delivered, ev)
	p.mu.Unlock()
	return nil
}

func (p *capturePeer) snapshot() []events.PeerEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]events.PeerEvent, len(p.delivered))
	copy(out, p.delivered)
	return out
}

func (p *capturePeer) clear() {
	p.mu.Lock()
	p.delivered = nil
	p.mu.Unlock()
}

func filterOpcode(peerEvents []events.PeerEvent, opcode domain.OpCode) []events.PeerEvent {
	var out []events.PeerEvent
	for _, ev := range peerEvents {
		if ev.Frame.OpCode == opcode {
			out = append(out, ev)
		}
	}
	return out
}

func opcodes(peerEvents []events.PeerEvent) []domain.OpCode {
	out := make([]domain.OpCode, len(peerEvents))
	for i, ev := range peerEvents {
		out[i] = ev.Frame.OpCode
	}
	return out
}

func mustEncodeRPC(t *testing.T, target state.RpcTarget, peerID domain.PeerID) []byte {
	t.Helper()
	c := &codec.RpcStateCodec{}
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
