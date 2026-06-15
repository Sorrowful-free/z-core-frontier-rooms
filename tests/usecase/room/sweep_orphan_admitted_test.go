package room_test

import (
	"context"
	"testing"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	useroom "github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/mocks"
	"go.uber.org/mock/gomock"
)

func TestSweepOrphanAdmitted_RevokesOrphanAfterTTL(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(10)
	)
	admittedAt := time.Now().Add(-time.Minute)

	registry.EXPECT().GetList(gomock.Any()).Return([]realtime.Room{room}, nil)
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	reservation.EXPECT().ListAdmittedPeers(gomock.Any(), roomID).Return([]domain.AdmittedSlot{{
		PeerID:     peerID,
		AdmittedAt: admittedAt,
	}}, nil)
	room.EXPECT().HasPeer(peerID).Return(false)
	logger.EXPECT().Warn(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
	reservation.EXPECT().Revoke(gomock.Any(), roomID, peerID).Return(nil)

	uc := useroom.NewSweepOrphanAdmittedUseCase(registry, reservation, 30*time.Second, logger)
	revoked, err := uc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if revoked != 1 {
		t.Fatalf("revoked = %d, want 1", revoked)
	}
}

func TestSweepOrphanAdmitted_SkipsWhenPeerInRoom(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const (
		roomID = domain.RoomID(2)
		peerID = domain.PeerID(20)
	)

	registry.EXPECT().GetList(gomock.Any()).Return([]realtime.Room{room}, nil)
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	reservation.EXPECT().ListAdmittedPeers(gomock.Any(), roomID).Return([]domain.AdmittedSlot{{
		PeerID:     peerID,
		AdmittedAt: time.Now().Add(-time.Hour),
	}}, nil)
	room.EXPECT().HasPeer(peerID).Return(true)

	uc := useroom.NewSweepOrphanAdmittedUseCase(registry, reservation, 0, logger)
	revoked, err := uc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if revoked != 0 {
		t.Fatalf("revoked = %d, want 0", revoked)
	}
}

func TestSweepOrphanAdmitted_SkipsBeforeTTL(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const (
		roomID = domain.RoomID(3)
		peerID = domain.PeerID(30)
	)

	registry.EXPECT().GetList(gomock.Any()).Return([]realtime.Room{room}, nil)
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	reservation.EXPECT().ListAdmittedPeers(gomock.Any(), roomID).Return([]domain.AdmittedSlot{{
		PeerID:     peerID,
		AdmittedAt: time.Now(),
	}}, nil)
	room.EXPECT().HasPeer(peerID).Return(false)

	uc := useroom.NewSweepOrphanAdmittedUseCase(registry, reservation, time.Minute, logger)
	revoked, err := uc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if revoked != 0 {
		t.Fatalf("revoked = %d, want 0", revoked)
	}
}
