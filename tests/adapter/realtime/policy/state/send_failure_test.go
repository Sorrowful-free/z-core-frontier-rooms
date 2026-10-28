package state_test

import (
	"errors"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	testpeer "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/peer"
)

// Падение Deliver при отправке full-state новому peer пробрасывается
// из OnJoin без потери исходной ошибки.
func TestOnJoin_FailedFullStateDeliver_PropagatesError(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(80), 4)
	shadow := mustJoinShadow(t, h)
	flaky := testpeer.New(1, "flaky")
	flaky.SetDeliverErr(errDeliverFailed)

	err := h.room.Join(flaky)
	if !errors.Is(err, errDeliverFailed) {
		t.Fatalf("Join err = %v, want errDeliverFailed", err)
	}
	assertWeakInvariants(t, shadow.view)

	member := testpeer.New(2, "member")
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
	flaky := testpeer.New(1, "flaky")
	other := testpeer.New(2, "other")
	if err := h.room.Join(flaky); err != nil {
		t.Fatalf("Join flaky: %v", err)
	}
	if err := h.room.Join(other); err != nil {
		t.Fatalf("Join other: %v", err)
	}
	shadow := mustJoinShadow(t, h)
	flaky.SetDeliverErr(errDeliverFailed)

	err := h.policy.OnTickFullState()
	if !errors.Is(err, errDeliverFailed) {
		t.Fatalf("OnTickFullState err = %v, want errDeliverFailed", err)
	}
	assertWeakInvariants(t, shadow.view)

	flaky.SetDeliverErr(nil)
	settleTick(t, h, shadow.view)
	resyncShadow(t, h)
	assertStrongInvariants(t, shadow.view, h.room)
}

// Отказ peer.Deliver при tick-broadcast patch пробрасывается из
// OnTickPatchState; мутация в state уже закоммичена — дельта не теряется
// (prevState коммитится только после успешного send).
func TestOnTickPatchState_PatchDeliverFails_PropagatesError(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(82), 4)
	master := testpeer.New(1, "master")
	member := testpeer.New(2, "member")
	if err := h.room.Join(master); err != nil {
		t.Fatalf("Join master: %v", err)
	}
	if err := h.room.Join(member); err != nil {
		t.Fatalf("Join member: %v", err)
	}
	shadow := mustJoinShadow(t, h)
	mustConverge(t, h)
	member.Clear()

	// Leave сам по себе ничего не шлёт (P34): мутация state + перевыбор
	// на месте — дельта копится до тика.
	if err := h.room.Leave(master); err != nil {
		t.Fatalf("Leave master: %v", err)
	}
	member.SetDeliverErr(errDeliverFailed)

	err := h.policy.OnTickPatchState()
	if !errors.Is(err, errDeliverFailed) {
		t.Fatalf("OnTickPatchState err = %v, want errDeliverFailed", err)
	}
	assertWeakInvariants(t, shadow.view)

	member.SetDeliverErr(nil)
	settleTick(t, h, shadow.view)
	resyncShadow(t, h)
	assertStrongInvariants(t, shadow.view, h.room)
}

// Упавший Deliver другого peer не влияет на OnMessage (P34): обработчик
// только мутирует state и завершается успехом; send-фейл всплывает на
// ближайшем patch-тике.
func TestOnMessage_FullEntities_BystanderDeliverFails_MessageSucceeds(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(83), 4)
	master := testpeer.New(1, "master")
	member := testpeer.New(2, "member")
	if err := h.room.Join(master); err != nil {
		t.Fatalf("Join master: %v", err)
	}
	if err := h.room.Join(member); err != nil {
		t.Fatalf("Join member: %v", err)
	}
	shadow := mustJoinShadow(t, h)
	mustConverge(t, h)

	member.SetDeliverErr(errDeliverFailed)

	entities := state.NewMapState[state.EntityID, state.EntityState]()
	entities[state.EntityID(10)] = state.EntityState{
		Owner:      domain.PeerID(1),
		Components: state.NewMapState[state.ComponentID, state.ComponentState](),
	}
	payload, err := h.entitiesCodec.Encode(entities)
	if err != nil {
		t.Fatalf("Encode entities: %v", err)
	}

	if err := h.policy.OnMessage(events.RoomEvent{
		PeerID: master.GetID(),
		Frame:  domain.Frame{OpCode: state.OpCodeFullEntities, Payload: payload},
	}); err != nil {
		t.Fatalf("OnMessage: %v (send-фейл не должен доходить до OnMessage)", err)
	}
	assertWeakInvariants(t, shadow.view)

	err = h.policy.OnTickPatchState()
	if !errors.Is(err, errDeliverFailed) {
		t.Fatalf("OnTickPatchState err = %v, want errDeliverFailed", err)
	}

	member.SetDeliverErr(nil)
	settleTick(t, h, shadow.view)
	resyncShadow(t, h)
	assertStrongInvariants(t, shadow.view, h.room)
}
