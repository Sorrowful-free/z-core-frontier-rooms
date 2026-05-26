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
	admission := mocks.NewMockAdmission(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	const (
		roomID   = domain.RoomID(1)
		peerID   = domain.PeerID(2)
		password = "secret"
	)
	wantToken := []byte("ticket-bytes")
	ttl := time.Hour

	admission.EXPECT().TTL().Return(ttl)
	reservation.EXPECT().
		Reserve(gomock.Any(), roomID, peerID, gomock.Any()).
		Return(nil)
	admission.EXPECT().
		Issue(gomock.Any(), roomID, peerID, password).
		Return(wantToken, nil)
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewIssueTicketUseCase(admission, reservation, logger)
	token, err := uc.IssueTicket(context.Background(), roomID, peerID, password)
	if err != nil {
		t.Fatalf("IssueTicket: %v", err)
	}
	if string(token) != string(wantToken) {
		t.Fatalf("token = %q, want %q", token, wantToken)
	}
}

func TestIssueTicket_ReserveError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	admission := mocks.NewMockAdmission(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	admission.EXPECT().TTL().Return(time.Hour)
	reservation.EXPECT().
		Reserve(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(domain.ErrReservationFull)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewIssueTicketUseCase(admission, reservation, logger)
	_, err := uc.IssueTicket(context.Background(), domain.RoomID(1), domain.PeerID(1), "")
	if !errors.Is(err, domain.ErrReservationFull) {
		t.Fatalf("err = %v, want ErrReservationFull", err)
	}
}

func TestIssueTicket_AdmissionErrorJoinsRevokeError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	admission := mocks.NewMockAdmission(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)

	admission.EXPECT().TTL().Return(time.Hour)
	reservation.EXPECT().Reserve(gomock.Any(), roomID, peerID, gomock.Any()).Return(nil)
	admission.EXPECT().Issue(gomock.Any(), roomID, peerID, gomock.Any()).Return(nil, domain.ErrInvalidCredentials)
	reservation.EXPECT().Revoke(gomock.Any(), roomID, peerID).Return(domain.ErrReservationNotFound)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).MinTimes(1)

	uc := useroom.NewIssueTicketUseCase(admission, reservation, logger)
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
	admission := mocks.NewMockAdmission(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)

	admission.EXPECT().TTL().Return(time.Hour)
	reservation.EXPECT().Reserve(gomock.Any(), roomID, peerID, gomock.Any()).Return(nil)
	admission.EXPECT().Issue(gomock.Any(), roomID, peerID, gomock.Any()).Return(nil, domain.ErrInvalidCredentials)
	reservation.EXPECT().Revoke(gomock.Any(), roomID, peerID).Return(nil)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).MinTimes(1)

	uc := useroom.NewIssueTicketUseCase(admission, reservation, logger)
	_, err := uc.IssueTicket(context.Background(), roomID, peerID, "")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}

func TestIssueTicket_CancelledContext(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	admission := mocks.NewMockAdmission(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	uc := useroom.NewIssueTicketUseCase(admission, reservation, logger)
	_, err := uc.IssueTicket(ctx, domain.RoomID(1), domain.PeerID(1), "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
