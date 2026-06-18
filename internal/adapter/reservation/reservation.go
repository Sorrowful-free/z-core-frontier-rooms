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
	peerID     domain.PeerID
	state      reservationState
	expiresAt  time.Time
	admittedAt time.Time
}

type reservationRoom struct {
	mu       sync.Mutex
	capacity int
	password string
	peers    map[domain.PeerID]peerSlot
}

type Reservation struct {
	rooms   map[domain.RoomID]*reservationRoom
	roomsMu sync.RWMutex
	logger  logging.Logger
}

func NewReservation(logger logging.Logger) *Reservation {
	return &Reservation{
		rooms:  make(map[domain.RoomID]*reservationRoom),
		logger: logger,
	}
}

func (r *Reservation) lookupRoom(roomID domain.RoomID) (*reservationRoom, error) {
	r.roomsMu.RLock()
	room, ok := r.rooms[roomID]
	r.roomsMu.RUnlock()
	if !ok {
		return nil, reservationErrRoom(domain.ErrReservationNotFound, roomID)
	}
	return room, nil
}

func (r *Reservation) RegisterRoom(ctx context.Context, roomID domain.RoomID, capacity int, password string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.roomsMu.Lock()
	defer r.roomsMu.Unlock()

	if _, ok := r.rooms[roomID]; ok {
		return reservationErrRoom(domain.ErrReservationAlreadyExists, roomID)
	}

	r.rooms[roomID] = &reservationRoom{
		capacity: capacity,
		password: password,
		peers:    make(map[domain.PeerID]peerSlot),
	}
	return nil
}

func (r *Reservation) VerifyRoomPassword(ctx context.Context, roomID domain.RoomID, password string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	room, err := r.lookupRoom(roomID)
	if err != nil {
		return err
	}

	room.mu.Lock()
	defer room.mu.Unlock()

	if room.password == "" {
		return nil
	}
	if room.password != password {
		return reservationErrRoom(domain.ErrInvalidCredentials, roomID)
	}
	return nil
}

func (r *Reservation) UnregisterRoom(ctx context.Context, roomID domain.RoomID) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.roomsMu.Lock()
	defer r.roomsMu.Unlock()

	if _, ok := r.rooms[roomID]; !ok {
		return reservationErrRoom(domain.ErrReservationNotFound, roomID)
	}

	delete(r.rooms, roomID)
	return nil
}

func (r *Reservation) Reserve(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID, expiresAt time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	room, err := r.lookupRoom(roomID)
	if err != nil {
		return err
	}

	room.mu.Lock()
	defer room.mu.Unlock()

	room.sweepExpiredLocked()

	if len(room.peers) >= room.capacity {
		return reservationErrRoom(domain.ErrReservationFull, roomID)
	}

	if _, ok := room.peers[peerID]; ok {
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

	room, err := r.lookupRoom(roomID)
	if err != nil {
		return err
	}

	room.mu.Lock()
	defer room.mu.Unlock()

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
		peerID:     peerID,
		state:      reservationStateAdmitted,
		admittedAt: time.Now(),
	}
	return nil
}

func (r *Reservation) State(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID) (domain.ReservationSlot, error) {
	if err := ctx.Err(); err != nil {
		return domain.ReservationSlotNone, err
	}

	room, err := r.lookupRoom(roomID)
	if err != nil {
		return domain.ReservationSlotNone, err
	}

	room.mu.Lock()
	defer room.mu.Unlock()

	peer, ok := room.peers[peerID]
	if !ok {
		return domain.ReservationSlotNone, nil
	}

	switch peer.state {
	case reservationStateReserved:
		return domain.ReservationSlotReserved, nil
	case reservationStateAdmitted:
		return domain.ReservationSlotAdmitted, nil
	default:
		return domain.ReservationSlotNone, nil
	}
}

func (r *Reservation) ListAdmittedPeers(ctx context.Context, roomID domain.RoomID) ([]domain.AdmittedSlot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	room, err := r.lookupRoom(roomID)
	if err != nil {
		return nil, err
	}

	room.mu.Lock()
	defer room.mu.Unlock()

	slots := make([]domain.AdmittedSlot, 0, len(room.peers))
	for peerID, peer := range room.peers {
		if peer.state != reservationStateAdmitted {
			continue
		}
		slots = append(slots, domain.AdmittedSlot{
			PeerID:     peerID,
			AdmittedAt: peer.admittedAt,
		})
	}
	return slots, nil
}

func (r *Reservation) Revoke(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	room, err := r.lookupRoom(roomID)
	if err != nil {
		return err
	}

	room.mu.Lock()
	defer room.mu.Unlock()

	if _, ok := room.peers[peerID]; !ok {
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

func (room *reservationRoom) sweepExpiredLocked() {
	for peerID, peer := range room.peers {
		if peer.state == reservationStateReserved && !peer.expiresAt.IsZero() && peer.expiresAt.Before(time.Now()) {
			delete(room.peers, peerID)
		}
	}
}
