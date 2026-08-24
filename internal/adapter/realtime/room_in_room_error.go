package realtime

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
)

func (r *Room) notifyInRoomError(peerID domain.PeerID, err error) {
	if !peerID.IsValid() {
		return
	}
	if sendErr := r.Send(events.PeerEvent{
		PeerID: peerID,
		Frame: domain.Frame{
			OpCode:   domain.InRoomErrorOpCode(err),
			Delivery: domain.DeliveryReliable,
			Payload:  nil,
		},
	}); sendErr != nil {
		r.logger.Error("send in-room error", "error", sendErr, "peerID", peerID, "cause", err)
	}
}
