//go:build enet && cgo

package enet

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	libenet "github.com/codecat/go-enet"
)

func frameFromPacket(packet libenet.Packet, maxIncomingFrameBytes int) (domain.Frame, error) {
	data := packet.GetData()
	frame, err := domain.DecodeIncomingFrame(data, maxIncomingFrameBytes)
	if err != nil {
		return domain.Frame{}, err
	}
	if packet.GetFlags() != 0 {
		frame.Delivery = deliveryFromPacketFlags(packet.GetFlags())
	}
	return frame, nil
}
