package room_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	useroom "github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/mocks"
	"go.uber.org/mock/gomock"
)

// newTestClaims собирает валидные join-claims (Kind=join, IssuedAt = now,
// ExpiresAt = +1h); nickName == "" — claims без ника.
func newTestClaims(roomID domain.RoomID, peerID domain.PeerID, nickName string) domain.Claims {
	now := time.Now()
	return domain.Claims{
		Kind:      domain.ClaimsKindJoin,
		RoomID:    roomID,
		PeerID:    peerID,
		NickName:  nickName,
		IssuedAt:  now,
		ExpiresAt: now.Add(time.Hour),
	}
}

func TestJoinRoom_GetRoomNotFound(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)
	claims := newTestClaims(roomID, peerID, "")

	registry := mocks.NewMockRoomRegistry(ctrl)
	peerFactory := mocks.NewMockPeerFactory(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(nil, domain.ErrRoomNotFound)

	uc := useroom.NewJoinRoomUseCase(peerFactory, registry, reservation, logger)
	session, err := uc.JoinRoom(context.Background(), mocks.NewMockConnection(ctrl), claims)
	if !errors.Is(err, domain.ErrRoomNotFound) {
		t.Fatalf("err = %v, want ErrRoomNotFound", err)
	}
	if session.PeerID.IsValid() {
		t.Fatalf("session.PeerID = %v, want invalid", session.PeerID)
	}
}

func TestJoinRoom_Success(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)
	const nickName = "player"
	claims := newTestClaims(roomID, peerID, nickName)

	registry := mocks.NewMockRoomRegistry(ctrl)
	peerFactory := mocks.NewMockPeerFactory(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer := mocks.NewMockPeer(ctrl)
	conn := mocks.NewMockConnection(ctrl)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().Admit(gomock.Any(), roomID, peerID).Return(nil)
	room.EXPECT().Context().Return(context.Background()).AnyTimes()
	peerFactory.EXPECT().CreatePeer(gomock.Any(), peerID, nickName, conn, logger).Return(peer, nil)
	room.EXPECT().HasPeer(peerID).Return(false)
	room.EXPECT().Join(peer).Return(nil)
	peer.EXPECT().Start(room).Return(nil)

	room.EXPECT().GetID().Return(roomID).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewJoinRoomUseCase(peerFactory, registry, reservation, logger)
	session, err := uc.JoinRoom(context.Background(), conn, claims)
	if err != nil {
		t.Fatalf("JoinRoom: %v", err)
	}
	if session.PeerID != peerID {
		t.Fatalf("session.PeerID = %v, want %v", session.PeerID, peerID)
	}
	if session.RoomID != roomID {
		t.Fatalf("session.RoomID = %v, want %v", session.RoomID, roomID)
	}
	if session.Peer != peer {
		t.Fatal("session.Peer must be the created peer")
	}
}

func TestJoinRoom_AdmitFailure(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)
	claims := newTestClaims(roomID, peerID, "")

	registry := mocks.NewMockRoomRegistry(ctrl)
	peerFactory := mocks.NewMockPeerFactory(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().Admit(gomock.Any(), roomID, peerID).Return(domain.ErrReservationNotReserved)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewJoinRoomUseCase(peerFactory, registry, reservation, logger)
	_, err := uc.JoinRoom(context.Background(), mocks.NewMockConnection(ctrl), claims)
	if !errors.Is(err, domain.ErrReservationNotReserved) {
		t.Fatalf("err = %v, want ErrReservationNotReserved", err)
	}
}

func TestJoinRoom_CreatePeerFailureRevokesReservation(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)
	claims := newTestClaims(roomID, peerID, "")
	createErr := errors.New("create peer failed")

	registry := mocks.NewMockRoomRegistry(ctrl)
	peerFactory := mocks.NewMockPeerFactory(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	conn := mocks.NewMockConnection(ctrl)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().Admit(gomock.Any(), roomID, peerID).Return(nil)
	room.EXPECT().Context().Return(context.Background())
	peerFactory.EXPECT().CreatePeer(gomock.Any(), peerID, gomock.Any(), conn, logger).Return(nil, createErr)
	room.EXPECT().HasPeer(peerID).Return(false)
	reservation.EXPECT().Revoke(gomock.Any(), roomID, peerID).Return(nil)

	uc := useroom.NewJoinRoomUseCase(peerFactory, registry, reservation, logger)
	_, err := uc.JoinRoom(context.Background(), conn, claims)
	if !errors.Is(err, createErr) {
		t.Fatalf("err = %v, want createErr", err)
	}
}

