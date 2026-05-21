package domain

type Frame struct {
	OpCode   OpCode
	Delivery Delivery
	Payload  Payload
}
