package domain

type RoomID uint32

const (
	RoomIDInvalid RoomID = 0
)

func (r RoomID) IsValid() bool {
	return r > 0
}

type PeerID uint32

const (
	PeerIDInvalid PeerID = 0
)

func (p PeerID) IsValid() bool {
	return p > 0
}

type OpCode byte
type Delivery byte
type Payload []byte

const (
	DeliveryDefault    Delivery = 0
	DeliveryReliable   Delivery = 1
	DeliveryUnreliable Delivery = 2
)
