package transport

import "github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"

type Connection interface {
	Send(frame domain.Frame) error
	Receive() (domain.Frame, error)
	Close() error
	// Ping returns round-trip time in milliseconds, or -1 if unknown.
	Ping() int64
	// IsAlive возвращает false, если транспорт мёртв и peer подлежит idle eviction;
	// каждая реализация транспорта сама решает, что считается живостью.
	IsAlive() bool
}
