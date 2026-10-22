package state

import (
	"errors"
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

// electMasterPeerID выбирает нового master среди peers.
// Среди peer с Ping() >= 0 — минимальный ping; при равенстве — минимальный PeerID.
// Если ни у кого нет инициализированного ping (все -1) — минимальный PeerID.
// Stale-записи (peer в state, но не в room — ErrPeerNotFound) пропускаются:
// выборы идут среди живых. Если все записи stale — PeerIDInvalid (пустые выборы,
// не ошибка). Прочие ошибки GetPeer фейлят выборы целиком.
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
		peer, err := room.GetPeer(id)
		if err != nil {
			if errors.Is(err, domain.ErrPeerNotFound) {
				continue
			}
			return domain.PeerIDInvalid, fmt.Errorf("get peer %d: %w", id, err)
		}
		allIDs = append(allIDs, id)
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

	if len(allIDs) == 0 {
		// Все записи state.Peers оказались stale — некого избрать.
		return domain.PeerIDInvalid, nil
	}

	bestID := allIDs[0]
	for _, id := range allIDs[1:] {
		if id < bestID {
			bestID = id
		}
	}
	return bestID, nil
}

// assignMaster назначает master. Validate-first: GetPeer(masterID) идёт
// до записи IsMaster-флагов — при ошибке state не трогается и не
// рекламирует master, которого нет у policy.
func (p *StateRoomPolicy) assignMaster(roomState *state.RoomState, masterID domain.PeerID) error {
	if !masterID.IsValid() {
		p.master = nil
		// Пустые выборы — master нет, флаги ни у кого не должны остаться.
		for id, peerState := range roomState.Peers {
			if peerState.IsMaster {
				peerState.IsMaster = false
				roomState.Peers[id] = peerState
			}
		}
		return nil
	}

	newMaster, err := p.room.GetPeer(masterID)
	if err != nil {
		return fmt.Errorf("get elected master %d: %w", masterID, err)
	}

	for id, peerState := range roomState.Peers {
		wantMaster := id == masterID
		if peerState.IsMaster != wantMaster {
			peerState.IsMaster = wantMaster
			roomState.Peers[id] = peerState
		}
	}

	p.master = newMaster
	return nil
}

// logMasterless — Error один раз на masterless-эпизод: tick re-elect и
// lazy RPC re-elect делят один флаг, повторные фейлы не спамят.
func (p *StateRoomPolicy) logMasterless(err error) {
	if p.masterlessLogged {
		return
	}
	p.logger.Error("elect master failed, room is masterless", "error", err)
	p.masterlessLogged = true
}

// masterAssigned закрывает masterless-эпизод (если был): Info о восстановлении
// и сброс дедуп-флага. Вызывается только на commit-точках — после того как
// новый master зафиксирован (tick, patch-send, join, lazy RPC), иначе
// откаченный assign дал бы ложное «episode over».
func (p *StateRoomPolicy) masterAssigned(masterID domain.PeerID) {
	if !p.masterlessLogged {
		return
	}
	p.logger.Info("master elected, masterless episode over", "peerID", masterID)
	p.masterlessLogged = false
}

func (p *StateRoomPolicy) electAndAssignMaster(roomState *state.RoomState) error {
	masterID, err := electMasterPeerID(p.room, roomState.Peers)
	if err != nil {
		return err
	}
	return p.assignMaster(roomState, masterID)
}
