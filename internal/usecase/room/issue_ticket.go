package room

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/admission"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/identity"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/reservation"
	portenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport/enet"
)

// IssueTicket policy (A — strict):
//   - no peer in room and no slot in reservation → Reserve + Issue;
//   - peer in room → ErrPeerAlreadyInRoom (Join/Replace, not a new ticket);
//   - slot reserved with the same nickName or slot already taken → ErrTicketSlotHeld;
//   - orphan admitted (slot exists, peer not in room) → Revoke + one Reserve retry;
//   - capacity reached in reservation → ErrReservationFull;
//   - room not in registry → ErrRoomNotFound;
//   - room not in reservation store → ErrReservationNotFound.
type IssueTicketResult struct {
	Token []byte
	// ReconnectToken - reconnect token (P20): resume credential under the
	// same peerID within the grace window; hostID - same binding as the
	// join ticket's.
	ReconnectToken []byte
	Room           RoomSummary
	// EnetPort - port of the assigned ENet host (P18): the client connects
	// to it. 0 - no binding (hostRegistry not wired, e.g. a WS-only
	// environment).
	EnetPort uint16
}

type IssueTicketUseCase struct {
	roomRegistry registry.RoomRegistry
	allocator    identity.Allocator
	admission    admission.Admission
	reservation  reservation.Reservation
	// hostRegistry - ENet host balancer (P18); nil - tickets without a
	// binding (claims.HostID = HostIDUnbound, admit over WS only).
	hostRegistry portenet.HostRegistry
	logger       logging.Logger
}

func NewIssueTicketUseCase(roomRegistry registry.RoomRegistry, allocator identity.Allocator, admission admission.Admission, reservation reservation.Reservation, hostRegistry portenet.HostRegistry, logger logging.Logger) *IssueTicketUseCase {
	return &IssueTicketUseCase{
		roomRegistry: roomRegistry,
		allocator:    allocator,
		admission:    admission,
		reservation:  reservation,
		hostRegistry: hostRegistry,
		logger:       logger,
	}
}

func (uc *IssueTicketUseCase) IssueTicket(ctx context.Context, roomID domain.RoomID, nickName string, password string) (IssueTicketResult, error) {
	if err := ctx.Err(); err != nil {
		return IssueTicketResult{}, err
	}
	if err := domain.ValidateNickName(nickName); err != nil {
		return IssueTicketResult{}, err
	}

	room, err := uc.roomRegistry.GetRoom(ctx, roomID)
	if err != nil {
		uc.logger.Error("issue ticket: get room failed", "error", err, "roomID", roomID)
		return IssueTicketResult{}, err
	}

	peerID, err := uc.allocator.AllocatePeerID(ctx)
	if err != nil {
		uc.logger.Error("issue ticket: allocate peer ID failed", "error", err, "roomID", roomID)
		return IssueTicketResult{}, err
	}

	if room.HasPeer(peerID) {
		uc.logger.Error("issue ticket: peer already in room", "roomID", roomID, "peerID", peerID)
		return IssueTicketResult{}, domain.ErrPeerAlreadyInRoom
	}

	if err := uc.reservation.VerifyRoomPassword(ctx, roomID, password); err != nil {
		uc.logger.Error("issue ticket: invalid room password", "error", err, "roomID", roomID, "peerID", peerID)
		return IssueTicketResult{}, err
	}

	// Host assignment before Reserve: a balancer error leaves no slot behind.
	var assignment portenet.HostAssignment
	if uc.hostRegistry != nil {
		assignment, err = uc.hostRegistry.AssignHost(ctx)
		if err != nil {
			uc.logger.Error("issue ticket: assign enet host failed", "error", err, "roomID", roomID, "peerID", peerID)
			return IssueTicketResult{}, err
		}
	}

	expiresAt := time.Now().Add(uc.admission.TTL())
	if err := uc.reserveForIssue(ctx, room, roomID, peerID, nickName, expiresAt); err != nil {
		uc.logger.Error("issue ticket: reserve failed", "error", err, "roomID", roomID, "peerID", peerID)
		return IssueTicketResult{}, err
	}

	token, err := uc.admission.Issue(ctx, roomID, peerID, nickName, assignment.HostID)
	if err != nil {
		uc.logger.Error("issue ticket: issue failed", "error", err, "roomID", roomID, "peerID", peerID)
		return IssueTicketResult{}, uc.revokeIssueReservation(ctx, roomID, peerID, err)
	}

	reconnectToken, err := uc.admission.IssueReconnect(ctx, roomID, peerID, assignment.HostID)
	if err != nil {
		uc.logger.Error("issue ticket: issue reconnect failed", "error", err, "roomID", roomID, "peerID", peerID)
		return IssueTicketResult{}, uc.revokeIssueReservation(ctx, roomID, peerID, err)
	}

	uc.logger.Info("issue ticket: success", "roomID", roomID, "peerID", peerID, "hostID", assignment.HostID)
	return IssueTicketResult{
		Token:          token,
		ReconnectToken: reconnectToken,
		Room:           *NewRoomSummaryFromRoom(room),
		EnetPort:       assignment.Port,
	}, nil
}

func (uc *IssueTicketUseCase) reserveForIssue(
	ctx context.Context,
	room realtime.Room,
	roomID domain.RoomID,
	peerID domain.PeerID,
	nickName string,
	expiresAt time.Time,
) error {
	err := uc.reservation.Reserve(ctx, roomID, peerID, nickName, expiresAt)
	if err == nil {
		return nil
	}

	mapped := mapIssueReserveError(err)
	if !errors.Is(mapped, domain.ErrTicketSlotHeld) || room.HasPeer(peerID) {
		return mapped
	}

	state, stateErr := uc.reservation.State(ctx, roomID, peerID)
	if stateErr != nil {
		return errors.Join(mapped, stateErr)
	}
	if state != domain.ReservationSlotAdmitted {
		return mapped
	}

	uc.logger.Warn("issue ticket: revoking orphan admitted slot", "roomID", roomID, "peerID", peerID)
	if revokeErr := uc.reservation.Revoke(ctx, roomID, peerID); revokeErr != nil {
		return errors.Join(mapped, revokeErr)
	}

	if err := uc.reservation.Reserve(ctx, roomID, peerID, nickName, expiresAt); err != nil {
		return mapIssueReserveError(err)
	}
	return nil
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
