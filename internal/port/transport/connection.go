package transport

import "github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"

type Connection interface {
	Send(frame domain.Frame) error
	Receive() (domain.Frame, error)
	Close() error
}
