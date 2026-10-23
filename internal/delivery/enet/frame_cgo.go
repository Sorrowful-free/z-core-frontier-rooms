//go:build enet && cgo

package enet

import (
	transportenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/enet"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	libenet "github.com/codecat/go-enet"
)

// frameFromPacket - packet-чтения диспетчатся через очередь loop'а: функция
// зовётся с run-горутины, а C-состояние packet'а трогает только loop-горутина.
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
