package state_test

import (
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

func TestRoomState_Clone_isolatesNestedMaps(t *testing.T) {
	t.Parallel()

	room := state.NewRoomState(domain.RoomID(1), 4, "")
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

func TestRoomState_Clone_MakePatch_afterInputPatch(t *testing.T) {
	t.Parallel()

	cur := state.NewRoomState(domain.RoomID(1), 4, "")
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
