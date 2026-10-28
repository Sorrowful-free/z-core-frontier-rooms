package codec_test

import (
	"bytes"
	"reflect"
	"slices"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/codec"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

// mustFuzzFrame encodes a valid domain state into a seed frame or fails.
func mustFuzzFrame[T any](f *testing.F, v T, encode func(T) ([]byte, error)) []byte {
	f.Helper()
	data, err := encode(v)
	if err != nil {
		f.Fatalf("seed encode: %v", err)
	}
	return data
}

// fuzzTruncOffsets — truncation points of a valid frame: after the first
// byte, the middle, and the last full byte.
func fuzzTruncOffsets(n int) []int {
	var out []int
	for _, off := range []int{1, n / 2, n - 1} {
		if off > 0 && off < n && !slices.Contains(out, off) {
			out = append(out, off)
		}
	}
	return out
}

// addFuzzSeeds — shared seed corpus: hand-written edge inputs plus
// mutations of valid encoded frames (truncation, trailing garbage,
// bit-flips of flags/counters in the first and last bytes).
func addFuzzSeeds(f *testing.F, frames ...[]byte) {
	f.Helper()
	f.Add([]byte{})
	f.Add([]byte{0x00})
	f.Add([]byte{0xFF})
	f.Add([]byte{0xFF, 0xFF, 0xFF, 0xFF})
	for _, frame := range frames {
		if len(frame) == 0 {
			continue
		}
		f.Add(frame)
		for _, off := range fuzzTruncOffsets(len(frame)) {
			f.Add(frame[:off])
		}
		f.Add(append(bytes.Clone(frame), 0xDE, 0xAD))
		head := bytes.Clone(frame)
		head[0] ^= 0xFF
		f.Add(head)
		tail := bytes.Clone(frame)
		tail[len(tail)-1] ^= 0xFF
		f.Add(tail)
	}
}

// fuzzSeedRoomState — a room with a peer, input and entity+component (all map sections).
func fuzzSeedRoomState() *state.RoomState {
	room := state.NewRoomState(domain.RoomID(7), 16)
	room.Peers[domain.PeerID(2)] = state.PeerState{NickName: "bob", Ping: 99, IsMaster: true}
	room.Inputs[domain.PeerID(2)] = state.InputState{
		Values: state.NewMapState[state.ValueId, state.ValueState](),
	}
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
	return room
}

// fuzzSeedRoomStatePatch — a patch lighting all flag bits: capacity +
// added/updated/removed across peers, inputs and entities.
func fuzzSeedRoomStatePatch() *state.RoomStatePatch {
	capacity := int8(9)
	nick := "mallory"
	owner := domain.PeerID(9)
	patch := &state.RoomStatePatch{
		Capacity: &capacity,
		Peers:    *state.NewMapStatePatch[domain.PeerID, state.PeerState, state.PeerStatePatch](),
		Inputs:   *state.NewMapStatePatch[domain.PeerID, state.InputState, state.InputStatePatch](),
		Entities: *state.NewMapStatePatch[state.EntityID, state.EntityState, state.EntityStatePatch](),
	}
	patch.Peers.Added[domain.PeerID(1)] = state.PeerState{NickName: "ann", Ping: 5}
	patch.Peers.Updated[domain.PeerID(2)] = state.PeerStatePatch{NickName: &nick}
	patch.Peers.Removed = append(patch.Peers.Removed, domain.PeerID(3))

	in := state.InputState{Values: state.NewMapState[state.ValueId, state.ValueState]()}
	in.Values[state.ValueId(1)] = state.ValueState([]byte{7})
	patch.Inputs.Added[domain.PeerID(1)] = in
	patch.Inputs.Updated[domain.PeerID(2)] = state.InputStatePatch{
		Values: *state.NewMapStatePatch[state.ValueId, state.ValueState, state.ValueState](),
	}
	patch.Inputs.Removed = append(patch.Inputs.Removed, domain.PeerID(4))

	patch.Entities.Added[state.EntityID(1)] = state.EntityState{
		EntityTypeID: state.EntityTypeID(3),
		Owner:        domain.PeerID(1),
		Components:   state.NewMapState[state.ComponentID, state.ComponentState](),
	}
	patch.Entities.Updated[state.EntityID(2)] = state.EntityStatePatch{
		Owner:      &owner,
		Components: *state.NewMapStatePatch[state.ComponentID, state.ComponentState, state.ComponentStatePatch](),
	}
	patch.Entities.Removed = append(patch.Entities.Removed, state.EntityID(3))
	return patch
}

// fuzzSeedEntitiesState — a map with one entity that has a component.
func fuzzSeedEntitiesState() state.EntitiesState {
	entities := state.NewMapState[state.EntityID, state.EntityState]()
	entities[state.EntityID(10)] = state.EntityState{
		EntityTypeID: state.EntityTypeID(3),
		Owner:        domain.PeerID(2),
		Components:   state.NewMapState[state.ComponentID, state.ComponentState](),
	}
	entities[state.EntityID(10)].Components[state.ComponentID(1)] = state.ComponentState{
		Values: state.NewMapState[state.ValueId, state.ValueState](),
	}
	entities[state.EntityID(10)].Components[state.ComponentID(1)].Values[state.ValueId(2)] =
		state.ValueState([]byte{1, 2, 3})
	return entities
}

// fuzzSeedEntitiesStatePatch — a patch with added/updated/removed sections.
func fuzzSeedEntitiesStatePatch() *state.EntitiesStatePatch {
	entityTypeID := state.EntityTypeID(7)
	owner := domain.PeerID(9)
	patch := state.NewMapStatePatch[state.EntityID, state.EntityState, state.EntityStatePatch]()
	patch.Added[state.EntityID(1)] = state.EntityState{
		EntityTypeID: state.EntityTypeID(3),
		Owner:        domain.PeerID(2),
		Components:   state.NewMapState[state.ComponentID, state.ComponentState](),
	}
	patch.Updated[state.EntityID(10)] = state.EntityStatePatch{
		EntityTypeID: &entityTypeID,
		Owner:        &owner,
		Components:   *state.NewMapStatePatch[state.ComponentID, state.ComponentState, state.ComponentStatePatch](),
	}
	patch.Removed = append(patch.Removed, state.EntityID(3))
	return patch
}

// fuzzSeedInputState — an input with two values.
func fuzzSeedInputState() *state.InputState {
	in := &state.InputState{Values: state.NewMapState[state.ValueId, state.ValueState]()}
	in.Values[state.ValueId(1)] = state.ValueState([]byte{9, 8})
	in.Values[state.ValueId(2)] = state.ValueState([]byte{0xFF})
	return in
}

// fuzzSeedInputStatePatch — a patch with added/updated/removed across values.
func fuzzSeedInputStatePatch() *state.InputStatePatch {
	patch := &state.InputStatePatch{
		Values: *state.NewMapStatePatch[state.ValueId, state.ValueState, state.ValueState](),
	}
	patch.Values.Added[state.ValueId(1)] = state.ValueState([]byte{1, 2, 3})
	patch.Values.Updated[state.ValueId(2)] = state.ValueState([]byte{0x42})
	patch.Values.Removed = append(patch.Values.Removed, state.ValueId(3))
	return patch
}

// fuzzSeedRpcState — an rpc with target peer and one value.
func fuzzSeedRpcState() *state.RpcState {
	rpc := &state.RpcState{
		ID:     state.RpcID(3),
		Target: state.RpcTargetPeer,
		Values: state.NewMapState[state.ValueId, state.ValueState](),
		PeerID: domain.PeerID(42),
	}
	rpc.Values[state.ValueId(1)] = state.ValueState([]byte{1, 2})
	return rpc
}

// Invariants in all targets: decode does not panic (the fuzzer catches
// that), re-decoding the same bytes yields the same result, Encode(decoded)
// does not fail. Byte-exact roundtrip is deliberately not checked — the wire
// format has variable-length fields and map ordering.

func FuzzRoomStateDecode(f *testing.F) {
	c := newRoomCodec()
	full := mustFuzzFrame(f, fuzzSeedRoomState(), c.Encode)
	empty := mustFuzzFrame(f, state.NewRoomState(domain.RoomID(1), 4), c.Encode)
	addFuzzSeeds(f, full, empty)

	f.Fuzz(func(t *testing.T, data []byte) {
		got, err := c.Decode(data)
		if err != nil {
			return
		}
		again, err := c.Decode(data)
		if err != nil {
			t.Fatalf("second Decode: %v", err)
		}
		if !reflect.DeepEqual(got, again) {
			t.Fatalf("non-deterministic Decode: %#v vs %#v", got, again)
		}
		if _, err := c.Encode(got); err != nil {
			t.Fatalf("Encode(decoded): %v", err)
		}
	})
}

func FuzzRoomStateDecodePatch(f *testing.F) {
	c := newRoomCodec()
	full := mustFuzzFrame(f, fuzzSeedRoomStatePatch(), c.EncodePatch)

	cur := state.NewRoomState(domain.RoomID(1), 4)
	next := state.NewRoomState(domain.RoomID(1), 8)
	diffPatch, err := cur.MakePatch(next)
	if err != nil {
		f.Fatalf("seed MakePatch: %v", err)
	}
	diff := mustFuzzFrame(f, diffPatch, c.EncodePatch)
	addFuzzSeeds(f, full, diff)

	f.Fuzz(func(t *testing.T, data []byte) {
		got, err := c.DecodePatch(data)
		if err != nil {
			return
		}
		again, err := c.DecodePatch(data)
		if err != nil {
			t.Fatalf("second DecodePatch: %v", err)
		}
		if !reflect.DeepEqual(got, again) {
			t.Fatalf("non-deterministic DecodePatch: %#v vs %#v", got, again)
		}
		if _, err := c.EncodePatch(got); err != nil {
			t.Fatalf("EncodePatch(decoded): %v", err)
		}
	})
}

func FuzzEntitiesStateDecode(f *testing.F) {
	c := codec.NewEntitiesStateCodec()
	full := mustFuzzFrame(f, fuzzSeedEntitiesState(), c.Encode)
	empty := mustFuzzFrame(f, state.NewMapState[state.EntityID, state.EntityState](), c.Encode)
	addFuzzSeeds(f, full, empty)

	f.Fuzz(func(t *testing.T, data []byte) {
		got, err := c.Decode(data)
		if err != nil {
			return
		}
		again, err := c.Decode(data)
		if err != nil {
			t.Fatalf("second Decode: %v", err)
		}
		if !reflect.DeepEqual(got, again) {
			t.Fatalf("non-deterministic Decode: %#v vs %#v", got, again)
		}
		if _, err := c.Encode(got); err != nil {
			t.Fatalf("Encode(decoded): %v", err)
		}
	})
}

func FuzzEntitiesStateDecodePatch(f *testing.F) {
	c := codec.NewEntitiesStateCodec()
	full := mustFuzzFrame(f, fuzzSeedEntitiesStatePatch(), c.EncodePatch)
	empty := mustFuzzFrame(f, state.NewMapStatePatch[
		state.EntityID, state.EntityState, state.EntityStatePatch](), c.EncodePatch)
	addFuzzSeeds(f, full, empty)

	f.Fuzz(func(t *testing.T, data []byte) {
		got, err := c.DecodePatch(data)
		if err != nil {
			return
		}
		again, err := c.DecodePatch(data)
		if err != nil {
			t.Fatalf("second DecodePatch: %v", err)
		}
		if !reflect.DeepEqual(got, again) {
			t.Fatalf("non-deterministic DecodePatch: %#v vs %#v", got, again)
		}
		if _, err := c.EncodePatch(got); err != nil {
			t.Fatalf("EncodePatch(decoded): %v", err)
		}
	})
}

func FuzzInputStateDecode(f *testing.F) {
	c := codec.NewInputStateCodec()
	full := mustFuzzFrame(f, fuzzSeedInputState(), c.Encode)
	empty := mustFuzzFrame(f, &state.InputState{
		Values: state.NewMapState[state.ValueId, state.ValueState](),
	}, c.Encode)
	addFuzzSeeds(f, full, empty)

	f.Fuzz(func(t *testing.T, data []byte) {
		got, err := c.Decode(data)
		if err != nil {
			return
		}
		again, err := c.Decode(data)
		if err != nil {
			t.Fatalf("second Decode: %v", err)
		}
		if !reflect.DeepEqual(got, again) {
			t.Fatalf("non-deterministic Decode: %#v vs %#v", got, again)
		}
		if _, err := c.Encode(got); err != nil {
			t.Fatalf("Encode(decoded): %v", err)
		}
	})
}

func FuzzInputStateDecodePatch(f *testing.F) {
	c := codec.NewInputStateCodec()
	full := mustFuzzFrame(f, fuzzSeedInputStatePatch(), c.EncodePatch)
	empty := mustFuzzFrame(f, &state.InputStatePatch{}, c.EncodePatch)
	addFuzzSeeds(f, full, empty)

	f.Fuzz(func(t *testing.T, data []byte) {
		got, err := c.DecodePatch(data)
		if err != nil {
			return
		}
		again, err := c.DecodePatch(data)
		if err != nil {
			t.Fatalf("second DecodePatch: %v", err)
		}
		if !reflect.DeepEqual(got, again) {
			t.Fatalf("non-deterministic DecodePatch: %#v vs %#v", got, again)
		}
		if _, err := c.EncodePatch(got); err != nil {
			t.Fatalf("EncodePatch(decoded): %v", err)
		}
	})
}

func FuzzRpcStateDecode(f *testing.F) {
	c := codec.NewRpcStateCodec()
	full := mustFuzzFrame(f, fuzzSeedRpcState(), c.Encode)
	master := fuzzSeedRpcState()
	master.Target = state.RpcTargetMaster
	master.PeerID = domain.PeerIDInvalid
	mastered := mustFuzzFrame(f, master, c.Encode)
	addFuzzSeeds(f, full, mastered)

	f.Fuzz(func(t *testing.T, data []byte) {
		got, err := c.Decode(data)
		if err != nil {
			return
		}
		again, err := c.Decode(data)
		if err != nil {
			t.Fatalf("second Decode: %v", err)
		}
		if !reflect.DeepEqual(got, again) {
			t.Fatalf("non-deterministic Decode: %#v vs %#v", got, again)
		}
		if _, err := c.Encode(got); err != nil {
			t.Fatalf("Encode(decoded): %v", err)
		}
	})
}
