package transport

import "github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"

type Connection interface {
	Send(frame domain.Frame) error
	Receive() (domain.Frame, error)
	Close() error
	// Ping returns round-trip time in milliseconds, or -1 if unknown.
	Ping() int64
	// IsAlive returns false if the transport is dead and the peer is subject
	// to idle eviction; each transport implementation decides what counts as alive.
	IsAlive() bool
}
