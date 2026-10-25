package room_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	useroom "github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/mocks"
	"go.uber.org/mock/gomock"
)

func TestDisconnectPeer_Success(t *testing.T) {
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

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	room.EXPECT().GetPeer(peerID).Return(peer, nil)
	reservation.EXPECT().MarkDisconnected(gomock.Any(), roomID, peerID).Return(nil)
	room.EXPECT().Leave(peer).Return(nil)
	peer.EXPECT().Stop().Return(nil)
	room.EXPECT().Send(gomock.Any()).Return(nil)
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewDisconnectPeerUseCase(registry, reservation, logger)
	err := uc.DisconnectPeer(context.Background(), useroom.PeerSession{RoomID: roomID, PeerID: peerID, Peer: peer})
	if err != nil {
		t.Fatalf("DisconnectPeer: %v", err)
	}
}

// P20: MarkDisconnected не взводит Revoke — слот остаётся в grace.
func TestDisconnectPeer_MarkErrorStillLeaves(t *testing.T) {
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

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	room.EXPECT().GetPeer(peerID).Return(peer, nil)
	reservation.EXPECT().MarkDisconnected(gomock.Any(), roomID, peerID).Return(domain.ErrReservationNotReserved)
	room.EXPECT().Leave(peer).Return(nil)
	peer.EXPECT().Stop().Return(nil)
	room.EXPECT().Send(gomock.Any()).Return(nil)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewDisconnectPeerUseCase(registry, reservation, logger)
	if err := uc.DisconnectPeer(context.Background(), useroom.PeerSession{RoomID: roomID, PeerID: peerID, Peer: peer}); err != nil {
		t.Fatalf("DisconnectPeer: %v", err)
	}
}

// T8: stale teardown (старый conn после Replace) — no-op, без Stop/Send.
func TestDisconnectPeer_StaleTeardownNoOp(t *testing.T) {
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

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	room.EXPECT().GetPeer(peerID).Return(peer, nil)
	reservation.EXPECT().MarkDisconnected(gomock.Any(), roomID, peerID).Return(nil)
	room.EXPECT().Leave(peer).Return(domain.ErrPeerStale)
	logger.EXPECT().Debug(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewDisconnectPeerUseCase(registry, reservation, logger)
	if err := uc.DisconnectPeer(context.Background(), useroom.PeerSession{RoomID: roomID, PeerID: peerID, Peer: peer}); err != nil {
		t.Fatalf("DisconnectPeer: %v", err)
	}
}

// P20-fix: teardown со stale session.Peer (старый conn после Replace)
// отклоняется до MarkDisconnected — метка lastSeen и рассылка не должны
// приходить от вытесненного объекта; reservation и остальные peer'ы
// не трогаются вообще.
func TestDisconnectPeer_StaleSessionSkipsMarkAndBroadcast(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		getPeer func(room *mocks.MockRoom, live, stale *mocks.MockPeer, peerID domain.PeerID)
	}{
		{
			name: "peer replaced by another object",
			getPeer: func(room *mocks.MockRoom, live, _ *mocks.MockPeer, peerID domain.PeerID) {
				room.EXPECT().GetPeer(peerID).Return(live, nil)
			},
		},
		{
			name: "peer already gone",
			getPeer: func(room *mocks.MockRoom, _, _ *mocks.MockPeer, peerID domain.PeerID) {
				room.EXPECT().GetPeer(peerID).Return(nil, domain.ErrPeerNotFound)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			registry := mocks.NewMockRoomRegistry(ctrl)
			reservation := mocks.NewMockReservation(ctrl)
			logger := mocks.NewMockLogger(ctrl)
			room := mocks.NewMockRoom(ctrl)
			stale := mocks.NewMockPeer(ctrl)
			live := mocks.NewMockPeer(ctrl)

			const (
				roomID = domain.RoomID(1)
				peerID = domain.PeerID(2)
			)

			registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
			tt.getPeer(room, live, stale, peerID)
			logger.EXPECT().Debug(gomock.Any(), gomock.Any()).AnyTimes()

			uc := useroom.NewDisconnectPeerUseCase(registry, reservation, logger)
			// MarkDisconnected/Leave/Stop/Send не ожидаются — gomock упадёт
			// на любом из них.
			if err := uc.DisconnectPeer(context.Background(), useroom.PeerSession{RoomID: roomID, PeerID: peerID, Peer: stale}); err != nil {
				t.Fatalf("DisconnectPeer: %v", err)
			}
		})
	}
}

func TestDisconnectPeer_PeerAlreadyLeft(t *testing.T) {
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

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	room.EXPECT().GetPeer(peerID).Return(peer, nil)
	reservation.EXPECT().MarkDisconnected(gomock.Any(), roomID, peerID).Return(nil)
	room.EXPECT().Leave(peer).Return(domain.ErrPeerNotFound)
	logger.EXPECT().Warn(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewDisconnectPeerUseCase(registry, reservation, logger)
	if err := uc.DisconnectPeer(context.Background(), useroom.PeerSession{RoomID: roomID, PeerID: peerID, Peer: peer}); err != nil {
		t.Fatalf("DisconnectPeer: %v", err)
	}
}

func TestDisconnectPeer_RoomNotFound(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	peer := mocks.NewMockPeer(ctrl)

	registry.EXPECT().
		GetRoom(gomock.Any(), domain.RoomID(1)).
		Return(nil, domain.ErrRoomNotFound)
	logger.EXPECT().Warn(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewDisconnectPeerUseCase(registry, reservation, logger)
	err := uc.DisconnectPeer(context.Background(), useroom.PeerSession{RoomID: 1, PeerID: 2, Peer: peer})
	if err != nil {
		t.Fatalf("DisconnectPeer: %v", err)
	}
}

func TestDisconnectPeer_LeaveFails_StillStopsAndBroadcasts(t *testing.T) {
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
	leaveErr := errors.New("leave failed")

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	room.EXPECT().GetPeer(peerID).Return(peer, nil)
	reservation.EXPECT().MarkDisconnected(gomock.Any(), roomID, peerID).Return(nil)
	room.EXPECT().Leave(peer).Return(leaveErr)
	peer.EXPECT().Stop().Return(nil)
	room.EXPECT().Send(gomock.Any()).Return(nil)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewDisconnectPeerUseCase(registry, reservation, logger)
	err := uc.DisconnectPeer(context.Background(), useroom.PeerSession{RoomID: roomID, PeerID: peerID, Peer: peer})
	if !errors.Is(err, leaveErr) {
		t.Fatalf("err = %v, want errors.Is leaveErr", err)
	}
}
