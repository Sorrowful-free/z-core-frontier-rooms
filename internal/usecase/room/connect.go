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

func (uc *ConnectUseCase) Connect(ctx context.Context, connection transport.Connection, token []byte) (realtime.Room, error) {
	claims, err := uc.admission.Validate(ctx, token)
	if err != nil {
		return nil, err
	}

	room, err := uc.roomRegistry.GetRoom(claims.RoomID)
	if err != nil {
		return nil, err
	}

	peer := uc.peerFactory.CreatePeer(claims.PeerID, connection, uc.logging)

	if err := room.Join(peer); err != nil {
		return nil, err
	}

	if err := peer.Start(); err != nil {
		peer.Stop()
		return nil, err
	}

	return room, nil
}
