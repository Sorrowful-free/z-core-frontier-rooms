package state_test

import (
	"errors"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

// Падение Deliver при отправке full-state новому peer пробрасывается
// из OnJoin без потери исходной ошибки.
func TestOnJoin_FailedFullStateDeliver_PropagatesError(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(80), 4)
	shadow := mustJoinShadow(t, h)
	flaky := newFlakyPeer(1, "flaky")
	flaky.setErr(errDeliverFailed)

	err := h.room.Join(flaky)
	if !errors.Is(err, errDeliverFailed) {
		t.Fatalf("Join err = %v, want errDeliverFailed", err)
	}
	assertWeakInvariants(t, shadow.view)

	member := newCapturePeer(2, "member")
	if err := h.room.Join(member); err != nil {
		t.Fatalf("Join member: %v", err)
	}
	settleTick(t, h, shadow.view)
	resyncShadow(t, h)
	assertStrongInvariants(t, shadow.view, h.room)
}

// Отказ peer.Deliver при broadcast full state пробрасывается из OnTickFullState.
func TestOnTickFullState_PeerDeliverFails_PropagatesError(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(81), 4)
	flaky := newFlakyCapturePeer(1, "flaky")
	other := newCapturePeer(2, "other")
	if err := h.room.Join(flaky); err != nil {
		t.Fatalf("Join flaky: %v", err)
	}
	if err := h.room.Join(other); err != nil {
		t.Fatalf("Join other: %v", err)
	}
	shadow := mustJoinShadow(t, h)
	flaky.setErr(errDeliverFailed)

	err := h.policy.OnTickFullState()
	if !errors.Is(err, errDeliverFailed) {
		t.Fatalf("OnTickFullState err = %v, want errDeliverFailed", err)
	}
	assertWeakInvariants(t, shadow.view)

	flaky.setErr(nil)
	settleTick(t, h, shadow.view)
	resyncShadow(t, h)
	assertStrongInvariants(t, shadow.view, h.room)
}

// Отказ peer.Deliver при broadcast patch после вычистки stale master
// пробрасывается из OnTickPatchState.
func TestOnTickPatchState_PrunePatchDeliverFails_PropagatesError(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(82), 4)
	master := newCapturePeer(1, "master")
	member := newFlakyCapturePeer(2, "member")
	if err := h.room.Join(master); err != nil {
		t.Fatalf("Join master: %v", err)
	}
	if err := h.room.Join(member); err != nil {
		t.Fatalf("Join member: %v", err)
	}
	shadow := mustJoinShadow(t, h)

	member.setErr(errDeliverFailed)
	if err := h.room.Leave(master); err == nil {
		t.Fatal("Leave master: want error from failed leave patch broadcast")
	}
	// state: {1: master (stale), 2, 900}; room: {2, 900}.
	assertWeakInvariants(t, shadow.view)

	err := h.policy.OnTickPatchState()
	if !errors.Is(err, errDeliverFailed) {
		t.Fatalf("OnTickPatchState err = %v, want errDeliverFailed", err)
	}
	assertWeakInvariants(t, shadow.view)

	member.setErr(nil)
	settleTick(t, h, shadow.view)
	resyncShadow(t, h)
	assertStrongInvariants(t, shadow.view, h.room)
}

// Отказ broadcast patch в publishRoomStateChange (master full-entities)
// пробрасывается из OnMessage.
func TestOnMessage_FullEntities_BroadcastDeliverFails_PropagatesError(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(83), 4)
	master := newCapturePeer(1, "master")
	member := newFlakyCapturePeer(2, "member")
	if err := h.room.Join(master); err != nil {
		t.Fatalf("Join master: %v", err)
	}
	if err := h.room.Join(member); err != nil {
		t.Fatalf("Join member: %v", err)
	}
	shadow := mustJoinShadow(t, h)

	member.setErr(errDeliverFailed)

	entities := state.NewMapState[state.EntityID, state.EntityState]()
	entities[state.EntityID(10)] = state.EntityState{
		Owner:      domain.PeerID(1),
		Components: state.NewMapState[state.ComponentID, state.ComponentState](),
	}
	payload, err := h.entitiesCodec.Encode(entities)
	if err != nil {
		t.Fatalf("Encode entities: %v", err)
	}

	err = h.policy.OnMessage(events.RoomEvent{
		PeerID: master.id,
		Frame:  domain.Frame{OpCode: state.OpCodeFullEntities, Payload: payload},
	})
	if !errors.Is(err, errDeliverFailed) {
		t.Fatalf("OnMessage err = %v, want errDeliverFailed", err)
	}
	assertWeakInvariants(t, shadow.view)

	member.setErr(nil)
	settleTick(t, h, shadow.view)
	resyncShadow(t, h)
	assertStrongInvariants(t, shadow.view, h.room)
}
