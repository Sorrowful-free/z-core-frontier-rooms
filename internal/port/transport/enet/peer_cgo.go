//go:build enet && cgo

package enet

import libenet "github.com/codecat/go-enet"

// Peer — tagged alias to the peer type from go-enet. Under enet+cgo it is
// libenet.Peer; in the stub build (peer_stub.go) it is any, so the
// EnetConnectionFactory signature is unified across all build tags while
// cgo calls stay type-safe.
type Peer = libenet.Peer
