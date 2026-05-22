package room

import (
	"context"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/admission"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
)

type ConnectUseCase struct {
	admission    admission.Admission
	peerFactory  realtime.PeerFactory
	roomRegistry registry.RoomRegistry
	logging      logging.Logger
}

func NewConnectUseCase(admission admission.Admission, peerFactory realtime.PeerFactory, roomRegistry registry.RoomRegistry, logging logging.Logger) *ConnectUseCase {
	return &ConnectUseCase{
		admission:    admission,
		peerFactory:  peerFactory,
		roomRegistry: roomRegistry,
		logging:      logging,
	}
}

func (uc *ConnectUseCase) Connect(ctx context.Context, connection transport.Connection, token []byte) (RoomSummary, error) {
	claims, err := uc.admission.Validate(ctx, token)
	if err != nil {
		return EmptyRoomSummary, err
	}

	room, err := uc.roomRegistry.GetRoom(ctx, claims.RoomID)
	if err != nil {
		return EmptyRoomSummary, err
	}

	peer := uc.peerFactory.CreatePeer(claims.PeerID, connection, room, uc.logging)

	if err := room.Join(peer); err != nil {
		uc.logging.Error("error joining room", "error", err)
		return EmptyRoomSummary, err
	}

	if err := peer.Start(); err != nil {
		room.Leave(peer)
		peer.Stop()
		uc.logging.Error("error starting peer", "error", err)
		return EmptyRoomSummary, err
	}

	roomSummary := NewRoomSummaryFromRoom(room)
	return *roomSummary, nil
}
