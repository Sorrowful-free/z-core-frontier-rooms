package state_test

import (
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	testpeer "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/peer"
)

// P10: property run of the StateRoomPolicy invariants — seeded rand,
// operations against a real Room + fault injections; after every step —
// weak invariants, periodically — settle + strong.
//
// Fault injections live on testpeer.Peer: SetAlive(false) simulates
// transport death without a leave (kill + evict), SetDeliverErr — Deliver
// failures, Stop — a normal stop after a leave.

const (
	propSteps       = 300
	propCapacity    = 10
	propPeerPool    = 20
	propSettleEvery = 25
	propTraceKeep   = 30
)

// propTrace — a ring buffer of the last operations for failure diagnostics.
type propTrace struct {
	entries []string
}

func (tr *propTrace) push(format string, args ...any) {
	tr.entries = append(tr.entries, fmt.Sprintf(format, args...))
	if len(tr.entries) > propTraceKeep {
		tr.entries = tr.entries[len(tr.entries)-propTraceKeep:]
	}
}

func (tr *propTrace) String() string {
	return strings.Join(tr.entries, "\n")
}

func TestPolicyInvariants_Property(t *testing.T) {
	t.Parallel()

	for _, seed := range []int64{1, 2, 3, 4, 5} {
		seed := seed
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			t.Parallel()
			runInvariantProperty(t, seed)
		})
	}
}

