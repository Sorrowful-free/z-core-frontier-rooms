package reservation_test

import (
	"fmt"
	"sync"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

// legacyGlobalReservation — in-memory reservation с одним глобальным mutex (до per-room lock).
// Только для бенчмарков сравнения; не production-код.
type legacyGlobalReservation struct {
	rooms map[domain.RoomID]legacyRoomSlot
	mu    sync.Mutex
}

type legacyRoomSlot struct {
	capacity int
	peers    map[domain.PeerID]legacyPeerSlot
}

type legacyPeerSlot struct {
	state     legacyReservationState
	expiresAt time.Time
}

type legacyReservationState int

const (
	legacyReservationReserved legacyReservationState = iota
	legacyReservationAdmitted
)

func newLegacyGlobalReservation() *legacyGlobalReservation {
	return &legacyGlobalReservation{
		rooms: make(map[domain.RoomID]legacyRoomSlot),
	}
}

func (r *legacyGlobalReservation) registerRoom(roomID domain.RoomID, capacity int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rooms[roomID] = legacyRoomSlot{
		capacity: capacity,
		peers:    make(map[domain.PeerID]legacyPeerSlot),
	}
}

func (r *legacyGlobalReservation) reserve(roomID domain.RoomID, peerID domain.PeerID, expiresAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	room, ok := r.rooms[roomID]
	if !ok {
		return fmt.Errorf("%w: room %d", domain.ErrReservationNotFound, roomID)
	}

	r.sweepExpiredLocked(&room)

	if len(room.peers) >= room.capacity {
		return fmt.Errorf("%w: room %d", domain.ErrReservationFull, roomID)
	}
	if _, ok := room.peers[peerID]; ok {
		return fmt.Errorf("%w: room %d peer %d", domain.ErrReservationAlreadyExists, roomID, peerID)
	}

	room.peers[peerID] = legacyPeerSlot{
		state:     legacyReservationReserved,
		expiresAt: expiresAt,
	}
	r.rooms[roomID] = room
	return nil
}

func (r *legacyGlobalReservation) admit(roomID domain.RoomID, peerID domain.PeerID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	room, ok := r.rooms[roomID]
	if !ok {
		return fmt.Errorf("%w: room %d", domain.ErrReservationNotFound, roomID)
	}

	peer, ok := room.peers[peerID]
	if !ok {
		return fmt.Errorf("%w: room %d peer %d", domain.ErrReservationNotReserved, roomID, peerID)
	}
	if peer.state == legacyReservationAdmitted {
		return nil
	}
	if peer.expiresAt.Before(time.Now()) {
		delete(room.peers, peerID)
		r.rooms[roomID] = room
		return fmt.Errorf("%w: room %d peer %d", domain.ErrReservationExpired, roomID, peerID)
	}

	room.peers[peerID] = legacyPeerSlot{
		state:     legacyReservationAdmitted,
		expiresAt: peer.expiresAt,
	}
	r.rooms[roomID] = room
	return nil
}

func (r *legacyGlobalReservation) revoke(roomID domain.RoomID, peerID domain.PeerID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	room, ok := r.rooms[roomID]
	if !ok {
		return fmt.Errorf("%w: room %d", domain.ErrReservationNotFound, roomID)
	}
	delete(room.peers, peerID)
	r.rooms[roomID] = room
	return nil
}

func (r *legacyGlobalReservation) sweepExpiredLocked(room *legacyRoomSlot) {
	now := time.Now()
	for peerID, peer := range room.peers {
		if peer.state == legacyReservationReserved && !peer.expiresAt.IsZero() && peer.expiresAt.Before(now) {
			delete(room.peers, peerID)
		}
	}
}
