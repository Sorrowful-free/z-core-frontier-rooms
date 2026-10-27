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

// HostID - идентификатор ENet host'а (shard) процесса: индекс+1 в списке
// EnetConfig.ListenPorts. Не порт и не транспортная деталь: ticket claims
// несут HostID для strict binding admit (P18).
type HostID uint16

const (
	// HostIDUnbound - отсутствие привязки к host'у: ticket выписан без
	// HostRegistry (WS-only deploy); на ENet-admit отклоняется (ErrWrongHost).
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
