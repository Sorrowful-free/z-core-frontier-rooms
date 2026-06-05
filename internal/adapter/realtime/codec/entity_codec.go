package codec

import (
	"bytes"
	"encoding/binary"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

const (
	EntityStateMaxComponents = 64
	EntitiesStateMaxEntities = 65535
)
const (
	EntityStateFlagsOwner = 1 << iota
	EntityStateFlagsComponents
)

type EntitiesStateCodec struct {
}

func NewEntitiesStateCodec() *EntitiesStateCodec {
	return &EntitiesStateCodec{}
}

func (c *EntitiesStateCodec) Encode(entities state.EntitiesState) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 1024))
	if err := writeMapState(buf, entities, func(k state.EntityID, buf *bytes.Buffer) error {
		return writeEntityID(buf, k)
	}, func(v *state.EntityState, buf *bytes.Buffer) error {
		return writeEntityState(buf, v)
	}, EntitiesStateMaxEntities); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (c *EntitiesStateCodec) Decode(data []byte) (state.EntitiesState, error) {
	buf := bytes.NewBuffer(data)
	return readMapState(buf, func(buf *bytes.Buffer) (state.EntityID, error) {
		return readEntityID(buf)
	}, func(buf *bytes.Buffer) (*state.EntityState, error) {
		return readEntityState(buf)
	}, EntitiesStateMaxEntities)
}

func (c *EntitiesStateCodec) EncodePatch(entitiesPatch *state.EntitiesStatePatch) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 1024))
	if err := writeMapPatсhState(buf, *entitiesPatch, func(k state.EntityID, buf *bytes.Buffer) error {
		return writeEntityID(buf, k)
	}, func(v *state.EntityState, buf *bytes.Buffer) error {
		return writeEntityState(buf, v)
	}, func(v *state.EntityStatePatch, buf *bytes.Buffer) error {
		return writeEntityStatePatch(buf, v)
	}, EntitiesStateMaxEntities); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (c *EntitiesStateCodec) DecodePatch(data []byte) (*state.EntitiesStatePatch, error) {
	buf := bytes.NewBuffer(data)
	return readMapPatchState(buf, func(buf *bytes.Buffer) (state.EntityID, error) {
		return readEntityID(buf)
	}, func(buf *bytes.Buffer) (*state.EntityState, error) {
		return readEntityState(buf)
	}, func(buf *bytes.Buffer) (*state.EntityStatePatch, error) {
		return readEntityStatePatch(buf)
	}, EntitiesStateMaxEntities)
}

func writeEntityID(buf *bytes.Buffer, id state.EntityID) error {
	return binary.Write(buf, binary.BigEndian, id)
}

func readEntityID(buf *bytes.Buffer) (state.EntityID, error) {
	var id state.EntityID
	if err := binary.Read(buf, binary.BigEndian, &id); err != nil {
		return state.EntityIDNone, err
	}
	return id, nil
}

func writeEntityState(buf *bytes.Buffer, entity *state.EntityState) error {
	if err := writePeerID(buf, entity.Owner); err != nil {
		return err
	}
	return writeMapState(buf, entity.Components, func(k state.ComponentID, buf *bytes.Buffer) error {
		return writeComponentID(buf, k)
	}, func(v *state.ComponentState, buf *bytes.Buffer) error {
		return writeComponentState(buf, v)
	}, EntityStateMaxComponents)
}

func readEntityState(buf *bytes.Buffer) (*state.EntityState, error) {
	owner, err := readPeerID(buf)
	if err != nil {
		return nil, err
	}
	components, err := readMapState(buf, func(buf *bytes.Buffer) (state.ComponentID, error) {
		return readComponentID(buf)
	}, func(buf *bytes.Buffer) (*state.ComponentState, error) {
		return readComponentState(buf)
	}, EntityStateMaxComponents)
	if err != nil {
		return nil, err
	}
	return &state.EntityState{Owner: owner, Components: components}, nil
}

func writeEntityStatePatch(buf *bytes.Buffer, patch *state.EntityStatePatch) error {

	flags := byte(0)
	if patch.Owner != nil {
		flags |= EntityStateFlagsOwner
	}
	if len(patch.Components.Added) > 0 || len(patch.Components.Updated) > 0 || len(patch.Components.Removed) > 0 {
		flags |= EntityStateFlagsComponents
	}
	if err := binary.Write(buf, binary.BigEndian, flags); err != nil {
		return err
	}
	if patch.Owner != nil {
		if err := binary.Write(buf, binary.BigEndian, *patch.Owner); err != nil {
			return err
		}
	}

	if flags&EntityStateFlagsComponents == EntityStateFlagsComponents {
		return writeMapPatсhState(buf, patch.Components, func(k state.ComponentID, buf *bytes.Buffer) error {
			return writeComponentID(buf, k)
		}, func(c *state.ComponentState, buf *bytes.Buffer) error {
			return writeComponentState(buf, c)
		}, func(c *state.ComponentStatePatch, buf *bytes.Buffer) error {
			return writeComponentStatePatch(buf, c)
		}, EntityStateMaxComponents)
	}
	return nil
}

func readEntityStatePatch(buf *bytes.Buffer) (*state.EntityStatePatch, error) {

	var flags byte
	if err := binary.Read(buf, binary.BigEndian, &flags); err != nil {
		return nil, err
	}

	var owner *domain.PeerID
	if flags&EntityStateFlagsOwner == EntityStateFlagsOwner {
		o, err := readPeerID(buf)
		if err != nil {
			return nil, err
		}
		owner = &o
	}

	components := state.MapStatePatch[state.ComponentID, state.ComponentState, state.ComponentStatePatch]{}

	if flags&EntityStateFlagsComponents == EntityStateFlagsComponents {
		c, err := readMapPatchState(buf, func(buf *bytes.Buffer) (state.ComponentID, error) {
			return readComponentID(buf)
		}, func(buf *bytes.Buffer) (*state.ComponentState, error) {
			return readComponentState(buf)
		}, func(buf *bytes.Buffer) (*state.ComponentStatePatch, error) {
			return readComponentStatePatch(buf)
		}, EntityStateMaxComponents)
		if err != nil {
			return nil, err
		}
		components = *c
	}
	return &state.EntityStatePatch{Owner: owner, Components: components}, nil
}
