package room_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	useroom "github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/mocks"
	"go.uber.org/mock/gomock"
)

func TestJoinRoom_ValidateError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)

	admission := mocks.NewMockAdmission(ctrl)
	registry := mocks.NewMockRoomRegistry(ctrl)
	peerFactory := mocks.NewMockPeerFactory(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	admission.EXPECT().
		Validate(gomock.Any(), gomock.Any()).
		Return(domain.Claims{}, domain.ErrInvalidToken)

	uc := useroom.NewJoinRoomUseCase(admission, peerFactory, registry, reservation, logger)
	_, peerID, err := uc.JoinRoom(context.Background(), mocks.NewMockConnection(ctrl), []byte("token"))
	if !errors.Is(err, domain.ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
	if peerID.IsValid() {
		t.Fatalf("peerID = %v, want invalid", peerID)
	}
}

func TestJoinRoom_Success(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)
	now := time.Now()
	const nickName = "player"
	claims := domain.Claims{
		RoomID:    roomID,
		PeerID:    peerID,
		NickName:  nickName,
		IssuedAt:  now,
		ExpiresAt: now.Add(time.Hour),
	}
	token := []byte("ticket")

	admission := mocks.NewMockAdmission(ctrl)
	registry := mocks.NewMockRoomRegistry(ctrl)
	peerFactory := mocks.NewMockPeerFactory(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer := mocks.NewMockPeer(ctrl)
	conn := mocks.NewMockConnection(ctrl)

	admission.EXPECT().Validate(gomock.Any(), token).Return(claims, nil)
	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().Admit(gomock.Any(), roomID, peerID).Return(nil)
	room.EXPECT().Context().Return(context.Background()).AnyTimes()
	peerFactory.EXPECT().CreatePeer(gomock.Any(), peerID, nickName, conn, room, logger).Return(peer, nil)
	room.EXPECT().HasPeer(peerID).Return(false)
	room.EXPECT().Join(peer).Return(nil)
	peer.EXPECT().Start().Return(nil)

	room.EXPECT().GetID().Return(roomID).AnyTimes()
	room.EXPECT().GetPeers().Return([]realtime.Peer{peer}).AnyTimes()
	room.EXPECT().GetAttributes().Return(nil).AnyTimes()
	peer.EXPECT().GetID().Return(peerID).AnyTimes()
	peer.EXPECT().GetNickName().Return(nickName).AnyTimes()
	peer.EXPECT().Ping().Return(int64(0)).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewJoinRoomUseCase(admission, peerFactory, registry, reservation, logger)
	summary, gotPeerID, err := uc.JoinRoom(context.Background(), conn, token)
	if err != nil {
		t.Fatalf("JoinRoom: %v", err)
	}
	if gotPeerID != peerID {
		t.Fatalf("peerID = %v, want %v", gotPeerID, peerID)
	}
	if summary.ID != roomID {
		t.Fatalf("summary.ID = %v, want %v", summary.ID, roomID)
	}
}

func TestJoinRoom_AdmitFailure(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)
	now := time.Now()
	claims := domain.Claims{
		RoomID:    roomID,
		PeerID:    peerID,
		IssuedAt:  now,
		ExpiresAt: now.Add(time.Hour),
	}

	admission := mocks.NewMockAdmission(ctrl)
	registry := mocks.NewMockRoomRegistry(ctrl)
	peerFactory := mocks.NewMockPeerFactory(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	admission.EXPECT().Validate(gomock.Any(), gomock.Any()).Return(claims, nil)
	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().Admit(gomock.Any(), roomID, peerID).Return(domain.ErrReservationNotReserved)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewJoinRoomUseCase(admission, peerFactory, registry, reservation, logger)
	_, _, err := uc.JoinRoom(context.Background(), mocks.NewMockConnection(ctrl), []byte("t"))
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
	now := time.Now()
	claims := domain.Claims{
		RoomID:    roomID,
		PeerID:    peerID,
		IssuedAt:  now,
		ExpiresAt: now.Add(time.Hour),
	}
	createErr := errors.New("create peer failed")

	admission := mocks.NewMockAdmission(ctrl)
	registry := mocks.NewMockRoomRegistry(ctrl)
	peerFactory := mocks.NewMockPeerFactory(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	conn := mocks.NewMockConnection(ctrl)

	admission.EXPECT().Validate(gomock.Any(), gomock.Any()).Return(claims, nil)
	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().Admit(gomock.Any(), roomID, peerID).Return(nil)
	room.EXPECT().Context().Return(context.Background())
	peerFactory.EXPECT().CreatePeer(gomock.Any(), peerID, gomock.Any(), conn, room, logger).Return(nil, createErr)
	room.EXPECT().HasPeer(peerID).Return(false)
	reservation.EXPECT().Revoke(gomock.Any(), roomID, peerID).Return(nil)

	uc := useroom.NewJoinRoomUseCase(admission, peerFactory, registry, reservation, logger)
	_, _, err := uc.JoinRoom(context.Background(), conn, []byte("t"))
	if !errors.Is(err, createErr) {
		t.Fatalf("err = %v, want createErr", err)
	}
}

func TestJoinRoom_ReplaceFailureDoesNotRevoke(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)
	now := time.Now()
	claims := domain.Claims{
		RoomID:    roomID,
		PeerID:    peerID,
		IssuedAt:  now,
		ExpiresAt: now.Add(time.Hour),
	}
	replaceErr := domain.ErrReplaceFailed

	admission := mocks.NewMockAdmission(ctrl)
	registry := mocks.NewMockRoomRegistry(ctrl)
	peerFactory := mocks.NewMockPeerFactory(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer := mocks.NewMockPeer(ctrl)
	conn := mocks.NewMockConnection(ctrl)

	admission.EXPECT().Validate(gomock.Any(), gomock.Any()).Return(claims, nil)
	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().Admit(gomock.Any(), roomID, peerID).Return(nil)
	room.EXPECT().Context().Return(context.Background())
	peerFactory.EXPECT().CreatePeer(gomock.Any(), peerID, gomock.Any(), conn, room, logger).Return(peer, nil)
	room.EXPECT().HasPeer(peerID).Return(true).Times(2)
	room.EXPECT().Replace(peer).Return(replaceErr)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewJoinRoomUseCase(admission, peerFactory, registry, reservation, logger)
	_, _, err := uc.JoinRoom(context.Background(), conn, []byte("t"))
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
	now := time.Now()
	claims := domain.Claims{
		RoomID:    roomID,
		PeerID:    peerID,
		IssuedAt:  now,
		ExpiresAt: now.Add(time.Hour),
	}
	startErr := errors.New("start failed")

	admission := mocks.NewMockAdmission(ctrl)
	registry := mocks.NewMockRoomRegistry(ctrl)
	peerFactory := mocks.NewMockPeerFactory(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer := mocks.NewMockPeer(ctrl)
	conn := mocks.NewMockConnection(ctrl)

	admission.EXPECT().Validate(gomock.Any(), gomock.Any()).Return(claims, nil)
	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().Admit(gomock.Any(), roomID, peerID).Return(nil)
	room.EXPECT().Context().Return(context.Background())
	peerFactory.EXPECT().CreatePeer(gomock.Any(), peerID, gomock.Any(), conn, room, logger).Return(peer, nil)
	room.EXPECT().HasPeer(peerID).Return(false).Times(2)
	room.EXPECT().Join(peer).Return(nil)
	peer.EXPECT().Start().Return(startErr)
	room.EXPECT().Leave(peer).Return(nil)
	peer.EXPECT().Stop().Return(nil)
	reservation.EXPECT().Revoke(gomock.Any(), roomID, peerID).Return(nil)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewJoinRoomUseCase(admission, peerFactory, registry, reservation, logger)
	_, _, err := uc.JoinRoom(context.Background(), conn, []byte("t"))
	if !errors.Is(err, domain.ErrPeerStartFailed) {
		t.Fatalf("err = %v, want ErrPeerStartFailed", err)
	}
}
