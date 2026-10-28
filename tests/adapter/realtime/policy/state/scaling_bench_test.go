package state_test

// Scaling benches of the state path (P34): the cost of
// OnMessage/Clone/MakePatch as a function of the Entities size — catches
// an O(|state|) regression on the hot path (tick-coalesced emission gave
// O(1) per incoming message).
//
// NB: the payloads alternate (two values) — otherwise after the first
// iteration the state does not change and the measurement slips into the
// nil-patch/error branch.

import (
	"fmt"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	testpeer "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/peer"
)

// buildEntities — N entities with C components and V values each (8 bytes).
func buildEntities(n, comps, vals int) state.EntitiesState {
	entities := state.NewMapState[state.EntityID, state.EntityState]()
	for i := 1; i <= n; i++ {
		components := state.NewMapState[state.ComponentID, state.ComponentState]()
		for c := 1; c <= comps; c++ {
			values := state.NewMapState[state.ValueId, state.ValueState]()
			for v := 1; v <= vals; v++ {
				values[state.ValueId(v)] = state.ValueState([]byte{byte(i), byte(c), byte(v), 1, 2, 3, 4, 5})
			}
			components[state.ComponentID(c)] = state.ComponentState{Values: values}
		}
		entities[state.EntityID(i)] = state.EntityState{
			EntityTypeID: state.EntityTypeID(1),
			Owner:        domain.PeerID(1),
			Components:   components,
		}
	}
	return entities
}

// seedEntities — pushes FullEntities from the master through the policy so
// that p.state.Entities fills up as in production.
func seedEntities(b *testing.B, h *benchHarness, entities state.EntitiesState) {
	b.Helper()
	payload, err := h.entitiesCodec.Encode(entities)
	if err != nil {
		b.Fatalf("Encode entities: %v", err)
	}
	if err := h.policy.OnMessage(events.RoomEvent{
		PeerID: domain.PeerID(1),
		Frame:  domain.Frame{OpCode: state.OpCodeFullEntities, Payload: payload},
	}); err != nil {
		b.Fatalf("seed full entities: %v", err)
	}
}

// seedInput — puts Inputs[peerID] with key 1 via FullInput so that
// subsequent Updated patches take the valid path.
func seedInput(b *testing.B, h *benchHarness, peerID domain.PeerID) {
	b.Helper()
	full := &state.InputState{Values: state.MapState[state.ValueId, state.ValueState]{
		state.ValueId(1): state.ValueState([]byte{0}),
	}}
	payload, err := h.inputCodec.Encode(full)
	if err != nil {
		b.Fatalf("Encode input seed: %v", err)
	}
	if err := h.policy.OnMessage(events.RoomEvent{
		PeerID: peerID,
		Frame:  domain.Frame{OpCode: state.OpCodeFullInput, Payload: payload},
	}); err != nil {
		b.Fatalf("seed full input: %v", err)
	}
}

func inputUpdatePayload(b *testing.B, h *benchHarness, b0, b1 byte) []byte {
	b.Helper()
	patch := &state.InputStatePatch{
		Values: *state.NewMapStatePatch[state.ValueId, state.ValueState, state.ValueState](),
	}
	patch.Values.Updated[state.ValueId(1)] = state.ValueState([]byte{b0, b1})
	payload, err := h.inputCodec.EncodePatch(patch)
	if err != nil {
		b.Fatalf("EncodePatch input: %v", err)
	}
	return payload
}

func entitiesUpdatePayload(b *testing.B, h *benchHarness, b0 byte) []byte {
	b.Helper()
	valuePatch := state.NewMapStatePatch[state.ValueId, state.ValueState, state.ValueState]()
	valuePatch.Updated[state.ValueId(1)] = state.ValueState([]byte{b0, b0, b0})
	compPatch := state.NewMapStatePatch[state.ComponentID, state.ComponentState, state.ComponentStatePatch]()
	compPatch.Updated[state.ComponentID(1)] = state.ComponentStatePatch{Values: *valuePatch}
	patch := state.NewMapStatePatch[state.EntityID, state.EntityState, state.EntityStatePatch]()
	patch.Updated[state.EntityID(1)] = state.EntityStatePatch{Components: *compPatch}
	payload, err := h.entitiesCodec.EncodePatch(patch)
	if err != nil {
		b.Fatalf("EncodePatch entities: %v", err)
	}
	return payload
}

