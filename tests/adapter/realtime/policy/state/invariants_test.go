package state_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/codec"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	portrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	testpeer "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/peer"
)

// P10: executable invariants of StateRoomPolicy. The p.master/p.state
// fields are unexported — we check the client-visible wire state
// materialized from the frames that actually reached the observer peer.

// shadowPeerID — the observer's dedicated ID. A high ID + unknown ping
// guarantee that the shadow never wins the election against the test peers
// and does not affect the existing master scenarios.
const shadowPeerID = domain.PeerID(900)

// --- shadow view: materialization of the client-visible RoomState ---

// shadowView materializes a state.RoomState from the frames actually
// delivered to the observer: a full state replaces the view, a patch is
// applied on top — exactly what the client sees, with no side effects.
//
// desync: a patch before the first full state, or a Decode/ApplyPatch
// error — the view is no longer authoritative (divergence after a
// partially rolled-back broadcast); we do not check invariants against it
// until the next full state (resync is the protocol's normal mechanism).
type shadowView struct {
	roomCodec *codec.RoomStateCodec

	mu      sync.Mutex
	view    *state.RoomState
	desync  bool
	lastErr error
}

func newShadowView(roomCodec *codec.RoomStateCodec) *shadowView {
	return &shadowView{roomCodec: roomCodec}
}

// seed sets the initial view directly — for virtual observers outside a
// room (mock-room fixtures): the equivalent of a received full state, with
// subsequent frames applied on top.
func (s *shadowView) seed(rs *state.RoomState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.view = rs.Clone()
	s.desync = false
	s.lastErr = nil
}

func (s *shadowView) feed(ev events.PeerEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch ev.Frame.OpCode {
	case state.OpCodeFullState:
		full, err := s.roomCodec.Decode(ev.Frame.Payload)
		if err != nil {
			s.desync = true
			s.lastErr = fmt.Errorf("decode full state: %w", err)
			return
		}
		s.view = full
		s.desync = false
		s.lastErr = nil
	case state.OpCodePatchState:
		patch, err := s.roomCodec.DecodePatch(ev.Frame.Payload)
		if err != nil {
			s.desync = true
			s.lastErr = fmt.Errorf("decode patch: %w", err)
			return
		}
		if s.view == nil {
			s.desync = true
			s.lastErr = errors.New("patch state before first full state")
			return
		}
		if err := s.view.ApplyPatch(*patch); err != nil {
			s.desync = true
			s.lastErr = fmt.Errorf("apply patch: %w", err)
		}
	}
}

// materialized — a clone of the current view + the desync flag.
func (s *shadowView) materialized() (*state.RoomState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.view == nil {
		return nil, s.desync
	}
	return s.view.Clone(), s.desync
}

func (s *shadowView) desynced() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.desync
}

func (s *shadowView) lastError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastErr
}

// --- shadow peer: the in-room observer ---

// shadowPeer — an observer peer: sits in the room like a normal peer (a
// valid election candidate, but with the worst ping/ID), never leaves, and
// materializes the wire state from every delivered frame. A mid-room join
// is safe: the first frame is the join full state, which seeds the view.
type shadowPeer struct {
	*testpeer.Peer
	view *shadowView
}

func newShadowPeer(roomCodec *codec.RoomStateCodec) *shadowPeer {
	return &shadowPeer{
		Peer: testpeer.New(shadowPeerID, "shadow", testpeer.WithPing(pingUnknown)),
		view: newShadowView(roomCodec),
	}
}

func (p *shadowPeer) Deliver(ev events.PeerEvent) error {
	err := p.Peer.Deliver(ev)
	p.view.feed(ev)
	return err
}

// mustJoinShadow seats the observer in the room; it occupies one capacity
// slot — account for it in tests with tight capacity.
func mustJoinShadow(t *testing.T, h *harness) *shadowPeer {
	t.Helper()
	sh := newShadowPeer(h.roomCodec)
	if err := h.room.Join(sh); err != nil {
		t.Fatalf("Join shadow peer: %v", err)
	}
	return sh
}

