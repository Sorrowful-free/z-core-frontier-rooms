package state

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

type RpcID uint32

type RpcTarget byte

const (
	RpcTargetNone   RpcTarget = 0
	RpcTargetPeer   RpcTarget = 1
	RpcTargetMaster RpcTarget = 2
	RpcTargetAll    RpcTarget = 3
)

const (
	RpcIDNone RpcID = 0
)

func (r RpcID) IsValid() bool {
	return r != RpcIDNone
}

type RpcState struct {
	ID     RpcID
	Target RpcTarget
	Values []ValueState

	PeerID *domain.PeerID
}
