package room_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	useroom "github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/mocks"
	"go.uber.org/mock/gomock"
)

// testReconnectTTL — the grace deadline in resume tests; a fresh LastSeen
// passes the gate, a stale one (now - 2*ttl) is rejected as expired.
const testReconnectTTL = time.Minute

func newReconnectClaims(roomID domain.RoomID, peerID domain.PeerID) domain.Claims {
	claims := newTestClaims(roomID, peerID, "")
	claims.Kind = domain.ClaimsKindReconnect
	return claims
}

// freshAdmittedSlot — a slot with a fresh lastSeen: the deadline gate (P24) lets it through.
func freshAdmittedSlot(peerID domain.PeerID, nick string) domain.AdmittedSlot {
	return domain.AdmittedSlot{PeerID: peerID, NickName: nick, LastSeen: time.Now()}
}

// Resume of an absent peer: Reconnect → CreatePeer from the slot → Join →
// OpPeerReconnected broadcast to the others. reservation.Admit is not called,
// the slot is not revoked.
func TestReconnectRoom_JoinAbsentPeer(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	peerFactory := mocks.NewMockPeerFactory(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer := mocks.NewMockPeer(ctrl)
	conn := mocks.NewMockConnection(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)
	claims := newReconnectClaims(roomID, peerID)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().
		SlotDetails(gomock.Any(), roomID, peerID).
		Return(freshAdmittedSlot(peerID, "player"), nil)
	reservation.EXPECT().
		Reconnect(gomock.Any(), roomID, peerID).
		Return(domain.AdmittedSlot{PeerID: peerID, NickName: "player"}, nil)
	room.EXPECT().Context().Return(context.Background()).AnyTimes()
	peerFactory.EXPECT().CreatePeer(gomock.Any(), peerID, "player", conn, logger).Return(peer, nil)
	room.EXPECT().HasPeer(peerID).Return(false).Times(2)
	room.EXPECT().Join(peer).Return(nil)
	peer.EXPECT().Start(room).Return(nil)
	room.EXPECT().Send(gomock.Any()).Return(nil)
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewReconnectRoomUseCase(peerFactory, registry, reservation, testReconnectTTL, logger, nil)
	session, err := uc.ReconnectRoom(context.Background(), conn, claims)
	if err != nil {
		t.Fatalf("ReconnectRoom: %v", err)
	}
	if session.Peer != peer || session.PeerID != peerID || session.RoomID != roomID {
		t.Fatalf("session = %+v", session)
	}
}

// Reconnect over a live peer — Replace (the newest connection wins).
func TestReconnectRoom_ReplaceLivePeer(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	peerFactory := mocks.NewMockPeerFactory(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer := mocks.NewMockPeer(ctrl)
	conn := mocks.NewMockConnection(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)
	claims := newReconnectClaims(roomID, peerID)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().
		SlotDetails(gomock.Any(), roomID, peerID).
		Return(freshAdmittedSlot(peerID, "player"), nil)
	reservation.EXPECT().
		Reconnect(gomock.Any(), roomID, peerID).
		Return(domain.AdmittedSlot{PeerID: peerID, NickName: "player"}, nil)
	room.EXPECT().Context().Return(context.Background()).AnyTimes()
	peerFactory.EXPECT().CreatePeer(gomock.Any(), peerID, "player", conn, logger).Return(peer, nil)
	room.EXPECT().HasPeer(peerID).Return(true).Times(2)
	room.EXPECT().Replace(peer).Return(nil)
	peer.EXPECT().Start(room).Return(nil)
	room.EXPECT().Send(gomock.Any()).Return(nil)
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewReconnectRoomUseCase(peerFactory, registry, reservation, testReconnectTTL, logger, nil)
	session, err := uc.ReconnectRoom(context.Background(), conn, claims)
	if err != nil {
		t.Fatalf("ReconnectRoom: %v", err)
	}
	if session.Peer != peer {
		t.Fatal("session.Peer must be the created peer")
	}
}

// Reject resume: the slot is missing / not admitted → ErrReservationNotReserved;
// Revoke and Admit are not called.
func TestReconnectRoom_SlotNotAdmitted(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	peerFactory := mocks.NewMockPeerFactory(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().
		SlotDetails(gomock.Any(), roomID, peerID).
		Return(domain.AdmittedSlot{}, domain.ErrReservationNotReserved)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewReconnectRoomUseCase(peerFactory, registry, reservation, testReconnectTTL, logger, nil)
	_, err := uc.ReconnectRoom(context.Background(), mocks.NewMockConnection(ctrl), newReconnectClaims(roomID, peerID))
	if !errors.Is(err, domain.ErrReservationNotReserved) {
		t.Fatalf("err = %v, want ErrReservationNotReserved", err)
	}
}

// Double-resume race of the same reconnect token: HasPeer is still false, but
// the winning resume has already inserted the peer — Join returns
// ErrPeerAlreadyInRoom (wrapped in ErrJoinDenied by the lifecycle). The loser
// pushes through with Replace (newest-wins): the winner's object stops the old
// one via old.Stop() inside Replace — no orphan is left outside the peers map.
func TestReconnectRoom_JoinRaceFallsBackToReplace(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	peerFactory := mocks.NewMockPeerFactory(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer := mocks.NewMockPeer(ctrl)
	conn := mocks.NewMockConnection(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)
	claims := newReconnectClaims(roomID, peerID)
	joinErr := fmt.Errorf("%w: %w", domain.ErrJoinDenied, domain.ErrPeerAlreadyInRoom)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().
		SlotDetails(gomock.Any(), roomID, peerID).
		Return(freshAdmittedSlot(peerID, "player"), nil)
	reservation.EXPECT().
		Reconnect(gomock.Any(), roomID, peerID).
		Return(domain.AdmittedSlot{PeerID: peerID, NickName: "player"}, nil)
	room.EXPECT().Context().Return(context.Background()).AnyTimes()
	peerFactory.EXPECT().CreatePeer(gomock.Any(), peerID, "player", conn, logger).Return(peer, nil)
	room.EXPECT().HasPeer(peerID).Return(false).Times(2)
	room.EXPECT().Join(peer).Return(joinErr)
	room.EXPECT().Replace(peer).Return(nil)
	peer.EXPECT().Start(room).Return(nil)
	room.EXPECT().Send(gomock.Any()).Return(nil)
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewReconnectRoomUseCase(peerFactory, registry, reservation, testReconnectTTL, logger, nil)
	session, err := uc.ReconnectRoom(context.Background(), conn, claims)
	if err != nil {
		t.Fatalf("ReconnectRoom: %v", err)
	}
	if session.Peer != peer {
		t.Fatal("session.Peer must be the created peer")
	}
}

// Replace after the lost double-resume race can also fail —
// the error goes out, the slot is not revoked.
func TestReconnectRoom_JoinRaceReplaceFailure(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	peerFactory := mocks.NewMockPeerFactory(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer := mocks.NewMockPeer(ctrl)
	conn := mocks.NewMockConnection(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)
	claims := newReconnectClaims(roomID, peerID)
	joinErr := fmt.Errorf("%w: %w", domain.ErrJoinDenied, domain.ErrPeerAlreadyInRoom)
	replaceErr := domain.ErrReplaceFailed

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().
		SlotDetails(gomock.Any(), roomID, peerID).
		Return(freshAdmittedSlot(peerID, "player"), nil)
	reservation.EXPECT().
		Reconnect(gomock.Any(), roomID, peerID).
		Return(domain.AdmittedSlot{PeerID: peerID, NickName: "player"}, nil)
	room.EXPECT().Context().Return(context.Background()).AnyTimes()
	peerFactory.EXPECT().CreatePeer(gomock.Any(), peerID, "player", conn, logger).Return(peer, nil)
	room.EXPECT().HasPeer(peerID).Return(false).Times(2)
	room.EXPECT().Join(peer).Return(joinErr)
	room.EXPECT().Replace(peer).Return(replaceErr)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewReconnectRoomUseCase(peerFactory, registry, reservation, testReconnectTTL, logger, nil)
	_, err := uc.ReconnectRoom(context.Background(), conn, claims)
	if !errors.Is(err, replaceErr) {
		t.Fatalf("err = %v, want ErrReplaceFailed", err)
	}
}

// Failure after Reconnect (join denied): the slot is not revoked.
func TestReconnectRoom_JoinFailureDoesNotRevoke(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	peerFactory := mocks.NewMockPeerFactory(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer := mocks.NewMockPeer(ctrl)
	conn := mocks.NewMockConnection(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)
	claims := newReconnectClaims(roomID, peerID)
	joinErr := errors.New("join denied")

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().
		SlotDetails(gomock.Any(), roomID, peerID).
		Return(freshAdmittedSlot(peerID, "player"), nil)
	reservation.EXPECT().
		Reconnect(gomock.Any(), roomID, peerID).
		Return(domain.AdmittedSlot{PeerID: peerID, NickName: "player"}, nil)
	room.EXPECT().Context().Return(context.Background()).AnyTimes()
	peerFactory.EXPECT().CreatePeer(gomock.Any(), peerID, "player", conn, logger).Return(peer, nil)
	room.EXPECT().HasPeer(peerID).Return(false).Times(2)
	room.EXPECT().Join(peer).Return(joinErr)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewReconnectRoomUseCase(peerFactory, registry, reservation, testReconnectTTL, logger, nil)
	_, err := uc.ReconnectRoom(context.Background(), conn, claims)
	if !errors.Is(err, joinErr) {
		t.Fatalf("err = %v, want joinErr", err)
	}
}

// P24: resume after lastSeen + reconnectTTL is rejected by the deadline gate
// before the refresh — ErrReservationExpired (on the wire — OpReservationExpired
// 0x74, the mapping is covered in tests/delivery/errors). Reconnect is not
// called: a rejected attempt does not extend the grace.
func TestReconnectRoom_ExpiredGraceRejected(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	peerFactory := mocks.NewMockPeerFactory(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)

	stale := domain.AdmittedSlot{
		PeerID:   peerID,
		NickName: "player",
		LastSeen: time.Now().Add(-2 * testReconnectTTL),
	}
	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().SlotDetails(gomock.Any(), roomID, peerID).Return(stale, nil)
	room.EXPECT().HasPeer(peerID).Return(false)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewReconnectRoomUseCase(peerFactory, registry, reservation, testReconnectTTL, logger, nil)
	_, err := uc.ReconnectRoom(context.Background(), mocks.NewMockConnection(ctrl), newReconnectClaims(roomID, peerID))
	if !errors.Is(err, domain.ErrReservationExpired) {
		t.Fatalf("err = %v, want ErrReservationExpired", err)
	}
}

// P24: a live peer (HasPeer) is not gated by the deadline — the lastSeen of a
// long-connected peer is legitimately old; the newest-wins Replace runs without
// the gate.
func TestReconnectRoom_LivePeerNotGatedByDeadline(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	peerFactory := mocks.NewMockPeerFactory(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer := mocks.NewMockPeer(ctrl)
	conn := mocks.NewMockConnection(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)
	claims := newReconnectClaims(roomID, peerID)

	// LastSeen is far past the deadline — normal for a live peer.
	stale := domain.AdmittedSlot{
		PeerID:   peerID,
		NickName: "player",
		LastSeen: time.Now().Add(-time.Hour),
	}
	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().SlotDetails(gomock.Any(), roomID, peerID).Return(stale, nil)
	reservation.EXPECT().
		Reconnect(gomock.Any(), roomID, peerID).
		Return(domain.AdmittedSlot{PeerID: peerID, NickName: "player"}, nil)
	room.EXPECT().Context().Return(context.Background()).AnyTimes()
	peerFactory.EXPECT().CreatePeer(gomock.Any(), peerID, "player", conn, logger).Return(peer, nil)
	room.EXPECT().HasPeer(peerID).Return(true).Times(2)
	room.EXPECT().Replace(peer).Return(nil)
	peer.EXPECT().Start(room).Return(nil)
	room.EXPECT().Send(gomock.Any()).Return(nil)
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewReconnectRoomUseCase(peerFactory, registry, reservation, testReconnectTTL, logger, nil)
	session, err := uc.ReconnectRoom(context.Background(), conn, claims)
	if err != nil {
		t.Fatalf("ReconnectRoom: %v", err)
	}
	if session.Peer != peer {
		t.Fatal("session.Peer must be the created peer")
	}
}

// P24: reconnectTTL = 0 ("no grace", the sweep zero-semantics) — any
// !HasPeer resume is expired even with a fresh lastSeen.
func TestReconnectRoom_ZeroTTLResumeExpired(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	peerFactory := mocks.NewMockPeerFactory(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().
		SlotDetails(gomock.Any(), roomID, peerID).
		Return(freshAdmittedSlot(peerID, "player"), nil)
	room.EXPECT().HasPeer(peerID).Return(false)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewReconnectRoomUseCase(peerFactory, registry, reservation, 0, logger, nil)
	_, err := uc.ReconnectRoom(context.Background(), mocks.NewMockConnection(ctrl), newReconnectClaims(roomID, peerID))
	if !errors.Is(err, domain.ErrReservationExpired) {
		t.Fatalf("err = %v, want ErrReservationExpired", err)
	}
}

// Sweep ↔ resume race (T5): SlotDetails still saw the slot, but by the time of
// Reconnect the sweep has revoked it — the losing resume gets
// ErrReservationNotReserved instead of succeeding on a stale snapshot.
func TestReconnectRoom_SlotRevokedBySweepBetweenCheckAndReconnect(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	peerFactory := mocks.NewMockPeerFactory(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().
		SlotDetails(gomock.Any(), roomID, peerID).
		Return(freshAdmittedSlot(peerID, "player"), nil)
	room.EXPECT().HasPeer(peerID).Return(false)
	reservation.EXPECT().
		Reconnect(gomock.Any(), roomID, peerID).
		Return(domain.AdmittedSlot{}, domain.ErrReservationNotReserved)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewReconnectRoomUseCase(peerFactory, registry, reservation, testReconnectTTL, logger, nil)
	_, err := uc.ReconnectRoom(context.Background(), mocks.NewMockConnection(ctrl), newReconnectClaims(roomID, peerID))
	if !errors.Is(err, domain.ErrReservationNotReserved) {
		t.Fatalf("err = %v, want ErrReservationNotReserved", err)
	}
}
