//go:build enet && cgo

package enet

import libenet "github.com/codecat/go-enet"

// Peer — tagged alias на тип peer из go-enet. Под enet+cgo это libenet.Peer,
// в stub-сборке (peer_stub.go) — any, поэтому сигнатура EnetConnectionFactory
// едина для всех build-тегов, а cgo-вызовы остаются типобезопасными.
type Peer = libenet.Peer
