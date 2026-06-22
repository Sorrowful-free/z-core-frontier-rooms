package codec_test

import (
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

func TestRoomStateCodec_roundTrip_emptyRoom(t *testing.T) {
	t.Parallel()

	c := newRoomCodec()
	room := state.NewRoomState(domain.RoomID(7), 16, "pw")

	data, err := c.Encode(room)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := c.Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.ID != room.ID || got.Capacity != room.Capacity || got.Password != room.Password {
		t.Fatalf("header mismatch: %#v", got)
	}
	if len(got.Peers)+len(got.Inputs)+len(got.Entities) != 0 {
		t.Fatalf("expected empty maps, got peers=%d inputs=%d entities=%d",
			len(got.Peers), len(got.Inputs), len(got.Entities))
	}
}

func TestRoomStateCodec_roundTrip_withPeerInputEntity(t *testing.T) {
	t.Parallel()

	c := newRoomCodec()
	room := state.NewRoomState(domain.RoomID(1), 8, "")

	room.Peers[domain.PeerID(2)] = state.PeerState{NickName: "bob", Ping: 99, IsMaster: true}
	room.Inputs[domain.PeerID(2)] = state.InputState{Values: state.NewMapState[state.ValueId, state.ValueState]()}
	room.Inputs[domain.PeerID(2)].Values[state.ValueId(1)] = state.ValueState([]byte{0xAA})

	room.Entities[state.EntityID(10)] = state.EntityState{
		EntityTypeID: state.EntityTypeID(5),
		Owner:        domain.PeerID(2),
		Components:   state.NewMapState[state.ComponentID, state.ComponentState](),
	}
	room.Entities[state.EntityID(10)].Components[state.ComponentID(1)] = state.ComponentState{
		Values: state.NewMapState[state.ValueId, state.ValueState](),
	}
	room.Entities[state.EntityID(10)].Components[state.ComponentID(1)].Values[state.ValueId(2)] =
		state.ValueState([]byte{1, 2, 3})

	data, err := c.Encode(room)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := c.Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	p2 := got.Peers[domain.PeerID(2)]
	if p2.NickName != "bob" || p2.Ping != 99 || !p2.IsMaster {
		t.Fatalf("peer: %#v", p2)
	}
	wantIn := room.Inputs[domain.PeerID(2)]
	gotIn := got.Inputs[domain.PeerID(2)]
	if !gotIn.Equals(&wantIn, valueEqual) {
		t.Fatalf("input mismatch")
	}
	ent := got.Entities[state.EntityID(10)]
	if ent.EntityTypeID != state.EntityTypeID(5) {
		t.Fatalf("entity_id = %d, want 5", ent.EntityTypeID)
	}
	if ent.Owner != domain.PeerID(2) {
		t.Fatalf("entity owner = %d", ent.Owner)
	}
	wantVal := room.Entities[state.EntityID(10)].Components[state.ComponentID(1)].Values[state.ValueId(2)]
	gotVal := ent.Components[state.ComponentID(1)].Values[state.ValueId(2)]
	if !gotVal.Equals(&wantVal) {
		t.Fatalf("value: got %v want %v", []byte(gotVal), []byte(wantVal))
	}
}

func TestRoomStateCodec_patch_capacityOnly(t *testing.T) {
	t.Parallel()

	c := newRoomCodec()
	cur := state.NewRoomState(domain.RoomID(1), 4, "x")
	next := state.NewRoomState(domain.RoomID(1), 8, "x")

	patch, err := cur.MakePatch(next)
	if err != nil {
		t.Fatalf("MakePatch: %v", err)
	}

	data, err := c.EncodePatch(patch)
	if err != nil {
		t.Fatalf("EncodePatch: %v", err)
	}
	got, err := c.DecodePatch(data)
	if err != nil {
		t.Fatalf("DecodePatch: %v", err)
	}
	if got.Capacity == nil || *got.Capacity != 8 {
		t.Fatalf("capacity patch = %v", got.Capacity)
	}
	if len(got.Peers.Added)+len(got.Peers.Updated)+len(got.Peers.Removed) != 0 {
		t.Fatalf("unexpected peer patch: %#v", got.Peers)
	}

	if err := cur.ApplyPatch(*got); err != nil {
		t.Fatalf("ApplyPatch: %v", err)
	}
	if cur.Capacity != 8 {
		t.Fatalf("capacity = %d", cur.Capacity)
	}
}

func TestRoomStateCodec_patch_addPeer(t *testing.T) {
	t.Parallel()

	c := newRoomCodec()
	cur := state.NewRoomState(domain.RoomID(1), 4, "")
	next := state.NewRoomState(domain.RoomID(1), 4, "")
	next.Peers[domain.PeerID(3)] = state.PeerState{NickName: "x", Ping: 1}

	patch, err := cur.MakePatch(next)
	if err != nil {
		t.Fatalf("MakePatch: %v", err)
	}
	data, err := c.EncodePatch(patch)
	if err != nil {
		t.Fatalf("EncodePatch: %v", err)
	}
	got, err := c.DecodePatch(data)
	if err != nil {
		t.Fatalf("DecodePatch: %v", err)
	}
	if err := cur.ApplyPatch(*got); err != nil {
		t.Fatalf("ApplyPatch: %v", err)
	}
	wantPeer := state.PeerState{NickName: "x", Ping: 1}
	gotPeer := cur.Peers[domain.PeerID(3)]
	if !gotPeer.Equals(&wantPeer) {
		t.Fatalf("peer = %#v", gotPeer)
	}
}
