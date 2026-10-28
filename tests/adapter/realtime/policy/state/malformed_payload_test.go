package state_test

import (
	"errors"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	testpeer "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/peer"
)

// Every in-room opcode × garbage payload → ErrInRoomInvalidPayload,
// the wire error opcode is OpInRoomInvalidPayload.
func TestOnMessage_GarbagePayload_ReturnsErrInRoomInvalidPayload(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(70), 4)
	master := testpeer.New(1, "master")
	member := testpeer.New(2, "member")
	mustJoin(t, h.room, master, member)

	garbage := []byte{0xAB, 0xCD, 0xEF}
	cases := []struct {
		name   string
		opcode domain.OpCode
		sender domain.PeerID
	}{
		// entities opcodes are master-gated — the sender must be the master.
		{"full entities", state.OpCodeFullEntities, master.GetID()},
		{"patch entities", state.OpCodePatchEntities, master.GetID()},
		{"full input", state.OpCodeFullInput, member.GetID()},
		{"patch input", state.OpCodePatchInput, member.GetID()},
		{"rpc", state.OpCodeRpc, member.GetID()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := h.policy.OnMessage(events.RoomEvent{
				PeerID: tc.sender,
				Frame:  domain.Frame{OpCode: tc.opcode, Payload: garbage},
			})
			if !errors.Is(err, domain.ErrInRoomInvalidPayload) {
				t.Fatalf("err = %v, want ErrInRoomInvalidPayload", err)
			}
			if op := domain.InRoomErrorOpCode(err); op != domain.OpInRoomInvalidPayload {
				t.Fatalf("InRoomErrorOpCode = %#x, want %#x", op, domain.OpInRoomInvalidPayload)
			}
		})
	}
}

// The InputStatePatch wire envelope is valid (DecodePatch succeeds), but
// the inner value patch contradicts the sender's input state — ApplyPatch
// inside onClientPatchInput must wrap the error in ErrInRoomInvalidPayload.
// Soft apply (P34): the only error left is Updated on a missing key —
// Added on an existing key is an upsert, Removed on a missing key is ignored.
func TestOnMessage_PatchInput_UpdatedMissingKey_ReturnsErrInRoomInvalidPayload(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(71), 4)
	master := testpeer.New(1, "master")
	member := testpeer.New(2, "member")
	mustJoin(t, h.room, master, member)

	patch := &state.InputStatePatch{
		Values: *state.NewMapStatePatch[state.ValueId, state.ValueState, state.ValueState](),
	}
	patch.Values.Updated[state.ValueId(7)] = state.ValueState([]byte{0x01})
	payload, err := h.inputCodec.EncodePatch(patch)
	if err != nil {
		t.Fatalf("EncodePatch input: %v", err)
	}
	err = h.policy.OnMessage(events.RoomEvent{
		PeerID: member.GetID(),
		Frame:  domain.Frame{OpCode: state.OpCodePatchInput, Payload: payload},
	})
	if !errors.Is(err, domain.ErrInRoomInvalidPayload) {
		t.Fatalf("err = %v, want ErrInRoomInvalidPayload", err)
	}
	if op := domain.InRoomErrorOpCode(err); op != domain.OpInRoomInvalidPayload {
		t.Fatalf("InRoomErrorOpCode = %#x, want %#x", op, domain.OpInRoomInvalidPayload)
	}
}

// PatchEntities from the master with Updated on a nonexistent entity — the
// same strict branch of soft apply: ErrInRoomInvalidPayload, state untouched.
func TestOnMessage_PatchEntities_UpdatedMissingEntity_ReturnsErrInRoomInvalidPayload(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(72), 4)
	master := testpeer.New(1, "master")
	member := testpeer.New(2, "member")
	mustJoin(t, h.room, master, member)
	shadow := mustJoinShadow(t, h)
	mustConverge(t, h)

	patch := state.NewMapStatePatch[state.EntityID, state.EntityState, state.EntityStatePatch]()
	patch.Updated[state.EntityID(99)] = state.EntityStatePatch{}
	payload, err := h.entitiesCodec.EncodePatch(patch)
	if err != nil {
		t.Fatalf("EncodePatch entities: %v", err)
	}
	err = h.policy.OnMessage(events.RoomEvent{
		PeerID: master.GetID(),
		Frame:  domain.Frame{OpCode: state.OpCodePatchEntities, Payload: payload},
	})
	if !errors.Is(err, domain.ErrInRoomInvalidPayload) {
		t.Fatalf("err = %v, want ErrInRoomInvalidPayload", err)
	}
	if op := domain.InRoomErrorOpCode(err); op != domain.OpInRoomInvalidPayload {
		t.Fatalf("InRoomErrorOpCode = %#x, want %#x", op, domain.OpInRoomInvalidPayload)
	}
	assertWeakInvariants(t, shadow.view)
}