func runInvariantProperty(t *testing.T, seed int64) {
	t.Helper()
	rnd := rand.New(rand.NewSource(seed))
	h := newHarness(t, domain.RoomID(1000+seed), propCapacity)

	// The observer sits in the room from the very beginning and never leaves.
	shadow := mustJoinShadow(t, h)
	trace := &propTrace{}

	joined := map[domain.PeerID]*testpeer.Peer{}

	// joinedIDs — sorted IDs: picking a peer via rnd indexes a stable
	// order, so a run by seed is deterministic.
	joinedIDs := func() []domain.PeerID {
		ids := make([]domain.PeerID, 0, len(joined))
		for id := range joined {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		return ids
	}
	randomJoined := func() *testpeer.Peer {
		ids := joinedIDs()
		if len(ids) == 0 {
			return nil
		}
		return joined[ids[rnd.Intn(len(ids))]]
	}
	randomJoinedExcept := func(except domain.PeerID) *testpeer.Peer {
		ids := joinedIDs()
		var rest []domain.PeerID
		for _, id := range ids {
			if id != except {
				rest = append(rest, id)
			}
		}
		if len(rest) == 0 {
			return nil
		}
		return joined[rest[rnd.Intn(len(rest))]]
	}

	// --- operation dictionary (clean + fault injections); operation errors
	// are a legal outcome, logged to the trace, they do not break invariants ---

	joinOp := func() string {
		if len(h.room.GetPeers()) >= propCapacity {
			return "join skipped (room full)"
		}
		var id domain.PeerID
		for tries := 0; tries < propPeerPool; tries++ {
			cand := domain.PeerID(1 + rnd.Intn(propPeerPool))
			if _, ok := joined[cand]; !ok {
				id = cand
				break
			}
		}
		if id == domain.PeerIDInvalid {
			return "join skipped (no free id)"
		}
		p := testpeer.New(id, fmt.Sprintf("p%d", id), testpeer.WithPing(int64(rnd.Intn(12)-1)))
		if err := h.room.Join(p); err != nil {
			return fmt.Sprintf("join %d err=%v", id, err)
		}
		joined[id] = p
		return fmt.Sprintf("join %d", id)
	}

	leaveOp := func() string {
		p := randomJoined()
		if p == nil {
			return "leave skipped (empty room)"
		}
		err := h.room.Leave(p)
		delete(joined, p.GetID())
		return fmt.Sprintf("leave %d err=%v", p.GetID(), err)
	}

	patchTickOp := func() string {
		return fmt.Sprintf("tick patch err=%v", h.policy.OnTickPatchState())
	}

	fullTickOp := func() string {
		return fmt.Sprintf("tick full err=%v", h.policy.OnTickFullState())
	}

	rpcOp := func() string {
		senderID := shadowPeerID
		if p := randomJoined(); p != nil && rnd.Intn(4) != 0 {
			senderID = p.GetID()
		}
		var target state.RpcTarget
		var targetID domain.PeerID
		switch rnd.Intn(4) {
		case 0:
			target = state.RpcTargetAll
		case 1:
			target = state.RpcTargetMaster
		case 2:
			target = state.RpcTargetPeer
			targetID = domain.PeerID(1 + rnd.Intn(propPeerPool))
		default:
			target = state.RpcTargetNone
		}
		payload := mustEncodeRPC(t, target, targetID)
		err := h.policy.OnMessage(events.RoomEvent{
			PeerID: senderID,
			Frame:  domain.Frame{OpCode: state.OpCodeRpc, Payload: payload},
		})
		return fmt.Sprintf("rpc sender=%d target=%d err=%v", senderID, target, err)
	}

	inputOp := func() string {
		p := randomJoined()
		if p == nil {
			return "input skipped (empty room)"
		}
		vid := state.ValueId(1 + rnd.Intn(5))
		var frame domain.Frame
		if rnd.Intn(2) == 0 {
			in := &state.InputState{Values: state.NewMapState[state.ValueId, state.ValueState]()}
			in.Values[vid] = state.ValueState([]byte{byte(rnd.Intn(256))})
			payload, err := h.inputCodec.Encode(in)
			if err != nil {
				return fmt.Sprintf("input encode err=%v", err)
			}
			frame = domain.Frame{OpCode: state.OpCodeFullInput, Payload: payload}
		} else {
			patch := &state.InputStatePatch{
				Values: *state.NewMapStatePatch[state.ValueId, state.ValueState, state.ValueState](),
			}
			patch.Values.Added[vid] = state.ValueState([]byte{byte(rnd.Intn(256))})
			payload, err := h.inputCodec.EncodePatch(patch)
			if err != nil {
				return fmt.Sprintf("input patch encode err=%v", err)
			}
			frame = domain.Frame{OpCode: state.OpCodePatchInput, Payload: payload}
		}
		err := h.policy.OnMessage(events.RoomEvent{PeerID: p.GetID(), Frame: frame})
		return fmt.Sprintf("input %d opcode=%#x err=%v", p.GetID(), frame.OpCode, err)
	}

	entitiesOp := func() string {
		p := randomJoined()
		if p == nil {
			return "entities skipped (empty room)"
		}
		entities := state.NewMapState[state.EntityID, state.EntityState]()
		for i := 0; i < rnd.Intn(3); i++ {
			eid := state.EntityID(1 + rnd.Intn(5))
			entities[eid] = state.EntityState{
				EntityTypeID: state.EntityTypeID(1),
				Owner:        p.GetID(),
				Components:   state.NewMapState[state.ComponentID, state.ComponentState](),
			}
		}
		payload, err := h.entitiesCodec.Encode(entities)
		if err != nil {
			return fmt.Sprintf("entities encode err=%v", err)
		}
		err = h.policy.OnMessage(events.RoomEvent{
			PeerID: p.GetID(),
			Frame:  domain.Frame{OpCode: state.OpCodeFullEntities, Payload: payload},
		})
		return fmt.Sprintf("entities %d err=%v", p.GetID(), err)
	}

	// Fault: a random survivor's Deliver fails. P34: leave itself sends
	// nothing (the delta rides on a tick), so the injection only affects
	// the wire delivery of subsequent tick broadcasts — a legal fault mode,
	// the shadow is healed by resync.
	flakyLeaveOp := func() string {
		p := randomJoined()
		if p == nil {
			return "flaky leave skipped (empty room)"
		}
		bystander := randomJoinedExcept(p.GetID())
		if bystander == nil {
			return fmt.Sprintf("flaky leave %d skipped (no bystander)", p.GetID())
		}
		bystander.SetDeliverErr(errDeliverFailed)
		err := h.room.Leave(p)
		bystander.SetDeliverErr(nil)
		delete(joined, p.GetID())
		return fmt.Sprintf("flaky leave %d via bystander %d err=%v", p.GetID(), bystander.GetID(), err)
	}

	// Fault: the transport died without a leave — the peer stays in the
	// room and in the state, its Deliver fails; the sweep evicts it
	// (evictDead).
	killOp := func() string {
		p := randomJoined()
		if p == nil {
			return "kill skipped (empty room)"
		}
		p.SetAlive(false)
		return fmt.Sprintf("kill %d (IsAlive=false)", p.GetID())
	}

	// Sweep equivalent of EvictIdlePeersUseCase: evicts the dead via Leave.
	// A dead Deliver does not break a leave by itself (P34 — leave sends
	// nothing), but it drops the tick broadcast until the peer is evicted.
	evictDeadOp := func() string {
		var out strings.Builder
		out.WriteString("evict dead:")
		n := 0
		for _, id := range joinedIDs() {
			p := joined[id]
			if p.IsAlive() {
				continue
			}
			err := h.room.Leave(p)
			delete(joined, id)
			fmt.Fprintf(&out, " %d(err=%v)", id, err)
			n++
		}
		if n == 0 {
			return "evict skipped (none dead)"
		}
		return out.String()
	}

	// LeaveRoom equivalent: a clean leave + peer.Stop.
	stopOp := func() string {
		p := randomJoined()
		if p == nil {
			return "stop skipped (empty room)"
		}
		err := h.room.Leave(p)
		stopErr := p.Stop()
		delete(joined, p.GetID())
		return fmt.Sprintf("leave+stop %d err=%v stopErr=%v", p.GetID(), err, stopErr)
	}

	ops := []func() string{
		joinOp, joinOp, joinOp,
		leaveOp, leaveOp,
		patchTickOp, patchTickOp,
		fullTickOp,
		rpcOp, inputOp, entitiesOp,
		flakyLeaveOp, killOp, evictDeadOp, stopOp,
	}

	desyncLogged := false
	checkWeak := func(step int) {
		view, desynced := shadow.view.materialized()
		if desynced {
			if !desyncLogged {
				trace.push("step %d: shadow desynced (%v), weak checks skipped until resync",
					step, shadow.view.lastError())
				desyncLogged = true
			}
			return
		}
		desyncLogged = false
		if err := checkWeakInvariants(view); err != nil {
			t.Fatalf("seed=%d step=%d weak invariant violated: %v\npeers=%#v\ntrace:\n%s",
				seed, step, err, view.Peers, trace.String())
		}
	}

	checkStrong := func(step int, final bool) {
		settleTick(t, h, shadow.view)
		if shadow.view.desynced() {
			resyncShadow(t, h)
		}
		view, desynced := shadow.view.materialized()
		if desynced {
			if final {
				t.Fatalf("seed=%d: shadow never resynced (%v)\ntrace:\n%s",
					seed, shadow.view.lastError(), trace.String())
			}
			trace.push("step %d: shadow still desynced after resync attempt, strong check skipped", step)
			return
		}
		if err := checkStrongInvariants(view, h.room); err != nil {
			t.Fatalf("seed=%d step=%d strong invariant violated: %v\npeers=%#v\ntrace:\n%s",
				seed, step, err, view.Peers, trace.String())
		}
	}

	for step := 0; step < propSteps; step++ {
		op := ops[rnd.Intn(len(ops))]
		trace.push("step %d: %s", step, op())

		checkWeak(step)

		if step%propSettleEvery == propSettleEvery-1 {
			checkStrong(step, false)
		}
	}

	// Finale: remove all fault injections and evict the dead — after that
	// the resync must reach the shadow, otherwise the protocol cannot
	// recover the client.
	for id, p := range joined {
		p.SetDeliverErr(nil)
		if !p.IsAlive() {
			if err := h.room.Leave(p); err != nil {
				trace.push("final evict %d err=%v", id, err)
			}
			delete(joined, id)
		}
	}
	checkStrong(propSteps, true)
}
