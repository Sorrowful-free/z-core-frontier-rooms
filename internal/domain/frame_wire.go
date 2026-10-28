package domain

import "errors"

// ErrIncomingFrameTooLarge — incoming frame (OpCode + payload) exceeds the data plane limit.
var ErrIncomingFrameTooLarge = errors.New("incoming frame too large")

// DecodeIncomingFrame parses a raw data plane frame: OpCode (1 byte) + payload.
// max > 0 — upper bound on len(data); 0 — no limit.
func DecodeIncomingFrame(data []byte, max int) (Frame, error) {
	if max > 0 && len(data) > max {
		return Frame{}, ErrIncomingFrameTooLarge
	}
	if len(data) == 0 {
		return Frame{}, errors.New("empty frame")
	}
	payload := make([]byte, len(data)-1)
	copy(payload, data[1:])
	return Frame{
		OpCode:   OpCode(data[0]),
		Delivery: DeliveryDefault,
		Payload:  Payload(payload),
	}, nil
}
