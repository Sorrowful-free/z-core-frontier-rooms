package codec

import (
	"bytes"
	"encoding/binary"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

const (
	PeerStateFlagsNickName = 1 << iota
	PeerStateFlagsPing
)

type PeerStateCodec struct {
}

func (c *PeerStateCodec) Encode(peer *state.PeerState) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 128))
	if err := writeString(buf, peer.NickName); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.BigEndian, peer.Ping); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (c *PeerStateCodec) Decode(data []byte) (*state.PeerState, error) {
	buf := bytes.NewBuffer(data)
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

func (c *PeerStateCodec) EncodePatch(patch *state.PeerStatePatch) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 128))
	flags := byte(0)
	if patch.NickName != nil {
		flags |= PeerStateFlagsNickName
	}
	if patch.Ping != nil {
		flags |= PeerStateFlagsPing
	}

	if err := binary.Write(buf, binary.BigEndian, flags); err != nil {
		return nil, err
	}
	if patch.NickName != nil {
		if err := writeString(buf, *patch.NickName); err != nil {
			return nil, err
		}
	}
	if patch.Ping != nil {
		if err := binary.Write(buf, binary.BigEndian, patch.Ping); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func (c *PeerStateCodec) DecodePatch(data []byte) (*state.PeerStatePatch, error) {
	buf := bytes.NewBuffer(data)
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
