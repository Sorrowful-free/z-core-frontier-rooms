//go:build linux

package loadgen

import "syscall"

// setSockMark — SO_MARK на сокете через RawConn.Control (Linux).
func setSockMark(c syscall.RawConn, mark uint32) error {
	var sockErr error
	err := c.Control(func(fd uintptr) {
		sockErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_MARK, int(mark))
	})
	if err != nil {
		return err
	}
	return sockErr
}
