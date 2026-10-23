package reservation

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"sync"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
)

type reservationState int

const (
	reservationStateReserved reservationState = iota
	reservationStateAdmitted
)

type peerSlot struct {
	peerID     domain.PeerID
	nickName   string
	state      reservationState
	expiresAt  time.Time
	admittedAt time.Time
}

type reservationRoom struct {
	mu           sync.Mutex
	capacity     int
	passwordHash []byte
	unregistered bool
	peers        map[domain.PeerID]peerSlot
}

type Reservation struct {
	rooms   map[domain.RoomID]*reservationRoom
	roomsMu sync.RWMutex
	logger  logging.Logger
	metrics portmetrics.Metrics
}

func NewReservation(logger logging.Logger, m portmetrics.Metrics) *Reservation {
	return &Reservation{
		rooms:   make(map[domain.RoomID]*reservationRoom),
		logger:  logger,
		metrics: portmetrics.OrNoop(m),
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
	if capacity < 1 || capacity > domain.MaxRoomCapacity {
		return reservationErrRoom(domain.ErrInvalidRoomCapacity, roomID)
	}

	var passwordHash []byte
	if password != "" {
		sum := sha256.Sum256([]byte(password))
		passwordHash = sum[:]
	}

	r.roomsMu.Lock()
	defer r.roomsMu.Unlock()

	if _, ok := r.rooms[roomID]; ok {
		return reservationErrRoom(domain.ErrReservationAlreadyExists, roomID)
	}

	r.rooms[roomID] = &reservationRoom{
		capacity:     capacity,
		passwordHash: passwordHash,
		peers:        make(map[domain.PeerID]peerSlot),
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

	if err := room.checkLive(roomID); err != nil {
		return err
	}
	if len(room.passwordHash) == 0 {
		return nil
	}
	sum := sha256.Sum256([]byte(password))
	if subtle.ConstantTimeCompare(room.passwordHash, sum[:]) != 1 {
		return reservationErrRoom(domain.ErrInvalidCredentials, roomID)
	}
	return nil
}

func (r *Reservation) UnregisterRoom(ctx context.Context, roomID domain.RoomID) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.roomsMu.Lock()
	room, ok := r.rooms[roomID]
	if ok {
		delete(r.rooms, roomID)
	}
	r.roomsMu.Unlock()

	if !ok {
		return reservationErrRoom(domain.ErrReservationNotFound, roomID)
	}

	// Tombstone: конкурентные операции, захватившие room до delete,
	// увидят unregistered под room.mu и откажутся писать в мёртвую map.
	room.mu.Lock()
	room.unregistered = true
	slots := len(room.peers)
	room.mu.Unlock()
	// Слоты комнаты умирают вместе с tombstone: gauge живых слотов
	// снимает их все (revoke-оповещения по одному не шлём - комната
	// исчезает целиком, а не по слоту).
	if slots > 0 {
		r.metrics.ReservationActiveSlots(-slots)
	}
	return nil
}

func (r *Reservation) Reserve(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID, nickName string, expiresAt time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	room, err := r.lookupRoom(roomID)
	if err != nil {
		return err
	}

	room.mu.Lock()
	defer room.mu.Unlock()

	if err := room.checkLive(roomID); err != nil {
		return err
	}

	if expired := room.sweepExpiredLocked(); expired > 0 {
		// Просроченный слот освобождён - тот же revoke по метрике:
		// счётчик сходится к живым слотам, gauge - к реальной дельте.
		for range expired {
			r.metrics.ReservationRevoked()
		}
		r.metrics.ReservationActiveSlots(-expired)
	}

	if len(room.peers) >= room.capacity {
		return reservationErrRoom(domain.ErrReservationFull, roomID)
	}

	if _, ok := room.peers[peerID]; ok {
		return reservationErrPeer(domain.ErrReservationAlreadyExists, roomID, peerID)
	}

	for _, slot := range room.peers {
		if slot.state == reservationStateReserved && slot.nickName == nickName {
			return reservationErrPeer(domain.ErrTicketSlotHeld, roomID, peerID)
		}
	}

	room.peers[peerID] = peerSlot{
		peerID:    peerID,
		nickName:  nickName,
		state:     reservationStateReserved,
		expiresAt: expiresAt,
	}
	r.metrics.ReservationReserved()
	r.metrics.ReservationActiveSlots(1)
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

	if err := room.checkLive(roomID); err != nil {
		return err
	}

	peer, ok := room.peers[peerID]
	if !ok {
		return reservationErrPeer(domain.ErrReservationNotReserved, roomID, peerID)
	}

	if peer.state == reservationStateAdmitted {
		return reservationErrPeer(domain.ErrReservationAlreadyAdmitted, roomID, peerID)
	}

	if peer.state != reservationStateReserved {
		return reservationErrPeer(domain.ErrReservationNotReserved, roomID, peerID)
	}

	if peer.expiresAt.Before(time.Now()) {
		delete(room.peers, peerID)
		// Просроченный слот удалён - это реальный revoke: счётчик
		// должен сходиться к живым слотам, а не только к явным Revoke.
		r.metrics.ReservationRevoked()
		r.metrics.ReservationActiveSlots(-1)
		return reservationErrPeer(domain.ErrReservationExpired, roomID, peerID)
	}

	room.peers[peerID] = peerSlot{
		peerID:     peerID,
		nickName:   peer.nickName,
		state:      reservationStateAdmitted,
		admittedAt: time.Now(),
	}
	r.metrics.ReservationAdmitted()
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

	if err := room.checkLive(roomID); err != nil {
		return domain.ReservationSlotNone, err
	}

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

	if err := room.checkLive(roomID); err != nil {
		return nil, err
	}

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

	if err := room.checkLive(roomID); err != nil {
		return err
	}

	if _, ok := room.peers[peerID]; !ok {
		return nil
	}

	delete(room.peers, peerID)
	r.metrics.ReservationRevoked()
	r.metrics.ReservationActiveSlots(-1)
	return nil
}

// checkLive отклоняет операции над tombstone-комнатой (после UnregisterRoom).
// Вызывать под room.mu.
func (room *reservationRoom) checkLive(roomID domain.RoomID) error {
	if room.unregistered {
		return reservationErrRoom(domain.ErrReservationNotFound, roomID)
	}
	return nil
}

func reservationErrRoom(err error, roomID domain.RoomID) error {
	return fmt.Errorf("%w: room %d", err, roomID)
}

func reservationErrPeer(err error, roomID domain.RoomID, peerID domain.PeerID) error {
	return fmt.Errorf("%w: room %d peer %d", err, roomID, peerID)
}

// sweepExpiredLocked вычищает просроченные reserved-слоты и возвращает
// число удалённых - caller эмитит revoke-метрики по факту удалений.
func (room *reservationRoom) sweepExpiredLocked() int {
	expired := 0
	for peerID, peer := range room.peers {
		if peer.state == reservationStateReserved && !peer.expiresAt.IsZero() && peer.expiresAt.Before(time.Now()) {
			delete(room.peers, peerID)
			expired++
		}
	}
	return expired
}
