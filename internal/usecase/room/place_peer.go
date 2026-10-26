package room

import (
	"errors"
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
)

// placePeerJoin — хвост join-потока (P20): ErrPeerAlreadyInRoom от Join
// отклоняется — занятый peerID для join-тикета легально недостижим.
func placePeerJoin(
	peerFactory realtime.PeerFactory,
	room realtime.Room,
	connection transport.Connection,
	peerID domain.PeerID,
	nickName string,
	logger logging.Logger,
) (PeerSession, error) {
	return placePeer(peerFactory, room, connection, peerID, nickName, logger, false)
}

// placePeerResume — хвост resume-потока (P20): ErrPeerAlreadyInRoom от
// Join — это проигрыш гонки double-resume одного v4-токена, победитель
// уже вставил peer'а; дожимаем Replace — newest-wins (Q10), вытесненный
// объект остановит old.Stop() внутри Replace.
func placePeerResume(
	peerFactory realtime.PeerFactory,
	room realtime.Room,
	connection transport.Connection,
	peerID domain.PeerID,
	nickName string,
	logger logging.Logger,
) (PeerSession, error) {
	return placePeer(peerFactory, room, connection, peerID, nickName, logger, true)
}

// placePeer — общий хвост admit-потоков join/resume (P20): CreatePeer →
// размещение (Replace по живому peer'у | Join) → Start с rollback'ом
// Leave+Stop → PeerSession. Судьба слота reservation при ошибке — на
// caller'е (join ревокает, resume — нет). Вызывающие идут через тонкие
// wrapper'ы placePeerJoin/placePeerResume — raceReplace-флаг не
// передаётся снаружи.
func placePeer(
	peerFactory realtime.PeerFactory,
	room realtime.Room,
	connection transport.Connection,
	peerID domain.PeerID,
	nickName string,
	logger logging.Logger,
	raceReplace bool,
) (PeerSession, error) {
	peer, err := peerFactory.CreatePeer(room.Context(), peerID, nickName, connection, room, logger)
	if err != nil {
		return PeerSession{}, err
	}

	// Новейшее соединение всегда побеждает (Q10): живой peer — Replace
	// (старый conn потом встретит identity-check teardown'а), нет — Join.
	if room.HasPeer(peerID) {
		if err := room.Replace(peer); err != nil {
			logger.Error("place peer: replace failed", "error", err, "peerID", peerID)
			return PeerSession{}, err
		}
		logger.Info("peer replaced", "peerID", peerID)
	} else if err := room.Join(peer); err != nil {
		if !raceReplace || !errors.Is(err, domain.ErrPeerAlreadyInRoom) {
			logger.Error("place peer: join failed", "error", err, "peerID", peerID)
			return PeerSession{}, err
		}
		if replaceErr := room.Replace(peer); replaceErr != nil {
			logger.Error("place peer: replace after join race failed", "error", replaceErr, "peerID", peerID)
			return PeerSession{}, replaceErr
		}
		logger.Info("peer replaced after join race", "peerID", peerID)
	}

	if err := peer.Start(); err != nil {
		if leaveErr := room.Leave(peer); leaveErr != nil {
			logger.Error("place peer: rollback leave failed", "error", leaveErr, "peerID", peerID)
		}
		if stopErr := peer.Stop(); stopErr != nil {
			logger.Error("place peer: rollback peer stop failed", "error", stopErr, "peerID", peerID)
		}
		startErr := fmt.Errorf("%w: %w", domain.ErrPeerStartFailed, err)
		logger.Error("place peer: start failed", "error", startErr, "peerID", peerID)
		return PeerSession{}, startErr
	}

	return PeerSession{RoomID: room.GetID(), PeerID: peerID, Peer: peer}, nil
}
