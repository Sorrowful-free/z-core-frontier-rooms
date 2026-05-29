package state

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

type RoomState struct {
	ID       domain.RoomID
	Capacity int8
	Password string

	Peers    MapState[domain.PeerID, PeerState]
	Inputs   MapState[domain.PeerID, InputState]
	Entities MapState[EntityID, EntityState]
}

type RoomStatePatch struct {
	Capacity *int8
	Password *string
	Peers    MapStatePatch[domain.PeerID, PeerState, PeerStatePatch]
	Inputs   MapStatePatch[domain.PeerID, InputState, InputStatePatch]
	Entities MapStatePatch[EntityID, EntityState, EntityStatePatch]
}

func NewRoomState(roomID domain.RoomID, capacity int8, password string) *RoomState {
	return &RoomState{
		ID:       roomID,
		Capacity: capacity,
		Password: password,
		Peers:    *NewMapState[domain.PeerID, PeerState](),
		Inputs:   *NewMapState[domain.PeerID, InputState](),
		Entities: *NewMapState[EntityID, EntityState](),
	}
}

func (s *RoomState) MakePatch(newRoomState *RoomState) (*RoomStatePatch, error) {
	patch := &RoomStatePatch{}

	hasChanges := false
	if s.Capacity != newRoomState.Capacity {
		patch.Capacity = &newRoomState.Capacity
		hasChanges = true
	}
	if s.Password != newRoomState.Password {
		patch.Password = &newRoomState.Password
		hasChanges = true
	}

	patchPeers, err := MakeMapStatePatch(&s.Peers, &newRoomState.Peers, func(p1, p2 PeerState) bool {
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

	patchInputs, err := MakeMapStatePatch(&s.Inputs, &newRoomState.Inputs, func(i1, i2 InputState) bool {
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

	patchEntities, err := MakeMapStatePatch(&s.Entities, &newRoomState.Entities, func(e1, e2 EntityState) bool {
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
	if patch.Password != nil && *patch.Password != s.Password {
		s.Password = *patch.Password
	}

	err := ApplyMapStatePatch(&s.Peers, patch.Peers, func(p1 PeerState, p2 PeerStatePatch) (*PeerState, error) {
		err := p1.ApplyPatch(p2)
		if err != nil {
			return nil, err
		}
		return &p1, nil
	})
	if err != nil {
		return err
	}

	err = ApplyMapStatePatch(&s.Inputs, patch.Inputs, func(i1 InputState, i2 InputStatePatch) (*InputState, error) {
		err := i1.ApplyPatch(i2)
		if err != nil {
			return nil, err
		}
		return &i1, nil
	})
	if err != nil {
		return err
	}

	err = ApplyMapStatePatch(&s.Entities, patch.Entities, func(e1 EntityState, e2 EntityStatePatch) (*EntityState, error) {
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
