package integration_test

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/codec"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/env"
)

// Shared client layer of the "live match" e2e (plans docs/plans/P11-e2e-match.md,
// P12-e2e-enet-mixed.md): the transport-agnostic part of the client + the
// scenario driver. Transport implementations: wsClient (match_e2e_test.go) and
// enetClient (match_e2e_enet_test.go, tag enet && cgo).
//
// One scenario, parameterized by the set of clients and the timings:
//
//	join → everyone sends PatchInput at ~20Hz, the current master — PatchEntities →
//	the host goes silent (dropout) → the server evicts it → master migration →
//	the second master leaves cleanly → migration #2 → final: ≥2 alive.
//
// Asserts run on wire-frame observations of the clients (masterSeq from
// is_master, entity Owner, Inputs), not on server internals.

const (
	clientSendInterval = 50 * time.Millisecond

	inputValueProbe = state.ValueId(1)
	posComponentID  = state.ComponentID(1)
	posValueID      = state.ValueId(1)
	entityTypeProbe = state.EntityTypeID(1)
)

// matchClient — what the scenario driver requires from a client of any transport.
// goSilent/leaveClean/stop — transport-specific ways of dropping and leaving.
type matchClient interface {
	nickName() string
	peer() domain.PeerID
	master() bool // the server holds is_master for us
	dead() bool   // transport/reads are dead (silent, disconnect, write fail)
	sendDisabled() bool
	goSilent()   // dropout: stop reading/sending, transport is formally alive
	leaveClean() // scheduled leave: the server gets a disconnect → LeaveRoom
	stop()       // final teardown (idempotent after silent/leftClean)

	mastersSeen() []domain.PeerID
	entityOwnersSeen() map[domain.PeerID]bool
	inputNicksSeen() map[string]bool
	ownInputObserved() bool
	lastFrame() time.Time
}

// matchClientBase — the transport-agnostic part of the client: codecs, shadow
// RoomState, observations (masterSeq/entityOwners/inputNicks), the stats loop
// and the send loop. A concrete client embeds the base and implements pump +
// writeFrame + the leave paths. The transport must close pumpDone when the
// pump exits — statsLoop finishes right after.
type matchClientBase struct {
	t    *testing.T
	nick string

	roomCodec     *codec.RoomStateCodec
	entitiesCodec *codec.EntitiesStateCodec
	inputCodec    *codec.InputStateCodec
	rnd           *rand.Rand // random input payload, seeded by nick — reproducible

	peerID    atomic.Uint32
	isMaster  atomic.Bool
	silent    atomic.Bool // dropout: we don't read or send, transport is alive
	connDead  atomic.Bool // pump saw a connection error/close
	leftClean atomic.Bool // scheduled leave/stop — the drop is expected

	quit     chan struct{} // closing stops the send loop
	quitOnce sync.Once
	pumpDone chan struct{}
	sendDone chan struct{}

	mu           sync.Mutex
	room         *state.RoomState         // local replica (nil until the first full)
	masterSeq    []domain.PeerID          // sequence of is_master holders
	peerNicks    map[domain.PeerID]string // nick lookup, kept after leave
	inputNicks   map[string]bool          // whose input was seen in state (payload check)
	ownInputSeen bool                     // own input came back via broadcast
	entityOwners map[domain.PeerID]bool   // distinct Owner in entities (sender masters)
	entityPos    map[domain.PeerID]string // Owner → latest position (for first-frame log)
	patchCount   int
	lastFrameAt  time.Time
}

func newMatchClientBase(t *testing.T, nick string) matchClientBase {
	t.Helper()

	seed := fnv.New64a()
	_, _ = seed.Write([]byte(nick))
	return matchClientBase{
		t:             t,
		nick:          nick,
		roomCodec:     codec.NewRoomStateCodec(),
		entitiesCodec: codec.NewEntitiesStateCodec(),
		inputCodec:    codec.NewInputStateCodec(),
		rnd:           rand.New(rand.NewPCG(seed.Sum64(), 0x9e3779b97f4a7c15)),
		quit:          make(chan struct{}),
		pumpDone:      make(chan struct{}),
		sendDone:      make(chan struct{}),
		peerNicks:     make(map[domain.PeerID]string),
		inputNicks:    make(map[string]bool),
		entityOwners:  make(map[domain.PeerID]bool),
		entityPos:     make(map[domain.PeerID]string),
	}
}

