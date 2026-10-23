package room_test

import (
	"context"
	"errors"
	"sync"
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

	uc := useroom.NewSweepOrphanAdmittedUseCase(registry, reservation, 30*time.Second, logger, nil)
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

	uc := useroom.NewSweepOrphanAdmittedUseCase(registry, reservation, 0, logger, nil)
	revoked, err := uc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if revoked != 0 {
		t.Fatalf("revoked = %d, want 0", revoked)
	}
}

func TestSweepOrphanAdmitted_BadRoomSkipped(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	badRoom := mocks.NewMockRoom(ctrl)
	okRoom := mocks.NewMockRoom(ctrl)

	const (
		badRoomID = domain.RoomID(1)
		okRoomID  = domain.RoomID(2)
		peerID    = domain.PeerID(20)
	)

	registry.EXPECT().GetList(gomock.Any()).Return([]realtime.Room{badRoom, okRoom}, nil)
	badRoom.EXPECT().GetID().Return(badRoomID).AnyTimes()
	okRoom.EXPECT().GetID().Return(okRoomID).AnyTimes()

	reservation.EXPECT().ListAdmittedPeers(gomock.Any(), badRoomID).Return(nil, errors.New("reservation store failed"))
	reservation.EXPECT().ListAdmittedPeers(gomock.Any(), okRoomID).Return([]domain.AdmittedSlot{{
		PeerID:     peerID,
		AdmittedAt: time.Now().Add(-time.Hour),
	}}, nil)
	okRoom.EXPECT().HasPeer(peerID).Return(false)
	reservation.EXPECT().Revoke(gomock.Any(), okRoomID, peerID).Return(nil)

	logger.EXPECT().Warn(gomock.Any(), gomock.Any()).AnyTimes()
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewSweepOrphanAdmittedUseCase(registry, reservation, 0, logger, nil)
	revoked, err := uc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if revoked != 1 {
		t.Fatalf("revoked = %d, want 1", revoked)
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

	uc := useroom.NewSweepOrphanAdmittedUseCase(registry, reservation, time.Minute, logger, nil)
	revoked, err := uc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if revoked != 0 {
		t.Fatalf("revoked = %d, want 0", revoked)
	}
}

func TestStartOrphanAdmittedSweep_StopWaitsForRunOnce(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	var once sync.Once
	started := make(chan struct{})
	release := make(chan struct{})
	registry.EXPECT().GetList(gomock.Any()).DoAndReturn(func(context.Context) ([]realtime.Room, error) {
		once.Do(func() { close(started) })
		<-release
		return nil, nil
	}).AnyTimes()

	logger.EXPECT().Warn(gomock.Any(), gomock.Any()).AnyTimes()
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewSweepOrphanAdmittedUseCase(registry, reservation, 0, logger, nil)
	handle := useroom.StartOrphanAdmittedSweep(context.Background(), uc, time.Millisecond, logger)
	if handle == nil {
		t.Fatal("handle = nil, want non-nil")
	}

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("RunOnce not started")
	}

	stopped := make(chan struct{})
	go func() {
		handle.Stop()
		close(stopped)
	}()

	select {
	case <-stopped:
		t.Fatal("Stop returned while RunOnce in flight")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return after RunOnce finished")
	}
}

func TestStartOrphanAdmittedSweep_ZeroInterval_NilHandle(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	uc := useroom.NewSweepOrphanAdmittedUseCase(registry, reservation, 0, logger, nil)
	handle := useroom.StartOrphanAdmittedSweep(context.Background(), uc, 0, logger)
	if handle != nil {
		t.Fatalf("handle = %v, want nil", handle)
	}
	handle.Stop()
}
