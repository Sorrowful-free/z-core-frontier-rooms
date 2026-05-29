package codec

import (
	"bytes"
	"encoding/binary"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

const (
	PeerStateFlagsNickName = 1 << iota
	PeerStateFlagsPing
)

func writePeerID(buf *bytes.Buffer, id domain.PeerID) error {
	return binary.Write(buf, binary.BigEndian, id)
}

func readPeerID(buf *bytes.Buffer) (domain.PeerID, error) {
	var id domain.PeerID
	if err := binary.Read(buf, binary.BigEndian, &id); err != nil {
		return domain.PeerIDInvalid, err
	}
	return id, nil
}

func writePeerState(buf *bytes.Buffer, peer *state.PeerState) error {
	if err := writeString(buf, peer.NickName); err != nil {
		return err
	}
	if err := binary.Write(buf, binary.BigEndian, peer.Ping); err != nil {
		return err
	}
	return nil
}

func readPeerState(buf *bytes.Buffer) (*state.PeerState, error) {
	nickName, err := readString(buf)
	if err != nil {
		return nil, err
	}
	var ping int64
	if err := binary.Read(buf, binary.BigEndian, &ping); err != nil {
		return nil, err
	}
	return &state.PeerState{NickName: nickName, Ping: ping}, nil
}

func writePeerStatePatch(buf *bytes.Buffer, patch *state.PeerStatePatch) error {
	flags := byte(0)
	if patch.NickName != nil {
		flags |= PeerStateFlagsNickName
	}
	if patch.Ping != nil {
		flags |= PeerStateFlagsPing
	}

	if err := binary.Write(buf, binary.BigEndian, flags); err != nil {
		return err
	}
	if patch.NickName != nil {
		if err := writeString(buf, *patch.NickName); err != nil {
			return err
		}
	}
	if patch.Ping != nil {
		if err := binary.Write(buf, binary.BigEndian, *patch.Ping); err != nil {
			return err
		}
	}
	return nil
}

func readPeerStatePatch(buf *bytes.Buffer) (*state.PeerStatePatch, error) {
	var flags byte
	if err := binary.Read(buf, binary.BigEndian, &flags); err != nil {
		return nil, err
	}
	var nickName *string
	if flags&PeerStateFlagsNickName == PeerStateFlagsNickName {
		n, err := readString(buf)
		if err != nil {
			return nil, err
		}
		nickName = &n
	}
	var ping *int64
	if flags&PeerStateFlagsPing == PeerStateFlagsPing {
		var p int64
		if err := binary.Read(buf, binary.BigEndian, &p); err != nil {
			return nil, err
		}
		ping = &p
	}
	return &state.PeerStatePatch{NickName: nickName, Ping: ping}, nil
}
