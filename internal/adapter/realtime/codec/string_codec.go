package codec

import (
	"bytes"
	"fmt"
)

func writeString(buf *bytes.Buffer, s string) error {
	strlen := len(s)
	if strlen > 255 {
		return fmt.Errorf("string too long: %d", strlen)
	}
	buf.WriteByte(byte(strlen))
	buf.Write([]byte(s))
	return nil
}

func readString(buf *bytes.Buffer) (string, error) {
	strlen, err := buf.ReadByte()
	if err != nil {
		return "", err
	}
	data := make([]byte, strlen)
	if _, err := buf.Read(data); err != nil {
		return "", err
	}
	return string(data), nil
}
