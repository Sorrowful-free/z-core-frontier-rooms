package state

type PeerState struct {
	NickName string
	IsMaster bool
	Ping     int64
}

type PeerStatePatch struct {
	NickName *string
	IsMaster *bool
	Ping     *int64
}

func (p *PeerState) Equals(other *PeerState) bool {
	return p.NickName == other.NickName && p.IsMaster == other.IsMaster && p.Ping == other.Ping
}

func FromPeerStateToPatch(peerState *PeerState) *PeerStatePatch {
	return &PeerStatePatch{
		NickName: &peerState.NickName,
		IsMaster: &peerState.IsMaster,
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
	if p.IsMaster != newPeerState.IsMaster {
		patch.IsMaster = &newPeerState.IsMaster
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
	if patch.IsMaster != nil && *patch.IsMaster != p.IsMaster {
		p.IsMaster = *patch.IsMaster
	}
	if patch.Ping != nil && *patch.Ping != p.Ping {
		p.Ping = *patch.Ping
	}
	return nil
}
