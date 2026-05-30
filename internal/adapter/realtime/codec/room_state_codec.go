package codec

import (
	"bytes"
	"encoding/binary"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
)

const (
	RoomStateMaxPeers    = 255
	RoomStateMaxInputs   = RoomStateMaxPeers
	RoomStateMaxEntities = 65535
)

const (
	RoomStateFlagsCapacity = 1 << iota
	RoomStateFlagsPassword
	RoomStateFlagsPeers
	RoomStateFlagsInputs
	RoomStateFlagsEntities
)

type RoomStateCodec struct {
	logger *logging.Logger
}

func NewRoomStateCodec(logger *logging.Logger) *RoomStateCodec {
	return &RoomStateCodec{logger: logger}
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
	if err := writeString(buf, room.Password); err != nil {
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
	password, err := readString(buf)
	if err != nil {
		return nil, err
	}
	peers, err := readMapState(buf, func(buf *bytes.Buffer) (domain.PeerID, error) {
		return readPeerID(buf)
	}, func(buf *bytes.Buffer) (*state.PeerState, error) {
		return readPeerState(buf)
	}, RoomStateMaxPeers)
	if err != nil {
		return nil, err
	}
	inputs, err := readMapState(buf, func(buf *bytes.Buffer) (domain.PeerID, error) {
		return readPeerID(buf)
	}, func(buf *bytes.Buffer) (*state.InputState, error) {
		return readInputState(buf)
	}, RoomStateMaxInputs)
	if err != nil {
		return nil, err
	}
	entities, err := readMapState(buf, func(buf *bytes.Buffer) (state.EntityID, error) {
		return readEntityID(buf)
	}, func(buf *bytes.Buffer) (*state.EntityState, error) {
		return readEntityState(buf)
	}, RoomStateMaxEntities)
	if err != nil {
		return nil, err
	}
	return &state.RoomState{ID: id, Capacity: capacity, Password: password, Peers: peers, Inputs: inputs, Entities: entities}, nil
}

func (c *RoomStateCodec) EncodePatch(patch *state.RoomStatePatch) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 1024))
	flags := byte(0)
	if patch.Capacity != nil {
		flags |= RoomStateFlagsCapacity
	}
	if patch.Password != nil {
		flags |= RoomStateFlagsPassword
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
	if patch.Password != nil {
		if err := writeString(buf, *patch.Password); err != nil {
			return nil, err
		}
	}
	if flags&RoomStateFlagsPeers == RoomStateFlagsPeers {
		if err := writeMapPatсhState(buf, patch.Peers, func(k domain.PeerID, buf *bytes.Buffer) error {
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
		if err := writeMapPatсhState(buf, patch.Inputs, func(k domain.PeerID, buf *bytes.Buffer) error {
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
		if err := writeMapPatсhState(buf, patch.Entities, func(k state.EntityID, buf *bytes.Buffer) error {
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
	var password *string
	if flags&RoomStateFlagsPassword == RoomStateFlagsPassword {
		p, err := readString(buf)
		if err != nil {
			return nil, err
		}
		password = &p
	}
	peers := state.MapStatePatch[domain.PeerID, state.PeerState, state.PeerStatePatch]{}
	if flags&RoomStateFlagsPeers == RoomStateFlagsPeers {
		p, err := readMapPatchState(buf, func(buf *bytes.Buffer) (domain.PeerID, error) {
			return readPeerID(buf)
		}, func(buf *bytes.Buffer) (*state.PeerState, error) {
			return readPeerState(buf)
		}, func(buf *bytes.Buffer) (*state.PeerStatePatch, error) {
			return readPeerStatePatch(buf)
		}, RoomStateMaxPeers)
		if err != nil {
			return nil, err
		}
		peers = *p
	}
	inputs := state.MapStatePatch[domain.PeerID, state.InputState, state.InputStatePatch]{}
	if flags&RoomStateFlagsInputs == RoomStateFlagsInputs {
		i, err := readMapPatchState(buf, func(buf *bytes.Buffer) (domain.PeerID, error) {
			return readPeerID(buf)
		}, func(buf *bytes.Buffer) (*state.InputState, error) {
			return readInputState(buf)
		}, func(buf *bytes.Buffer) (*state.InputStatePatch, error) {
			return readInputStatePatch(buf)
		}, RoomStateMaxInputs)
		if err != nil {
			return nil, err
		}
		inputs = *i
	}
	entities := state.MapStatePatch[state.EntityID, state.EntityState, state.EntityStatePatch]{}
	if flags&RoomStateFlagsEntities == RoomStateFlagsEntities {
		e, err := readMapPatchState(buf, func(buf *bytes.Buffer) (state.EntityID, error) {
			return readEntityID(buf)
		}, func(buf *bytes.Buffer) (*state.EntityState, error) {
			return readEntityState(buf)
		}, func(buf *bytes.Buffer) (*state.EntityStatePatch, error) {
			return readEntityStatePatch(buf)
		}, RoomStateMaxEntities)
		if err != nil {
			return nil, err
		}
		entities = *e
	}
	return &state.RoomStatePatch{Capacity: capacity, Password: password, Peers: peers, Inputs: inputs, Entities: entities}, nil
}
