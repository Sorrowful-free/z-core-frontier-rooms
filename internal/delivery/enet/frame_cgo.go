//go:build enet && cgo

package enet

import (
	transportenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/enet"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	libenet "github.com/codecat/go-enet"
)

// frameFromPacket - packet reads are dispatched through the loop's queue: the
// function is called from the run goroutine, while only the loop goroutine
// may touch the packet's C state.
func frameFromPacket(loop *transportenet.HostLoop, packet libenet.Packet, maxIncomingFrameBytes int) (domain.Frame, error) {
	var data []byte
	var flags libenet.PacketFlags
	_ = loop.Do(func() error {
		data = packet.GetData()
		flags = packet.GetFlags()
		return nil
	})
	frame, err := domain.DecodeIncomingFrame(data, maxIncomingFrameBytes)
	if err != nil {
		return domain.Frame{}, err
	}
	frame.Delivery = deliveryFromPacketFlags(flags)
	return frame, nil
}
