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

const testNickName = "player"

func TestIssueTicket_Success(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	allocator := mocks.NewMockAllocator(ctrl)
	admission := mocks.NewMockAdmission(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const (
		roomID   = domain.RoomID(1)
		peerID   = domain.PeerID(2)
		password = "secret"
	)
	wantToken := []byte("ticket-bytes")
	ttl := time.Hour

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	allocator.EXPECT().AllocatePeerID(gomock.Any()).Return(peerID, nil)
	room.EXPECT().HasPeer(peerID).Return(false)
	reservation.EXPECT().VerifyRoomPassword(gomock.Any(), roomID, password).Return(nil)
	admission.EXPECT().TTL().Return(ttl)
	reservation.EXPECT().
		Reserve(gomock.Any(), roomID, peerID, gomock.Any()).
		Return(nil)
	admission.EXPECT().
		Issue(gomock.Any(), roomID, peerID, testNickName, password).
		Return(wantToken, nil)
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewIssueTicketUseCase(registry, allocator, admission, reservation, logger)
	token, err := uc.IssueTicket(context.Background(), roomID, testNickName, password)
	if err != nil {
		t.Fatalf("IssueTicket: %v", err)
	}
	if string(token) != string(wantToken) {
		t.Fatalf("token = %q, want %q", token, wantToken)
	}
}

func TestIssueTicket_RoomNotFound(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	allocator := mocks.NewMockAllocator(ctrl)
	admission := mocks.NewMockAdmission(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	registry.EXPECT().
		GetRoom(gomock.Any(), domain.RoomID(1)).
		Return(nil, domain.ErrRoomNotFound)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewIssueTicketUseCase(registry, allocator, admission, reservation, logger)
	_, err := uc.IssueTicket(context.Background(), domain.RoomID(1), testNickName, "")
	if !errors.Is(err, domain.ErrRoomNotFound) {
		t.Fatalf("err = %v, want ErrRoomNotFound", err)
	}
}

func TestIssueTicket_AllocatorError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	allocator := mocks.NewMockAllocator(ctrl)
	admission := mocks.NewMockAdmission(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const roomID = domain.RoomID(1)
	allocateErr := errors.New("allocate peer id failed")

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	allocator.EXPECT().
		AllocatePeerID(gomock.Any()).
		Return(domain.PeerIDInvalid, allocateErr)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewIssueTicketUseCase(registry, allocator, admission, reservation, logger)
	_, err := uc.IssueTicket(context.Background(), roomID, testNickName, "")
	if !errors.Is(err, allocateErr) {
		t.Fatalf("err = %v, want allocate error", err)
	}
}

func TestIssueTicket_PeerAlreadyInRoom(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	allocator := mocks.NewMockAllocator(ctrl)
	admission := mocks.NewMockAdmission(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	allocator.EXPECT().AllocatePeerID(gomock.Any()).Return(peerID, nil)
	room.EXPECT().HasPeer(peerID).Return(true)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewIssueTicketUseCase(registry, allocator, admission, reservation, logger)
	_, err := uc.IssueTicket(context.Background(), roomID, testNickName, "")
	if !errors.Is(err, domain.ErrPeerAlreadyInRoom) {
		t.Fatalf("err = %v, want ErrPeerAlreadyInRoom", err)
	}
}

func TestIssueTicket_ReservationFull(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	allocator := mocks.NewMockAllocator(ctrl)
	admission := mocks.NewMockAdmission(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	allocator.EXPECT().AllocatePeerID(gomock.Any()).Return(peerID, nil)
	room.EXPECT().HasPeer(peerID).Return(false)
	reservation.EXPECT().VerifyRoomPassword(gomock.Any(), roomID, "").Return(nil)
	admission.EXPECT().TTL().Return(time.Hour)
	reservation.EXPECT().
		Reserve(gomock.Any(), roomID, peerID, gomock.Any()).
		Return(domain.ErrReservationFull)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewIssueTicketUseCase(registry, allocator, admission, reservation, logger)
	_, err := uc.IssueTicket(context.Background(), roomID, testNickName, "")
	if !errors.Is(err, domain.ErrReservationFull) {
		t.Fatalf("err = %v, want ErrReservationFull", err)
	}
}

func TestIssueTicket_SlotHeld(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	allocator := mocks.NewMockAllocator(ctrl)
	admission := mocks.NewMockAdmission(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	allocator.EXPECT().AllocatePeerID(gomock.Any()).Return(peerID, nil)
	room.EXPECT().HasPeer(peerID).Return(false).Times(2)
	reservation.EXPECT().VerifyRoomPassword(gomock.Any(), roomID, "").Return(nil)
	admission.EXPECT().TTL().Return(time.Hour)
	reservation.EXPECT().
		Reserve(gomock.Any(), roomID, peerID, gomock.Any()).
		Return(domain.ErrReservationAlreadyExists)
	reservation.EXPECT().
		State(gomock.Any(), roomID, peerID).
		Return(domain.ReservationSlotReserved, nil)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewIssueTicketUseCase(registry, allocator, admission, reservation, logger)
	_, err := uc.IssueTicket(context.Background(), roomID, testNickName, "")
	if !errors.Is(err, domain.ErrTicketSlotHeld) {
		t.Fatalf("err = %v, want ErrTicketSlotHeld", err)
	}
	if !errors.Is(err, domain.ErrReservationAlreadyExists) {
		t.Fatalf("err = %v, want wrapped ErrReservationAlreadyExists", err)
	}
}

func TestIssueTicket_OrphanAdmittedCleanup(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	allocator := mocks.NewMockAllocator(ctrl)
	admission := mocks.NewMockAdmission(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const (
		roomID   = domain.RoomID(1)
		peerID   = domain.PeerID(2)
		password = "secret"
	)
	wantToken := []byte("ticket-after-orphan-cleanup")

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	allocator.EXPECT().AllocatePeerID(gomock.Any()).Return(peerID, nil)
	room.EXPECT().HasPeer(peerID).Return(false).Times(2)
	reservation.EXPECT().VerifyRoomPassword(gomock.Any(), roomID, password).Return(nil)
	admission.EXPECT().TTL().Return(time.Hour)
	gomock.InOrder(
		reservation.EXPECT().
			Reserve(gomock.Any(), roomID, peerID, gomock.Any()).
			Return(domain.ErrReservationAlreadyAdmitted),
		reservation.EXPECT().
			State(gomock.Any(), roomID, peerID).
			Return(domain.ReservationSlotAdmitted, nil),
		reservation.EXPECT().Revoke(gomock.Any(), roomID, peerID).Return(nil),
		reservation.EXPECT().
			Reserve(gomock.Any(), roomID, peerID, gomock.Any()).
			Return(nil),
	)
	admission.EXPECT().
		Issue(gomock.Any(), roomID, peerID, testNickName, password).
		Return(wantToken, nil)
	logger.EXPECT().Warn(gomock.Any(), gomock.Any()).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewIssueTicketUseCase(registry, allocator, admission, reservation, logger)
	token, err := uc.IssueTicket(context.Background(), roomID, testNickName, password)
	if err != nil {
		t.Fatalf("IssueTicket: %v", err)
	}
	if string(token) != string(wantToken) {
		t.Fatalf("token = %q, want %q", token, wantToken)
	}
}

func TestIssueTicket_AdmissionErrorJoinsRevokeError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	allocator := mocks.NewMockAllocator(ctrl)
	admission := mocks.NewMockAdmission(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	allocator.EXPECT().AllocatePeerID(gomock.Any()).Return(peerID, nil)
	room.EXPECT().HasPeer(peerID).Return(false)
	reservation.EXPECT().VerifyRoomPassword(gomock.Any(), roomID, "").Return(nil)
	admission.EXPECT().TTL().Return(time.Hour)
	reservation.EXPECT().Reserve(gomock.Any(), roomID, peerID, gomock.Any()).Return(nil)
	admission.EXPECT().Issue(gomock.Any(), roomID, peerID, gomock.Any(), gomock.Any()).Return(nil, domain.ErrInvalidCredentials)
	reservation.EXPECT().Revoke(gomock.Any(), roomID, peerID).Return(domain.ErrReservationNotFound)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).MinTimes(1)

	uc := useroom.NewIssueTicketUseCase(registry, allocator, admission, reservation, logger)
	_, err := uc.IssueTicket(context.Background(), roomID, testNickName, "")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
	if !errors.Is(err, domain.ErrReservationNotFound) {
		t.Fatalf("err = %v, want ErrReservationNotFound joined", err)
	}
}

func TestIssueTicket_AdmissionErrorRevokesReserve(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	allocator := mocks.NewMockAllocator(ctrl)
	admission := mocks.NewMockAdmission(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	allocator.EXPECT().AllocatePeerID(gomock.Any()).Return(peerID, nil)
	room.EXPECT().HasPeer(peerID).Return(false)
	reservation.EXPECT().VerifyRoomPassword(gomock.Any(), roomID, "").Return(nil)
	admission.EXPECT().TTL().Return(time.Hour)
	reservation.EXPECT().Reserve(gomock.Any(), roomID, peerID, gomock.Any()).Return(nil)
	admission.EXPECT().Issue(gomock.Any(), roomID, peerID, gomock.Any(), gomock.Any()).Return(nil, domain.ErrInvalidCredentials)
	reservation.EXPECT().Revoke(gomock.Any(), roomID, peerID).Return(nil)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).MinTimes(1)

	uc := useroom.NewIssueTicketUseCase(registry, allocator, admission, reservation, logger)
	_, err := uc.IssueTicket(context.Background(), roomID, testNickName, "")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}

func TestIssueTicket_InvalidRoomPassword(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	allocator := mocks.NewMockAllocator(ctrl)
	admission := mocks.NewMockAdmission(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	allocator.EXPECT().AllocatePeerID(gomock.Any()).Return(peerID, nil)
	room.EXPECT().HasPeer(peerID).Return(false)
	reservation.EXPECT().
		VerifyRoomPassword(gomock.Any(), roomID, "wrong").
		Return(domain.ErrInvalidCredentials)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewIssueTicketUseCase(registry, allocator, admission, reservation, logger)
	_, err := uc.IssueTicket(context.Background(), roomID, testNickName, "wrong")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}

func TestIssueTicket_CancelledContext(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	allocator := mocks.NewMockAllocator(ctrl)
	admission := mocks.NewMockAdmission(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	uc := useroom.NewIssueTicketUseCase(registry, allocator, admission, reservation, logger)
	_, err := uc.IssueTicket(ctx, domain.RoomID(1), testNickName, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
