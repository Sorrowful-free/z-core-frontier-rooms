//go:build !enet || !cgo

package enet

import "context"

func (h *RoomsHandler) run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	h.logger.Warn("enet delivery disabled: build with -tags enet and CGO_ENABLED=1")
	return nil
}
