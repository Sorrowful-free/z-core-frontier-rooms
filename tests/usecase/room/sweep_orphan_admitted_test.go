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

// P31: revoking the last grace slot — this is where "empty" happens once
// the TTL of the last reconnect window expires: no peers, no admitted →
// the room dies (DeleteRoom + UnregisterRoom).
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
	lastSeen := time.Now().Add(-time.Minute)

	registry.EXPECT().GetList(gomock.Any()).Return([]realtime.Room{room}, nil)
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	// First listing — the revoke loop; second — the closeRoomIfEmpty recheck
	// after the revoke (no slots left → kill).
	gomock.InOrder(
		reservation.EXPECT().ListAdmittedPeers(gomock.Any(), roomID).Return([]domain.AdmittedSlot{{
			PeerID:   peerID,
			LastSeen: lastSeen,
		}}, nil),
		reservation.EXPECT().ListAdmittedPeers(gomock.Any(), roomID).Return(nil, nil),
	)
	room.EXPECT().HasPeer(peerID).Return(false)
	logger.EXPECT().Warn(gomock.Any(), gomock.Any()).AnyTimes()
	reservation.EXPECT().Revoke(gomock.Any(), roomID, peerID).Return(nil)

	room.EXPECT().GetPeers().Return(nil)
	registry.EXPECT().DeleteRoom(gomock.Any(), roomID).Return(nil)
	reservation.EXPECT().UnregisterRoom(gomock.Any(), roomID).Return(nil)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewSweepOrphanAdmittedUseCase(registry, reservation, 30*time.Second, logger, nil)
	revoked, err := uc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if revoked != 1 {
		t.Fatalf("revoked = %d, want 1", revoked)
	}
}

// P31: one grace slot is revoked, but the room has another admitted —
// the room does NOT die (someone may still return via grace).
func TestSweepOrphanAdmitted_RevokeLeavesOtherGrace_RoomSurvives(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const (
		roomID    = domain.RoomID(1)
		peerID    = domain.PeerID(10)
		otherPeer = domain.PeerID(11)
	)
	lastSeen := time.Now().Add(-time.Minute)

	registry.EXPECT().GetList(gomock.Any()).Return([]realtime.Room{room}, nil)
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	gomock.InOrder(
		reservation.EXPECT().ListAdmittedPeers(gomock.Any(), roomID).Return([]domain.AdmittedSlot{{
			PeerID:   peerID,
			LastSeen: lastSeen,
		}, {
			PeerID:   otherPeer,
			LastSeen: time.Now(), // live grace — not revoked
		}}, nil),
		// closeRoomIfEmpty rechecks: the second slot is still admitted.
		reservation.EXPECT().ListAdmittedPeers(gomock.Any(), roomID).Return([]domain.AdmittedSlot{{
			PeerID:   otherPeer,
			LastSeen: time.Now(),
		}}, nil),
	)
	room.EXPECT().HasPeer(peerID).Return(false)
	room.EXPECT().HasPeer(otherPeer).Return(false)
	reservation.EXPECT().Revoke(gomock.Any(), roomID, peerID).Return(nil)
	room.EXPECT().GetPeers().Return(nil)
	// DeleteRoom/UnregisterRoom are not expected — gomock fails on a call.
	logger.EXPECT().Warn(gomock.Any(), gomock.Any()).AnyTimes()
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

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
		PeerID:   peerID,
		LastSeen: time.Now().Add(-time.Hour),
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
	gomock.InOrder(
		reservation.EXPECT().ListAdmittedPeers(gomock.Any(), okRoomID).Return([]domain.AdmittedSlot{{
			PeerID:   peerID,
			LastSeen: time.Now().Add(-time.Hour),
		}}, nil),
		// P31: closeRoomIfEmpty after the revoke — the room still has another
		// admitted slot, no kill happens.
		reservation.EXPECT().ListAdmittedPeers(gomock.Any(), okRoomID).Return([]domain.AdmittedSlot{{
			PeerID:   domain.PeerID(21),
			LastSeen: time.Now(),
		}}, nil),
	)
	okRoom.EXPECT().HasPeer(peerID).Return(false)
	reservation.EXPECT().Revoke(gomock.Any(), okRoomID, peerID).Return(nil)
	okRoom.EXPECT().GetPeers().Return(nil)

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
		PeerID:   peerID,
		LastSeen: time.Now(),
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
