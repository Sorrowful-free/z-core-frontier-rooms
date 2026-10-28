package codec_test

import (
	"bytes"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

func TestRoomStateCodec_roundTrip_emptyRoom(t *testing.T) {
	t.Parallel()

	c := newRoomCodec()
	room := state.NewRoomState(domain.RoomID(7), 16)

	data, err := c.Encode(room)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := c.Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.ID != room.ID || got.Capacity != room.Capacity {
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
	room := state.NewRoomState(domain.RoomID(1), 8)

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
		t.Fatalf("entity_type_id = %d, want 5", ent.EntityTypeID)
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
	cur := state.NewRoomState(domain.RoomID(1), 4)
	next := state.NewRoomState(domain.RoomID(1), 8)

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

func TestRoomStateCodec_decodePatch_truncatedPeerNick(t *testing.T) {
	t.Parallel()

	c := newRoomCodec()
	patch := &state.RoomStatePatch{}
	patch.Peers.Added = map[domain.PeerID]state.PeerState{
		domain.PeerID(1): {NickName: "abc"},
	}
	data, err := c.EncodePatch(patch)
	if err != nil {
		t.Fatalf("EncodePatch: %v", err)
	}
	// truncated peer payload: buf.Read used to silently return short
	if _, err := c.DecodePatch(data[:len(data)-1]); err == nil {
		t.Fatal("expected error on truncated payload")
	}
}

func TestRoomStateCodec_patch_addPeer(t *testing.T) {
	t.Parallel()

	c := newRoomCodec()
	cur := state.NewRoomState(domain.RoomID(1), 4)
	next := state.NewRoomState(domain.RoomID(1), 4)
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

// D2 regression: the room password is a server-side secret and must never
// appear on the wire. An empty room encodes into exactly header + three
// empty maps, with no password field.
func TestRoomStateCodec_encode_neverSerializesPassword(t *testing.T) {
	t.Parallel()

	c := newRoomCodec()
	room := state.NewRoomState(domain.RoomID(7), 16)

	data, err := c.Encode(room)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	// room_id u32 | capacity i8 | peers u16 | inputs u16 | entities u16
	want := []byte{0, 0, 0, 7, 16, 0, 0, 0, 0, 0, 0}
	if !bytes.Equal(data, want) {
		t.Fatalf("encoded = %v, want exact layout %v (no password field)", data, want)
	}
}

func TestRoomStateCodec_decode_trailingBytes(t *testing.T) {
	t.Parallel()

	c := newRoomCodec()
	room := state.NewRoomState(domain.RoomID(1), 4)
	data, err := c.Encode(room)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if _, err := c.Decode(append(data, 0xFF)); err == nil {
		t.Fatal("expected error on trailing bytes")
	}
}

func TestRoomStateCodec_decodePatch_trailingBytes(t *testing.T) {
	t.Parallel()

	c := newRoomCodec()
	cap8 := int8(8)
	data, err := c.EncodePatch(&state.RoomStatePatch{Capacity: &cap8})
	if err != nil {
		t.Fatalf("EncodePatch: %v", err)
	}
	if _, err := c.DecodePatch(append(data, 0xFF)); err == nil {
		t.Fatal("expected error on trailing bytes")
	}
}
