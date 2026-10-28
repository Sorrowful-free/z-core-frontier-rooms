//go:build !enet || !cgo

package loadgen

import (
	"context"
	"fmt"
)

// dialEnet — stub for builds without `-tags enet` (no cgo/libenet): the
// enet share of TRANSPORT_MIX fails on dial and is counted in
// dial_errors{transport=enet}. Implementation — client_enet.go (P27 inc3).
func dialEnet(_ context.Context, _ DialParams) (DataConn, error) {
	return nil, fmt.Errorf("transport enet: %w", ErrEnetUnsupported)
}
