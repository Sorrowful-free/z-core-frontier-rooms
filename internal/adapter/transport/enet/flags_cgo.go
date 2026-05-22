//go:build enet && cgo

package enet

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	libenet "github.com/codecat/go-enet"
)

func packetFlagsFromDelivery(d domain.Delivery) libenet.PacketFlags {
	switch d {
	case domain.DeliveryReliable:
		return libenet.PacketFlagReliable
	case domain.DeliveryUnreliable:
		return 0
	default:
		return libenet.PacketFlagReliable
	}
}