func (b *matchClientBase) nickName() string    { return b.nick }
func (b *matchClientBase) peer() domain.PeerID { return domain.PeerID(b.peerID.Load()) }
func (b *matchClientBase) master() bool        { return b.isMaster.Load() }
func (b *matchClientBase) dead() bool          { return b.connDead.Load() }
func (b *matchClientBase) stopSend()           { b.quitOnce.Do(func() { close(b.quit) }) }
func (b *matchClientBase) sendDisabled() bool {
	return b.silent.Load() || b.connDead.Load() || b.leftClean.Load()
}

// statsLoop — a periodic digest of the observed state: shows the state is
// actually flowing (positions change, patches arrive), not just the "first"
// events. Printed every 5s while the client is alive.
func (b *matchClientBase) statsLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-b.pumpDone:
			return
		case <-ticker.C:
			b.logStats()
		}
	}
}

func (b *matchClientBase) logStats() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.room == nil {
		b.t.Logf("[%s] stats: no state yet (patches=%d)", b.nick, b.patchCount)
		return
	}
	master := "-"
	positions := make([]string, 0, len(b.entityPos))
	for pid, ps := range b.room.Peers {
		if ps.IsMaster {
			master = b.peerNicks[pid]
		}
	}
	for owner, pos := range b.entityPos {
		positions = append(positions, fmt.Sprintf("%s=%s", b.peerNicks[owner], pos))
	}
	b.t.Logf("[%s] stats: peers=%d master=%s entities={%s} inputsSeen=%d patches=%d me=%d",
		b.nick, len(b.room.Peers), master, strings.Join(positions, ","),
		len(b.inputNicks), b.patchCount, b.peerID.Load())
}

// --- incoming frames ----------------------------------------------------------

// handleFrame decodes server→client frames (0x01/0x02 state, 0x07 rpc,
// 0x60-0x6F in-room errors) — the wire protocol shared by WS and ENet.
func (b *matchClientBase) handleFrame(op domain.OpCode, payload []byte) {
	switch op {
	case state.OpCodeFullState:
		full, err := b.roomCodec.Decode(payload)
		if err != nil {
			b.t.Logf("[%s] full state decode failed: %v", b.nick, err)
			return
		}
		b.applyFull(full)
	case state.OpCodePatchState:
		patch, err := b.roomCodec.DecodePatch(payload)
		if err != nil {
			b.t.Logf("[%s] patch state decode failed: %v", b.nick, err)
			return
		}
		b.applyPatch(patch)
	case state.OpCodeRpc:
		b.t.Logf("[%s] rpc frame, %d bytes", b.nick, len(payload))
	default:
		if op >= 0x60 && op <= 0x6F {
			// In-room errors (e.g. OpInRoomNotMaster) — an event feed, not a failure.
			b.t.Logf("[%s] in-room error frame: op=0x%02x", b.nick, byte(op))
			return
		}
		b.t.Logf("[%s] unknown frame op=0x%02x, %d bytes", b.nick, byte(op), len(payload))
	}
}

func (b *matchClientBase) applyFull(full *state.RoomState) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.room = full
	b.observeLocked()
}

func (b *matchClientBase) applyPatch(patch *state.RoomStatePatch) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.room == nil {
		b.t.Logf("[%s] patch before first full state — skipped", b.nick)
		return
	}
	if err := b.room.ApplyPatch(*patch); err != nil {
		// the full-state resync every 4s will heal the local replica.
		b.t.Logf("[%s] apply patch failed: %v", b.nick, err)
		return
	}
	b.patchCount++
	b.observeLocked()
}

