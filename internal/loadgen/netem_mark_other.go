//go:build !linux

package loadgen

import (
	"fmt"
	"syscall"
)

// setSockMark — SO_MARK есть только на Linux (контейнерный netem-режим);
// на прочих ОС share-профили не поддерживаются.
func setSockMark(_ syscall.RawConn, mark uint32) error {
	return fmt.Errorf("SO_MARK unsupported on this platform (mark=%d)", mark)
}
