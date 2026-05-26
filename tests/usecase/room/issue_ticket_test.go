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

func TestIssueTicket_Success(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
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
	room.EXPECT().HasPeer(peerID).Return(false)
	admission.EXPECT().TTL().Return(ttl)
	reservation.EXPECT().
		Reserve(gomock.Any(), roomID, peerID, gomock.Any()).
		Return(nil)
	admission.EXPECT().
		Issue(gomock.Any(), roomID, peerID, password).
		Return(wantToken, nil)
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewIssueTicketUseCase(registry, admission, reservation, logger)
	token, err := uc.IssueTicket(context.Background(), roomID, peerID, password)
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
	admission := mocks.NewMockAdmission(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	registry.EXPECT().
		GetRoom(gomock.Any(), domain.RoomID(1)).
		Return(nil, domain.ErrRoomNotFound)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewIssueTicketUseCase(registry, admission, reservation, logger)
	_, err := uc.IssueTicket(context.Background(), domain.RoomID(1), domain.PeerID(1), "")
	if !errors.Is(err, domain.ErrRoomNotFound) {
		t.Fatalf("err = %v, want ErrRoomNotFound", err)
	}
}

func TestIssueTicket_PeerAlreadyInRoom(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	admission := mocks.NewMockAdmission(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	room.EXPECT().HasPeer(peerID).Return(true)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewIssueTicketUseCase(registry, admission, reservation, logger)
	_, err := uc.IssueTicket(context.Background(), roomID, peerID, "")
	if !errors.Is(err, domain.ErrPeerAlreadyInRoom) {
		t.Fatalf("err = %v, want ErrPeerAlreadyInRoom", err)
	}
}

func TestIssueTicket_ReservationFull(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	admission := mocks.NewMockAdmission(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	room.EXPECT().HasPeer(peerID).Return(false)
	admission.EXPECT().TTL().Return(time.Hour)
	reservation.EXPECT().
		Reserve(gomock.Any(), roomID, peerID, gomock.Any()).
		Return(domain.ErrReservationFull)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewIssueTicketUseCase(registry, admission, reservation, logger)
	_, err := uc.IssueTicket(context.Background(), roomID, peerID, "")
	if !errors.Is(err, domain.ErrReservationFull) {
		t.Fatalf("err = %v, want ErrReservationFull", err)
	}
}

func TestIssueTicket_SlotHeld(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	admission := mocks.NewMockAdmission(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	room.EXPECT().HasPeer(peerID).Return(false)
	admission.EXPECT().TTL().Return(time.Hour)
	reservation.EXPECT().
		Reserve(gomock.Any(), roomID, peerID, gomock.Any()).
		Return(domain.ErrReservationAlreadyExists)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewIssueTicketUseCase(registry, admission, reservation, logger)
	_, err := uc.IssueTicket(context.Background(), roomID, peerID, "")
	if !errors.Is(err, domain.ErrTicketSlotHeld) {
		t.Fatalf("err = %v, want ErrTicketSlotHeld", err)
	}
	if !errors.Is(err, domain.ErrReservationAlreadyExists) {
		t.Fatalf("err = %v, want wrapped ErrReservationAlreadyExists", err)
	}
}

func TestIssueTicket_AdmissionErrorJoinsRevokeError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	admission := mocks.NewMockAdmission(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	room.EXPECT().HasPeer(peerID).Return(false)
	admission.EXPECT().TTL().Return(time.Hour)
	reservation.EXPECT().Reserve(gomock.Any(), roomID, peerID, gomock.Any()).Return(nil)
	admission.EXPECT().Issue(gomock.Any(), roomID, peerID, gomock.Any()).Return(nil, domain.ErrInvalidCredentials)
	reservation.EXPECT().Revoke(gomock.Any(), roomID, peerID).Return(domain.ErrReservationNotFound)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).MinTimes(1)

	uc := useroom.NewIssueTicketUseCase(registry, admission, reservation, logger)
	_, err := uc.IssueTicket(context.Background(), roomID, peerID, "")
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
	admission := mocks.NewMockAdmission(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	room.EXPECT().HasPeer(peerID).Return(false)
	admission.EXPECT().TTL().Return(time.Hour)
	reservation.EXPECT().Reserve(gomock.Any(), roomID, peerID, gomock.Any()).Return(nil)
	admission.EXPECT().Issue(gomock.Any(), roomID, peerID, gomock.Any()).Return(nil, domain.ErrInvalidCredentials)
	reservation.EXPECT().Revoke(gomock.Any(), roomID, peerID).Return(nil)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).MinTimes(1)

	uc := useroom.NewIssueTicketUseCase(registry, admission, reservation, logger)
	_, err := uc.IssueTicket(context.Background(), roomID, peerID, "")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}

func TestIssueTicket_CancelledContext(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	admission := mocks.NewMockAdmission(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	uc := useroom.NewIssueTicketUseCase(registry, admission, reservation, logger)
	_, err := uc.IssueTicket(ctx, domain.RoomID(1), domain.PeerID(1), "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
