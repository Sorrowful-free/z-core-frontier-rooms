//go:build !enet || !cgo

package enet

import "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"

type EnetConnectionFactory interface {
	CreateConnection() transport.Connection
}
