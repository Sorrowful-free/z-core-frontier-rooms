//go:build !linux

package loadgen

import (
	"fmt"
	"syscall"
)

// setSockMark — SO_MARK exists only on Linux (the containerized netem
// mode); share profiles are unsupported on other OSes.
func setSockMark(_ syscall.RawConn, mark uint32) error {
	return fmt.Errorf("SO_MARK unsupported on this platform (mark=%d)", mark)
}
