package domain

import "encoding/binary"

// Группы OpCode (1 байт):
//   0x01–0x3F — игровой трафик / relay (зарезервировано)
//   0x40–0x4F — ошибки join / admit (клиент мапит код → UI)
//   0x50–0x5F — внутренние ошибки сервера при join
//   0x60–0x6F — ошибки внутри комнаты (после admit)
//   0x70–0x7F — ошибки reservation (бронь / admit слота)
//   0x80+ — room notifications (не ошибки, P20): peer в grace / вернулся;
//   payload — peerID u64 BE.
// Payload для кодов ошибок (join, reservation, in-room): пустой.

const (
	OpEmptyToken    OpCode = 0x40
	OpInvalidToken  OpCode = 0x41
	OpExpiredToken  OpCode = 0x42
	OpRoomNotFound  OpCode = 0x43
	OpJoinDenied    OpCode = 0x44
	OpReplaceFailed OpCode = 0x45
	OpPeerNotFound  OpCode = 0x46

	OpInternal        OpCode = 0x50
	OpPeerStartFailed OpCode = 0x51

	// In-room (0x60–0x6F) — после admit; расширять по мере появления сценариев.
	OpInRoomInternal        OpCode = 0x60
	OpInRoomNotMaster       OpCode = 0x61
	OpInRoomInvalidPayload  OpCode = 0x62
	OpInRoomInvalidRpc      OpCode = 0x63
	OpInRoomRpcPeerNotFound OpCode = 0x64
	OpInRoomNoMaster        OpCode = 0x65
	OpInRoomUnknownOpcode   OpCode = 0x66

	// Reservation (0x70–0x7F) — control plane / join admit по слоту.
	OpReservationNotFound        OpCode = 0x70
	OpReservationFull            OpCode = 0x71
	OpReservationSlotHeld        OpCode = 0x72
	OpReservationNotReserved     OpCode = 0x73
	OpReservationExpired         OpCode = 0x74
	OpReservationAlreadyAdmitted OpCode = 0x75
	OpPeerAlreadyInRoom          OpCode = 0x76
	// OpWrongHost - admit на ENet host'е, не назначенном ticket'ом (claims.hostID, P18).
	OpWrongHost           OpCode = 0x77
	OpReservationInternal OpCode = 0x7F

	// Room notifications (0x80+): рассылает use case через room.Send —
	// peer ушёл в grace (может вернуться) / воскрес под тем же peerID (P20).
	OpPeerDisconnected OpCode = 0x80
	OpPeerReconnected  OpCode = 0x81
)

// NewPeerNotificationFrame — кадр room-notification (0x80+): payload —
// peerID u64 BE, доставка reliable (порядок «state-патч → opcode» важен).
func NewPeerNotificationFrame(op OpCode, peerID PeerID) Frame {
	payload := make([]byte, 8)
	binary.BigEndian.PutUint64(payload, uint64(peerID))
	return Frame{OpCode: op, Delivery: DeliveryReliable, Payload: payload}
}
