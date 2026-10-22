package codec

import (
	"bytes"
	"encoding/binary"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

const (
	RoomStateMaxPeers  = 255
	RoomStateMaxInputs = RoomStateMaxPeers
	// Entities кодируются одним u16-счётчиком — лимит общий с EntitiesStateMaxEntities.
	RoomStateMaxEntities = EntitiesStateMaxEntities
)

const (
	RoomStateFlagsCapacity = 1 << iota
	RoomStateFlagsPeers
	RoomStateFlagsInputs
	RoomStateFlagsEntities
)

type RoomStateCodec struct {
}

func NewRoomStateCodec() *RoomStateCodec {
	return &RoomStateCodec{}
}

func writeRoomID(buf *bytes.Buffer, id domain.RoomID) error {
	return binary.Write(buf, binary.BigEndian, id)
}

func readRoomID(buf *bytes.Buffer) (domain.RoomID, error) {
	var id domain.RoomID
	if err := binary.Read(buf, binary.BigEndian, &id); err != nil {
		return domain.RoomIDInvalid, err
	}
	return id, nil
}

func (c *RoomStateCodec) Encode(room *state.RoomState) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 1024))

	if err := writeRoomID(buf, room.ID); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.BigEndian, room.Capacity); err != nil {
		return nil, err
	}
	if err := writeMapState(buf, room.Peers, func(k domain.PeerID, buf *bytes.Buffer) error {
		return writePeerID(buf, k)
	}, func(v *state.PeerState, buf *bytes.Buffer) error {
		return writePeerState(buf, v)
	}, RoomStateMaxPeers); err != nil {
		return nil, err
	}
	if err := writeMapState(buf, room.Inputs, func(k domain.PeerID, buf *bytes.Buffer) error {
		return writePeerID(buf, k)
	}, func(v *state.InputState, buf *bytes.Buffer) error {
		return writeInputState(buf, v)
	}, RoomStateMaxInputs); err != nil {
		return nil, err
	}
	if err := writeMapState(buf, room.Entities, func(k state.EntityID, buf *bytes.Buffer) error {
		return writeEntityID(buf, k)
	}, func(v *state.EntityState, buf *bytes.Buffer) error {
		return writeEntityState(buf, v)
	}, RoomStateMaxEntities); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (c *RoomStateCodec) Decode(data []byte) (*state.RoomState, error) {
	buf := bytes.NewBuffer(data)
	id, err := readRoomID(buf)
	if err != nil {
		return nil, err
	}
	var capacity int8
	if err = binary.Read(buf, binary.BigEndian, &capacity); err != nil {
		return nil, err
	}
	peers, err := readMapState(buf, readPeerID, readPeerState, RoomStateMaxPeers)
	if err != nil {
		return nil, err
	}
	inputs, err := readMapState(buf, readPeerID, readInputState, RoomStateMaxInputs)
	if err != nil {
		return nil, err
	}
	entities, err := readMapState(buf, readEntityID, readEntityState, RoomStateMaxEntities)
	if err != nil {
		return nil, err
	}
	if err := checkNoTrailingBytes(buf); err != nil {
		return nil, err
	}
	return &state.RoomState{ID: id, Capacity: capacity, Peers: peers, Inputs: inputs, Entities: entities}, nil
}

