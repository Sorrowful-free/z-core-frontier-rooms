package room

import (
	"context"
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/admission"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
)

type JoinRoomUseCase struct {
	admission    admission.Admission
	peerFactory  realtime.PeerFactory
	roomRegistry registry.RoomRegistry
	logging      logging.Logger
}

func NewJoinRoomUseCase(admission admission.Admission, peerFactory realtime.PeerFactory, roomRegistry registry.RoomRegistry, logging logging.Logger) *JoinRoomUseCase {
	return &JoinRoomUseCase{
		admission:    admission,
		peerFactory:  peerFactory,
		roomRegistry: roomRegistry,
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

	peer, err := uc.peerFactory.CreatePeer(room.Context(), claims.PeerID, connection, room, uc.logging)
	if err != nil {
		return EmptyRoomSummary, domain.PeerIDInvalid, err
	}

	if room.HasPeer(claims.PeerID) {
		if err := room.Replace(peer); err != nil {
			uc.logging.Error("error replacing peer", "error", err)
			return EmptyRoomSummary, domain.PeerIDInvalid, err
		}
		uc.logging.Info("peer replaced", "peerID", claims.PeerID)
	} else if err := room.Join(peer); err != nil {
		uc.logging.Error("error joining room", "error", err)
		return EmptyRoomSummary, domain.PeerIDInvalid, err
	}

	if err := peer.Start(); err != nil {
		room.Leave(peer)
		peer.Stop()
		uc.logging.Error("error starting peer", "error", err)
		return EmptyRoomSummary, domain.PeerIDInvalid, fmt.Errorf("%w: %w", domain.ErrPeerStartFailed, err)
	}

	roomSummary := NewRoomSummaryFromRoom(room)
	return *roomSummary, claims.PeerID, nil
}
