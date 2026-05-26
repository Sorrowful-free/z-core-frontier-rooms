package domain

import (
	"errors"
	"fmt"
	"math"
	"strconv"
)

var (
	ErrInvalidRoomID = errors.New("invalid room id")
	ErrInvalidPeerID = errors.New("invalid peer id")
)

// MaxRoomID и MaxPeerID — верхняя граница wire/API (uint32, Godot-friendly).
const (
	MaxRoomID = RoomID(math.MaxUint32)
	MaxPeerID = PeerID(math.MaxUint32)
)

func ParseRoomIDString(s string) (RoomID, error) {
	if s == "" {
		return RoomIDInvalid, fmt.Errorf("%w: empty", ErrInvalidRoomID)
	}
	u, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return RoomIDInvalid, fmt.Errorf("%w: %w", ErrInvalidRoomID, err)
	}
	return roomIDFromUint32(uint32(u))
}

func RoomIDFromInt64(v int64) (RoomID, error) {
	if v < 0 {
		return RoomIDInvalid, fmt.Errorf("%w: negative value %d", ErrInvalidRoomID, v)
	}
	return RoomIDFromUint64(uint64(v))
}

func RoomIDFromUint64(v uint64) (RoomID, error) {
	if v > math.MaxUint32 {
		return RoomIDInvalid, fmt.Errorf("%w: exceeds max uint32 (%d)", ErrInvalidRoomID, v)
	}
	return roomIDFromUint32(uint32(v))
}

func roomIDFromUint32(v uint32) (RoomID, error) {
	id := RoomID(v)
	if !id.IsValid() {
		return RoomIDInvalid, fmt.Errorf("%w: must be positive, got %d", ErrInvalidRoomID, v)
	}
	return id, nil
}

func ParsePeerIDString(s string) (PeerID, error) {
	if s == "" {
		return PeerIDInvalid, fmt.Errorf("%w: empty", ErrInvalidPeerID)
	}
	u, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return PeerIDInvalid, fmt.Errorf("%w: %w", ErrInvalidPeerID, err)
	}
	return peerIDFromUint32(uint32(u))
}

func PeerIDFromInt64(v int64) (PeerID, error) {
	if v < 0 {
		return PeerIDInvalid, fmt.Errorf("%w: negative value %d", ErrInvalidPeerID, v)
	}
	return PeerIDFromUint64(uint64(v))
}

func PeerIDFromUint64(v uint64) (PeerID, error) {
	if v > math.MaxUint32 {
		return PeerIDInvalid, fmt.Errorf("%w: exceeds max uint32 (%d)", ErrInvalidPeerID, v)
	}
	return peerIDFromUint32(uint32(v))
}

func peerIDFromUint32(v uint32) (PeerID, error) {
	id := PeerID(v)
	if !id.IsValid() {
		return PeerIDInvalid, fmt.Errorf("%w: must be positive, got %d", ErrInvalidPeerID, v)
	}
	return id, nil
}
