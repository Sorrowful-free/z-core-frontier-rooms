package state

import (
	"errors"
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

// masterMachine - машина master-эпизодов StateRoomPolicy.
//
// Состояния выводятся из peer + masterlessLogged:
//   - masterSet: peer != nil (master избран, его запись держит IsMaster);
//   - masterless: peer == nil; masterlessLogged=true - открытый эпизод
//     (Error уже залогирован, повторные фейлы re-elect не спамят).
//
// Триггеры (join/leave/tick/rpc) не знают ритуала выборов: они эмитят
// masterEvent, а dispatch решает переход - election, IsMaster-флаги
// roomState и лог masterless-эпизода живут в одном месте. Переходы,
// исполняемые внутри stage-then-commit (join-snapshot, ручной откат
// OnLeave), мутируют m.peer до commit-точки; владелец транзакции
// откатывает его через restore на ветках ошибок.
type masterMachine struct {
	peer             realtime.Peer
	masterlessLogged bool
	logger           logging.Logger
	metrics          portmetrics.Metrics
}

// masterEventKind - события триггеров, способные сменить master.
type masterEventKind int

const (
	// evPeerJoined - OnJoin: машина решает, становится ли peer master
	// (только первый peer пустой комнаты), и пишет его запись в roomState.
	evPeerJoined masterEventKind = iota
	// evPeerLeft - OnLeave: запись ушедшего уже удалена из roomState,
	// переход нужен только если он держал IsMaster.
	evPeerLeft
	// evTickPrune - OnTickPatchState: stale-prune мог вычистить master,
	// плюс recovery-окно masterless при живых peers.
	evTickPrune
	// evRpcMaster - OnMessage RPC->master при masterless: lazy election.
	evRpcMaster
)

// masterEvent - событие триггера; заполняются только поля нужного kind.
type masterEvent struct {
	kind         masterEventKind
	peer         realtime.Peer // evPeerJoined: вошедший peer
	leftIsMaster bool          // evPeerLeft: ушедший держал IsMaster
	masterPruned bool          // evTickPrune: master попал в stale-зачистку
}

// dispatch - единый диспетчер переходов. Мутирует roomState (запись
// вошедшего peer, IsMaster-флаги) и m.peer. Ошибку возвращает только
// evPeerLeft: падение election на leave фейлит Leave целиком; ошибки
// tick/rpc-переходов машина глотает в лог эпизода.
func (m *masterMachine) dispatch(ev masterEvent, room realtime.Room, roomState *state.RoomState) error {
	switch ev.kind {
	case evPeerJoined:
		return m.onPeerJoined(ev.peer, roomState)
	case evPeerLeft:
		return m.onPeerLeft(ev.leftIsMaster, room, roomState)
	case evTickPrune:
		m.onTickPrune(ev.masterPruned, room, roomState)
	case evRpcMaster:
		m.onRpcMaster(room, roomState)
	}
	return nil
}

// onPeerJoined: masterless -> masterSet только для первого peer комнаты;
// в непустой комнате join master не трогает. Переход staged: m.peer
// назначается до commit-точки, caller откатывает через restore.
func (m *masterMachine) onPeerJoined(peer realtime.Peer, roomState *state.RoomState) error {
	if _, ok := roomState.Peers[peer.GetID()]; ok {
		return nil
	}
	first := len(roomState.Peers) == 0
	roomState.Peers[peer.GetID()] = state.PeerState{
		NickName: peer.GetNickName(),
		IsMaster: first,
		Ping:     peer.Ping(),
	}
	if first {
		m.peer = peer
		// Первое избрание - то же "назначение master", что и assign при
		// перевыборах: считаем здесь, иначе первый элекшн комнаты
		// невидим для master_elections_total. Эмит staged (до commit-
		// точки join): откаченный join даёт лишний счёт - приближение
		// то же, что у staged-эмита в assign.
		m.metrics.MasterElected()
	}
	return nil
}

// onPeerLeft: masterSet -> masterSet перевыборами либо -> masterless
// в опустевшей комнате. Ошибка election уходит наружу (Leave фейлится),
// m.peer откатывает транзакция caller'а.
func (m *masterMachine) onPeerLeft(leftIsMaster bool, room realtime.Room, roomState *state.RoomState) error {
	if !leftIsMaster {
		return nil
	}
	if len(roomState.Peers) == 0 {
		m.peer = nil
		return nil
	}
	return m.electAndAssign(room, roomState)
}

// onTickPrune: masterSet -> masterless, если master вычищен stale-prune,
// либо masterless -> masterSet recovery при живых peers. Падение election
// не уходит наружу: peer сбрасывается, эпизод логируется.
func (m *masterMachine) onTickPrune(masterPruned bool, room realtime.Room, roomState *state.RoomState) {
	// Переизбираем, если master вычищен вместе со stale-записями или его
	// нет вовсе (прошлая попытка election упала) - пока есть живые peers.
	if !masterPruned && (m.peer != nil || len(roomState.Peers) == 0) {
		return
	}
	if err := m.electAndAssign(room, roomState); err != nil {
		// master не должен висеть на удалённом peer - сбрасываем.
		m.peer = nil
		m.logMasterless(err)
		return
	}
	m.assigned()
}

// onRpcMaster: lazy election по живому state при masterless; фейл
// логируется в эпизод, наружу уходит только обновлённый peerID().
func (m *masterMachine) onRpcMaster(room realtime.Room, roomState *state.RoomState) {
	if m.peer != nil {
		return
	}
	if err := m.electAndAssign(room, roomState); err != nil {
		m.logMasterless(err)
		return
	}
	m.assigned()
}

// electAndAssign - переход "перевыборы": кандидат из electMasterPeerID,
// затем validate-first assign. Мутирует IsMaster-флаги roomState и m.peer.
func (m *masterMachine) electAndAssign(room realtime.Room, roomState *state.RoomState) error {
	masterID, err := electMasterPeerID(room, roomState.Peers)
	if err != nil {
		return err
	}
	return m.assign(room, roomState, masterID)
}

// electMasterPeerID выбирает нового master среди peers.
// Среди peer с Ping() >= 0 - минимальный ping; при равенстве - минимальный PeerID.
// Если ни у кого нет инициализированного ping (все -1) - минимальный PeerID.
// Stale-записи (peer в state, но не в room - ErrPeerNotFound) пропускаются:
// выборы идут среди живых. Если все записи stale - PeerIDInvalid (пустые выборы,
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
		// Все записи state.Peers оказались stale - некого избрать.
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

// assign назначает master. Validate-first: GetPeer(masterID) идёт
// до записи IsMaster-флагов - при ошибке state не трогается и не
// рекламирует master, которого нет у policy.
func (m *masterMachine) assign(room realtime.Room, roomState *state.RoomState, masterID domain.PeerID) error {
	if !masterID.IsValid() {
		m.peer = nil
		// Пустые выборы - master нет, флаги ни у кого не должны остаться.
		for id, peerState := range roomState.Peers {
			if peerState.IsMaster {
				peerState.IsMaster = false
				roomState.Peers[id] = peerState
			}
		}
		return nil
	}

	newMaster, err := room.GetPeer(masterID)
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

	m.peer = newMaster
	m.metrics.MasterElected()
	return nil
}

// logMasterless - Error один раз на masterless-эпизод: tick re-elect и
// lazy RPC re-elect делят один флаг, повторные фейлы не спамят.
func (m *masterMachine) logMasterless(err error) {
	if m.masterlessLogged {
		return
	}
	m.logger.Error("elect master failed, room is masterless", "error", err)
	m.masterlessLogged = true
}

// assigned закрывает masterless-эпизод (если был): Info о восстановлении
// и сброс дедуп-флага. Триггеры вызывают её только на commit-точках
// (после того как новый master зафиксирован: tick, patch-send, join,
// lazy RPC), иначе откаченный переход дал бы ложное "episode over".
func (m *masterMachine) assigned() {
	if m.peer == nil || !m.masterlessLogged {
		return
	}
	m.logger.Info("master elected, masterless episode over", "peerID", m.peer.GetID())
	m.masterlessLogged = false
}

// peerID - текущий master или PeerIDInvalid при masterless.
func (m *masterMachine) peerID() domain.PeerID {
	if m.peer == nil {
		return domain.PeerIDInvalid
	}
	return m.peer.GetID()
}

// isMaster - true, если peerID - текущий master policy.
func (m *masterMachine) isMaster(peerID domain.PeerID) bool {
	return m.peer != nil && m.peer.GetID() == peerID
}

// current - снапшот master для stage-then-commit транзакций.
func (m *masterMachine) current() realtime.Peer {
	return m.peer
}

// restore - откат staged-перехода при сорванном commit.
func (m *masterMachine) restore(master realtime.Peer) {
	m.peer = master
}
