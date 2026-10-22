package codec

import (
	"bytes"
	"fmt"
)

// checkNoTrailingBytes rejects payloads with bytes left after the parsed value.
func checkNoTrailingBytes(buf *bytes.Buffer) error {
	if buf.Len() > 0 {
		return fmt.Errorf("trailing bytes: %d", buf.Len())
	}
	return nil
}