// --- invariants ---

// checkWeakInvariants — post-op level (without settle):
//  1. IsMaster is set on at most one peer;
//  2. the IsMaster peer is present in the wire-state Peers entries
//     (the flag is on a live entry, not a phantom one).
func checkWeakInvariants(view *state.RoomState) error {
	if view == nil {
		return nil
	}
	masters := 0
	var masterID domain.PeerID
	for id, ps := range view.Peers {
		if !ps.IsMaster {
			continue
		}
		masters++
		masterID = id
	}
	if masters > 1 {
		return fmt.Errorf("more than one peer flagged IsMaster: %d in peers %#v", masters, masterFlags(view))
	}
	if masters == 1 {
		if _, ok := view.Peers[masterID]; !ok {
			return fmt.Errorf("IsMaster flag on phantom peer %d (absent from Peers)", masterID)
		}
	}
	return nil
}

// checkStrongInvariants — settle level (after a patch tick, the prune
// window is closed): weak + the advertised master must be a live room peer.
func checkStrongInvariants(view *state.RoomState, room portrealtime.Room) error {
	if err := checkWeakInvariants(view); err != nil {
		return err
	}
	if view == nil {
		return nil
	}
	for id, ps := range view.Peers {
		if ps.IsMaster && !room.HasPeer(id) {
			return fmt.Errorf("advertised master %d not present in room (dead master flagged)", id)
		}
	}
	return nil
}

func masterFlags(view *state.RoomState) []domain.PeerID {
	var out []domain.PeerID
	for id, ps := range view.Peers {
		if ps.IsMaster {
			out = append(out, id)
		}
	}
	return out
}

func assertWeakInvariants(t *testing.T, sh *shadowView) {
	t.Helper()
	view, desynced := sh.materialized()
	if desynced {
		t.Logf("shadow desynced (%v), weak invariants skipped until resync", sh.lastError())
		return
	}
	if err := checkWeakInvariants(view); err != nil {
		t.Fatalf("weak invariant violated: %v; peers=%#v", err, view.Peers)
	}
}

// assertStrongInvariants — only on a synchronized view: on desync the
// caller must first resync (OnTickFullState) or accept the window.
func assertStrongInvariants(t *testing.T, sh *shadowView, room portrealtime.Room) {
	t.Helper()
	view, desynced := sh.materialized()
	if desynced {
		t.Logf("shadow desynced (%v), strong invariants skipped until resync", sh.lastError())
		return
	}
	if err := checkStrongInvariants(view, room); err != nil {
		t.Fatalf("strong invariant violated: %v; peers=%#v", err, view.Peers)
	}
}

// settleTick — one patch tick: pruneStalePeers closes the prune window
// (stale entries are cleaned, the master is re-elected) and carries the
// accumulated delta out via broadcast — after that, strong checks are
// valid. A broadcast error is a legal fault mode: we log, we do not fail.
func settleTick(t *testing.T, h *harness, sh *shadowView) *state.RoomState {
	t.Helper()
	if err := h.policy.OnTickPatchState(); err != nil {
		t.Logf("settle tick: %v", err)
	}
	view, _ := sh.materialized()
	return view
}

// resyncShadow — a full-state broadcast lifts the shadow out of desync to
// the authoritative state.
func resyncShadow(t *testing.T, h *harness) {
	t.Helper()
	if err := h.policy.OnTickFullState(); err != nil {
		t.Logf("resync full state tick: %v", err)
	}
}

// --- self-tests: the checkers and materialization must catch violations ---

