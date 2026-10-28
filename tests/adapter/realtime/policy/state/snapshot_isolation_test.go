package state_test

import (
	"fmt"
	"math/rand"
	"slices"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	testpeer "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/peer"
)

// P34: prevState — shallow-снапшот (значения разделяются со state, COW).
// Если бы записи мутировались in-place, снапшот «догонял» бы state и
// diff проглатывал мутации. Wire-проверка: две подряд мутации одного
// ключа обязаны дать два разных тиковых патча.
func TestTickPatchState_ConsecutiveMutations_EachEmitted(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(200), 4)
	master := testpeer.New(1, "master")
	member := testpeer.New(2, "member")
	mustJoin(t, h.room, master, member)
	mustConverge(t, h)
	master.Clear()

	send := func(val byte) {
		t.Helper()
		patch := &state.InputStatePatch{
			Values: *state.NewMapStatePatch[state.ValueId, state.ValueState, state.ValueState](),
		}
		patch.Values.Added[state.ValueId(1)] = state.ValueState([]byte{val})
		payload, err := h.inputCodec.EncodePatch(patch)
		if err != nil {
			t.Fatalf("EncodePatch input: %v", err)
		}
		if err := h.policy.OnMessage(events.RoomEvent{
			PeerID: member.GetID(),
			Frame:  domain.Frame{OpCode: state.OpCodePatchInput, Payload: payload},
		}); err != nil {
			t.Fatalf("OnMessage patch input: %v", err)
		}
	}

	// Первая мутация — новая запись Inputs[member] в diff'е (Added).
	send(0xA1)
	if err := h.policy.OnTickPatchState(); err != nil {
		t.Fatalf("OnTickPatchState 1: %v", err)
	}
	patches := master.EventsByOpcode(state.OpCodePatchState)
	if len(patches) != 1 {
		t.Fatalf("master patches = %d, want 1 — дельта проглочена", len(patches))
	}
	got, err := h.roomCodec.DecodePatch(patches[0].Frame.Payload)
	if err != nil {
		t.Fatalf("DecodePatch 1: %v", err)
	}
	added, ok := got.Inputs.Added[member.GetID()]
	if !ok {
		t.Fatalf("inputs.added = %#v, want member input", got.Inputs.Added)
	}
	if v := added.Values[state.ValueId(1)]; len(v) != 1 || v[0] != 0xA1 {
		t.Fatalf("input value = %v, want [0xA1]", []byte(v))
	}
	master.Clear()

	// Вторая мутация того же ключа — Updated-дельта. Здесь и ломался бы
	// in-place снапшот: prevState.Inputs шарил бы уже обновлённую Values-
	// мапу и diff был бы пустым.
	send(0xB2)
	if err := h.policy.OnTickPatchState(); err != nil {
		t.Fatalf("OnTickPatchState 2: %v", err)
	}
	patches = master.EventsByOpcode(state.OpCodePatchState)
	if len(patches) != 1 {
		t.Fatalf("master patches = %d, want 1 — вторая дельта проглочена (snapshot isolation broken)", len(patches))
	}
	got, err = h.roomCodec.DecodePatch(patches[0].Frame.Payload)
	if err != nil {
		t.Fatalf("DecodePatch 2: %v", err)
	}
	upd, ok := got.Inputs.Updated[member.GetID()]
	if !ok {
		t.Fatalf("inputs.updated = %#v, want member input patch", got.Inputs.Updated)
	}
	if v := upd.Values.Updated[state.ValueId(1)]; len(v) != 1 || v[0] != 0xB2 {
		t.Fatalf("updated input value = %v, want [0xB2]", []byte(v))
	}
}

