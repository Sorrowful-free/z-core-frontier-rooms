package room_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
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
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	// P31: the "is empty" check — the grace slot stays admitted, the room lives on.
	room.EXPECT().GetPeers().Return(nil)
	reservation.EXPECT().ListAdmittedPeers(gomock.Any(), roomID).Return([]domain.AdmittedSlot{{
		PeerID: peerID,
	}}, nil)
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewDisconnectPeerUseCase(registry, reservation, logger)
	err := uc.DisconnectPeer(context.Background(), useroom.PeerSession{RoomID: roomID, PeerID: peerID, Peer: peer})
	if err != nil {
		t.Fatalf("DisconnectPeer: %v", err)
	}
}

// P31: disconnecting the last peer does NOT kill the room — the grace slot
// (MarkDisconnected) keeps it until RESERVATION_RECONNECT_TTL expires,
// otherwise P20 resume would break (nothing to return to).
func TestDisconnectPeer_LastPeerGrace_KeepsRoom(t *testing.T) {
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

	// The room is empty by peers, but the grace slot is admitted → kill is
	// forbidden: DeleteRoom/UnregisterRoom are not expected — gomock fails on a call.
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	room.EXPECT().GetPeers().Return(nil)
	reservation.EXPECT().ListAdmittedPeers(gomock.Any(), roomID).Return([]domain.AdmittedSlot{{
		PeerID:   peerID,
		LastSeen: time.Now(),
	}}, nil)
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewDisconnectPeerUseCase(registry, reservation, logger)
	if err := uc.DisconnectPeer(context.Background(), useroom.PeerSession{RoomID: roomID, PeerID: peerID, Peer: peer}); err != nil {
		t.Fatalf("DisconnectPeer: %v", err)
	}
}

// P20: MarkDisconnected does not trigger Revoke — the slot stays in grace.
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
	// P31: the "is empty" check — the departed peer's slot is still admitted → no-op.
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	room.EXPECT().GetPeers().Return(nil)
	reservation.EXPECT().ListAdmittedPeers(gomock.Any(), roomID).Return([]domain.AdmittedSlot{{
		PeerID: peerID,
	}}, nil)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewDisconnectPeerUseCase(registry, reservation, logger)
	if err := uc.DisconnectPeer(context.Background(), useroom.PeerSession{RoomID: roomID, PeerID: peerID, Peer: peer}); err != nil {
		t.Fatalf("DisconnectPeer: %v", err)
	}
}

// T8: stale teardown (old conn after Replace) — no-op, no Stop/Send.
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

// P20-fix: teardown with a stale session.Peer (old conn after Replace)
// is rejected before MarkDisconnected — the lastSeen mark and the broadcast
// must not come from an evicted object; reservation and the other peers
// are not touched at all.
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
			// MarkDisconnected/Leave/Stop/Send are not expected — gomock fails
			// on any of them.
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

// P33: master disconnect in a no-migration room — the room dies at once:
// grace/OpPeerDisconnected broadcast are skipped, the remaining peers get
// OpRoomClosed and disconnect, the room is unregistered, slots are revoked by
// UnregisterRoom.
func TestDisconnectPeer_RoomShouldClose_ClosesRoom(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	master := mocks.NewMockPeer(ctrl)
	survivor := mocks.NewMockPeer(ctrl)

	const (
		roomID     = domain.RoomID(1)
		masterID   = domain.PeerID(2)
		survivorID = domain.PeerID(3)
	)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	room.EXPECT().GetPeer(masterID).Return(master, nil)
	reservation.EXPECT().MarkDisconnected(gomock.Any(), roomID, masterID).Return(nil)
	room.EXPECT().Leave(master).Return(domain.ErrRoomShouldClose)
	master.EXPECT().Stop().Return(nil)

	room.EXPECT().GetID().Return(roomID).AnyTimes()
	room.EXPECT().GetPeers().Return([]realtime.Peer{survivor})
	survivor.EXPECT().GetID().Return(survivorID).AnyTimes()
	survivor.EXPECT().Deliver(gomock.Any()).DoAndReturn(func(ev events.PeerEvent) error {
		if ev.Frame.OpCode != domain.OpRoomClosed {
			t.Fatalf("delivered opcode = %#x, want OpRoomClosed (0x82)", ev.Frame.OpCode)
		}
		if len(ev.Frame.Payload) != 0 {
			t.Fatalf("OpRoomClosed payload = %x, want empty", ev.Frame.Payload)
		}
		return nil
	})
	survivor.EXPECT().Stop().Return(nil)
	registry.EXPECT().DeleteRoom(gomock.Any(), roomID).Return(nil)
	reservation.EXPECT().UnregisterRoom(gomock.Any(), roomID).Return(nil)
	// room.Send (OpPeerDisconnected) is not expected — gomock fails on a call.
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewDisconnectPeerUseCase(registry, reservation, logger)
	err := uc.DisconnectPeer(context.Background(), useroom.PeerSession{RoomID: roomID, PeerID: masterID, Peer: master})
	if err != nil {
		t.Fatalf("DisconnectPeer: %v", err)
	}
}
