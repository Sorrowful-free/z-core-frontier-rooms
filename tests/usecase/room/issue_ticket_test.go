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

func TestIssueTicket_Success(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	admission := mocks.NewMockAdmission(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	const (
		roomID   = domain.RoomID(1)
		peerID   = domain.PeerID(2)
		password = "secret"
	)
	wantToken := []byte("ticket-bytes")

	admission.EXPECT().
		Issue(gomock.Any(), roomID, peerID, password).
		Return(wantToken, nil)

	uc := useroom.NewIssueTicketUseCase(admission, logger)
	token, err := uc.IssueTicket(context.Background(), roomID, peerID, password)
	if err != nil {
		t.Fatalf("IssueTicket: %v", err)
	}
	if string(token) != string(wantToken) {
		t.Fatalf("token = %q, want %q", token, wantToken)
	}
}

func TestIssueTicket_AdmissionError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	admission := mocks.NewMockAdmission(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	admission.EXPECT().
		Issue(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, domain.ErrInvalidToken)

	uc := useroom.NewIssueTicketUseCase(admission, logger)
	_, err := uc.IssueTicket(context.Background(), domain.RoomID(1), domain.PeerID(1), "")
	if !errors.Is(err, domain.ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
}

func TestIssueTicket_CancelledContext(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	admission := mocks.NewMockAdmission(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	uc := useroom.NewIssueTicketUseCase(admission, logger)
	_, err := uc.IssueTicket(ctx, domain.RoomID(1), domain.PeerID(1), "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