func (c *RoomStateCodec) EncodePatch(patch *state.RoomStatePatch) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 1024))
	flags := byte(0)
	if patch.Capacity != nil {
		flags |= RoomStateFlagsCapacity
	}
	if len(patch.Peers.Added) > 0 || len(patch.Peers.Updated) > 0 || len(patch.Peers.Removed) > 0 {
		flags |= RoomStateFlagsPeers
	}
	if len(patch.Inputs.Added) > 0 || len(patch.Inputs.Updated) > 0 || len(patch.Inputs.Removed) > 0 {
		flags |= RoomStateFlagsInputs
	}
	if len(patch.Entities.Added) > 0 || len(patch.Entities.Updated) > 0 || len(patch.Entities.Removed) > 0 {
		flags |= RoomStateFlagsEntities
	}
	if err := binary.Write(buf, binary.BigEndian, flags); err != nil {
		return nil, err
	}
	if patch.Capacity != nil {
		if err := binary.Write(buf, binary.BigEndian, *patch.Capacity); err != nil {
			return nil, err
		}
	}
	if flags&RoomStateFlagsPeers == RoomStateFlagsPeers {
		if err := writeMapPatchState(buf, patch.Peers, func(k domain.PeerID, buf *bytes.Buffer) error {
			return writePeerID(buf, k)
		}, func(s *state.PeerState, buf *bytes.Buffer) error {
			return writePeerState(buf, s)
		}, func(p *state.PeerStatePatch, buf *bytes.Buffer) error {
			return writePeerStatePatch(buf, p)
		}, RoomStateMaxPeers); err != nil {
			return nil, err
		}
	}
	if flags&RoomStateFlagsInputs == RoomStateFlagsInputs {
		if err := writeMapPatchState(buf, patch.Inputs, func(k domain.PeerID, buf *bytes.Buffer) error {
			return writePeerID(buf, k)
		}, func(s *state.InputState, buf *bytes.Buffer) error {
			return writeInputState(buf, s)
		}, func(p *state.InputStatePatch, buf *bytes.Buffer) error {
			return writeInputStatePatch(buf, p)
		}, RoomStateMaxInputs); err != nil {
			return nil, err
		}
	}
	if flags&RoomStateFlagsEntities == RoomStateFlagsEntities {
		if err := writeMapPatchState(buf, patch.Entities, func(k state.EntityID, buf *bytes.Buffer) error {
			return writeEntityID(buf, k)
		}, func(s *state.EntityState, buf *bytes.Buffer) error {
			return writeEntityState(buf, s)
		}, func(p *state.EntityStatePatch, buf *bytes.Buffer) error {
			return writeEntityStatePatch(buf, p)
		}, RoomStateMaxEntities); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func (c *RoomStateCodec) DecodePatch(data []byte) (*state.RoomStatePatch, error) {
	buf := bytes.NewBuffer(data)

	var flags byte
	if err := binary.Read(buf, binary.BigEndian, &flags); err != nil {
		return nil, err
	}
	var capacity *int8
	if flags&RoomStateFlagsCapacity == RoomStateFlagsCapacity {
		var c int8
		if err := binary.Read(buf, binary.BigEndian, &c); err != nil {
			return nil, err
		}
		capacity = &c
	}
	peers := state.MapStatePatch[domain.PeerID, state.PeerState, state.PeerStatePatch]{}
	if flags&RoomStateFlagsPeers == RoomStateFlagsPeers {
		p, err := readMapPatchState(buf, readPeerID, readPeerState, readPeerStatePatch, RoomStateMaxPeers)
		if err != nil {
			return nil, err
		}
		peers = *p
	}
	inputs := state.MapStatePatch[domain.PeerID, state.InputState, state.InputStatePatch]{}
	if flags&RoomStateFlagsInputs == RoomStateFlagsInputs {
		i, err := readMapPatchState(buf, readPeerID, readInputState, readInputStatePatch, RoomStateMaxInputs)
		if err != nil {
			return nil, err
		}
		inputs = *i
	}
	entities := state.MapStatePatch[state.EntityID, state.EntityState, state.EntityStatePatch]{}
	if flags&RoomStateFlagsEntities == RoomStateFlagsEntities {
		e, err := readMapPatchState(buf, readEntityID, readEntityState, readEntityStatePatch, RoomStateMaxEntities)
		if err != nil {
			return nil, err
		}
		entities = *e
	}
	if err := checkNoTrailingBytes(buf); err != nil {
		return nil, err
	}
	return &state.RoomStatePatch{Capacity: capacity, Peers: peers, Inputs: inputs, Entities: entities}, nil
}
