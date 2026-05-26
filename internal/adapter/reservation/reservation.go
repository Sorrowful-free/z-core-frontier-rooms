package reservation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
)

type reservationState int

const (
	reservationStateReserved reservationState = iota
	reservationStateAdmitted
)

type peerSlot struct {
	peerID    domain.PeerID
	state     reservationState
	expiresAt time.Time
}
type roomSlot struct {
	capacity int
	peers    map[domain.PeerID]peerSlot
}

type Reservation struct {
	rooms  map[domain.RoomID]roomSlot
	mutex  sync.Mutex
	logger logging.Logger
}

func NewReservation(logger logging.Logger) *Reservation {
	return &Reservation{
		rooms:  make(map[domain.RoomID]roomSlot),
		mutex:  sync.Mutex{},
		logger: logger,
	}
}

func (r *Reservation) RegisterRoom(ctx context.Context, roomID domain.RoomID, capacity int) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()

	_, ok := r.rooms[roomID]
	if ok {
		return reservationErrRoom(domain.ErrReservationAlreadyExists, roomID)
	}

	r.rooms[roomID] = roomSlot{
		capacity: capacity,
		peers:    make(map[domain.PeerID]peerSlot),
	}
	return nil
}

func (r *Reservation) UnregisterRoom(ctx context.Context, roomID domain.RoomID) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()

	_, ok := r.rooms[roomID]
	if !ok {
		return reservationErrRoom(domain.ErrReservationNotFound, roomID)
	}

	delete(r.rooms, roomID)
	return nil
}

func (r *Reservation) Reserve(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID, expiresAt time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()

	room, ok := r.rooms[roomID]
	if !ok {
		return reservationErrRoom(domain.ErrReservationNotFound, roomID)
	}

	r.sweepExpiredLocked(&room)

	if len(room.peers) >= room.capacity {
		return reservationErrRoom(domain.ErrReservationFull, roomID)
	}

	_, ok = room.peers[peerID]
	if ok {
		return reservationErrPeer(domain.ErrReservationAlreadyExists, roomID, peerID)
	}

	room.peers[peerID] = peerSlot{
		peerID:    peerID,
		state:     reservationStateReserved,
		expiresAt: expiresAt,
	}
	return nil
}

func (r *Reservation) Admit(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()

	room, ok := r.rooms[roomID]
	if !ok {
		return reservationErrRoom(domain.ErrReservationNotFound, roomID)
	}

	peer, ok := room.peers[peerID]
	if !ok {
		return reservationErrPeer(domain.ErrReservationNotReserved, roomID, peerID)
	}

	if peer.state == reservationStateAdmitted {
		return nil
	}

	if peer.state != reservationStateReserved {
		return reservationErrPeer(domain.ErrReservationNotReserved, roomID, peerID)
	}

	if peer.expiresAt.Before(time.Now()) {
		delete(room.peers, peerID)
		return reservationErrPeer(domain.ErrReservationExpired, roomID, peerID)
	}

	room.peers[peerID] = peerSlot{
		peerID:    peerID,
		state:     reservationStateAdmitted,
		expiresAt: time.Time{},
	}
	return nil
}

func (r *Reservation) Revoke(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()

	room, ok := r.rooms[roomID]
	if !ok {
		return reservationErrRoom(domain.ErrReservationNotFound, roomID)
	}

	_, ok = room.peers[peerID]
	if !ok {
		return nil
	}

	delete(room.peers, peerID)
	return nil
}

func reservationErrRoom(err error, roomID domain.RoomID) error {
	return fmt.Errorf("%w: room %d", err, roomID)
}

func reservationErrPeer(err error, roomID domain.RoomID, peerID domain.PeerID) error {
	return fmt.Errorf("%w: room %d peer %d", err, roomID, peerID)
}

func (r *Reservation) sweepExpiredLocked(room *roomSlot) {
	for peerID, peer := range room.peers {
		if peer.state == reservationStateReserved && !peer.expiresAt.IsZero() && peer.expiresAt.Before(time.Now()) {
			delete(room.peers, peerID)
		}
	}
}
