package codec_test

import (
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/codec"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

func TestEntitiesStateCodec_roundTrip_entityTypeID(t *testing.T) {
	t.Parallel()

	c := codec.NewEntitiesStateCodec()
	entities := state.NewMapState[state.EntityID, state.EntityState]()
	entities[state.EntityID(10)] = state.EntityState{
		EntityTypeID: state.EntityTypeID(3),
		Owner:        domain.PeerID(2),
		Components:   state.NewMapState[state.ComponentID, state.ComponentState](),
	}

	data, err := c.Encode(entities)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := c.Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	ent := got[state.EntityID(10)]
	if ent.EntityTypeID != state.EntityTypeID(3) {
		t.Fatalf("entity_type_id = %d, want 3", ent.EntityTypeID)
	}
	if ent.Owner != domain.PeerID(2) {
		t.Fatalf("owner = %d, want 2", ent.Owner)
	}
}

func TestEntitiesStateCodec_patch_entityTypeIDOnly(t *testing.T) {
	t.Parallel()

	c := codec.NewEntitiesStateCodec()
	newType := state.EntityTypeID(7)
	patch := state.NewMapStatePatch[state.EntityID, state.EntityState, state.EntityStatePatch]()
	patch.Updated[state.EntityID(10)] = state.EntityStatePatch{
		EntityTypeID: &newType,
	}

	data, err := c.EncodePatch(patch)
	if err != nil {
		t.Fatalf("EncodePatch: %v", err)
	}
	got, err := c.DecodePatch(data)
	if err != nil {
		t.Fatalf("DecodePatch: %v", err)
	}

	updated, ok := got.Updated[state.EntityID(10)]
	if !ok {
		t.Fatal("expected updated entity patch")
	}
	if updated.EntityTypeID == nil || *updated.EntityTypeID != state.EntityTypeID(7) {
		t.Fatalf("entity_type_id patch = %v, want 7", updated.EntityTypeID)
	}
}
