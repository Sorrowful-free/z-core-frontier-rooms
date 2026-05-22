package room

import (
	"context"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
)

type LeaveRoomUseCase struct {
	roomRegistry registry.RoomRegistry
	logging      logging.Logger
}

func NewLeaveRoomUseCase(roomRegistry registry.RoomRegistry, logging logging.Logger) *LeaveRoomUseCase {
	return &LeaveRoomUseCase{
		roomRegistry: roomRegistry,
		logging:      logging,
	}
}

func (uc *LeaveRoomUseCase) LeaveRoom(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID) error {
	room, err := uc.roomRegistry.GetRoom(ctx, roomID)
	if err != nil {
		return err
	}

	var peer realtime.Peer
	peer, err = room.GetPeer(peerID)
	if err != nil {
		return err
	}

	if err := room.Leave(peer); err != nil {
		uc.logging.Error("leave room: room leave failed", "error", err, "roomID", roomID, "peerID", peerID)
		return err
	}
	if err := peer.Stop(); err != nil {
		uc.logging.Error("leave room: peer stop failed", "error", err, "roomID", roomID, "peerID", peerID)
		return err
	}
	uc.logging.Info("peer left room", "roomID", roomID, "peerID", peerID)
	return nil
}
