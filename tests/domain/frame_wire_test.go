package domain_test

import (
	"errors"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

func TestDecodeIncomingFrame_Success(t *testing.T) {
	t.Parallel()

	frame, err := domain.DecodeIncomingFrame([]byte{0x01, 0xAA, 0xBB}, 256)
	if err != nil {
		t.Fatalf("DecodeIncomingFrame: %v", err)
	}
	if frame.OpCode != domain.OpCode(0x01) {
		t.Fatalf("opcode = %#x", frame.OpCode)
	}
	if len(frame.Payload) != 2 || frame.Payload[0] != 0xAA {
		t.Fatalf("payload = %v", frame.Payload)
	}
}

func TestDecodeIncomingFrame_Empty(t *testing.T) {
	t.Parallel()

	_, err := domain.DecodeIncomingFrame(nil, 256)
	if err == nil {
		t.Fatal("expected error for empty frame")
	}
}

func TestDecodeIncomingFrame_TooLarge(t *testing.T) {
	t.Parallel()

	data := make([]byte, 5)
	data[0] = 0x02

	_, err := domain.DecodeIncomingFrame(data, 4)
	if !errors.Is(err, domain.ErrIncomingFrameTooLarge) {
		t.Fatalf("err = %v, want ErrIncomingFrameTooLarge", err)
	}
}

func TestDecodeIncomingFrame_NoLimit(t *testing.T) {
	t.Parallel()

	data := make([]byte, 1024)
	data[0] = 0x03

	frame, err := domain.DecodeIncomingFrame(data, 0)
	if err != nil {
		t.Fatalf("DecodeIncomingFrame: %v", err)
	}
	if len(frame.Payload) != 1023 {
		t.Fatalf("payload len = %d", len(frame.Payload))
	}
}
