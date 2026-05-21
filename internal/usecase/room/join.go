package room

import (
	"context"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/admission"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
)

type JoinUseCase struct {
	admission admission.Admission
	logger    logging.Logger
}

func NewJoinUseCase(admission admission.Admission, logger logging.Logger) *JoinUseCase {
	return &JoinUseCase{
		admission: admission,
		logger:    logger,
	}
}

func (uc *JoinUseCase) Join(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID, password string) ([]byte, error) {
	token, err := uc.admission.Issue(ctx, roomID, peerID, password)
	if err != nil {
		return nil, err
	}
	return token, nil
}
