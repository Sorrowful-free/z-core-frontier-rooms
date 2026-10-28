//go:build !enet || !cgo

package enet

// Peer — in the stub build (without enet/cgo) it is reduced to any: the
// untagged EnetConnectionFactory signature compiles without libenet, and
// only cgo calls pass a real peer (there the alias is libenet.Peer, see
// peer_cgo.go).
type Peer = any
