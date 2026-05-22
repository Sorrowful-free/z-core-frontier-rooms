package errors

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
)

func sendErrorFrame(conn transport.Connection, op domain.OpCode) {
	_ = conn.Send(domain.Frame{
		OpCode:   op,
		Delivery: domain.DeliveryReliable,
		Payload:  nil,
	})
}

// IsJoinRejectOpCode reports whether op belongs to the join/admit reject range (0x40–0x5F).
func IsJoinRejectOpCode(op domain.OpCode) bool {
	return op >= 0x40 && op <= 0x5F
}

// IsInRoomErrorOpCode reports whether op belongs to the in-room error range (0x60–0x6F).
func IsInRoomErrorOpCode(op domain.OpCode) bool {
	return op >= 0x60 && op <= 0x6F
}