// BenchmarkScaling_PatchInputVsEntities — PatchInput from a client with
// N entities in the state: the per-message path pays 2×Clone + a full diff
// over ALL entities even though only one input changes.
func BenchmarkScaling_PatchInputVsEntities(b *testing.B) {
	for _, entCount := range []int{0, 50, 200, 500} {
		b.Run(fmt.Sprintf("entities=%d", entCount), func(b *testing.B) {
			h := newBenchHarness(b, domain.RoomID(100), 16)
			master := testpeer.New(1, "master", testpeer.WithoutCapture())
			client := testpeer.New(2, "client", testpeer.WithoutCapture())
			mustJoinBench(b, h.room, master, client)
			seedEntities(b, h, buildEntities(entCount, 2, 4))
			seedInput(b, h, domain.PeerID(2))

			evs := [2]events.RoomEvent{
				{PeerID: domain.PeerID(2), Frame: domain.Frame{OpCode: state.OpCodePatchInput,
					Payload: inputUpdatePayload(b, h, 0xA0, 0x01)}},
				{PeerID: domain.PeerID(2), Frame: domain.Frame{OpCode: state.OpCodePatchInput,
					Payload: inputUpdatePayload(b, h, 0xB0, 0x02)}},
			}
			i := 0
			b.ReportAllocs()
			b.ResetTimer()
			var sink error
			for b.Loop() {
				i++
				sink = h.policy.OnMessage(evs[i&1])
				if sink != nil {
					b.Fatalf("OnMessage: %v", sink)
				}
			}
			_ = sink
		})
	}
}

// BenchmarkScaling_PatchEntitiesVsEntities — PatchEntities from the master
// (one entity per patch) with N entities: triple clone on the hot path.
func BenchmarkScaling_PatchEntitiesVsEntities(b *testing.B) {
	// Skip 0: an Updated patch requires an existing entity.
	for _, entCount := range []int{50, 200, 500} {
		b.Run(fmt.Sprintf("entities=%d", entCount), func(b *testing.B) {
			h := newBenchHarness(b, domain.RoomID(101), 16)
			master := testpeer.New(1, "master", testpeer.WithoutCapture())
			client := testpeer.New(2, "client", testpeer.WithoutCapture())
			mustJoinBench(b, h.room, master, client)
			seedEntities(b, h, buildEntities(entCount, 2, 4))

			evs := [2]events.RoomEvent{
				{PeerID: domain.PeerID(1), Frame: domain.Frame{OpCode: state.OpCodePatchEntities,
					Payload: entitiesUpdatePayload(b, h, 0xC0)}},
				{PeerID: domain.PeerID(1), Frame: domain.Frame{OpCode: state.OpCodePatchEntities,
					Payload: entitiesUpdatePayload(b, h, 0xD0)}},
			}
			i := 0
			b.ReportAllocs()
			b.ResetTimer()
			var sink error
			for b.Loop() {
				i++
				sink = h.policy.OnMessage(evs[i&1])
				if sink != nil {
					b.Fatalf("OnMessage: %v", sink)
				}
			}
			_ = sink
		})
	}
}

// BenchmarkScaling_RoomStateClone — the pure cost of Clone() vs state size.
func BenchmarkScaling_RoomStateClone(b *testing.B) {
	for _, entCount := range []int{0, 50, 200, 500} {
		b.Run(fmt.Sprintf("entities=%d", entCount), func(b *testing.B) {
			rs := state.NewRoomState(domain.RoomID(1), 16)
			for i := 1; i <= 8; i++ {
				rs.Peers[domain.PeerID(i)] = state.PeerState{NickName: "peer", Ping: int64(i)}
				rs.Inputs[domain.PeerID(i)] = state.InputState{Values: state.MapState[state.ValueId, state.ValueState]{
					state.ValueId(1): state.ValueState([]byte{1, 2, 3}),
				}}
			}
			rs.Entities = buildEntities(entCount, 2, 4)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				_ = rs.Clone()
			}
		})
	}
}

// BenchmarkScaling_RoomStateMakePatch — MakePatch(state, clone) with no
// changes: the lower bound of a full diff sweep.
func BenchmarkScaling_RoomStateMakePatch(b *testing.B) {
	for _, entCount := range []int{0, 50, 200, 500} {
		b.Run(fmt.Sprintf("entities=%d", entCount), func(b *testing.B) {
			rs := state.NewRoomState(domain.RoomID(1), 16)
			for i := 1; i <= 8; i++ {
				rs.Peers[domain.PeerID(i)] = state.PeerState{NickName: "peer", Ping: int64(i)}
				rs.Inputs[domain.PeerID(i)] = state.InputState{Values: state.MapState[state.ValueId, state.ValueState]{
					state.ValueId(1): state.ValueState([]byte{1, 2, 3}),
				}}
			}
			rs.Entities = buildEntities(entCount, 2, 4)
			other := rs.Clone()
			b.ReportAllocs()
			b.ResetTimer()
			var sink *state.RoomStatePatch
			for b.Loop() {
				sink, _ = rs.MakePatch(other)
			}
			_ = sink
		})
	}
}
