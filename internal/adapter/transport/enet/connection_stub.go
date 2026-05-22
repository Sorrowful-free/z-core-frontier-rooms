//go:build !enet || !cgo

package enet

import (
	"io"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

type EnetConnection struct {
	incoming chan domain.Frame
}

func NewEnetConnection(_ any, incoming chan domain.Frame) *EnetConnection {
	return &EnetConnection{incoming: incoming}
}

func (c *EnetConnection) Send(_ domain.Frame) error {
	return nil
}

func (c *EnetConnection) Receive() (domain.Frame, error) {
	frame, ok := <-c.incoming
	if !ok {
		return domain.Frame{}, io.EOF
	}
	return frame, nil
}

func (c *EnetConnection) Close() error {
	if c.incoming != nil {
		close(c.incoming)
	}
	return nil
}
