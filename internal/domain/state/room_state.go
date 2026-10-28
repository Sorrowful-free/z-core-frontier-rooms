package state

import (
	"maps"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

// RoomState — authoritative состояние комнаты.
//
// COW-инвариант: записанные в мапы значения (`InputState.Values`,
// `EntityState.Components`, `ValueState`) после записи не мутируются
// in-place — любая мутация только clone-then-replace. Это позволяет
// Snapshot разделять значения со state: shallow-копия остаётся
// корректной прошлой версией для diff.
type RoomState struct {
	ID       domain.RoomID
	Capacity int8

	Peers    MapState[domain.PeerID, PeerState]
	Inputs   MapState[domain.PeerID, InputState]
	Entities MapState[EntityID, EntityState]
}

type RoomStatePatch struct {
	Capacity *int8
	Peers    MapStatePatch[domain.PeerID, PeerState, PeerStatePatch]
	Inputs   MapStatePatch[domain.PeerID, InputState, InputStatePatch]
	Entities MapStatePatch[EntityID, EntityState, EntityStatePatch]
}

func NewRoomState(roomID domain.RoomID, capacity int8) *RoomState {
	return &RoomState{
		ID:       roomID,
		Capacity: capacity,
		Peers:    NewMapState[domain.PeerID, PeerState](),
		Inputs:   NewMapState[domain.PeerID, InputState](),
		Entities: NewMapState[EntityID, EntityState](),
	}
}

func (s *RoomState) MakePatch(newRoomState *RoomState) (*RoomStatePatch, error) {
	patch := &RoomStatePatch{}

	hasChanges := false
	if s.Capacity != newRoomState.Capacity {
		patch.Capacity = &newRoomState.Capacity
		hasChanges = true
	}
	patchPeers, err := MakeMapStatePatch(s.Peers, newRoomState.Peers, func(p1, p2 PeerState) bool {
		return p1.Equals(&p2)
	}, func(p1, p2 PeerState) (*PeerStatePatch, error) {
		return p1.MakePatch(&p2)
	})
	if err != nil {
		return nil, err
	}
	if patchPeers != nil {
		patch.Peers = *patchPeers
		hasChanges = true
	}

	patchInputs, err := MakeMapStatePatch(s.Inputs, newRoomState.Inputs, func(i1, i2 InputState) bool {
		return i1.Equals(&i2, func(v1, v2 ValueState) bool {
			return v1.Equals(&v2)
		})
	}, func(i1, i2 InputState) (*InputStatePatch, error) {
		return i1.MakePatch(&i2)
	})
	if err != nil {
		return nil, err
	}
	if patchInputs != nil {
		patch.Inputs = *patchInputs
		hasChanges = true
	}

	patchEntities, err := MakeMapStatePatch(s.Entities, newRoomState.Entities, func(e1, e2 EntityState) bool {
		return e1.Equals(&e2, func(c1, c2 ComponentState) bool {
			return c1.Equals(&c2, func(v1, v2 ValueState) bool {
				return v1.Equals(&v2)
			})
		})
	}, func(e1, e2 EntityState) (*EntityStatePatch, error) {
		return e1.MakePatch(&e2)
	})
	if err != nil {
		return nil, err
	}
	if patchEntities != nil {
		patch.Entities = *patchEntities
		hasChanges = true
	}

	if !hasChanges {
		return nil, nil
	}
	return patch, nil
}

func (s *RoomState) ApplyPatch(patch RoomStatePatch) error {
	if patch.Capacity != nil && *patch.Capacity != s.Capacity {
		s.Capacity = *patch.Capacity
	}
	err := ApplyMapStatePatch(s.Peers, patch.Peers, func(p1 PeerState, p2 PeerStatePatch) (*PeerState, error) {
		err := p1.ApplyPatch(p2)
		if err != nil {
			return nil, err
		}
		return &p1, nil
	})
	if err != nil {
		return err
	}

	err = ApplyMapStatePatch(s.Inputs, patch.Inputs, func(i1 InputState, i2 InputStatePatch) (*InputState, error) {
		// i1 шарит Values-мапу с хранимой записью — клонируем до патча,
		// иначе ApplyPatch мутирует state in-place (COW-инвариант).
		next := i1.Clone()
		if err := next.ApplyPatch(i2); err != nil {
			return nil, err
		}
		return &next, nil
	})
	if err != nil {
		return err
	}

	err = ApplyMapStatePatch(s.Entities, patch.Entities, func(e1 EntityState, e2 EntityStatePatch) (*EntityState, error) {
		err := e1.ApplyPatch(e2)
		if err != nil {
			return nil, err
		}
		return &e1, nil
	})
	if err != nil {
		return err
	}

	return nil
}

// Snapshot — shallow-снапшот для чтения (diff, encode): struct-копия +
// maps.Clone трёх верхних мап. Значения разделяются со state и остаются
// корректными благодаря COW-инварианту RoomState. Для мутации копии —
// Clone.
func (s *RoomState) Snapshot() *RoomState {
	return &RoomState{
		ID:       s.ID,
		Capacity: s.Capacity,
		Peers:    maps.Clone(s.Peers),
		Inputs:   maps.Clone(s.Inputs),
		Entities: maps.Clone(s.Entities),
	}
}

// Clone returns a deep copy safe for snapshot-before-mutate (e.g. MakePatch).
func (s *RoomState) Clone() *RoomState {
	peers := make(MapState[domain.PeerID, PeerState], len(s.Peers))
	maps.Copy(peers, s.Peers)
	inputs := CloneMapState(s.Inputs, func(i InputState) InputState { return i.Clone() })
	entities := CloneMapState(s.Entities, func(e EntityState) EntityState { return e.Clone() })
	return &RoomState{
		ID:       s.ID,
		Capacity: s.Capacity,
		Peers:    peers,
		Inputs:   inputs,
		Entities: entities,
	}
}