// Property-вариант: replay-реплика наблюдателя (join-full + все tick-патчи)
// обязана сойтись с финальным full state — иначе часть дельт потерялась
// (in-place мутация снапшота) или пришла битой. Покрывает все opcode-пути
// мутаций state (input/entities), join/leave + election-флаги.
func TestPolicySnapshotIsolation_Property(t *testing.T) {
	t.Parallel()

	for _, seed := range []int64{1, 2, 3} {
		seed := seed
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			t.Parallel()
			runSnapshotIsolation(t, seed)
		})
	}
}

func runSnapshotIsolation(t *testing.T, seed int64) {
	t.Helper()
	rnd := rand.New(rand.NewSource(seed))
	h := newHarness(t, domain.RoomID(300+seed), 16)

	master := testpeer.New(1, "master")
	observer := testpeer.New(2, "observer")
	mustJoin(t, h.room, master, observer)

	joined := map[domain.PeerID]*testpeer.Peer{
		master.GetID(): master,
	}
	joinedIDs := func() []domain.PeerID {
		ids := make([]domain.PeerID, 0, len(joined))
		for id := range joined {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		return ids
	}
	randomMember := func() *testpeer.Peer {
		ids := joinedIDs()
		rest := make([]domain.PeerID, 0, len(ids))
		for _, id := range ids {
			if id != master.GetID() {
				rest = append(rest, id)
			}
		}
		if len(rest) == 0 {
			return nil
		}
		return joined[rest[rnd.Intn(len(rest))]]
	}

	inputOp := func() string {
		member := randomMember()
		if member == nil {
			return "input skipped"
		}
		vid := state.ValueId(1 + rnd.Intn(3))
		patch := &state.InputStatePatch{
			Values: *state.NewMapStatePatch[state.ValueId, state.ValueState, state.ValueState](),
		}
		patch.Values.Added[vid] = state.ValueState([]byte{byte(rnd.Intn(256))})
		payload, err := h.inputCodec.EncodePatch(patch)
		if err != nil {
			t.Fatalf("EncodePatch input: %v", err)
		}
		err = h.policy.OnMessage(events.RoomEvent{
			PeerID: member.GetID(),
			Frame:  domain.Frame{OpCode: state.OpCodePatchInput, Payload: payload},
		})
		return fmt.Sprintf("input %d err=%v", member.GetID(), err)
	}

	entitiesOp := func() string {
		eid := state.EntityID(1 + rnd.Intn(3))
		if rnd.Intn(2) == 0 {
			entities := state.NewMapState[state.EntityID, state.EntityState]()
			components := state.NewMapState[state.ComponentID, state.ComponentState]()
			components[state.ComponentID(1)] = state.ComponentState{
				Values: state.MapState[state.ValueId, state.ValueState]{
					state.ValueId(1): state.ValueState([]byte{byte(rnd.Intn(256))}),
				},
			}
			entities[eid] = state.EntityState{Owner: master.GetID(), Components: components}
			payload, err := h.entitiesCodec.Encode(entities)
			if err != nil {
				t.Fatalf("Encode entities: %v", err)
			}
			err = h.policy.OnMessage(events.RoomEvent{
				PeerID: master.GetID(),
				Frame:  domain.Frame{OpCode: state.OpCodeFullEntities, Payload: payload},
			})
			return fmt.Sprintf("full entities %d err=%v", eid, err)
		}
		patch := state.NewMapStatePatch[state.EntityID, state.EntityState, state.EntityStatePatch]()
		components := state.NewMapState[state.ComponentID, state.ComponentState]()
		components[state.ComponentID(1)] = state.ComponentState{
			Values: state.MapState[state.ValueId, state.ValueState]{
				state.ValueId(1): state.ValueState([]byte{byte(rnd.Intn(256))}),
			},
		}
		patch.Added[eid] = state.EntityState{Owner: master.GetID(), Components: components}
		payload, err := h.entitiesCodec.EncodePatch(patch)
		if err != nil {
			t.Fatalf("EncodePatch entities: %v", err)
		}
		err = h.policy.OnMessage(events.RoomEvent{
			PeerID: master.GetID(),
			Frame:  domain.Frame{OpCode: state.OpCodePatchEntities, Payload: payload},
		})
		return fmt.Sprintf("patch entities %d err=%v", eid, err)
	}

	nextID := domain.PeerID(100)
	joinOp := func() string {
		nextID++
		p := testpeer.New(nextID, fmt.Sprintf("p%d", nextID))
		err := h.room.Join(p)
		if err == nil {
			joined[nextID] = p
		}
		return fmt.Sprintf("join %d err=%v", nextID, err)
	}

	leaveOp := func() string {
		member := randomMember()
		if member == nil {
			return "leave skipped"
		}
		err := h.room.Leave(member)
		delete(joined, member.GetID())
		return fmt.Sprintf("leave %d err=%v", member.GetID(), err)
	}

	ops := []func() string{
		inputOp, inputOp, inputOp,
		entitiesOp, entitiesOp,
		joinOp, leaveOp,
	}
	for step := 0; step < 80; step++ {
		_ = ops[rnd.Intn(len(ops))]()
		if err := h.policy.OnTickPatchState(); err != nil {
			t.Fatalf("step %d tick: %v", step, err)
		}
	}

	// Replay-реплика: join-full observer'а + все патчи по порядку — ровно
	// то, что увидел бы клиент без resync.
	var replica *state.RoomState
	for _, ev := range observer.Snapshot() {
		switch ev.Frame.OpCode {
		case state.OpCodeFullState:
			full, err := h.roomCodec.Decode(ev.Frame.Payload)
			if err != nil {
				t.Fatalf("decode observer full: %v", err)
			}
			if replica == nil {
				replica = full
			}
		case state.OpCodePatchState:
			patch, err := h.roomCodec.DecodePatch(ev.Frame.Payload)
			if err != nil {
				t.Fatalf("decode observer patch: %v", err)
			}
			if replica == nil {
				t.Fatal("patch before first full state")
			}
			if err := replica.ApplyPatch(*patch); err != nil {
				t.Fatalf("replica apply patch: %v", err)
			}
		}
	}
	if replica == nil {
		t.Fatal("observer never received join full state")
	}

	observer.Clear()
	if err := h.policy.OnTickFullState(); err != nil {
		t.Fatalf("OnTickFullState: %v", err)
	}
	finals := observer.EventsByOpcode(state.OpCodeFullState)
	if len(finals) != 1 {
		t.Fatalf("observer full states = %d, want 1", len(finals))
	}
	final, err := h.roomCodec.Decode(finals[0].Frame.Payload)
	if err != nil {
		t.Fatalf("Decode final full: %v", err)
	}
	assertRoomStatesEqual(t, replica, final)
}

func assertRoomStatesEqual(t *testing.T, got, want *state.RoomState) {
	t.Helper()
	if got.ID != want.ID || got.Capacity != want.Capacity {
		t.Fatalf("scalars: got %d/%d want %d/%d", got.ID, got.Capacity, want.ID, want.Capacity)
	}
	if !got.Peers.Equals(want.Peers, func(a, b state.PeerState) bool { return a.Equals(&b) }) {
		t.Fatalf("peers: got %#v want %#v", got.Peers, want.Peers)
	}
	if !got.Inputs.Equals(want.Inputs, func(a, b state.InputState) bool {
		return a.Equals(&b, func(v1, v2 state.ValueState) bool { return v1.Equals(&v2) })
	}) {
		t.Fatalf("inputs: got %#v want %#v", got.Inputs, want.Inputs)
	}
	if !got.Entities.Equals(want.Entities, func(a, b state.EntityState) bool {
		return a.Equals(&b, func(c1, c2 state.ComponentState) bool {
			return c1.Equals(&c2, func(v1, v2 state.ValueState) bool { return v1.Equals(&v2) })
		})
	}) {
		t.Fatalf("entities: got %#v want %#v", got.Entities, want.Entities)
	}
}
