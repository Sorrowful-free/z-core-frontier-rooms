package codec

import (
	"bytes"
	"encoding/binary"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

const (
	RpcStateMaxValues = 32
)

type RpcStateCodec struct {
}

func writeRpcID(buf *bytes.Buffer, id state.RpcID) error {
	return binary.Write(buf, binary.BigEndian, id)
}

func readRpcID(buf *bytes.Buffer) (state.RpcID, error) {
	var id state.RpcID
	if err := binary.Read(buf, binary.BigEndian, &id); err != nil {
		return state.RpcIDNone, err
	}
	return id, nil
}

func writeRpcTarget(buf *bytes.Buffer, target state.RpcTarget) error {
	return binary.Write(buf, binary.BigEndian, target)
}

func readRpcTarget(buf *bytes.Buffer) (state.RpcTarget, error) {
	var target state.RpcTarget
	if err := binary.Read(buf, binary.BigEndian, &target); err != nil {
		return state.RpcTargetNone, err
	}
	return target, nil
}

func (c *RpcStateCodec) Encode(rpc *state.RpcState) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 128))
	if err := writeRpcID(buf, rpc.ID); err != nil {
		return nil, err
	}
	if err := writeRpcTarget(buf, rpc.Target); err != nil {
		return nil, err
	}
	if err := writeMapState(buf, rpc.Values, func(k state.ValueId, buf *bytes.Buffer) error {
		return binary.Write(buf, binary.BigEndian, k)
	}, func(v *state.ValueState, buf *bytes.Buffer) error {
		return writeValueState(buf, v)
	}, RpcStateMaxValues); err != nil {
		return nil, err
	}
	if err := writePeerID(buf, rpc.PeerID); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (c *RpcStateCodec) Decode(data []byte) (*state.RpcState, error) {
	buf := bytes.NewBuffer(data)
	id, err := readRpcID(buf)
	if err != nil {
		return nil, err
	}
	target, err := readRpcTarget(buf)
	if err != nil {
		return nil, err
	}
	values, err := readMapState(buf, func(buf *bytes.Buffer) (state.ValueId, error) {
		return readValueID(buf)
	}, func(buf *bytes.Buffer) (*state.ValueState, error) {
		return readValueState(buf)
	}, RpcStateMaxValues)
	if err != nil {
		return nil, err
	}

	peerID, err := readPeerID(buf)
	if err != nil {
		return nil, err
	}
	return &state.RpcState{ID: id, Target: target, Values: values, PeerID: peerID}, nil
}
