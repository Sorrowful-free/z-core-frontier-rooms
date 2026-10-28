package room_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	useroom "github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/mocks"
	"go.uber.org/mock/gomock"
)

func newDeleteUseCase(
	t *testing.T,
	registry *mocks.MockRoomRegistry,
	reservation *mocks.MockReservation,
	logger *mocks.MockLogger,
) *useroom.DeleteUseCase {
	t.Helper()
	return useroom.NewDeleteUseCase(registry, reservation, logger)
}

func TestDelete_Success(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const roomID = domain.RoomID(3)
	gomock.InOrder(
		reservation.EXPECT().VerifyRoomPassword(gomock.Any(), roomID, "room-secret").Return(nil),
		registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil),
	)
	room.EXPECT().GetPeers().Return(nil)
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	registry.EXPECT().DeleteRoom(gomock.Any(), roomID).Return(nil)
	reservation.EXPECT().UnregisterRoom(gomock.Any(), roomID).Return(nil)
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := newDeleteUseCase(t, registry, reservation, logger)
	if err := uc.Delete(context.Background(), roomID, "room-secret"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

// P31: DELETE goes through the same closeRoom path as policy-death — every
// kicked peer gets OpRoomClosed (0x82) before disconnect, Stops are
// parallel, the serial LeaveRoom loop is gone.
func TestDelete_DeliversRoomClosedBeforeStop(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peerA := mocks.NewMockPeer(ctrl)
	peerB := mocks.NewMockPeer(ctrl)

	const (
		roomID  = domain.RoomID(4)
		peerIDA = domain.PeerID(10)
		peerIDB = domain.PeerID(11)
	)

	reservation.EXPECT().VerifyRoomPassword(gomock.Any(), roomID, "").Return(nil)
	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	room.EXPECT().GetPeers().Return([]realtime.Peer{peerA, peerB})
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	peerA.EXPECT().GetID().Return(peerIDA).AnyTimes()
	peerB.EXPECT().GetID().Return(peerIDB).AnyTimes()

	for _, peer := range []*mocks.MockPeer{peerA, peerB} {
		peer.EXPECT().Deliver(gomock.Any()).DoAndReturn(func(ev events.PeerEvent) error {
			if ev.Frame.OpCode != domain.OpRoomClosed {
				t.Fatalf("delivered opcode = %#x, want OpRoomClosed (0x82)", ev.Frame.OpCode)
			}
			if len(ev.Frame.Payload) != 0 {
				t.Fatalf("OpRoomClosed payload = %x, want empty", ev.Frame.Payload)
			}
			return nil
		})
		// Stops are parallel (P30 drain-aware) — the order between peers
		// is not fixed.
		peer.EXPECT().Stop().Return(nil)
	}
	registry.EXPECT().DeleteRoom(gomock.Any(), roomID).Return(nil)
	reservation.EXPECT().UnregisterRoom(gomock.Any(), roomID).Return(nil)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := newDeleteUseCase(t, registry, reservation, logger)
	if err := uc.Delete(context.Background(), roomID, ""); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestDelete_NotFound(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	reservation.EXPECT().
		VerifyRoomPassword(gomock.Any(), domain.RoomID(99), "").
		Return(domain.ErrReservationNotFound)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := newDeleteUseCase(t, registry, reservation, logger)
	err := uc.Delete(context.Background(), domain.RoomID(99), "")
	if !errors.Is(err, domain.ErrReservationNotFound) {
		t.Fatalf("err = %v, want ErrReservationNotFound", err)
	}
}

// P31: ErrRoomNotFound from DeleteRoom is tolerated — a race with a parallel
// room death (closeRoom, empty-death, another DELETE); the goal is achieved,
// the response must not be an error. Fixes a latent post-P29 bug: on a
// no-migration room the serial kick triggered closeRoom and DeleteRoom itself
// leaked ErrRoomNotFound into the HTTP response.
func TestDelete_RoomGoneBeforeDelete_Tolerated(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer := mocks.NewMockPeer(ctrl)

	const (
		roomID = domain.RoomID(6)
		peerID = domain.PeerID(20)
	)

	reservation.EXPECT().VerifyRoomPassword(gomock.Any(), roomID, "").Return(nil)
	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	room.EXPECT().GetPeers().Return([]realtime.Peer{peer})
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	peer.EXPECT().GetID().Return(peerID).AnyTimes()
	peer.EXPECT().Deliver(gomock.Any()).Return(nil)
	registry.EXPECT().DeleteRoom(gomock.Any(), roomID).Return(domain.ErrRoomNotFound)
	reservation.EXPECT().UnregisterRoom(gomock.Any(), roomID).Return(nil)
	peer.EXPECT().Stop().Return(nil)
	logger.EXPECT().Warn(gomock.Any(), gomock.Any()).AnyTimes()
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := newDeleteUseCase(t, registry, reservation, logger)
	if err := uc.Delete(context.Background(), roomID, ""); err != nil {
		t.Fatalf("Delete: %v, want nil (ErrRoomNotFound tolerated)", err)
	}
}

func TestDelete_UnregisterAfterRegistrySuccess(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const roomID = domain.RoomID(5)
	reservation.EXPECT().VerifyRoomPassword(gomock.Any(), roomID, "").Return(nil)
	// The room is deleted from the registry, but UnregisterRoom failed — the
	// "room deleted" contract is fulfilled: a repeated GetRoom confirms the
	// absence → Warn + nil, teardown errors don't turn into 5xx.
	gomock.InOrder(
		registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil),
		registry.EXPECT().DeleteRoom(gomock.Any(), roomID).Return(nil),
		reservation.EXPECT().UnregisterRoom(gomock.Any(), roomID).Return(domain.ErrReservationNotFound),
		registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(nil, domain.ErrRoomNotFound),
	)
	room.EXPECT().GetPeers().Return(nil)
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()
	logger.EXPECT().Warn(gomock.Any(), gomock.Any()).AnyTimes()

	uc := newDeleteUseCase(t, registry, reservation, logger)
	if err := uc.Delete(context.Background(), roomID, ""); err != nil {
		t.Fatalf("err = %v, want nil (комната удалена — teardown-ошибки Warn)", err)
	}
}

func TestDelete_InvalidPassword(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	const roomID = domain.RoomID(8)
	reservation.EXPECT().
		VerifyRoomPassword(gomock.Any(), roomID, "bad").
		Return(domain.ErrInvalidCredentials)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := newDeleteUseCase(t, registry, reservation, logger)
	err := uc.Delete(context.Background(), roomID, "bad")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}
