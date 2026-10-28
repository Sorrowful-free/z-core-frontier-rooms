package state_test

// Scaling-бенчи state-пути (P34): стоимость OnMessage/Clone/MakePatch
// в зависимости от размера Entities — ловит регресс O(|state|) на
// горячем пути (tick-coalesced эмиссия дала O(1) на входящее сообщение).
//
// NB: payload'ы чередуются (два значения) — иначе после первой итерации
// state не меняется и замер уходит в nil-patch/error-ветку.

import (
	"fmt"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	testpeer "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/peer"
)

// buildEntities — N сущностей по C компонентов и V значений (байты 8).
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

// seedEntities — гоняет FullEntities от master'а через policy, чтобы
// p.state.Entities наполнился как в проде.
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

// seedInput — кладёт Inputs[peerID] с ключом 1 через FullInput, чтобы
// Updated-патчи дальше шли по валидному пути.
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

// BenchmarkScaling_PatchInputVsEntities — PatchInput от клиента при
// N сущностях в state: per-message путь платит 2×Clone + полный diff
// по ВСЕМ entities, хотя меняется один input.
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

// BenchmarkScaling_PatchEntitiesVsEntities — PatchEntities от master'а
// (одна entity-патчем) при N сущностях: тройной clone горячего пути.
func BenchmarkScaling_PatchEntitiesVsEntities(b *testing.B) {
	// 0 пропускаем: Updated-патч требует существующую сущность.
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

// BenchmarkScaling_RoomStateClone — чистая стоимость Clone() по размеру state.
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

// BenchmarkScaling_RoomStateMakePatch — MakePatch(state, clone) без
// изменений: нижняя граница стоимости полного diff-sweep'а.
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
