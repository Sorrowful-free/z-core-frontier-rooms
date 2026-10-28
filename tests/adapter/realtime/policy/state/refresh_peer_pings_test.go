package state_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/stdlib"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/codec"
	statepolicy "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/policy/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/mocks"
	testpeer "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/peer"
	"go.uber.org/mock/gomock"
)

// P34: prune-половина бывшего refreshPeerPings живёт на patch-тике
// (pruneStalePeers): записи peers, которых уже нет в room (GetPeer →
// ErrPeerNotFound), вычищаются за один тик, master переизбирается.
// С реальной Room stale-запись не получить (leave вычищает state
// атомарно с изъятием из r.peers) — фикстура на mock room.
func TestOnTickPatchState_PrunesAllStalePeersAndReelectsMaster(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	room := mocks.NewMockRoom(ctrl)

	var sent []events.PeerEvent
	room.EXPECT().GetID().Return(domain.RoomID(60)).AnyTimes()
	room.EXPECT().GetCapacity().Return(int8(4)).AnyTimes()
	room.EXPECT().Send(gomock.Any()).DoAndReturn(func(ev events.PeerEvent) error {
		sent = append(sent, ev)
		return nil
	}).AnyTimes()

	master := testpeer.New(1, "master")
	member := testpeer.New(2, "member")
	survivor := testpeer.New(3, "survivor")
	// master и member выпали из room — записи в state остались (stale);
	// survivor жив — ему достаётся переизбрание.
	room.EXPECT().GetPeer(master.GetID()).
		Return(nil, fmt.Errorf("%w: %d", domain.ErrPeerNotFound, master.GetID())).AnyTimes()
	room.EXPECT().GetPeer(member.GetID()).
		Return(nil, fmt.Errorf("%w: %d", domain.ErrPeerNotFound, member.GetID())).AnyTimes()
	room.EXPECT().GetPeer(survivor.GetID()).Return(survivor, nil).AnyTimes()

	policy := statepolicy.NewStateRoomPolicy(
		stdlib.New("state-policy-test"),
		codec.NewRoomStateCodec(),
		codec.NewEntitiesStateCodec(),
		codec.NewInputStateCodec(),
		codec.NewRpcStateCodec(),
		time.Hour,
		time.Hour,
	)
	if err := policy.OnStart(room); err != nil {
		t.Fatalf("OnStart: %v", err)
	}
	if err := policy.OnJoin(master); err != nil {
		t.Fatalf("OnJoin master: %v", err)
	}
	if err := policy.OnJoin(member); err != nil {
		t.Fatalf("OnJoin member: %v", err)
	}
	if err := policy.OnJoin(survivor); err != nil {
		t.Fatalf("OnJoin survivor: %v", err)
	}
	sent = nil

	if err := policy.OnTickPatchState(); err != nil {
		t.Fatalf("OnTickPatchState: %v", err)
	}

	// Один тик: stale {1,2} вычищены из state, survivor избран — на wire
	// это Added с is_master (prevState ещё не видел ни одной записи).
	patches := testpeer.FilterByOpcode(sent, state.OpCodePatchState)
	if len(patches) != 1 {
		t.Fatalf("patches = %d, want 1", len(patches))
	}
	patch, err := codec.NewRoomStateCodec().DecodePatch(patches[0].Frame.Payload)
	if err != nil {
		t.Fatalf("DecodePatch: %v", err)
	}
	added, ok := patch.Peers.Added[survivor.GetID()]
	if !ok || !added.IsMaster {
		t.Fatalf("peers.added[%d] = %#v ok=%v, want IsMaster=true", survivor.GetID(), added, ok)
	}
	if len(patch.Peers.Added) != 1 {
		t.Fatalf("peers.added = %#v, want only survivor (stale не рекламируются)", patch.Peers.Added)
	}

	sent = nil
	if err := policy.OnTickFullState(); err != nil {
		t.Fatalf("OnTickFullState: %v", err)
	}
	fulls := testpeer.FilterByOpcode(sent, state.OpCodeFullState)
	if len(fulls) != 1 {
		t.Fatalf("full states = %d, want 1", len(fulls))
	}
	got, err := codec.NewRoomStateCodec().Decode(fulls[0].Frame.Payload)
	if err != nil {
		t.Fatalf("Decode full state: %v", err)
	}
	if len(got.Peers) != 1 {
		t.Fatalf("state peers = %#v, want only survivor after stale prune", got.Peers)
	}
	if !got.Peers[survivor.GetID()].IsMaster {
		t.Fatal("survivor must be master after stale master pruned")
	}
}

// P34-сплит: ping перечитывается только на full-тике и едет в full-кадре.
// Patch-тик ping в state не пишет — patch-diff его не видит и не несёт.
func TestPeerPings_RefreshedOnFullTickOnly(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(61), 4)
	master := testpeer.New(1, "master")
	member := testpeer.New(2, "member")
	mustJoin(t, h.room, master, member)
	mustConverge(t, h)
	master.Clear()
	member.Clear()

	member.SetPing(123)

	if err := h.policy.OnTickPatchState(); err != nil {
		t.Fatalf("OnTickPatchState: %v", err)
	}
	if got := master.Snapshot(); len(got) != 0 {
		t.Fatalf("master events on patch tick = %v, want none (ping не едет в patch)",
			testpeer.Opcodes(got))
	}

	if err := h.policy.OnTickFullState(); err != nil {
		t.Fatalf("OnTickFullState: %v", err)
	}
	full := master.EventsByOpcode(state.OpCodeFullState)
	if len(full) != 1 {
		t.Fatalf("master full states = %d, want 1", len(full))
	}
	got, err := h.roomCodec.Decode(full[0].Frame.Payload)
	if err != nil {
		t.Fatalf("Decode full state: %v", err)
	}
	if ping := got.Peers[member.GetID()].Ping; ping != 123 {
		t.Fatalf("member ping in full state = %d, want 123", ping)
	}
}
