package integration_test

// Content e2e (P34): exact bytes of Inputs/Entities after a
// client→server→client round trip over the wire. Guarantees that
// tick-coalesced emission and the shallow prevState snapshot do not distort
// the data: the client replica (join-full + tick patches) must contain
// EXACTLY the sent values and converge with the authoritative full state
// (brought by the observer client on join — immediate deliver).
//
// Covers three patch ops: Added (exact bytes in every peer's replica),
// Updated (the second mutation of a key arrives with the new value — the
// in-place snapshot trap), Removed (the key disappears instead of going zombie).

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/env"
)

// stopSendAndWait silences the send loop and waits for its exit: after the
// return nothing writes to conn — a writeFrame from the test goroutine does
// not race the background send (a websocket conn is not write-thread-safe).
func (b *matchClientBase) stopSendAndWait() {
	b.stopSend()
	<-b.sendDone
}

// replica — a thread-safe snapshot of the client's local replica (deep clone,
// so the maps can be read outside the mutex).
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

// --- content fixtures ---------------------------------------------------------

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

// --- frame sending ------------------------------------------------------------

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

// --- content asserts ----------------------------------------------------------

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

// wantEntity — the reference entity exactly as we sent it.
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

// equals closures for a deep comparison of replica maps vs the authoritative full state.
func inputsEqual(a, b state.InputState) bool {
	return a.Equals(&b, func(v1, v2 state.ValueState) bool { return v1.Equals(&v2) })
}

func entitiesEqual(a, b state.EntityState) bool {
	return a.Equals(&b, func(c1, c2 state.ComponentState) bool {
		return c1.Equals(&c2, func(v1, v2 state.ValueState) bool { return v1.Equals(&v2) })
	})
}

// TestStateContent_ExactValues — see the file header.
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

	// Stop the send loops: the background 20Hz input/entities must not churn
	// values under the feet of the exact asserts — from here on we mutate state
	// only manually, with deterministic frames. Wait for the exit: a concurrent
	// write on conn is a data race.
	master.stopSendAndWait()
	member.stopSendAndWait()

	// --- Phase 1: Added, exact bytes reach everyone ----------------------------

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

	// Exact bytes in EVERY peer's replica (including the echo to the sender).
	for _, c := range []*wsClient{master, member} {
		r := c.replica()
		assertValue(t, r, memberPeer, contentValueKeyA, contentInputA)
		assertValue(t, r, memberPeer, contentValueKeyB, contentInputB)
		assertEntity(t, r, contentEntityID, wantEntity(masterPeer))
	}

	// --- Phase 2: Updated — the second mutation of a key arrives with the new value
	// This is where the in-place prevState corruption would bite: a diff against
	// the "caught-up" snapshot would yield an empty patch, and the old value
	// would stay forever.

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

	// --- Phase 3: Removed — the key disappears, no zombie ---------------------

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

	// --- Phase 4: convergence with the authoritative full state ----------------
	// The observer gets an immediate full state on join — that is the server's
	// authoritative version at join time. The member/master replicas, assembled
	// from join-full + tick patches, must deeply converge with it on
	// Inputs/Entities (not just on key presence).

	observer := newWSClient(t, e, "observer", e.IssueTicket(t, roomID, "observer"))
	observer.stopSendAndWait()
	defer observer.stop()

	waitReplica(t, observer, 10*time.Second, "observer got join full state", func(r *state.RoomState) bool {
		return len(r.Peers) >= 3
	})

	// The observer may have sent a loop input before stopSend — wait until its
	// entry reaches the others' replicas and the maps become equal.
	waitReplica(t, member, 5*time.Second, "converged vs authoritative full (Inputs+Entities)", func(r *state.RoomState) bool {
		full := observer.replica()
		if full == nil {
			return false
		}
		return r.Inputs.Equals(full.Inputs, inputsEqual) &&
			r.Entities.Equals(full.Entities, entitiesEqual)
	})
}
