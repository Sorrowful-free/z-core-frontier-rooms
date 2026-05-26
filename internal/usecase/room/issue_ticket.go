package room

import (
	"context"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/admission"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
)

type IssueTicketUseCase struct {
	admission admission.Admission
	logger    logging.Logger
}

func NewIssueTicketUseCase(admission admission.Admission, logger logging.Logger) *IssueTicketUseCase {
	return &IssueTicketUseCase{
		admission: admission,
		logger:    logger,
	}
}

func (uc *IssueTicketUseCase) IssueTicket(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID, password string) ([]byte, error) {

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	token, err := uc.admission.Issue(ctx, roomID, peerID, password)
	if err != nil {
		return nil, err
	}
	return token, nil
}
