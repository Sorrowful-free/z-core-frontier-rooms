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

func TestEvictIdlePeers_DeadPeerEvicted(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer := mocks.NewMockPeer(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)

	registry.EXPECT().GetList(gomock.Any()).Return([]realtime.Room{room}, nil)
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	room.EXPECT().GetPeers().Return([]realtime.Peer{peer}).AnyTimes()
	peer.EXPECT().GetID().Return(peerID).AnyTimes()
	peer.EXPECT().IsAlive().Return(false)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	room.EXPECT().GetPeer(peerID).Return(peer, nil)
	reservation.EXPECT().MarkDisconnected(gomock.Any(), roomID, peerID).Return(nil)
	room.EXPECT().Leave(peer).Return(nil)
	peer.EXPECT().Stop().Return(nil)
	room.EXPECT().Send(gomock.Any()).Return(nil)

	logger.EXPECT().Warn(gomock.Any(), gomock.Any()).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	disconnectUC := useroom.NewDisconnectPeerUseCase(registry, reservation, logger)
	uc := useroom.NewEvictIdlePeersUseCase(registry, disconnectUC, logger, nil)
	evicted, err := uc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if evicted != 1 {
		t.Fatalf("evicted = %d, want 1", evicted)
	}
}

func TestEvictIdlePeers_AlivePeerSkipped(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer := mocks.NewMockPeer(ctrl)

	const (
		roomID = domain.RoomID(1)
	)

	registry.EXPECT().GetList(gomock.Any()).Return([]realtime.Room{room}, nil)
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	room.EXPECT().GetPeers().Return([]realtime.Peer{peer}).AnyTimes()
	peer.EXPECT().IsAlive().Return(true)

	disconnectUC := useroom.NewDisconnectPeerUseCase(registry, reservation, logger)
	uc := useroom.NewEvictIdlePeersUseCase(registry, disconnectUC, logger, nil)
	evicted, err := uc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if evicted != 0 {
		t.Fatalf("evicted = %d, want 0", evicted)
	}
}

func TestEvictIdlePeers_LeaveErrorTolerated(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer1 := mocks.NewMockPeer(ctrl)
	peer2 := mocks.NewMockPeer(ctrl)

	const (
		roomID  = domain.RoomID(1)
		peer1ID = domain.PeerID(2)
		peer2ID = domain.PeerID(3)
	)
	leaveErr := errors.New("room leave failed")

	registry.EXPECT().GetList(gomock.Any()).Return([]realtime.Room{room}, nil)
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	room.EXPECT().GetPeers().Return([]realtime.Peer{peer1, peer2}).AnyTimes()
	peer1.EXPECT().GetID().Return(peer1ID).AnyTimes()
	peer1.EXPECT().IsAlive().Return(false)
	peer2.EXPECT().GetID().Return(peer2ID).AnyTimes()
	peer2.EXPECT().IsAlive().Return(false)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil).Times(2)

	room.EXPECT().GetPeer(peer1ID).Return(peer1, nil)
	reservation.EXPECT().MarkDisconnected(gomock.Any(), roomID, peer1ID).Return(nil)
	room.EXPECT().Leave(peer1).Return(leaveErr)
	// Leave removes the peer even on error — Stop and broadcast still run.
	peer1.EXPECT().Stop().Return(nil)
	room.EXPECT().Send(gomock.Any()).Return(nil)

	room.EXPECT().GetPeer(peer2ID).Return(peer2, nil)
	reservation.EXPECT().MarkDisconnected(gomock.Any(), roomID, peer2ID).Return(nil)
	room.EXPECT().Leave(peer2).Return(nil)
	peer2.EXPECT().Stop().Return(nil)
	room.EXPECT().Send(gomock.Any()).Return(nil)

	logger.EXPECT().Warn(gomock.Any(), gomock.Any()).AnyTimes()
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	disconnectUC := useroom.NewDisconnectPeerUseCase(registry, reservation, logger)
	uc := useroom.NewEvictIdlePeersUseCase(registry, disconnectUC, logger, nil)
	evicted, err := uc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if evicted != 1 {
		t.Fatalf("evicted = %d, want 1", evicted)
	}
}

func TestStartIdlePeerSweep_StopWaitsForRunOnce(t *testing.T) {
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

	disconnectUC := useroom.NewDisconnectPeerUseCase(registry, reservation, logger)
	uc := useroom.NewEvictIdlePeersUseCase(registry, disconnectUC, logger, nil)
	handle := useroom.StartIdlePeerSweep(context.Background(), uc, time.Millisecond, logger)
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

// Regression (P31 review): an unconditional closeRoomIfEmpty on every room
// killed a never-joined room (peers==0, a live reserve ticket) on the very
// first sweep pass. An empty room without evictions must not be touched —
// its kill is handled by SweepEmptyRooms by slots, not by the idle sweep.
func TestEvictIdlePeers_NeverJoinedRoomNotKilled(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const roomID = domain.RoomID(1)

	registry.EXPECT().GetList(gomock.Any()).Return([]realtime.Room{room}, nil)
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	room.EXPECT().GetPeers().Return(nil).AnyTimes()
	// No DeleteRoom/UnregisterRoom/Delete calls are expected — gomock
	// fails on any of them (strict default).

	disconnectUC := useroom.NewDisconnectPeerUseCase(registry, reservation, logger)
	uc := useroom.NewEvictIdlePeersUseCase(registry, disconnectUC, logger, nil)
	evicted, err := uc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if evicted != 0 {
		t.Fatalf("evicted = %d, want 0", evicted)
	}
}
