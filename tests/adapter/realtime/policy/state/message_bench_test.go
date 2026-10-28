package state_test

import (
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	testpeer "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/peer"
)

// OnMessage benches (P34): hot path — decode + a targeted COW mutation of
// state, no clone/diff/encode/send. Two alternating payloads guarantee a
// real mutation on every iteration.

func BenchmarkOnMessage_PatchInput(b *testing.B) {
	h := newBenchHarness(b, domain.RoomID(7), 8)
	master := testpeer.New(1, "master", testpeer.WithoutCapture())
	client := testpeer.New(2, "client", testpeer.WithoutCapture())
	mustJoinBench(b, h.room, master, client)
	benchSeedInput(b, h, client.GetID())

	payloads := [2][]byte{
		benchEncodeInputPatch(b, h.inputCodec, 0xA1),
		benchEncodeInputPatch(b, h.inputCodec, 0xB2),
	}
	evs := [2]events.RoomEvent{
		{PeerID: domain.PeerID(2), Frame: domain.Frame{OpCode: state.OpCodePatchInput, Payload: payloads[0]}},
		{PeerID: domain.PeerID(2), Frame: domain.Frame{OpCode: state.OpCodePatchInput, Payload: payloads[1]}},
	}

	var sink error
	i := 0
	b.ResetTimer()
	for b.Loop() {
		i++
		sink = h.policy.OnMessage(evs[i&1])
	}
	_ = sink
}

func BenchmarkOnMessage_PatchEntities(b *testing.B) {
	b.Run("add_entity", func(b *testing.B) {
		h := newBenchHarness(b, domain.RoomID(8), 8)
		master := testpeer.New(1, "master", testpeer.WithoutCapture())
		client := testpeer.New(2, "client", testpeer.WithoutCapture())
		mustJoinBench(b, h.room, master, client)

		payload := benchEncodeEntitiesAddPatch(b, h.entitiesCodec)
		ev := events.RoomEvent{
			PeerID: domain.PeerID(1),
			Frame: domain.Frame{
				OpCode:  state.OpCodePatchEntities,
				Payload: payload,
			},
		}

		var sink error
		b.ResetTimer()
		for b.Loop() {
			sink = h.policy.OnMessage(ev)
		}
		_ = sink
	})

	b.Run("update_value", func(b *testing.B) {
		h := newBenchHarness(b, domain.RoomID(9), 8)
		master := testpeer.New(1, "master", testpeer.WithoutCapture())
		client := testpeer.New(2, "client", testpeer.WithoutCapture())
		mustJoinBench(b, h.room, master, client)
		benchSeedEntityForUpdate(b, h, master)

		payloads := [2][]byte{
			benchEncodeEntitiesUpdatePatch(b, h.entitiesCodec, 0xFF),
			benchEncodeEntitiesUpdatePatch(b, h.entitiesCodec, 0x11),
		}
		evs := [2]events.RoomEvent{
			{PeerID: domain.PeerID(1), Frame: domain.Frame{OpCode: state.OpCodePatchEntities, Payload: payloads[0]}},
			{PeerID: domain.PeerID(1), Frame: domain.Frame{OpCode: state.OpCodePatchEntities, Payload: payloads[1]}},
		}

		var sink error
		i := 0
		b.ResetTimer()
		for b.Loop() {
			i++
			sink = h.policy.OnMessage(evs[i&1])
		}
		_ = sink
	})
}
