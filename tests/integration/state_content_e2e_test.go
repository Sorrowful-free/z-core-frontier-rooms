package integration_test

// Контентный e2e (P34): точные байты Inputs/Entities после прохода
// client→server→client по проводу. Гарантия, что tick-coalesced эмиссия
// и shallow prevState-Snapshot не искажают данные: клиентская реплика
// (join-full + тиковые патчи) обязана содержать РОВНО отправленные
// значения и сходиться с авторитетным full state (его приносит
// observer-клиент при join — immediate deliver).
//
// Покрывает три patch-опа: Added (точные байты в реплике всех peer),
// Updated (вторая мутация ключа доезжает новым значением — ловушка
// in-place снапшота), Removed (ключ исчезает, а не зомбирует).

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/env"
)

// stopSendAndWait глушит send-loop и ждёт его выхода: после возврата на
// conn никто не пишет — writeFrame из тест-горутины не гонится с
// фоновой отправкой (websocket conn не потокобезопасен на write).
func (b *matchClientBase) stopSendAndWait() {
	b.stopSend()
	<-b.sendDone
}

// replica — потокобезопасный снапшот локальной реплики клиента (deep clone,
// чтобы читать мапы вне мьютекса).
func (b *matchClientBase) replica() *state.RoomState {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.room == nil {
		return nil
	}
	return b.room.Clone()
}

func waitReplica(t *testing.T, c *wsClient, timeout time.Duration, desc string, cond func(*state.RoomState) bool) {
	t.Helper()
	waitFor(t, timeout, desc, func() bool {
		r := c.replica()
		return r != nil && cond(r)
	})
}

// --- фикстуры контента --------------------------------------------------------

var (
	contentValueKeyA = state.ValueId(10)
	contentValueKeyB = state.ValueId(20)
	contentEntityID  = state.EntityID(777)
	contentEntityTyp = state.EntityTypeID(9)
	contentCompID    = state.ComponentID(5)
	contentCompValA  = state.ValueId(7)
	contentCompValB  = state.ValueId(8)
)

var contentInputA = []byte{0xDE, 0xAD, 0xBE, 0xEF}
var contentInputA2 = []byte{0xCA, 0xFE, 0xF0, 0x0D}
var contentInputB = []byte{0x01, 0x02, 0x03, 0x04, 0x05}
var contentEntityValA = []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x00, 0x11}
var contentEntityValB = []byte{0x22, 0x33}

// --- отправка кадров ----------------------------------------------------------

func sendInputAdded(t *testing.T, c *wsClient, adds map[state.ValueId][]byte) {
	t.Helper()
	patch := &state.InputStatePatch{
		Values: *state.NewMapStatePatch[state.ValueId, state.ValueState, state.ValueState](),
	}
	for k, v := range adds {
		patch.Values.Added[k] = state.ValueState(v)
	}
	payload, err := c.inputCodec.EncodePatch(patch)
	if err != nil {
		t.Fatalf("EncodePatch input: %v", err)
	}
	if err := c.writeFrame(state.OpCodePatchInput, payload); err != nil {
		t.Fatalf("write input patch: %v", err)
	}
}

func sendInputUpdated(t *testing.T, c *wsClient, key state.ValueId, v []byte) {
	t.Helper()
	patch := &state.InputStatePatch{
		Values: *state.NewMapStatePatch[state.ValueId, state.ValueState, state.ValueState](),
	}
	patch.Values.Updated[key] = state.ValueState(v)
	payload, err := c.inputCodec.EncodePatch(patch)
	if err != nil {
		t.Fatalf("EncodePatch input upd: %v", err)
	}
	if err := c.writeFrame(state.OpCodePatchInput, payload); err != nil {
		t.Fatalf("write input upd: %v", err)
	}
}

func sendInputRemoved(t *testing.T, c *wsClient, keys ...state.ValueId) {
	t.Helper()
	patch := &state.InputStatePatch{
		Values: *state.NewMapStatePatch[state.ValueId, state.ValueState, state.ValueState](),
	}
	patch.Values.Removed = append(patch.Values.Removed, keys...)
	payload, err := c.inputCodec.EncodePatch(patch)
	if err != nil {
		t.Fatalf("EncodePatch input rm: %v", err)
	}
	if err := c.writeFrame(state.OpCodePatchInput, payload); err != nil {
		t.Fatalf("write input rm: %v", err)
	}
}

