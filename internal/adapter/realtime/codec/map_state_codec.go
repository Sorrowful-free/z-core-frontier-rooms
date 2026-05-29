package codec

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

func writeMapState[K comparable, V any](buf *bytes.Buffer, m *state.MapState[K, V], writeKey func(K, *bytes.Buffer) error, writeValue func(V, *bytes.Buffer) error) error {
	mapLen := len(m.Items)

	if mapLen > math.MaxUint16 {
		return fmt.Errorf("map length too large: %d", mapLen)
	}

	if err := binary.Write(buf, binary.BigEndian, uint16(mapLen)); err != nil {
		return err
	}
	for k, v := range m.Items {
		if err := writeKey(k, buf); err != nil {
			return err
		}
		if err := writeValue(v, buf); err != nil {
			return err
		}
	}
	return nil
}

func readMapState[K comparable, V any](buf *bytes.Buffer, readKey func(*bytes.Buffer) (K, error), readValue func(*bytes.Buffer) (V, error)) (*state.MapState[K, V], error) {
	mapLen := uint16(0)
	if err := binary.Read(buf, binary.BigEndian, &mapLen); err != nil {
		return nil, err
	}
	if mapLen > math.MaxUint16 {
		return nil, fmt.Errorf("map length too large: %d", mapLen)
	}
	mapState := state.NewMapState[K, V]()
	for i := uint16(0); i < mapLen; i++ {
		key, err := readKey(buf)
		if err != nil {
			return nil, err
		}
		value, err := readValue(buf)
		if err != nil {
			return nil, err
		}
		if _, ok := mapState.Items[key]; ok {
			return nil, fmt.Errorf("key %v already exists in map", key)
		}
		mapState.Items[key] = value
	}
	return mapState, nil
}

func writeMapPatсhState[K comparable, S any, P any](buf *bytes.Buffer, m *state.MapStatePatch[K, S, P], writeKey func(K, *bytes.Buffer) error, writeValue func(S, *bytes.Buffer) error, writePatch func(P, *bytes.Buffer) error) error {
	addedLen := len(m.Added)
	updatedLen := len(m.Updated)
	removedLen := len(m.Removed)
	if addedLen > math.MaxUint16 || updatedLen > math.MaxUint16 || removedLen > math.MaxUint16 {
		return fmt.Errorf("map length too large: %d", addedLen+updatedLen+removedLen)
	}

	if err := binary.Write(buf, binary.BigEndian, uint16(addedLen)); err != nil {
		return err
	}
	for k, v := range m.Added {
		if err := writeKey(k, buf); err != nil {
			return err
		}
		if err := writeValue(v, buf); err != nil {
			return err
		}
	}
	if err := binary.Write(buf, binary.BigEndian, uint16(updatedLen)); err != nil {
		return err
	}
	for k, v := range m.Updated {
		if err := writeKey(k, buf); err != nil {
			return err
		}
		if err := writePatch(v, buf); err != nil {
			return err
		}
	}
	if err := binary.Write(buf, binary.BigEndian, uint16(removedLen)); err != nil {
		return err
	}
	for _, k := range m.Removed {
		if err := writeKey(k, buf); err != nil {
			return err
		}
	}
	return nil
}

func readMapPathState[K comparable, S any, P any](buf *bytes.Buffer, readKey func(*bytes.Buffer) (K, error), readValue func(*bytes.Buffer) (S, error), readPatch func(*bytes.Buffer) (P, error)) (*state.MapStatePatch[K, S, P], error) {
	addedLen := uint16(0)
	if err := binary.Read(buf, binary.BigEndian, &addedLen); err != nil {
		return nil, err
	}
	if addedLen > math.MaxUint16 {
		return nil, fmt.Errorf("added length too large: %d", addedLen)
	}
	added := make(map[K]S)
	for i := uint16(0); i < addedLen; i++ {
		key, err := readKey(buf)
		if err != nil {
			return nil, err
		}
		value, err := readValue(buf)
		if err != nil {
			return nil, err
		}
		if _, ok := added[key]; ok {
			return nil, fmt.Errorf("key %v already exists in map", key)
		}
		added[key] = value
	}
	updatedLen := uint16(0)
	if err := binary.Read(buf, binary.BigEndian, &updatedLen); err != nil {
		return nil, err
	}
	if updatedLen > math.MaxUint16 {
		return nil, fmt.Errorf("updated length too large: %d", updatedLen)
	}
	updated := make(map[K]P)
	for i := uint16(0); i < updatedLen; i++ {
		key, err := readKey(buf)
		if err != nil {
			return nil, err
		}
		patch, err := readPatch(buf)
		if err != nil {
			return nil, err
		}
		if _, ok := updated[key]; ok {
			return nil, fmt.Errorf("key %v already exists in map", key)
		}
		updated[key] = patch
	}
	removedLen := uint16(0)
	if err := binary.Read(buf, binary.BigEndian, &removedLen); err != nil {
		return nil, err
	}
	if removedLen > math.MaxUint16 {
		return nil, fmt.Errorf("removed length too large: %d", removedLen)
	}
	removed := make([]K, 0)
	for i := uint16(0); i < removedLen; i++ {
		key, err := readKey(buf)
		if err != nil {
			return nil, err
		}
		removed = append(removed, key)
	}
	return &state.MapStatePatch[K, S, P]{
		Added:   added,
		Updated: updated,
		Removed: removed,
	}, nil
}
