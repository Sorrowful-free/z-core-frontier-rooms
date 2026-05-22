//go:build enet && cgo

package enet

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	libenet "github.com/codecat/go-enet"
)

func deliveryFromPacketFlags(flags libenet.PacketFlags) domain.Delivery {
	if flags&libenet.PacketFlagReliable != 0 {
		return domain.DeliveryReliable
	}
	return domain.DeliveryUnreliable
}
