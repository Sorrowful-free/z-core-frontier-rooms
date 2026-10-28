package state_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/stdlib"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/codec"
	statepolicy "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/policy/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	testpeer "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/peer"
)

// newNoMigrationHarness — the same assembly as newHarness, but the room
// gets a StateRoomPolicy with allowMasterMigration=false (P33) via the
// production factory with a StatePolicyConfig.
func newNoMigrationHarness(t *testing.T, roomID domain.RoomID, capacity int) *harness {
	t.Helper()

	logger := stdlib.New("no-migration-policy-test")
	policy := statepolicy.NewStateRoomPolicyFactory(logger,
		statepolicy.StatePolicyConfig{AllowMasterMigration: false}, nil).CreateRoomPolicy()
	room := realtime.NewRoom(context.Background(), roomID, policy, capacity, nil, 0, logger)
	if err := room.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = room.Stop() })

	return &harness{
		room:          room,
		policy:        policy,
		roomCodec:     codec.NewRoomStateCodec(),
		entitiesCodec: codec.NewEntitiesStateCodec(),
		inputCodec:    codec.NewInputStateCodec(),
	}
}

func TestStateRoomPolicyFactory_BuildsStateRoomPolicy(t *testing.T) {
	t.Parallel()

	logger := stdlib.New("policy-factory-test")
	for _, cfg := range []statepolicy.StatePolicyConfig{
		{AllowMasterMigration: true},
		{AllowMasterMigration: false},
	} {
		got := statepolicy.NewStateRoomPolicyFactory(logger, cfg, nil).CreateRoomPolicy()
		if _, ok := got.(*statepolicy.StateRoomPolicy); !ok {
			t.Fatalf("cfg %+v → %T, want *StateRoomPolicy", cfg, got)
		}
	}
}

// P33: in a no-migration deployment the master's leave kills the room —
// OnLeave answers ErrRoomShouldClose with no re-election and no remove patch.
func TestNoMigrationPolicy_MasterLeave_RoomShouldClose(t *testing.T) {
	t.Parallel()

	h := newNoMigrationHarness(t, domain.RoomID(60), 4)
	master := testpeer.New(1, "master")
	client := testpeer.New(2, "client")
	mustJoin(t, h.room, master, client)
	master.Clear()
	client.Clear()

	err := h.room.Leave(master)
	if !errors.Is(err, domain.ErrRoomShouldClose) {
		t.Fatalf("Leave master err = %v, want ErrRoomShouldClose", err)
	}
	if h.room.HasPeer(master.GetID()) {
		t.Fatal("master must be removed from room peers")
	}
	// No re-election/patch to the remaining peer — the room is dying.
	if got := len(client.Snapshot()); got != 0 {
		t.Fatalf("client events after master leave = %d, want 0 (no re-elect patch)", got)
	}
}

// P33: the master leaving an emptying room (it was the only one) — the
// same death signal: the room is closed by teardown instead of living on
// empty.
func TestNoMigrationPolicy_LastPeerMasterLeave_RoomShouldClose(t *testing.T) {
	t.Parallel()

	h := newNoMigrationHarness(t, domain.RoomID(61), 4)
	master := testpeer.New(1, "master")
	mustJoin(t, h.room, master)

	if err := h.room.Leave(master); !errors.Is(err, domain.ErrRoomShouldClose) {
		t.Fatalf("Leave err = %v, want ErrRoomShouldClose", err)
	}
}

// P33: a non-master leaving — the regular StateRoomPolicy path: a remove
// patch to the master, no errors, the room lives on.
func TestNoMigrationPolicy_NonMasterLeave_RegularStateFlow(t *testing.T) {
	t.Parallel()

	h := newNoMigrationHarness(t, domain.RoomID(62), 4)
	master := testpeer.New(1, "master")
	client := testpeer.New(2, "client")
	mustJoin(t, h.room, master, client)
	mustConverge(t, h)
	master.Clear()
	client.Clear()

	if err := h.room.Leave(client); err != nil {
		t.Fatalf("Leave client: %v", err)
	}
	// The remove patch goes out not with the leave but on the next patch tick (P34).
	mustConverge(t, h)

	patches := master.EventsByOpcode(state.OpCodePatchState)
	if len(patches) != 1 {
		t.Fatalf("master patches after client leave = %d, want 1 remove-patch", len(patches))
	}
	patch, err := h.roomCodec.DecodePatch(patches[0].Frame.Payload)
	if err != nil {
		t.Fatalf("DecodePatch: %v", err)
	}
	if !slices.Contains(patch.Peers.Removed, domain.PeerID(2)) {
		t.Fatalf("patch peers.removed = %#v, want peer 2", patch.Peers.Removed)
	}
	if got := master.EventsByOpcode(domain.OpRoomClosed); len(got) != 0 {
		t.Fatal("OpRoomClosed must not be sent by policy — it's emitted by usecase teardown")
	}
}
