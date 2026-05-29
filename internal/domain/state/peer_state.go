package state

type PeerState struct {
	NickName string
	Ping     int64
}

type PeerStatePatch struct {
	NickName *string
	Ping     *int64
}

func (p *PeerState) Equals(other *PeerState) bool {
	return p.NickName == other.NickName && p.Ping == other.Ping
}

func FromPeerStateToPatch(peerState *PeerState) *PeerStatePatch {
	return &PeerStatePatch{
		NickName: &peerState.NickName,
		Ping:     &peerState.Ping,
	}
}

func (p *PeerState) MakePatch(newPeerState *PeerState) (*PeerStatePatch, error) {
	hasChanges := false

	patch := &PeerStatePatch{}
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
	if patch.NickName != nil && *patch.NickName != p.NickName {
		p.NickName = *patch.NickName
	}
	if patch.Ping != nil && *patch.Ping != p.Ping {
		p.Ping = *patch.Ping
	}
	return nil
}
