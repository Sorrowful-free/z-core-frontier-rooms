//go:build !enet || !cgo

package enet

// Peer — в stub-сборке (без enet/cgo) сведён к any: untagged-сигнатура
// EnetConnectionFactory компилируется без libenet, а реальный peer
// передают только cgo-вызовы (там alias — libenet.Peer, см. peer_cgo.go).
type Peer = any