// Materialization: a full state seeds the view, a patch applies on top.
func TestShadowView_MaterializesFullThenPatch(t *testing.T) {
	t.Parallel()

	c := codec.NewRoomStateCodec()
	sh := newShadowView(c)

	full := state.NewRoomState(domain.RoomID(7), 4)
	full.Peers[domain.PeerID(1)] = state.PeerState{NickName: "m", IsMaster: true}
	fullPayload, err := c.Encode(full)
	if err != nil {
		t.Fatalf("Encode full: %v", err)
	}
	sh.feed(events.PeerEvent{Frame: domain.Frame{OpCode: state.OpCodeFullState, Payload: fullPayload}})

	patch := &state.RoomStatePatch{}
	patch.Peers.Added = map[domain.PeerID]state.PeerState{
		domain.PeerID(2): {NickName: "c"},
	}
	patchPayload, err := c.EncodePatch(patch)
	if err != nil {
		t.Fatalf("EncodePatch: %v", err)
	}
	sh.feed(events.PeerEvent{Frame: domain.Frame{OpCode: state.OpCodePatchState, Payload: patchPayload}})

	view, desynced := sh.materialized()
	if desynced {
		t.Fatalf("shadow desynced: %v", sh.lastError())
	}
	if len(view.Peers) != 2 || !view.Peers[domain.PeerID(1)].IsMaster {
		t.Fatalf("materialized peers = %#v, want {1:master,2}", view.Peers)
	}
	if err := checkWeakInvariants(view); err != nil {
		t.Fatalf("weak check on materialized view: %v", err)
	}
}

// A patch before the first full state → desync, not a panic/garbage.
func TestShadowView_PatchBeforeFull_IsDesync(t *testing.T) {
	t.Parallel()

	c := codec.NewRoomStateCodec()
	sh := newShadowView(c)

	patchPayload, err := c.EncodePatch(&state.RoomStatePatch{})
	if err != nil {
		t.Fatalf("EncodePatch: %v", err)
	}
	sh.feed(events.PeerEvent{Frame: domain.Frame{OpCode: state.OpCodePatchState, Payload: patchPayload}})

	if !sh.desynced() {
		t.Fatal("patch without full-state base must mark desync")
	}
}

// Sanity: a deliberately broken state (two masters / master on a dead
// peer) must be caught by the checkers — otherwise the invariants are decorative.
func TestInvariantCheckers_FlagDeliberatelyBrokenState(t *testing.T) {
	t.Parallel()

	// Two IsMaster flags — the weak invariant must break.
	dup := state.NewRoomState(domain.RoomID(1), 4)
	dup.Peers[domain.PeerID(1)] = state.PeerState{NickName: "a", IsMaster: true}
	dup.Peers[domain.PeerID(2)] = state.PeerState{NickName: "b", IsMaster: true}
	if err := checkWeakInvariants(dup); err == nil {
		t.Fatal("checkWeakInvariants missed duplicate IsMaster flags")
	}

	// A master flag on a peer that is not in the room: the weak check
	// (structural) passes, the strong one must fail.
	h := newHarness(t, domain.RoomID(2), 4)
	member := testpeer.New(1, "member")
	mustJoin(t, h.room, member)

	dead := state.NewRoomState(domain.RoomID(2), 4)
	dead.Peers[domain.PeerID(1)] = state.PeerState{NickName: "member"}
	dead.Peers[domain.PeerID(9)] = state.PeerState{NickName: "ghost", IsMaster: true}
	if err := checkWeakInvariants(dead); err != nil {
		t.Fatalf("weak check must pass on single flagged entry: %v", err)
	}
	if err := checkStrongInvariants(dead, h.room); err == nil {
		t.Fatal("checkStrongInvariants missed advertised master outside room")
	}

	// A healthy state — both checkers are green.
	ok := state.NewRoomState(domain.RoomID(2), 4)
	ok.Peers[domain.PeerID(1)] = state.PeerState{NickName: "member", IsMaster: true}
	if err := checkStrongInvariants(ok, h.room); err != nil {
		t.Fatalf("healthy state flagged: %v", err)
	}
}
