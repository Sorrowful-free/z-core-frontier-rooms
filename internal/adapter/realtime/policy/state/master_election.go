package state

import (
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

const pingUnknown = int64(-1)

// electMasterPeerID выбирает нового master среди peers.
// Среди peer с Ping() >= 0 — минимальный ping; при равенстве — минимальный PeerID.
// Если ни у кого нет инициализированного ping (все -1) — минимальный PeerID.
func electMasterPeerID(room realtime.Room, peers state.MapState[domain.PeerID, state.PeerState]) (domain.PeerID, error) {
	if len(peers) == 0 {
		return domain.PeerIDInvalid, nil
	}

	type candidate struct {
		id   domain.PeerID
		ping int64
	}

	validPing := make([]candidate, 0, len(peers))
	allIDs := make([]domain.PeerID, 0, len(peers))

	for id := range peers {
		allIDs = append(allIDs, id)
		peer, err := room.GetPeer(id)
		if err != nil {
			return domain.PeerIDInvalid, fmt.Errorf("get peer %d: %w", id, err)
		}
		ping := peer.Ping()
		if ping >= 0 {
			validPing = append(validPing, candidate{id: id, ping: ping})
		}
	}

	if len(validPing) > 0 {
		best := validPing[0]
		for _, c := range validPing[1:] {
			if c.ping < best.ping || (c.ping == best.ping && c.id < best.id) {
				best = c
			}
		}
		return best.id, nil
	}

	bestID := allIDs[0]
	for _, id := range allIDs[1:] {
		if id < bestID {
			bestID = id
		}
	}
	return bestID, nil
}

func (p *StateRoomPolicy) assignMaster(roomState *state.RoomState, masterID domain.PeerID) error {
	if !masterID.IsValid() {
		p.master = nil
		return nil
	}

	for id, peerState := range roomState.Peers {
		wantMaster := id == masterID
		if peerState.IsMaster != wantMaster {
			peerState.IsMaster = wantMaster
			roomState.Peers[id] = peerState
		}
	}

	newMaster, err := p.room.GetPeer(masterID)
	if err != nil {
		return err
	}
	p.master = newMaster
	return nil
}

func (p *StateRoomPolicy) electAndAssignMaster(roomState *state.RoomState) error {
	masterID, err := electMasterPeerID(p.room, roomState.Peers)
	if err != nil {
		return err
	}
	return p.assignMaster(roomState, masterID)
}
