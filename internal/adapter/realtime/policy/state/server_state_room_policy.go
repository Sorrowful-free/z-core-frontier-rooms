package state

import (
	"errors"
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

func (p *StateRoomPolicy) OnTickFullState() error {
	// Ping едет в full-кадре: свежие значения пишутся до encode, а
	// commitPrevState после send закрывает и их — patch-diff ping
	// не видит и не несёт.
	if err := p.refreshPeerPings(); err != nil {
		return fmt.Errorf("refresh peer pings: %w", err)
	}
	if err := p.sendFullState(&p.state); err != nil {
		return fmt.Errorf("send full state: %w", err)
	}
	return nil
}

// OnTickPatchState — единственный регулярный эмиттер OpCodePatchState
// (tick-coalesced эмиссия, P34): все дельты, накопленные в p.state с
// прошлого коммита (join/leave/election/клиентские сообщения), уходят
// одним broadcast всем без исключений — publisher получает эхо своих
// дельт, client-side apply идемпотентен.
func (p *StateRoomPolicy) OnTickPatchState() error {
	if err := p.pruneStalePeers(); err != nil {
		return fmt.Errorf("prune stale peers: %w", err)
	}
	patch, err := p.prevState.MakePatch(&p.state)
	if err != nil {
		return fmt.Errorf("make patch state: %w", err)
	}
	if patch == nil {
		return nil
	}
	return p.sendPatchState(patch)
}

// refreshPeerPings переносит живые ping из room в state. Выполняется на
// full-тике до sendFullState — каденция ping = fullStateInterval, отдельного
// механизма нет. Stale-записи пропускаются: их зачистка и master-переходы
// живут в pruneStalePeers на patch-тике.
func (p *StateRoomPolicy) refreshPeerPings() error {
	for peerID, peerState := range p.state.Peers {
		peer, err := p.room.GetPeer(peerID)
		if err != nil {
			// stale - только когда peer реально отсутствует в room;
			// прочие ошибки GetPeer не повод выкидывать peer из state.
			if errors.Is(err, domain.ErrPeerNotFound) {
				continue
			}
			return fmt.Errorf("get peer %d: %w", peerID, err)
		}
		peerState.Ping = peer.Ping()
		p.state.Peers[peerID] = peerState
	}
	return nil
}

// pruneStalePeers вычищает записи peers, которых уже нет в room (stale —
// только ErrPeerNotFound), и эмитит evTickPrune: машина решает, нужен ли
// переход master (вычищен или отсутствует при живых peers). Привязан к
// patch-тику — masterless recovery держит каденцию patchStateInterval,
// а не fullStateInterval.
func (p *StateRoomPolicy) pruneStalePeers() error {
	var stale []domain.PeerID
	for peerID := range p.state.Peers {
		_, err := p.room.GetPeer(peerID)
		if err != nil {
			if errors.Is(err, domain.ErrPeerNotFound) {
				stale = append(stale, peerID)
				continue
			}
			return fmt.Errorf("get peer %d: %w", peerID, err)
		}
	}

	// Сначала вычищаем все stale-записи, потом один раз перевыборы master -
	// иначе выборы могли бы попасть на ещё не вычищенный stale peer.
	masterPruned := false
	for _, peerID := range stale {
		p.logger.Warn("pruning stale peer from room state", "peerID", peerID)
		delete(p.state.Peers, peerID)
		delete(p.state.Inputs, peerID)
		if p.master.isMaster(peerID) {
			masterPruned = true
		}
	}
	// Событие tick-prune: машина решает, нужен ли переход master (вычищен
	// или отсутствует при живых peers) и ведёт лог masterless-эпизода.
	return p.master.dispatch(masterEvent{kind: evTickPrune, masterPruned: masterPruned}, p.room, &p.state)
}

func (p *StateRoomPolicy) joinPeerAndSyncState(peer realtime.Peer) error {
	peerID := peer.GetID()
	// Encode-only view: машина пишет запись peer в shallow-снапшот (его
	// Peers — отдельная мапа), p.state не трогается до commit-точки.
	view := p.state.Snapshot()

	oldMaster := p.master.current()
	committed := false
	// Событие join может перебрать master до commit-точки (первый peer
	// пустой комнаты) - на всех ветках ошибок откатываем.
	defer func() {
		if !committed {
			p.master.restore(oldMaster)
		}
	}()

	// Машина пишет запись peer в staged-state и решает его IsMaster-флаг.
	if err := p.master.dispatch(masterEvent{kind: evPeerJoined, peer: peer}, p.room, view); err != nil {
		return err
	}

	if err := p.deliverFullStateToPeer(peer, view); err != nil {
		return fmt.Errorf("send full state to peer %d: %w", peerID, err)
	}

	// Commit — точечная запись в p.state; join-дельта остальным доезжает
	// ближайшим patch-тиком (prevState здесь не коммитим).
	p.state.Peers[peerID] = view.Peers[peerID]
	committed = true
	// Post-commit: join мог перевести masterless -> masterSet и закрыть эпизод.
	p.master.assigned()
	return nil
}

func (p *StateRoomPolicy) sendFullState(full *state.RoomState) error {
	if err := p.sendFullStateToPeer(domain.PeerIDInvalid, full); err != nil {
		return err
	}
	p.commitPrevState()
	return nil
}

func (p *StateRoomPolicy) sendFullStateToPeer(peerID domain.PeerID, full *state.RoomState) error {
	payload, err := p.stateCodec.Encode(full)
	if err != nil {
		return err
	}
	peerEvent := events.PeerEvent{
		Frame: domain.Frame{
			OpCode:  state.OpCodeFullState,
			Payload: payload,
		},
	}
	if peerID.IsValid() {
		peerEvent.PeerID = peerID
	}
	return p.room.Send(peerEvent)
}

// deliverFullStateToPeer шлёт full state напрямую в peer: на момент OnJoin
// peer ещё не вставлен в r.peers, поэтому room.Send по peerID не находит его.
// Единственная immediate-эмиссия вне тиков — джойнеру нельзя ждать full-тик.
func (p *StateRoomPolicy) deliverFullStateToPeer(peer realtime.Peer, full *state.RoomState) error {
	payload, err := p.stateCodec.Encode(full)
	if err != nil {
		return err
	}
	return peer.Deliver(events.PeerEvent{
		PeerID: peer.GetID(),
		Frame: domain.Frame{
			OpCode:  state.OpCodeFullState,
			Payload: payload,
		},
	})
}

// sendPatchState — broadcast патча всем без исключений (универсальный
// broadcast, P34). prevState коммитится только после успешного send —
// сорванная рассылка не теряет дельту, она уйдёт следующим тиком.
func (p *StateRoomPolicy) sendPatchState(patch *state.RoomStatePatch) error {
	payload, err := p.stateCodec.EncodePatch(patch)
	if err != nil {
		return err
	}
	if err := p.room.Send(events.PeerEvent{
		Frame: domain.Frame{
			OpCode:  state.OpCodePatchState,
			Payload: payload,
		},
	}); err != nil {
		return err
	}
	p.commitPrevState()
	return nil
}

// commitPrevState — точки коммита prevState: после успешных sendFullState /
// sendPatchState. Снапшот shallow (значения разделяются со state) — корректно
// при COW-инварианте RoomState и не платит deep clone на каждый тик.
func (p *StateRoomPolicy) commitPrevState() {
	p.prevState = *p.state.Snapshot()
}