// observeLocked — all the state-driven client logic: own peer id by nick,
// is_master → the send loop starts streaming entities, masterSeq, inputs,
// entities.
func (b *matchClientBase) observeLocked() {
	rs := b.room
	if rs == nil {
		return
	}
	b.lastFrameAt = time.Now()

	for pid, ps := range rs.Peers {
		b.peerNicks[pid] = ps.NickName
	}

	selfID := domain.PeerID(b.peerID.Load())
	if !selfID.IsValid() {
		for pid, ps := range rs.Peers {
			if ps.NickName == b.nick {
				b.peerID.Store(uint32(pid))
				selfID = pid
				b.t.Logf("[%s] joined as peer %d (room %d, capacity %d)", b.nick, pid, rs.ID, rs.Capacity)
			}
		}
	}

	for pid, ps := range rs.Peers {
		if !ps.IsMaster {
			continue
		}
		if len(b.masterSeq) == 0 || b.masterSeq[len(b.masterSeq)-1] != pid {
			b.masterSeq = append(b.masterSeq, pid)
			b.t.Logf("[%s] master is now %s (peer %d)", b.nick, b.peerNicks[pid], pid)
		}
	}

	if selfID.IsValid() {
		if ps, ok := rs.Peers[selfID]; ok && ps.IsMaster != b.isMaster.Load() {
			b.isMaster.Store(ps.IsMaster)
			if ps.IsMaster {
				b.t.Logf("[%s] became master — start entities stream", b.nick)
			} else {
				b.t.Logf("[%s] lost master — stop entities stream", b.nick)
			}
		}
		if in, ok := rs.Inputs[selfID]; ok && !b.ownInputSeen {
			if v, ok := in.Values[inputValueProbe]; ok && strings.HasPrefix(string(v), b.nick+":") {
				b.ownInputSeen = true
				b.t.Logf("[%s] my input observed in room state", b.nick)
			}
		}
	}

	for pid, in := range rs.Inputs {
		v, ok := in.Values[inputValueProbe]
		if !ok {
			continue
		}
		nick := b.peerNicks[pid]
		// the payload is verified by the nick prefix — the input is definitely this client's.
		if nick != "" && strings.HasPrefix(string(v), nick+":") && !b.inputNicks[nick] {
			b.inputNicks[nick] = true
			b.t.Logf("[%s] observed input of %s (peer %d)", b.nick, nick, pid)
		}
	}

	for eid, ent := range rs.Entities {
		pos := entityPosKey(ent)
		prev, seen := b.entityPos[ent.Owner]
		if !seen {
			b.entityOwners[ent.Owner] = true
			b.entityPos[ent.Owner] = pos
			b.t.Logf("[%s] first entity snapshot id=%d from owner %s (peer %d), pos %s",
				b.nick, eid, b.peerNicks[ent.Owner], ent.Owner, pos)
		} else if prev != pos {
			b.entityPos[ent.Owner] = pos
		}
	}
}

func entityPosKey(ent state.EntityState) string {
	comp, ok := ent.Components[posComponentID]
	if !ok {
		return ""
	}
	v, ok := comp.Values[posValueID]
	if !ok {
		return ""
	}
	return fmt.Sprintf("%x", []byte(v))
}

// --- send loop --------------------------------------------------------------

