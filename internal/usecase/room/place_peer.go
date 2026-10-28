package room

import (
	"errors"
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
)

// placePeerJoin — tail of the join flow (P20): ErrPeerAlreadyInRoom from
// Join is rejected — an occupied peerID is legitimately unreachable for a
// join ticket.
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

// placePeerResume — tail of the resume flow (P20): ErrPeerAlreadyInRoom
// from Join is a lost double-resume race on the same reconnect token — the
// winner already placed its peer; we follow through with Replace —
// newest-wins (Q10), the displaced object is stopped via old.Stop() inside
// Replace.
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

// placePeer — shared tail of the join/resume admit flows (P20): CreatePeer →
// placement (Replace over a live peer | Join) → Start with Leave+Stop
// rollback → PeerSession. The fate of the reservation slot on error is the
// caller's concern (join revokes it, resume does not). Callers go through
// the thin wrappers placePeerJoin/placePeerResume — the raceReplace flag is
// not passed from outside.
func placePeer(
	peerFactory realtime.PeerFactory,
	room realtime.Room,
	connection transport.Connection,
	peerID domain.PeerID,
	nickName string,
	logger logging.Logger,
	raceReplace bool,
) (PeerSession, error) {
	peer, err := peerFactory.CreatePeer(room.Context(), peerID, nickName, connection, logger)
	if err != nil {
		return PeerSession{}, err
	}

	// The newest connection always wins (Q10): a live peer → Replace
	// (the old conn will later hit the teardown identity check), none →
	// Join.
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

	if err := peer.Start(room); err != nil {
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
