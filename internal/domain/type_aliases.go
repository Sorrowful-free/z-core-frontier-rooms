package domain

type RoomID int64

const (
	RoomIDInvalid RoomID = 0
)

type PeerID int64

const (
	PeerIDInvalid PeerID = 0
)

type OpCode byte
type Delivery byte
type Payload []byte

func (r RoomID) IsValid() bool {
	return r > 0
}

func (p PeerID) IsValid() bool {
	return p > 0
}
