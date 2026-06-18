package state_test

import (
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

func BenchmarkOnMessage_PatchInput(b *testing.B) {
	h := newBenchHarness(b, domain.RoomID(7), 8)
	master := newBenchCapturePeer(1, "master")
	client := newBenchCapturePeer(2, "client")
	mustJoinBench(b, h.room, master, client)
	master.clear()
	client.clear()

	payload := benchEncodeInputPatch(b, h.inputCodec)
	ev := events.RoomEvent{
		PeerID: domain.PeerID(2),
		Frame: domain.Frame{
			OpCode:  state.OpCodePatchInput,
			Payload: payload,
		},
	}

	var sink error
	b.ResetTimer()
	for b.Loop() {
		sink = h.policy.OnMessage(ev)
	}
	_ = sink
}

func BenchmarkOnMessage_PatchEntities(b *testing.B) {
	b.Run("add_entity", func(b *testing.B) {
		h := newBenchHarness(b, domain.RoomID(8), 8)
		master := newBenchCapturePeer(1, "master")
		client := newBenchCapturePeer(2, "client")
		mustJoinBench(b, h.room, master, client)
		master.clear()
		client.clear()

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
		master := newBenchCapturePeer(1, "master")
		client := newBenchCapturePeer(2, "client")
		mustJoinBench(b, h.room, master, client)
		benchSeedEntityForUpdate(b, h, master)
		client.clear()

		payload := benchEncodeEntitiesUpdatePatch(b, h.entitiesCodec)
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
}
