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

// P10: исполняемые инварианты StateRoomPolicy. Поля p.master/p.state
// неэкспортируемы — проверяем клиент-видимое wire-state, материализованное
// из тех кадров, что реально дошли до peer-наблюдателя.

// shadowPeerID — выделенный ID наблюдателя. Высокий ID + unknown ping
// гарантируют: shadow не выигрывает election у тестовых peers и не влияет
// на существующие master-scenario'и.
const shadowPeerID = domain.PeerID(900)

// --- shadow view: материализация клиент-видимого RoomState ---

// shadowView материализует state.RoomState из кадров, реально доставленных
// наблюдателю: full-state заменяет вид, patch применяется поверх — ровно то,
// что видит клиент, без сайд-эффектов.
//
// desync: patch до первого full-state либо ошибка Decode/ApplyPatch — вид
// перестаёт быть авторитетным (расхождение после частично откаченного
// broadcast); инварианты по нему не проверяем до следующего full-state
// (resync — штатный механизм протокола).
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

// seed задаёт начальный вид напрямую — для виртуальных наблюдателей вне room
// (mock-room фикстуры): эквивалент полученного full-state, последующие кадры
// идут поверх.
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

// materialized — клон текущего вида + флаг desync.
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

// --- shadow peer: in-room наблюдатель ---

// shadowPeer — peer-наблюдатель: сидит в комнате как обычный peer (валидный
// кандидат election, но с худшими ping/ID), никогда не уходит, материализует
// wire-state из каждого доставленного кадра. Join mid-room безопасен: первый
// кадр — join full-state, который сидит вид.
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

// mustJoinShadow подсаживает наблюдателя в комнату; он занимает один слот
// capacity — учитывать в тестах с плотной вместимостью.
func mustJoinShadow(t *testing.T, h *harness) *shadowPeer {
	t.Helper()
	sh := newShadowPeer(h.roomCodec)
	if err := h.room.Join(sh); err != nil {
		t.Fatalf("Join shadow peer: %v", err)
	}
	return sh
}

// --- инварианты ---

// checkWeakInvariants — post-op уровень (без settle):
//  1. IsMaster выставлен не более чем у одного peer;
//  2. IsMaster peer присутствует в записях Peers wire-state
//     (флаг у живой записи, не у фантомной).
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

// checkStrongInvariants — settle уровень (после patch-тика, prune-окно
// закрыто): weak + рекламируемый master обязан быть живым peer комнаты.
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

// assertStrongInvariants — только на синхронизированном виде: при desync
// вызывающий код обязан сначала resync (OnTickFullState) или признать окно.
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

// settleTick — один patch-тик: pruneStalePeers закрывает prune-окно
// (stale-записи вычищены, master переизбран) и выносит накопленную
// дельту broadcast'ом — дальше допустимы сильные проверки. Ошибка
// broadcast — легальный fault mode: логируем, не падаем.
func settleTick(t *testing.T, h *harness, sh *shadowView) *state.RoomState {
	t.Helper()
	if err := h.policy.OnTickPatchState(); err != nil {
		t.Logf("settle tick: %v", err)
	}
	view, _ := sh.materialized()
	return view
}

// resyncShadow — full-state рассылка поднимает shadow из desync до
// authoritative state.
func resyncShadow(t *testing.T, h *harness) {
	t.Helper()
	if err := h.policy.OnTickFullState(); err != nil {
		t.Logf("resync full state tick: %v", err)
	}
}

// --- self-tests: чекеры и материализация обязаны ловить нарушения ---

// Материализация: full-state сидит вид, patch применяется поверх.
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

// Patch до первого full-state → desync, а не паника/мусор.
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

// Sanity: заведомо битое состояние (два master / master на мёртвом peer)
// обязано ловиться чекерами — иначе инварианты декоративные.
func TestInvariantCheckers_FlagDeliberatelyBrokenState(t *testing.T) {
	t.Parallel()

	// Два IsMaster — слабый инвариант обязан сломаться.
	dup := state.NewRoomState(domain.RoomID(1), 4)
	dup.Peers[domain.PeerID(1)] = state.PeerState{NickName: "a", IsMaster: true}
	dup.Peers[domain.PeerID(2)] = state.PeerState{NickName: "b", IsMaster: true}
	if err := checkWeakInvariants(dup); err == nil {
		t.Fatal("checkWeakInvariants missed duplicate IsMaster flags")
	}

	// Master-флаг на peer, которого нет в room: слабый чек (структурный)
	// проходит, сильный — обязан упасть.
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

	// Здоровое состояние — оба чекера зелёные.
	ok := state.NewRoomState(domain.RoomID(2), 4)
	ok.Peers[domain.PeerID(1)] = state.PeerState{NickName: "member", IsMaster: true}
	if err := checkStrongInvariants(ok, h.room); err != nil {
		t.Fatalf("healthy state flagged: %v", err)
	}
}
