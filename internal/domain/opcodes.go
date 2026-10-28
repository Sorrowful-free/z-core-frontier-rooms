package domain

import "encoding/binary"

// OpCode groups (1 byte):
//   0x01–0x3F — game traffic / relay (reserved)
//   0x40–0x4F — join / admit errors (client maps code → UI)
//   0x50–0x5F — internal server errors during join
//   0x60–0x6F — in-room errors (after admit)
//   0x70–0x7F — reservation errors (slot reservation / admit)
//   0x80+ — room notifications (not errors, P20): peer in grace / returned,
//   payload — peerID u64 BE; 0x82 OpRoomClosed (P29) — room closed by the
//   server, empty payload, a disconnect follows the frame.
// Payload for error codes (join, reservation, in-room): empty.

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

	// In-room (0x60–0x6F) — after admit; extend as scenarios appear.
	OpInRoomInternal        OpCode = 0x60
	OpInRoomNotMaster       OpCode = 0x61
	OpInRoomInvalidPayload  OpCode = 0x62
	OpInRoomInvalidRpc      OpCode = 0x63
	OpInRoomRpcPeerNotFound OpCode = 0x64
	OpInRoomNoMaster        OpCode = 0x65
	OpInRoomUnknownOpcode   OpCode = 0x66

	// Reservation (0x70–0x7F) — control plane / slot-based join admit.
	OpReservationNotFound        OpCode = 0x70
	OpReservationFull            OpCode = 0x71
	OpReservationSlotHeld        OpCode = 0x72
	OpReservationNotReserved     OpCode = 0x73
	OpReservationExpired         OpCode = 0x74
	OpReservationAlreadyAdmitted OpCode = 0x75
	OpPeerAlreadyInRoom          OpCode = 0x76
	// OpWrongHost - admit on an ENet host not assigned by the ticket (claims.hostID, P18).
	OpWrongHost           OpCode = 0x77
	OpReservationInternal OpCode = 0x7F

	// Room notifications (0x80+): sent by the use case via room.Send —
	// a peer went into grace (may return) / revived under the same peerID (P20).
	OpPeerDisconnected OpCode = 0x80
	OpPeerReconnected  OpCode = 0x81
	// OpRoomClosed - room closed by the server. Sources (P31):
	// no-migration deploy (P33), host left; DELETE /rooms/:id; process
	// shutdown. Empty payload; the connection is closed after the frame.
	// Sent by the usecase via peer.Deliver to the remaining peers before
	// their disconnect (an empty-death has no one to listen — no frame).
	OpRoomClosed OpCode = 0x82
)

// NewPeerNotificationFrame — room-notification frame (0x80+): payload —
// peerID u64 BE, reliable delivery (the "state patch → opcode" order matters).
func NewPeerNotificationFrame(op OpCode, peerID PeerID) Frame {
	payload := make([]byte, 8)
	binary.BigEndian.PutUint64(payload, uint64(peerID))
	return Frame{OpCode: op, Delivery: DeliveryReliable, Payload: payload}
}
