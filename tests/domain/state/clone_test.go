package state_test

import (
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

func TestRoomState_Clone_isolatesNestedMaps(t *testing.T) {
	t.Parallel()

	room := state.NewRoomState(domain.RoomID(1), 4)
	room.Inputs[domain.PeerID(2)] = state.InputState{
		Values: state.NewMapState[state.ValueId, state.ValueState](),
	}
	room.Inputs[domain.PeerID(2)].Values[state.ValueId(1)] = state.ValueState([]byte{1})

	snap := room.Clone()
	room.Inputs[domain.PeerID(2)].Values[state.ValueId(1)] = state.ValueState([]byte{9})

	want := state.ValueState([]byte{1})
	got := snap.Inputs[domain.PeerID(2)].Values[state.ValueId(1)]
	if !got.Equals(&want) {
		t.Fatalf("snapshot value = %v, want %v", []byte(got), []byte(want))
	}
}

// Snapshot — shallow-снапшот: значения разделяются со state и остаются
// корректными при COW-мутациях state (clone-then-replace записей,
// point-апдейты PeerState-структур, delete).
func TestRoomState_Snapshot_isolatedByCOWMutations(t *testing.T) {
	t.Parallel()

	room := state.NewRoomState(domain.RoomID(1), 4)
	room.Peers[domain.PeerID(1)] = state.PeerState{NickName: "a", IsMaster: true, Ping: 5}
	room.Peers[domain.PeerID(2)] = state.PeerState{NickName: "b", Ping: 7}
	room.Inputs[domain.PeerID(2)] = state.InputState{Values: state.MapState[state.ValueId, state.ValueState]{
		state.ValueId(1): state.ValueState([]byte{1}),
	}}
	room.Entities[state.EntityID(10)] = state.EntityState{
		Owner: domain.PeerID(1),
		Components: state.MapState[state.ComponentID, state.ComponentState]{
			state.ComponentID(1): {Values: state.MapState[state.ValueId, state.ValueState]{
				state.ValueId(1): state.ValueState([]byte{2}),
			}},
		},
	}

	snap := room.Snapshot()
	frozen := snap.Clone() // эталон: каким снапшот был на момент снятия

	// COW-мутации state в стиле policy-путей: clone-then-replace input,
	// point-замена PeerState (ping/IsMaster), delete записей, замена
	// entities-мапы — ни одна не должна тронуть снапшот.
	input := room.Inputs[domain.PeerID(2)].Clone()
	input.Values[state.ValueId(1)] = state.ValueState([]byte{9})
	room.Inputs[domain.PeerID(2)] = input

	ps := room.Peers[domain.PeerID(1)]
	ps.Ping = 42
	room.Peers[domain.PeerID(1)] = ps

	delete(room.Peers, domain.PeerID(2))
	delete(room.Inputs, domain.PeerID(2))
	room.Entities = state.NewMapState[state.EntityID, state.EntityState]()
	room.Capacity = 8

	snapInput := snap.Inputs[domain.PeerID(2)]
	frozenInput := frozen.Inputs[domain.PeerID(2)]
	if !snapInput.Equals(&frozenInput, func(a, b state.ValueState) bool {
		return a.Equals(&b)
	}) {
		t.Fatalf("snapshot input drifted: %#v", snapInput)
	}
	if !snap.Peers.Equals(frozen.Peers, func(a, b state.PeerState) bool { return a.Equals(&b) }) {
		t.Fatalf("snapshot peers drifted: %#v", snap.Peers)
	}
	if !snap.Entities.Equals(frozen.Entities, func(a, b state.EntityState) bool {
		return a.Equals(&b, func(c1, c2 state.ComponentState) bool {
			return c1.Equals(&c2, func(v1, v2 state.ValueState) bool { return v1.Equals(&v2) })
		})
	}) {
		t.Fatalf("snapshot entities drifted: %#v", snap.Entities)
	}
	if snap.Capacity != frozen.Capacity || snap.ID != frozen.ID {
		t.Fatalf("snapshot scalars drifted: %d/%d", snap.Capacity, snap.ID)
	}
}

// COW-инвариант ApplyPatch (P34): patch Inputs-записи не должен
// мутировать Values-мапу хранимой записи — apply работает на клоне.
func TestRoomState_ApplyPatch_inputsDoesNotMutateSharedValues(t *testing.T) {
	t.Parallel()

	room := state.NewRoomState(domain.RoomID(1), 4)
	room.Inputs[domain.PeerID(1)] = state.InputState{Values: state.MapState[state.ValueId, state.ValueState]{
		state.ValueId(1): state.ValueState([]byte{1}),
	}}

	// Снапшот шарит Values-мапу записи — если ApplyPatch мутирует её
	// in-place, снапшот увидит «будущее» значение.
	snap := room.Snapshot()

	patch := state.RoomStatePatch{}
	patch.Inputs.Updated = map[domain.PeerID]state.InputStatePatch{
		domain.PeerID(1): {
			Values: state.MapStatePatch[state.ValueId, state.ValueState, state.ValueState]{
				Updated: map[state.ValueId]state.ValueState{
					state.ValueId(1): state.ValueState([]byte{9}),
				},
			},
		},
	}
	if err := room.ApplyPatch(patch); err != nil {
		t.Fatalf("ApplyPatch: %v", err)
	}

	want := state.ValueState([]byte{9})
	if got := room.Inputs[domain.PeerID(1)].Values[state.ValueId(1)]; !got.Equals(&want) {
		t.Fatalf("applied input value = %v, want %v", []byte(got), []byte(want))
	}
	old := state.ValueState([]byte{1})
	if got := snap.Inputs[domain.PeerID(1)].Values[state.ValueId(1)]; !got.Equals(&old) {
		t.Fatalf("snapshot input value mutated to %v — ApplyPatch violated COW", []byte(got))
	}
}

func TestRoomState_Clone_MakePatch_afterInputPatch(t *testing.T) {
	t.Parallel()

	cur := state.NewRoomState(domain.RoomID(1), 4)
	cur.Inputs[domain.PeerID(1)] = state.InputState{
		Values: state.NewMapState[state.ValueId, state.ValueState](),
	}
	cur.Inputs[domain.PeerID(1)].Values[state.ValueId(1)] = state.ValueState([]byte{1})

	old := cur.Clone()
	next := cur.Clone()
	patch := state.InputStatePatch{
		Values: state.MapStatePatch[state.ValueId, state.ValueState, state.ValueState]{
			Updated: map[state.ValueId]state.ValueState{
				state.ValueId(1): state.ValueState([]byte{2}),
			},
		},
	}
	input := next.Inputs[domain.PeerID(1)]
	if err := input.ApplyPatch(patch); err != nil {
		t.Fatalf("ApplyPatch: %v", err)
	}
	next.Inputs[domain.PeerID(1)] = input

	roomPatch, err := old.MakePatch(next)
	if err != nil {
		t.Fatalf("MakePatch: %v", err)
	}
	if roomPatch == nil || len(roomPatch.Inputs.Updated) != 1 {
		t.Fatalf("expected input update patch, got %#v", roomPatch)
	}
}
