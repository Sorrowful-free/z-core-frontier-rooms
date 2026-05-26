package room

import (
	"context"
	"errors"
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/admission"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/reservation"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
)

type JoinRoomUseCase struct {
	admission    admission.Admission
	peerFactory  realtime.PeerFactory
	roomRegistry registry.RoomRegistry
	reservation  reservation.Reservation
	logging      logging.Logger
}

func NewJoinRoomUseCase(
	admission admission.Admission,
	peerFactory realtime.PeerFactory,
	roomRegistry registry.RoomRegistry,
	reservation reservation.Reservation,
	logging logging.Logger,
) *JoinRoomUseCase {
	return &JoinRoomUseCase{
		admission:    admission,
		peerFactory:  peerFactory,
		roomRegistry: roomRegistry,
		reservation:  reservation,
		logging:      logging,
	}
}

func (uc *JoinRoomUseCase) JoinRoom(ctx context.Context, connection transport.Connection, token []byte) (RoomSummary, domain.PeerID, error) {

	if err := ctx.Err(); err != nil {
		return EmptyRoomSummary, domain.PeerIDInvalid, err
	}

	claims, err := uc.admission.Validate(ctx, token)
	if err != nil {
		return EmptyRoomSummary, domain.PeerIDInvalid, err
	}

	room, err := uc.roomRegistry.GetRoom(ctx, claims.RoomID)
	if err != nil {
		return EmptyRoomSummary, domain.PeerIDInvalid, err
	}

	if err := uc.reservation.Admit(ctx, claims.RoomID, claims.PeerID); err != nil {
		uc.logging.Error("join room: admit failed", "error", err, "roomID", claims.RoomID, "peerID", claims.PeerID)
		return EmptyRoomSummary, domain.PeerIDInvalid, err
	}

	peer, err := uc.peerFactory.CreatePeer(room.Context(), claims.PeerID, connection, room, uc.logging)
	if err != nil {
		return EmptyRoomSummary, domain.PeerIDInvalid, uc.revokeJoinReservation(ctx, room, claims.RoomID, claims.PeerID, err)
	}

	if room.HasPeer(claims.PeerID) {
		if err := room.Replace(peer); err != nil {
			uc.logging.Error("join room: replace peer failed", "error", err, "peerID", claims.PeerID)
			return EmptyRoomSummary, domain.PeerIDInvalid, uc.revokeJoinReservation(ctx, room, claims.RoomID, claims.PeerID, err)
		}
		uc.logging.Info("peer replaced", "peerID", claims.PeerID)
	} else if err := room.Join(peer); err != nil {
		uc.logging.Error("join room: join peer failed", "error", err, "peerID", claims.PeerID)
		return EmptyRoomSummary, domain.PeerIDInvalid, uc.revokeJoinReservation(ctx, room, claims.RoomID, claims.PeerID, err)
	}

	if err := peer.Start(); err != nil {
		_ = room.Leave(peer)
		_ = peer.Stop()
		startErr := fmt.Errorf("%w: %w", domain.ErrPeerStartFailed, err)
		uc.logging.Error("join room: start peer failed", "error", startErr, "peerID", claims.PeerID)
		return EmptyRoomSummary, domain.PeerIDInvalid, uc.revokeJoinReservation(ctx, room, claims.RoomID, claims.PeerID, startErr)
	}

	roomSummary := NewRoomSummaryFromRoom(room)
	uc.logging.Info("join room: join room success", "roomID", claims.RoomID, "peerID", claims.PeerID)
	return *roomSummary, claims.PeerID, nil
}

// revokeJoinReservation откатывает слот после неудачного join.
// При ошибке Replace старый peer остаётся в комнате — Revoke не вызывается.
// Ошибка Revoke объединяется с joinErr и возвращается вызывающему.
func (uc *JoinRoomUseCase) revokeJoinReservation(
	ctx context.Context,
	room realtime.Room,
	roomID domain.RoomID,
	peerID domain.PeerID,
	joinErr error,
) error {
	if joinErr == nil {
		return nil
	}
	if room.HasPeer(peerID) {
		return joinErr
	}
	if revokeErr := uc.reservation.Revoke(ctx, roomID, peerID); revokeErr != nil {
		uc.logging.Error("join room: revoke reservation failed", "error", revokeErr, "roomID", roomID, "peerID", peerID)
		return errors.Join(joinErr, revokeErr)
	}
	return joinErr
}
