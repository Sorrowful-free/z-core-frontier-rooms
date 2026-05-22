//go:build enet && cgo

package enet

import (
	"errors"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	libenet "github.com/codecat/go-enet"
)

var errEmptyPacket = errors.New("enet: empty packet")

func frameFromPacket(packet libenet.Packet) (domain.Frame, error) {
	data := packet.GetData()
	if len(data) == 0 {
		return domain.Frame{}, errEmptyPacket
	}
	return domain.Frame{
		OpCode:   domain.OpCode(data[0]),
		Delivery: deliveryFromPacketFlags(packet.GetFlags()),
		Payload:  domain.Payload(append([]byte(nil), data[1:]...)),
	}, nil
}
