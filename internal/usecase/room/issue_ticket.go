package room

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/admission"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/reservation"
)

// Политика IssueTicket (A — строгая):
//   - нет peer в room и нет слота в reservation → Reserve + Issue;
//   - peer в room → ErrPeerAlreadyInRoom (Join/Replace, не новый ticket);
//   - слот reserved или admitted → ErrTicketSlotHeld;
//   - capacity в reservation → ErrReservationFull;
//   - комната не в registry → ErrRoomNotFound;
//   - комната не в reservation store → ErrReservationNotFound.

type IssueTicketUseCase struct {
	roomRegistry registry.RoomRegistry
	admission    admission.Admission
	reservation  reservation.Reservation
	logger       logging.Logger
}

func NewIssueTicketUseCase(roomRegistry registry.RoomRegistry, admission admission.Admission, reservation reservation.Reservation, logger logging.Logger) *IssueTicketUseCase {
	return &IssueTicketUseCase{
		roomRegistry: roomRegistry,
		admission:    admission,
		reservation:  reservation,
		logger:       logger,
	}
}

func (uc *IssueTicketUseCase) IssueTicket(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID, password string) ([]byte, error) {

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	room, err := uc.roomRegistry.GetRoom(ctx, roomID)
	if err != nil {
		uc.logger.Error("issue ticket: get room failed", "error", err, "roomID", roomID)
		return nil, err
	}

	if room.HasPeer(peerID) {
		uc.logger.Error("issue ticket: peer already in room", "roomID", roomID, "peerID", peerID)
		return nil, domain.ErrPeerAlreadyInRoom
	}

	expiresAt := time.Now().Add(uc.admission.TTL())
	if err := uc.reservation.Reserve(ctx, roomID, peerID, expiresAt); err != nil {
		uc.logger.Error("issue ticket: reserve failed", "error", err, "roomID", roomID, "peerID", peerID)
		return nil, mapIssueReserveError(err)
	}

	token, err := uc.admission.Issue(ctx, roomID, peerID, password)
	if err != nil {
		uc.logger.Error("issue ticket: issue failed", "error", err, "roomID", roomID, "peerID", peerID)
		return nil, uc.revokeIssueReservation(ctx, roomID, peerID, err)
	}

	uc.logger.Info("issue ticket: success", "roomID", roomID, "peerID", peerID)
	return token, nil
}

func mapIssueReserveError(err error) error {
	switch {
	case errors.Is(err, domain.ErrReservationAlreadyExists),
		errors.Is(err, domain.ErrReservationAlreadyAdmitted):
		return fmt.Errorf("%w: %w", domain.ErrTicketSlotHeld, err)
	default:
		return err
	}
}

func (uc *IssueTicketUseCase) revokeIssueReservation(
	ctx context.Context,
	roomID domain.RoomID,
	peerID domain.PeerID,
	issueErr error,
) error {
	if issueErr == nil {
		return nil
	}
	if revokeErr := uc.reservation.Revoke(ctx, roomID, peerID); revokeErr != nil {
		uc.logger.Error("issue ticket: revoke after issue failed", "error", revokeErr, "roomID", roomID, "peerID", peerID)
		return errors.Join(issueErr, revokeErr)
	}
	return issueErr
}
