package domain

import "errors"

// ErrIncomingFrameTooLarge — входящий кадр (OpCode + payload) превышает лимит data plane.
var ErrIncomingFrameTooLarge = errors.New("incoming frame too large")

// DecodeIncomingFrame разбирает сырой кадр data plane: OpCode (1 байт) + payload.
// max > 0 — верхняя граница len(data); 0 — без лимита.
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