func sendEntitiesAdded(t *testing.T, c *wsClient, eid state.EntityID, ent state.EntityState) {
	t.Helper()
	patch := state.NewMapStatePatch[state.EntityID, state.EntityState, state.EntityStatePatch]()
	patch.Added[eid] = ent
	payload, err := c.entitiesCodec.EncodePatch(patch)
	if err != nil {
		t.Fatalf("EncodePatch entities: %v", err)
	}
	if err := c.writeFrame(state.OpCodePatchEntities, payload); err != nil {
		t.Fatalf("write entities patch: %v", err)
	}
}

// --- ассерты содержимого ------------------------------------------------------

func assertValue(t *testing.T, r *state.RoomState, peerID domain.PeerID, key state.ValueId, want []byte) {
	t.Helper()
	in, ok := r.Inputs[peerID]
	if !ok {
		t.Fatalf("replica: no Inputs[%d]", peerID)
	}
	got, ok := in.Values[key]
	if !ok {
		t.Fatalf("replica: Inputs[%d].Values[%d] missing, keys=%v", peerID, key, keysOf(in.Values))
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("replica: Inputs[%d].Values[%d] = %x, want %x", peerID, key, []byte(got), want)
	}
}

func keysOf[K comparable, V any](m map[K]V) []K {
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// wantEntity — эталонная сущность как мы её отправили.
func wantEntity(owner domain.PeerID) state.EntityState {
	return state.EntityState{
		EntityTypeID: contentEntityTyp,
		Owner:        owner,
		Components: state.MapState[state.ComponentID, state.ComponentState]{
			contentCompID: {Values: state.MapState[state.ValueId, state.ValueState]{
				contentCompValA: state.ValueState(contentEntityValA),
				contentCompValB: state.ValueState(contentEntityValB),
			}},
		},
	}
}

func assertEntity(t *testing.T, r *state.RoomState, eid state.EntityID, want state.EntityState) {
	t.Helper()
	got, ok := r.Entities[eid]
	if !ok {
		t.Fatalf("replica: Entities[%d] missing, ids=%v", eid, keysOf(map[state.EntityID]state.EntityState(r.Entities)))
	}
	if got.EntityTypeID != want.EntityTypeID {
		t.Fatalf("entity %d type = %d, want %d", eid, got.EntityTypeID, want.EntityTypeID)
	}
	if got.Owner != want.Owner {
		t.Fatalf("entity %d owner = %d, want %d", eid, got.Owner, want.Owner)
	}
	wantComp := want.Components[contentCompID]
	gotComp, ok := got.Components[contentCompID]
	if !ok {
		t.Fatalf("entity %d: component %d missing, comps=%v", eid, contentCompID, keysOf(map[state.ComponentID]state.ComponentState(got.Components)))
	}
	for k, wantV := range wantComp.Values {
		gotV, ok := gotComp.Values[k]
		if !ok {
			t.Fatalf("entity %d comp %d: value %d missing", eid, contentCompID, k)
		}
		if !bytes.Equal(gotV, wantV) {
			t.Fatalf("entity %d comp %d value %d = %x, want %x", eid, contentCompID, k, []byte(gotV), []byte(wantV))
		}
	}
	if len(gotComp.Values) != len(wantComp.Values) {
		t.Fatalf("entity %d comp %d values = %v, want exactly %d keys",
			eid, contentCompID, keysOf(map[state.ValueId]state.ValueState(gotComp.Values)), len(wantComp.Values))
	}
}

// equals-замыкания для deep-сверки мап реплика vs авторитетный full state.
func inputsEqual(a, b state.InputState) bool {
	return a.Equals(&b, func(v1, v2 state.ValueState) bool { return v1.Equals(&v2) })
}

func entitiesEqual(a, b state.EntityState) bool {
	return a.Equals(&b, func(c1, c2 state.ComponentState) bool {
		return c1.Equals(&c2, func(v1, v2 state.ValueState) bool { return v1.Equals(&v2) })
	})
}

// TestStateContent_ExactValues — см. шапку файла.
func TestStateContent_ExactValues(t *testing.T) {
	t.Parallel()

	e := env.New(t)

	roomID, masterToken := e.CreateRoom(t, "master")
	master := newWSClient(t, e, "master", masterToken)
	member := newWSClient(t, e, "member", e.IssueTicket(t, roomID, "member"))
	defer func() {
		master.stop()
		member.stop()
	}()

	waitFor(t, 10*time.Second, "clients discovered peer ids", func() bool {
		return master.peer().IsValid() && member.peer().IsValid()
	})
	masterPeer, memberPeer := master.peer(), member.peer()

	// Стопаем send-loop'ы: фоновый 20Гц input/entities не должен гонять
	// значения под ногами у точных ассертов — дальше мутируем state только
	// вручную, детерминированными кадрами. Ждём выхода: конкурентный
	// write на conn — data race.
	master.stopSendAndWait()
	member.stopSendAndWait()

	// --- Фаза 1: Added, точные байты доезжают всем ----------------------------

	sendInputAdded(t, member, map[state.ValueId][]byte{
		contentValueKeyA: contentInputA,
		contentValueKeyB: contentInputB,
	})
	sendEntitiesAdded(t, master, contentEntityID, wantEntity(masterPeer))

	for _, c := range []*wsClient{master, member} {
		waitReplica(t, c, 5*time.Second, fmt.Sprintf("%s: input+entities added", c.nick), func(r *state.RoomState) bool {
			in, ok := r.Inputs[memberPeer]
			if !ok {
				return false
			}
			_, okA := in.Values[contentValueKeyA]
			_, okB := in.Values[contentValueKeyB]
			_, okE := r.Entities[contentEntityID]
			return okA && okB && okE
		})
	}

	// Точные байты в реплике КАЖДОГО peer (включая эхо отправителю).
	for _, c := range []*wsClient{master, member} {
		r := c.replica()
		assertValue(t, r, memberPeer, contentValueKeyA, contentInputA)
		assertValue(t, r, memberPeer, contentValueKeyB, contentInputB)
		assertEntity(t, r, contentEntityID, wantEntity(masterPeer))
	}

	// --- Фаза 2: Updated — вторая мутация ключа доезжает новым значением ------
	// Здесь и ломалась бы порча от in-place prevState: diff по «догнавшему»
	// снапшоту дал бы пустой патч, и старое значение осталось бы навсегда.

	sendInputUpdated(t, member, contentValueKeyA, contentInputA2)
	waitReplica(t, member, 5*time.Second, "member: updated input value", func(r *state.RoomState) bool {
		in, ok := r.Inputs[memberPeer]
		if !ok {
			return false
		}
		v, ok := in.Values[contentValueKeyA]
		return ok && bytes.Equal(v, contentInputA2)
	})
	assertValue(t, master.replica(), memberPeer, contentValueKeyA, contentInputA2)
	assertValue(t, master.replica(), memberPeer, contentValueKeyB, contentInputB)

	// --- Фаза 3: Removed — ключ исчезает, не зомбирует ------------------------

	sendInputRemoved(t, member, contentValueKeyB)
	waitReplica(t, member, 5*time.Second, "member: removed input key", func(r *state.RoomState) bool {
		in, ok := r.Inputs[memberPeer]
		if !ok {
			return false
		}
		_, stillThere := in.Values[contentValueKeyB]
		return !stillThere
	})
	masterRep := master.replica()
	if _, stillThere := masterRep.Inputs[memberPeer].Values[contentValueKeyB]; stillThere {
		t.Fatalf("master replica: removed key %d still present", contentValueKeyB)
	}

	// --- Фаза 4: конвергенция с авторитетным full state ------------------------
	// Observer при join получает immediate full state — это серверная
	// authoritative версия на момент join. Реплики member/master, собранные
	// join-full + тиковыми патчами, обязаны сойтись с ней по Inputs/Entities
	// глубоко (не только по наличию ключей).

	observer := newWSClient(t, e, "observer", e.IssueTicket(t, roomID, "observer"))
	observer.stopSendAndWait()
	defer observer.stop()

	waitReplica(t, observer, 10*time.Second, "observer got join full state", func(r *state.RoomState) bool {
		return len(r.Peers) >= 3
	})

	// Observer мог успеть отправить loop-input до stopSend — дожидаемся, когда
	// его запись дойдёт до реплик остальных и карты сравняются.
	waitReplica(t, member, 5*time.Second, "converged vs authoritative full (Inputs+Entities)", func(r *state.RoomState) bool {
		full := observer.replica()
		if full == nil {
			return false
		}
		return r.Inputs.Equals(full.Inputs, inputsEqual) &&
			r.Entities.Equals(full.Entities, entitiesEqual)
	})
}