// Soft apply (P34): Added on an existing key is an upsert (the value is
// overwritten), Removed on a missing key is ignored. Needed for the wire
// semantics: a replica may run ahead of the delta stream (full state to the
// joiner + echo of patches to the sender).
func TestOnMessage_PatchInput_SoftApply_UpsertAndIgnore(t *testing.T) {
	t.Parallel()

	newPatch := func(mut func(*state.MapStatePatch[state.ValueId, state.ValueState, state.ValueState])) *state.InputStatePatch {
		patch := &state.InputStatePatch{
			Values: *state.NewMapStatePatch[state.ValueId, state.ValueState, state.ValueState](),
		}
		mut(&patch.Values)
		return patch
	}

	cases := []struct {
		name      string
		seedInput bool
		patch     *state.InputStatePatch
		wantValue state.ValueState // expected Values[1] after apply
	}{
		{
			name: "removed value not in input",
			patch: newPatch(func(p *state.MapStatePatch[state.ValueId, state.ValueState, state.ValueState]) {
				p.Removed = append(p.Removed, state.ValueId(7))
			}),
		},
		{
			name:      "added value already in input",
			seedInput: true,
			patch: newPatch(func(p *state.MapStatePatch[state.ValueId, state.ValueState, state.ValueState]) {
				p.Added[state.ValueId(1)] = state.ValueState([]byte{0xFF})
			}),
			wantValue: state.ValueState([]byte{0xFF}),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, domain.RoomID(73), 4)
			master := testpeer.New(1, "master")
			member := testpeer.New(2, "member")
			mustJoin(t, h.room, master, member)

			if tc.seedInput {
				seed := &state.InputState{Values: state.NewMapState[state.ValueId, state.ValueState]()}
				seed.Values[state.ValueId(1)] = state.ValueState([]byte{0x01})
				payload, err := h.inputCodec.Encode(seed)
				if err != nil {
					t.Fatalf("Encode seed input: %v", err)
				}
				if err := h.policy.OnMessage(events.RoomEvent{
					PeerID: member.GetID(),
					Frame:  domain.Frame{OpCode: state.OpCodeFullInput, Payload: payload},
				}); err != nil {
					t.Fatalf("OnMessage seed input: %v", err)
				}
			}

			payload, err := h.inputCodec.EncodePatch(tc.patch)
			if err != nil {
				t.Fatalf("EncodePatch input: %v", err)
			}
			if err := h.policy.OnMessage(events.RoomEvent{
				PeerID: member.GetID(),
				Frame:  domain.Frame{OpCode: state.OpCodePatchInput, Payload: payload},
			}); err != nil {
				t.Fatalf("OnMessage patch input: %v (soft apply не должен фейлить)", err)
			}

			master.Clear()
			if err := h.policy.OnTickFullState(); err != nil {
				t.Fatalf("OnTickFullState: %v", err)
			}
			full := master.EventsByOpcode(state.OpCodeFullState)
			if len(full) != 1 {
				t.Fatalf("master full states = %d, want 1", len(full))
			}
			got, err := h.roomCodec.Decode(full[0].Frame.Payload)
			if err != nil {
				t.Fatalf("Decode full state: %v", err)
			}
			input, ok := got.Inputs[member.GetID()]
			if tc.wantValue == nil {
				if ok && len(input.Values) != 0 {
					t.Fatalf("member input values = %#v, want empty/absent", input.Values)
				}
				return
			}
			if !ok {
				t.Fatal("member input missing after upsert")
			}
			if v := input.Values[state.ValueId(1)]; !v.Equals(&tc.wantValue) {
				t.Fatalf("member input value = %v, want %v (upsert)", []byte(v), []byte(tc.wantValue))
			}
		})
	}
}
