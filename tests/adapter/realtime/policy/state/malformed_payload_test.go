package state_test

import (
	"errors"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	testpeer "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/peer"
)

// Каждый in-room opcode × garbage payload → ErrInRoomInvalidPayload,
// wire-опкод ошибки — OpInRoomInvalidPayload.
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
		// entities-опкоды — master-gated, отправитель обязан быть master.
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

// Wire-конверт InputStatePatch валиден (DecodePatch успешен), но внутренний
// value-patch противоречит состоянию input'а отправителя — ApplyPatch внутри
// onClientPatchInput обязан завернуть ошибку в ErrInRoomInvalidPayload.
func TestOnMessage_PatchInput_BrokenInnerPatch_ReturnsErrInRoomInvalidPayload(t *testing.T) {
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
	}{
		{
			name: "updated value not in input",
			patch: newPatch(func(p *state.MapStatePatch[state.ValueId, state.ValueState, state.ValueState]) {
				p.Updated[state.ValueId(7)] = state.ValueState([]byte{0x01})
			}),
		},
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
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, domain.RoomID(71), 4)
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
		})
	}
}
