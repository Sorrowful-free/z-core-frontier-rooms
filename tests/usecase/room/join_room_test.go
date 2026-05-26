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
	claims := domain.Claims{
		RoomID:    roomID,
		PeerID:    peerID,
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
	peerFactory.EXPECT().CreatePeer(gomock.Any(), peerID, conn, room, logger).Return(peer, nil)
	room.EXPECT().HasPeer(peerID).Return(false)
	room.EXPECT().Join(peer).Return(nil)
	peer.EXPECT().Start().Return(nil)

	room.EXPECT().GetID().Return(roomID).AnyTimes()
	room.EXPECT().GetPeers().Return([]realtime.Peer{peer}).AnyTimes()
	peer.EXPECT().GetID().Return(peerID).AnyTimes()
	peer.EXPECT().GetNickName().Return("").AnyTimes()
	peer.EXPECT().GetPing().Return(int64(0)).AnyTimes()
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
