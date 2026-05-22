//go:build !enet || !cgo

package enet

import (
	"io"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

type EnetConnection struct {
}

func NewEnetConnection() *EnetConnection {
	return &EnetConnection{}
}

func (c *EnetConnection) Send(_ domain.Frame) error {
	return nil
}

func (c *EnetConnection) Receive() (domain.Frame, error) {
	return domain.Frame{}, io.EOF
}

func (c *EnetConnection) Close() error {
	return nil
}
