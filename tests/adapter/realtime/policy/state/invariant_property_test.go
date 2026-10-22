package state_test

import (
	"errors"
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

// P10: property-прогон инвариантов StateRoomPolicy — seeded rand, операции
// над реальной Room + fault-инъекции; после каждого шага — слабые
// инварианты, периодически — settle + сильные.

var errPeerDead = errors.New("peer transport dead")

// mortalPeer — capturePeer с переключаемой живостью и опциональной ошибкой
// Deliver: моделирует смерть транспорта без leave (kill + evict) и штатный
// stop после leave.
type mortalPeer struct {
	*capturePeer

	flagsMu    sync.Mutex
	alive      bool
	stopped    bool
	deliverErr error
}

func newMortalPeer(id domain.PeerID, nick string, ping int64) *mortalPeer {
	return &mortalPeer{capturePeer: newCapturePeerWithPing(id, nick, ping), alive: true}
}

func (p *mortalPeer) IsAlive() bool {
	p.flagsMu.Lock()
	defer p.flagsMu.Unlock()
	return p.alive
}

func (p *mortalPeer) Stop() error {
	p.flagsMu.Lock()
	defer p.flagsMu.Unlock()
	p.stopped = true
	p.alive = false
	return nil
}

func (p *mortalPeer) Deliver(ev events.PeerEvent) error {
	p.flagsMu.Lock()
	err := p.deliverErr
	alive := p.alive
	p.flagsMu.Unlock()
	if err != nil {
		return err
	}
	if !alive {
		return errPeerDead
	}
	return p.capturePeer.Deliver(ev)
}

func (p *mortalPeer) setErr(err error) {
	p.flagsMu.Lock()
	p.deliverErr = err
	p.flagsMu.Unlock()
}

func (p *mortalPeer) setAlive(alive bool) {
	p.flagsMu.Lock()
	p.alive = alive
	p.flagsMu.Unlock()
}

const (
	propSteps       = 300
	propCapacity    = 10
	propPeerPool    = 20
	propSettleEvery = 25
	propTraceKeep   = 30
)

// propTrace — ring buffer последних операций для диагностики падений.
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

	// Наблюдатель сидит в комнате с самого начала и никогда не уходит.
	shadow := mustJoinShadow(t, h)
	trace := &propTrace{}

	joined := map[domain.PeerID]*mortalPeer{}

	// joinedIDs — отсортированные ID: выбор peer через rnd индексирует
	// стабильный порядок, прогон по seed детерминирован.
	joinedIDs := func() []domain.PeerID {
		ids := make([]domain.PeerID, 0, len(joined))
		for id := range joined {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		return ids
	}
	randomJoined := func() *mortalPeer {
		ids := joinedIDs()
		if len(ids) == 0 {
			return nil
		}
		return joined[ids[rnd.Intn(len(ids))]]
	}

	// --- словарь операций (clean + fault-инъекции); ошибки операций —
	// легальный исход, логируются в trace, инварианты не ломают ---

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
		p := newMortalPeer(id, fmt.Sprintf("p%d", id), int64(rnd.Intn(12)-1))
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
		delete(joined, p.id)
		return fmt.Sprintf("leave %d err=%v", p.id, err)
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
			senderID = p.id
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
		err := h.policy.OnMessage(events.RoomEvent{PeerID: p.id, Frame: frame})
		return fmt.Sprintf("input %d opcode=%#x err=%v", p.id, frame.OpCode, err)
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
				Owner:        p.id,
				Components:   state.NewMapState[state.ComponentID, state.ComponentState](),
			}
		}
		payload, err := h.entitiesCodec.Encode(entities)
		if err != nil {
			return fmt.Sprintf("entities encode err=%v", err)
		}
		err = h.policy.OnMessage(events.RoomEvent{
			PeerID: p.id,
			Frame:  domain.Frame{OpCode: state.OpCodeFullEntities, Payload: payload},
		})
		return fmt.Sprintf("entities %d err=%v", p.id, err)
	}

	// Fault: Deliver падает во время leave-broadcast — OnLeave откатывается,
	// запись остаётся stale в state.Peers.
	flakyLeaveOp := func() string {
		p := randomJoined()
		if p == nil {
			return "flaky leave skipped (empty room)"
		}
		p.setErr(errDeliverFailed)
		err := h.room.Leave(p)
		p.setErr(nil)
		delete(joined, p.id)
		return fmt.Sprintf("flaky leave %d err=%v", p.id, err)
	}

	// Fault: транспорт умер без leave — peer остаётся в room и в state,
	// его Deliver падает; выводит sweep (evictDead).
	killOp := func() string {
		p := randomJoined()
		if p == nil {
			return "kill skipped (empty room)"
		}
		p.setAlive(false)
		return fmt.Sprintf("kill %d (IsAlive=false)", p.id)
	}

	// Sweep-эквивалент EvictIdlePeersUseCase: выводит мёртвых через Leave;
	// leave-broadcast упирается в мёртвый Deliver → stale-записи.
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

	// LeaveRoom-эквивалент: чистый leave + peer.Stop.
	stopOp := func() string {
		p := randomJoined()
		if p == nil {
			return "stop skipped (empty room)"
		}
		err := h.room.Leave(p)
		stopErr := p.Stop()
		delete(joined, p.id)
		return fmt.Sprintf("leave+stop %d err=%v stopErr=%v", p.id, err, stopErr)
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

	// Финал: снимаем все fault-инъекции и выгоняем мёртвых — после этого
	// resync обязан доехать до shadow, иначе протокол не может
	// восстановить клиента.
	for id, p := range joined {
		p.setErr(nil)
		if !p.IsAlive() {
			if err := h.room.Leave(p); err != nil {
				trace.push("final evict %d err=%v", id, err)
			}
			delete(joined, id)
		}
	}
	checkStrong(propSteps, true)
}