func TestJoinRoom_RevokeFailureJoinsErrors(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)
	claims := newTestClaims(roomID, peerID, "")
	createErr := errors.New("create peer failed")
	revokeErr := errors.New("revoke reservation failed")

	registry := mocks.NewMockRoomRegistry(ctrl)
	peerFactory := mocks.NewMockPeerFactory(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	conn := mocks.NewMockConnection(ctrl)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().Admit(gomock.Any(), roomID, peerID).Return(nil)
	room.EXPECT().Context().Return(context.Background())
	peerFactory.EXPECT().CreatePeer(gomock.Any(), peerID, gomock.Any(), conn, logger).Return(nil, createErr)
	room.EXPECT().HasPeer(peerID).Return(false)
	reservation.EXPECT().Revoke(gomock.Any(), roomID, peerID).Return(revokeErr)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewJoinRoomUseCase(peerFactory, registry, reservation, logger)
	_, err := uc.JoinRoom(context.Background(), conn, claims)
	if !errors.Is(err, createErr) {
		t.Fatalf("err = %v, want errors.Is createErr", err)
	}
	if !errors.Is(err, revokeErr) {
		t.Fatalf("err = %v, want errors.Is revokeErr", err)
	}
}

func TestJoinRoom_ReplaceFailureDoesNotRevoke(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)
	claims := newTestClaims(roomID, peerID, "")
	replaceErr := domain.ErrReplaceFailed

	registry := mocks.NewMockRoomRegistry(ctrl)
	peerFactory := mocks.NewMockPeerFactory(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer := mocks.NewMockPeer(ctrl)
	conn := mocks.NewMockConnection(ctrl)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().Admit(gomock.Any(), roomID, peerID).Return(nil)
	room.EXPECT().Context().Return(context.Background())
	peerFactory.EXPECT().CreatePeer(gomock.Any(), peerID, gomock.Any(), conn, logger).Return(peer, nil)
	room.EXPECT().HasPeer(peerID).Return(true).Times(2)
	room.EXPECT().Replace(peer).Return(replaceErr)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewJoinRoomUseCase(peerFactory, registry, reservation, logger)
	_, err := uc.JoinRoom(context.Background(), conn, claims)
	if !errors.Is(err, replaceErr) {
		t.Fatalf("err = %v, want replaceErr", err)
	}
}

func TestJoinRoom_StartFailureRevokesWhenPeerNotInRoom(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)
	claims := newTestClaims(roomID, peerID, "")
	startErr := errors.New("start failed")

	registry := mocks.NewMockRoomRegistry(ctrl)
	peerFactory := mocks.NewMockPeerFactory(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer := mocks.NewMockPeer(ctrl)
	conn := mocks.NewMockConnection(ctrl)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().Admit(gomock.Any(), roomID, peerID).Return(nil)
	room.EXPECT().Context().Return(context.Background())
	peerFactory.EXPECT().CreatePeer(gomock.Any(), peerID, gomock.Any(), conn, logger).Return(peer, nil)
	room.EXPECT().HasPeer(peerID).Return(false).Times(2)
	room.EXPECT().Join(peer).Return(nil)
	peer.EXPECT().Start(room).Return(startErr)
	room.EXPECT().Leave(peer).Return(nil)
	peer.EXPECT().Stop().Return(nil)
	reservation.EXPECT().Revoke(gomock.Any(), roomID, peerID).Return(nil)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewJoinRoomUseCase(peerFactory, registry, reservation, logger)
	_, err := uc.JoinRoom(context.Background(), conn, claims)
	if !errors.Is(err, domain.ErrPeerStartFailed) {
		t.Fatalf("err = %v, want ErrPeerStartFailed", err)
	}
}

func TestJoinRoom_StartFailure_RollbackErrorsStillReturnStartErr(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)
	claims := newTestClaims(roomID, peerID, "")
	startErr := errors.New("start failed")
	leaveErr := errors.New("rollback leave failed")
	stopErr := errors.New("rollback stop failed")

	registry := mocks.NewMockRoomRegistry(ctrl)
	peerFactory := mocks.NewMockPeerFactory(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer := mocks.NewMockPeer(ctrl)
	conn := mocks.NewMockConnection(ctrl)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().Admit(gomock.Any(), roomID, peerID).Return(nil)
	room.EXPECT().Context().Return(context.Background())
	peerFactory.EXPECT().CreatePeer(gomock.Any(), peerID, gomock.Any(), conn, logger).Return(peer, nil)
	room.EXPECT().HasPeer(peerID).Return(false).Times(2)
	room.EXPECT().Join(peer).Return(nil)
	peer.EXPECT().Start(room).Return(startErr)
	room.EXPECT().Leave(peer).Return(leaveErr)
	peer.EXPECT().Stop().Return(stopErr)
	reservation.EXPECT().Revoke(gomock.Any(), roomID, peerID).Return(nil)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewJoinRoomUseCase(peerFactory, registry, reservation, logger)
	_, err := uc.JoinRoom(context.Background(), conn, claims)
	if !errors.Is(err, domain.ErrPeerStartFailed) {
		t.Fatalf("err = %v, want ErrPeerStartFailed", err)
	}
}