// sendLoop — the shared send loop; write — the transport-specific frame write
// (OpCode + payload). PatchInput always (~20Hz), PatchEntities — while the
// client holds the is_master flag.
func (b *matchClientBase) sendLoop(write func(domain.OpCode, []byte) error) {
	defer close(b.sendDone)
	ticker := time.NewTicker(clientSendInterval)
	defer ticker.Stop()

	prevInput := state.InputState{Values: state.NewMapState[state.ValueId, state.ValueState]()}
	prevEntities := state.NewMapState[state.EntityID, state.EntityState]()
	var tick uint64

	for {
		select {
		case <-b.quit:
			return
		case <-ticker.C:
			// peerID becomes valid only after admit + the first full state:
			// sending frames before that is pointless (the peer is not in the
			// room yet), and on ENet it is dangerous: frames that hit the wire
			// before the ticket packet get consumed by the server as an admit →
			// invalid token → disconnect.
			if b.sendDisabled() || !domain.PeerID(b.peerID.Load()).IsValid() {
				continue
			}
			tick++
			b.sendInput(tick, &prevInput, write)
			// State-driven: entities only while the server keeps us as master.
			if b.isMaster.Load() {
				b.sendEntities(tick, &prevEntities, write)
			}
		}
	}
}

// sendInput — PatchInput (0x06) with a random "nick:<rand>:<tick>" value in
// inputValueProbe. The patch is built by MakePatch against the previous state →
// the first frame comes out as Added, then Updated — the server-side apply
// requires exactly this distinction.
func (b *matchClientBase) sendInput(tick uint64, prev *state.InputState, write func(domain.OpCode, []byte) error) {
	next := state.InputState{Values: state.MapState[state.ValueId, state.ValueState]{
		inputValueProbe: state.ValueState(fmt.Sprintf("%s:%x:%d", b.nick, b.rnd.Uint64(), tick)),
	}}
	patch, err := prev.MakePatch(&next)
	*prev = next
	if err != nil {
		b.t.Logf("[%s] input make patch: %v", b.nick, err)
		return
	}
	if patch == nil {
		return
	}
	payload, err := b.inputCodec.EncodePatch(patch)
	if err != nil {
		b.t.Logf("[%s] input encode: %v", b.nick, err)
		return
	}
	if err := write(state.OpCodePatchInput, payload); err != nil {
		b.connDead.Store(true)
		b.t.Logf("[%s] input write failed: %v", b.nick, err)
	}
}

// sendEntities — PatchEntities (0x04), entity id = own peer id, Owner = own
// peer id → the Owner on the wire shows which master is streaming positions.
func (b *matchClientBase) sendEntities(tick uint64, prev *state.EntitiesState, write func(domain.OpCode, []byte) error) {
	selfID := domain.PeerID(b.peerID.Load())
	if !selfID.IsValid() {
		return
	}
	pos := make([]byte, 8)
	binary.BigEndian.PutUint64(pos, tick)
	next := state.EntitiesState{
		state.EntityID(selfID): state.EntityState{
			EntityTypeID: entityTypeProbe,
			Owner:        selfID,
			Components: state.MapState[state.ComponentID, state.ComponentState]{
				posComponentID: {Values: state.MapState[state.ValueId, state.ValueState]{posValueID: pos}},
			},
		},
	}
	patch, err := state.MakeMapStatePatch(*prev, next,
		func(a, b state.EntityState) bool {
			return a.Equals(&b, func(c1, c2 state.ComponentState) bool {
				return c1.Equals(&c2, func(v1, v2 state.ValueState) bool { return v1.Equals(&v2) })
			})
		},
		func(a, b state.EntityState) (*state.EntityStatePatch, error) {
			return a.MakePatch(&b)
		})
	*prev = next
	if err != nil {
		b.t.Logf("[%s] entities make patch: %v", b.nick, err)
		return
	}
	if patch == nil {
		return
	}
	payload, err := b.entitiesCodec.EncodePatch(patch)
	if err != nil {
		b.t.Logf("[%s] entities encode: %v", b.nick, err)
		return
	}
	if err := write(state.OpCodePatchEntities, payload); err != nil {
		b.connDead.Store(true)
		b.t.Logf("[%s] entities write failed: %v", b.nick, err)
	}
}

// --- observations for asserts (mu) --------------------------------------------

func (b *matchClientBase) mastersSeen() []domain.PeerID {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]domain.PeerID(nil), b.masterSeq...)
}

