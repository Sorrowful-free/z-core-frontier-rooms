package state_test

import (
	"context"
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

type benchHarness struct {
	room          *realtime.Room
	policy        portpolicy.RoomPolicy
	roomCodec     *codec.RoomStateCodec
	entitiesCodec *codec.EntitiesStateCodec
	inputCodec    *codec.InputStateCodec
}

func newBenchHarness(b *testing.B, roomID domain.RoomID, capacity int) *benchHarness {
	b.Helper()

	logger := stdlib.New("state-policy-bench")
	policy := statepolicy.NewStateRoomPolicy(
		logger,
		codec.NewRoomStateCodec(&logger),
		codec.NewEntitiesStateCodec(),
		codec.NewInputStateCodec(),
		&codec.RpcStateCodec{},
		time.Hour,
		time.Hour,
	)
	room := realtime.NewRoom(context.Background(), roomID, policy, capacity, nil, 0, logger)
	if err := room.Start(); err != nil {
		b.Fatalf("Start: %v", err)
	}
	b.Cleanup(func() { _ = room.Stop() })

	return &benchHarness{
		room:          room,
		policy:        policy,
		roomCodec:     codec.NewRoomStateCodec(&logger),
		entitiesCodec: codec.NewEntitiesStateCodec(),
		inputCodec:    codec.NewInputStateCodec(),
	}
}

func mustJoinBench(b *testing.B, room *realtime.Room, peers ...*benchCapturePeer) {
	b.Helper()
	for _, p := range peers {
		if err := room.Join(p); err != nil {
			b.Fatalf("Join peer %d: %v", p.id, err)
		}
	}
}

type benchCapturePeer struct {
	id        domain.PeerID
	nick      string
	ping      int64
	mu        sync.Mutex
	delivered []events.PeerEvent
}

func newBenchCapturePeer(id domain.PeerID, nick string) *benchCapturePeer {
	return &benchCapturePeer{id: id, nick: nick}
}

func (p *benchCapturePeer) GetID() domain.PeerID { return p.id }
func (p *benchCapturePeer) GetNickName() string  { return p.nick }
func (p *benchCapturePeer) Ping() int64          { return p.ping }
func (p *benchCapturePeer) Start() error         { return nil }
func (p *benchCapturePeer) Stop() error          { return nil }
func (p *benchCapturePeer) Deliver(ev events.PeerEvent) error {
	p.mu.Lock()
	p.delivered = append(p.delivered, ev)
	p.mu.Unlock()
	return nil
}

func (p *benchCapturePeer) clear() {
	p.mu.Lock()
	p.delivered = nil
	p.mu.Unlock()
}

func benchEncodeInputPatch(b *testing.B, inputCodec *codec.InputStateCodec) []byte {
	b.Helper()
	inputPatch := &state.InputStatePatch{
		Values: *state.NewMapStatePatch[state.ValueId, state.ValueState, state.ValueState](),
	}
	inputPatch.Values.Added[state.ValueId(1)] = state.ValueState([]byte{0xAB})
	payload, err := inputCodec.EncodePatch(inputPatch)
	if err != nil {
		b.Fatalf("EncodePatch input: %v", err)
	}
	return payload
}

func benchEncodeEntitiesAddPatch(b *testing.B, entitiesCodec *codec.EntitiesStateCodec) []byte {
	b.Helper()

	components := state.NewMapState[state.ComponentID, state.ComponentState]()
	components[state.ComponentID(1)] = state.ComponentState{
		Values: state.NewMapState[state.ValueId, state.ValueState](),
	}
	components[state.ComponentID(1)].Values[state.ValueId(1)] = state.ValueState(make([]byte, 16))

	patch := state.NewMapStatePatch[state.EntityID, state.EntityState, state.EntityStatePatch]()
	patch.Added[state.EntityID(10)] = state.EntityState{
		Owner:      domain.PeerID(1),
		Components: components,
	}

	payload, err := entitiesCodec.EncodePatch(patch)
	if err != nil {
		b.Fatalf("EncodePatch entities add: %v", err)
	}
	return payload
}

func benchEncodeEntitiesUpdatePatch(b *testing.B, entitiesCodec *codec.EntitiesStateCodec) []byte {
	b.Helper()

	valuePatch := state.NewMapStatePatch[state.ValueId, state.ValueState, state.ValueState]()
	valuePatch.Updated[state.ValueId(1)] = state.ValueState([]byte{0xFF})

	compPatch := state.NewMapStatePatch[state.ComponentID, state.ComponentState, state.ComponentStatePatch]()
	compPatch.Updated[state.ComponentID(1)] = state.ComponentStatePatch{
		Values: *valuePatch,
	}

	patch := state.NewMapStatePatch[state.EntityID, state.EntityState, state.EntityStatePatch]()
	patch.Updated[state.EntityID(10)] = state.EntityStatePatch{
		Components: *compPatch,
	}

	payload, err := entitiesCodec.EncodePatch(patch)
	if err != nil {
		b.Fatalf("EncodePatch entities update: %v", err)
	}
	return payload
}

func benchSeedEntityForUpdate(b *testing.B, h *benchHarness, master *benchCapturePeer) {
	b.Helper()

	entities := state.NewMapState[state.EntityID, state.EntityState]()
	components := state.NewMapState[state.ComponentID, state.ComponentState]()
	components[state.ComponentID(1)] = state.ComponentState{
		Values: state.NewMapState[state.ValueId, state.ValueState](),
	}
	components[state.ComponentID(1)].Values[state.ValueId(1)] = state.ValueState([]byte{0x01})
	entities[state.EntityID(10)] = state.EntityState{
		Owner:      domain.PeerID(1),
		Components: components,
	}

	payload, err := h.entitiesCodec.Encode(entities)
	if err != nil {
		b.Fatalf("Encode entities seed: %v", err)
	}
	if err := h.policy.OnMessage(events.RoomEvent{
		PeerID: domain.PeerID(1),
		Frame: domain.Frame{
			OpCode:  state.OpCodeFullEntities,
			Payload: payload,
		},
	}); err != nil {
		b.Fatalf("OnMessage full entities seed: %v", err)
	}
	master.clear()
}
