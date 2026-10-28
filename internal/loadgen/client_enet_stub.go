//go:build !enet || !cgo

package loadgen

import (
	"context"
	"fmt"
)

// dialEnet — stub для сборок без `-tags enet` (нет cgo/libenet): enet-доля
// TRANSPORT_MIX падает на dial и считается в dial_errors{transport=enet}.
// Реализация — client_enet.go (P27 inc3).
func dialEnet(_ context.Context, _ DialParams) (DataConn, error) {
	return nil, fmt.Errorf("transport enet: %w", ErrEnetUnsupported)
}