func (b *matchClientBase) entityOwnersSeen() map[domain.PeerID]bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make(map[domain.PeerID]bool, len(b.entityOwners))
	for k, v := range b.entityOwners {
		out[k] = v
	}
	return out
}

func (b *matchClientBase) inputNicksSeen() map[string]bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make(map[string]bool, len(b.inputNicks))
	for k, v := range b.inputNicks {
		out[k] = v
	}
	return out
}

func (b *matchClientBase) ownInputObserved() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ownInputSeen
}

func (b *matchClientBase) lastFrame() time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastFrameAt
}

// --- scenario helpers -------------------------------------------------------

func waitFor(t *testing.T, timeout time.Duration, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout %s waiting for: %s", timeout, desc)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Logf("[scenario] %s — ok", desc)
}

func sleepUntil(deadline time.Time) {
	if d := time.Until(deadline); d > 0 {
		time.Sleep(d)
	}
}

// currentMaster returns the client that currently holds is_master.
func currentMaster(t *testing.T, clients []matchClient) matchClient {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		for _, c := range clients {
			if c.master() && !c.sendDisabled() {
				return c
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("no live master client found")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// waitPeerCount — e.WaitPeerCount with a configurable deadline: the default
// pollPeers in env gives ~1s, too little for ENet join/evict (a handshake with
// libenet retries can take seconds; the drop timeout is awaited separately in
// evictBound).
func waitPeerCount(t *testing.T, e *env.Env, roomID domain.RoomID, want int, timeout time.Duration) {
	t.Helper()
	waitFor(t, timeout, fmt.Sprintf("peer count = %d", want), func() bool {
		room, err := e.Registry.GetRoom(context.Background(), roomID)
		if err != nil {
			t.Fatalf("get room %d: %v", roomID, err)
		}
		return len(room.GetPeers()) == want
	})
}

// --- scenario driver ----------------------------------------------------------

// liveMatchTiming — scenario chronology parameters. They differ in the
// mechanism that evicts the silent host: WS — the idle sweep (~6s), ENet — the
// honest libenet peer timeout (~5–30s).
type liveMatchTiming struct {
	hostSilentAt  time.Duration // t: host dropout
	evictBound    time.Duration // bound for awaiting the eviction + migration #1
	secondLeaveAt time.Duration // t: clean leave of master #2
	finishAt      time.Duration // t: final checks
}

// runLiveMatch — the shared "live match" scenario on top of matchClient of any
// transport: host (first joiner, initial master) + slaves. Assumes the clients
// are already created and connecting; teardown is the caller's responsibility.
func runLiveMatch(t *testing.T, e *env.Env, roomID domain.RoomID, host matchClient, slaves []matchClient, tm liveMatchTiming) {
	t.Helper()

	start := time.Now()
	clients := append([]matchClient{host}, slaves...)

	waitPeerCount(t, e, roomID, len(clients), 30*time.Second)

	waitFor(t, 10*time.Second, "all clients discovered their peer id", func() bool {
		for _, c := range clients {
			if !c.peer().IsValid() {
				return false
			}
		}
		return true
	})
	hostPeerID := host.peer()

	// Phase 1: master=host streams positions, everyone's input flows into state.
	waitFor(t, 12*time.Second, "each slave observed entity positions from host", func() bool {
		for _, s := range slaves {
			if !s.entityOwnersSeen()[hostPeerID] {
				return false
			}
		}
		return true
	})
	waitFor(t, 10*time.Second, "each client sees own input in room state", func() bool {
		for _, c := range clients {
			if !c.ownInputObserved() {
				return false
			}
		}
		return true
	})

	// t=hostSilentAt — host dropout: transport open, we stop reading/sending.
	sleepUntil(start.Add(tm.hostSilentAt))
	t.Logf("[scenario] host goes silent (transport open, no reads, no sends)")
	host.goSilent()

	// Migration #1: the server evicts the host (idle sweep / enet timeout) → is_master moves on.
	waitFor(t, tm.evictBound, "master migrated off silent host", func() bool {
		for _, s := range slaves {
			if seq := s.mastersSeen(); len(seq) >= 2 && seq[len(seq)-1] != hostPeerID {
				return true
			}
		}
		return false
	})
	waitPeerCount(t, e, roomID, len(clients)-1, 15*time.Second)

	// The new master is state-driven: it saw is_master → started sending its entities.
	waitFor(t, 15*time.Second, "slave observed entity positions from second master", func() bool {
		for _, s := range slaves {
			for owner := range s.entityOwnersSeen() {
				if owner != hostPeerID {
					return true
				}
			}
		}
		return false
	})

	// t=secondLeaveAt — the current master leaves cleanly → migration #2.
	sleepUntil(start.Add(tm.secondLeaveAt))
	m2 := currentMaster(t, slaves)
	t.Logf("[scenario] master %s (peer %d) leaves cleanly", m2.nickName(), m2.peer())
	m2.leaveClean()

	waitFor(t, 15*time.Second, "second master migration observed", func() bool {
		for _, s := range slaves {
			if s == m2 {
				continue
			}
			seq := s.mastersSeen()
			if len(seq) >= 3 {
				last := seq[len(seq)-1]
				if last != hostPeerID && last != m2.peer() {
					return true
				}
			}
		}
		return false
	})
	waitPeerCount(t, e, roomID, len(clients)-2, 15*time.Second)

	// Phase 3: life under master #3 until the end of the scenario.
	sleepUntil(start.Add(tm.finishAt))

	var survivors []matchClient
	for _, s := range slaves {
		if s != m2 {
			survivors = append(survivors, s)
		}
	}
	if want := len(slaves) - 1; len(survivors) != want {
		t.Fatalf("survivors = %d, want %d", len(survivors), want)
	}

	// (a) slaves saw positions from the host AND from a second distinct master.
	for _, s := range survivors {
		owners := s.entityOwnersSeen()
		if !owners[hostPeerID] {
			t.Fatalf("[%s] entity positions from host not observed (owners %v)", s.nickName(), owners)
		}
		second := false
		for owner := range owners {
			if owner != hostPeerID {
				second = true
			}
		}
		if !second {
			t.Fatalf("[%s] no entity positions from a second master (owners %v)", s.nickName(), owners)
		}
	}

	// (b) every slave's input reached someone else's state (payload verified by
	// nick); in the mixed scenario this also covers cross-transport (WS↔ENet).
	for _, s := range slaves {
		seen := false
		for _, other := range clients {
			if other == s {
				continue
			}
			if other.inputNicksSeen()[s.nickName()] {
				seen = true
				break
			}
		}
		if !seen {
			t.Fatalf("input of %s not observed in any other client's room state", s.nickName())
		}
	}

	// (c) two migrations via is_master: host → m2 → m3 on every survivor.
	for _, s := range survivors {
		seq := s.mastersSeen()
		if len(seq) < 3 {
			t.Fatalf("[%s] master sequence %v: want >=3 holders (2 migrations)", s.nickName(), seq)
		}
		if seq[0] != hostPeerID {
			t.Fatalf("[%s] master sequence %v: first holder must be host peer %d", s.nickName(), seq, hostPeerID)
		}
	}

	// (d) final: ≥2 clients alive and frames flowing (patch tick 50ms + input traffic).
	live := 0
	for _, c := range clients {
		if !c.dead() {
			live++
		}
	}
	if live < 2 {
		t.Fatalf("live clients = %d, want >=2", live)
	}
	for _, s := range survivors {
		if s.dead() {
			t.Fatalf("[%s] survivor connection is dead", s.nickName())
		}
		if since := time.Since(s.lastFrame()); since > 4*time.Second {
			t.Fatalf("[%s] no state frames for %s — stream stalled", s.nickName(), since)
		}
	}

	// (e) peers in the registry converged with what the clients see.
	waitPeerCount(t, e, roomID, len(clients)-2, 15*time.Second)
}
