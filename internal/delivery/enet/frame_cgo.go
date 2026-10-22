//go:build enet && cgo

package enet

import (
	transportenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/enet"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	libenet "github.com/codecat/go-enet"
)

func frameFromPacket(packet libenet.Packet, maxIncomingFrameBytes int) (domain.Frame, error) {
	var data []byte
	var flags libenet.PacketFlags
	transportenet.Do(func() {
		data = packet.GetData()
		flags = packet.GetFlags()
	})
	frame, err := domain.DecodeIncomingFrame(data, maxIncomingFrameBytes)
	if err != nil {
		return domain.Frame{}, err
	}
	frame.Delivery = deliveryFromPacketFlags(flags)
	return frame, nil
}
