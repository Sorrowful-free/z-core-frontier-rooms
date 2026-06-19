package room

import (
	"context"
	"errors"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/httplimits"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/identity"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/reservation"
)

type ticketIssuer interface {
	IssueTicket(ctx context.Context, roomID domain.RoomID, nickName string, password string) (IssueTicketResult, error)
}

type CreateUseCase struct {
	roomRegistry registry.RoomRegistry
	allocator    identity.Allocator
	reservation  reservation.Reservation
	limits       httplimits.Limits
	ticketIssuer ticketIssuer
	logging      logging.Logger
}

func NewCreateUseCase(
	roomRegistry registry.RoomRegistry,
	allocator identity.Allocator,
	reservation reservation.Reservation,
	limits httplimits.Limits,
	ticketIssuer ticketIssuer,
	logging logging.Logger,
) *CreateUseCase {
	return &CreateUseCase{
		roomRegistry: roomRegistry,
		allocator:    allocator,
		reservation:  reservation,
		limits:       limits,
		ticketIssuer: ticketIssuer,
		logging:      logging,
	}
}

func (uc *CreateUseCase) Create(
	ctx context.Context,
	capacity int,
	password string,
	attributes domain.RoomAttributes,
	nickName string,
) (IssueTicketResult, error) {
	if err := ctx.Err(); err != nil {
		return IssueTicketResult{}, err
	}

	rooms, err := uc.roomRegistry.GetList(ctx)
	if err != nil {
		return IssueTicketResult{}, err
	}
	if err := uc.limits.AllowCreateRoom(len(rooms)); err != nil {
		uc.logging.Error("create room: rooms limit reached", "error", err, "current", len(rooms))
		return IssueTicketResult{}, err
	}

	roomID, err := uc.allocator.AllocateRoomID(ctx)
	if err != nil {
		uc.logging.Error("create room: allocate room ID failed", "error", err)
		return IssueTicketResult{}, err
	}

	if err := uc.reservation.RegisterRoom(ctx, roomID, capacity, password); err != nil {
		uc.logging.Error("create room: register room failed", "error", err, "roomID", roomID, "capacity", capacity)
		return IssueTicketResult{}, err
	}

	_, err = uc.roomRegistry.CreateRoom(ctx, roomID, capacity, attributes)
	if err != nil {
		createErr := err
		if unregisterErr := uc.reservation.UnregisterRoom(ctx, roomID); unregisterErr != nil {
			uc.logging.Error("create room: unregister room failed", "error", unregisterErr, "roomID", roomID)
			return IssueTicketResult{}, errors.Join(createErr, unregisterErr)
		}
		uc.logging.Error("create room: create room failed", "error", createErr, "roomID", roomID, "capacity", capacity)
		return IssueTicketResult{}, createErr
	}

	result, err := uc.ticketIssuer.IssueTicket(ctx, roomID, nickName, password)
	if err != nil {
		uc.logging.Error("create room: issue ticket failed", "error", err, "roomID", roomID)
		return IssueTicketResult{}, uc.rollbackCreatedRoom(ctx, roomID, err)
	}

	uc.logging.Info("create room: success", "roomID", roomID, "capacity", capacity)
	return result, nil
}

func (uc *CreateUseCase) rollbackCreatedRoom(ctx context.Context, roomID domain.RoomID, issueErr error) error {
	var rollbackErr error
	if deleteErr := uc.roomRegistry.DeleteRoom(ctx, roomID); deleteErr != nil {
		uc.logging.Error("create room: rollback delete room failed", "error", deleteErr, "roomID", roomID)
		rollbackErr = errors.Join(rollbackErr, deleteErr)
	}
	if unregisterErr := uc.reservation.UnregisterRoom(ctx, roomID); unregisterErr != nil {
		uc.logging.Error("create room: rollback unregister room failed", "error", unregisterErr, "roomID", roomID)
		rollbackErr = errors.Join(rollbackErr, unregisterErr)
	}
	if rollbackErr != nil {
		return errors.Join(issueErr, rollbackErr)
	}
	return issueErr
}
