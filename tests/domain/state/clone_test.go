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

// Snapshot — a shallow snapshot: values are shared with the state and stay
// correct under COW mutations of the state (clone-then-replace of entries,
// point updates of PeerState structs, delete).
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
	frozen := snap.Clone() // reference: what the snapshot looked like when taken

	// COW mutations of the state in the style of the policy paths:
	// clone-then-replace of the input, point replacement of a PeerState
	// (ping/IsMaster), delete of entries, replacement of the entities map —
	// none of them must touch the snapshot.
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

// COW invariant of ApplyPatch (P34): a patch of an Inputs entry must not
// mutate the Values map of the stored entry — apply works on a clone.
func TestRoomState_ApplyPatch_inputsDoesNotMutateSharedValues(t *testing.T) {
	t.Parallel()

	room := state.NewRoomState(domain.RoomID(1), 4)
	room.Inputs[domain.PeerID(1)] = state.InputState{Values: state.MapState[state.ValueId, state.ValueState]{
		state.ValueId(1): state.ValueState([]byte{1}),
	}}

	// The snapshot shares the entry's Values map — if ApplyPatch mutates it
	// in place, the snapshot will see the "future" value.
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
