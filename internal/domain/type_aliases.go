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

// HostID - identifier of the process's ENet host (shard): index+1 in the
// EnetConfig.ListenPorts list. Not a port and not a transport detail:
// ticket claims carry HostID for strict binding admit (P18).
type HostID uint16

const (
	// HostIDUnbound - no host binding: the ticket was issued without
	// HostRegistry (WS-only deploy); ENet admit rejects it (ErrWrongHost).
	HostIDUnbound HostID = 0
)

func (h HostID) IsValid() bool {
	return h > 0
}

type OpCode byte
type Delivery byte
type Payload []byte

const (
	DeliveryDefault    Delivery = 0
	DeliveryReliable   Delivery = 1
	DeliveryUnreliable Delivery = 2
)
