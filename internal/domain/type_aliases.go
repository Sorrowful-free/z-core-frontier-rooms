package domain

type RoomID int64
type UserID int64
type PeerID int64

type OpCode byte
type Delivery byte
type Payload []byte

func (p PeerID) IsValid() bool {
	return p > 0
}
