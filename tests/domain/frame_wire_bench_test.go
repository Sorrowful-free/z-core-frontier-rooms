package domain_test

import (
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

func makeIncomingFrame(payloadLen int) []byte {
	data := make([]byte, 1+payloadLen)
	data[0] = 0x01
	return data
}

func BenchmarkDecodeIncomingFrame(b *testing.B) {
	cases := []struct {
		name       string
		payloadLen int
	}{
		{name: "payload=2B", payloadLen: 2},
		{name: "payload=1KiB", payloadLen: 1024},
		{name: "payload=64KiB", payloadLen: 64 * 1024},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			data := makeIncomingFrame(tc.payloadLen)
			var sink domain.Frame
			b.ResetTimer()
			for b.Loop() {
				var err error
				sink, err = domain.DecodeIncomingFrame(data, 0)
				if err != nil {
					b.Fatal(err)
				}
			}
			_ = sink
		})
	}
}
