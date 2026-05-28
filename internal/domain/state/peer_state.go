package state

import (
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

type PeerState struct {
	ID       domain.PeerID
	NickName string
	Ping     int64
}

type PeerStatePatch struct {
	ID       domain.PeerID
	NickName *string
	Ping     *int64
}

func (p *PeerState) Equals(other *PeerState) bool {
	return p.ID == other.ID && p.NickName == other.NickName && p.Ping == other.Ping
}

func FromPeerStateToPatch(peerState *PeerState) *PeerStatePatch {
	return &PeerStatePatch{
		ID:       peerState.ID,
		NickName: &peerState.NickName,
		Ping:     &peerState.Ping,
	}
}

func (p *PeerState) MakePatch(newPeerState *PeerState) (*PeerStatePatch, error) {
	if !newPeerState.ID.IsValid() || newPeerState.ID != p.ID {
		return nil, fmt.Errorf("peer id mismatch: %d != %d", newPeerState.ID, p.ID)
	}

	hasChanges := false

	patch := &PeerStatePatch{
		ID: p.ID,
	}
	if p.NickName != newPeerState.NickName {
		patch.NickName = &newPeerState.NickName
		hasChanges = true
	}
	if p.Ping != newPeerState.Ping {
		patch.Ping = &newPeerState.Ping
		hasChanges = true
	}
	if !hasChanges {
		return nil, nil
	}
	return patch, nil
}

func (p *PeerState) ApplyPatch(patch PeerStatePatch) error {
	if !patch.ID.IsValid() || patch.ID != p.ID {
		return fmt.Errorf("invalid peer id: %d", patch.ID)
	}
	if patch.NickName != nil && *patch.NickName != p.NickName {
		p.NickName = *patch.NickName
	}
	if patch.Ping != nil && *patch.Ping != p.Ping {
		p.Ping = *patch.Ping
	}
	return nil
}
