package codec_test

import (
	"bytes"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/codec"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
)

type nopLogger struct{}

func (nopLogger) Error(string, ...any) {}
func (nopLogger) Info(string, ...any)  {}
func (nopLogger) Debug(string, ...any) {}
func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Fatal(string, ...any) {}

func newRoomCodec() *codec.RoomStateCodec {
	var log logging.Logger = nopLogger{}
	return codec.NewRoomStateCodec(&log)
}

func assertNoTrailing(t *testing.T, data []byte) {
	t.Helper()
	if len(data) != 0 {
		t.Fatalf("trailing payload bytes: %d", len(data))
	}
}

func valueEqual(a, b state.ValueState) bool {
	return a.Equals(&b)
}

func inputEqual(a, b *state.InputState) bool {
	return a.Equals(b, valueEqual)
}

func bytesReader(data []byte) *bytes.Buffer {
	return bytes.NewBuffer(data)
}
