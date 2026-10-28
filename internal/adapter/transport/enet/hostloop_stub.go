//go:build !enet || !cgo

package enet

// HostLoop - stub counterpart of the tagged HostLoop (hostloop_cgo.go): only
// needed so untagged signatures (NewEnetConnectionFactory, handler fields)
// compile without libenet. There is no real host or loop goroutine in the
// stub build.
type HostLoop struct{}

// Stop - no-op: in the stub build the loop is never published (run is a
// stub); a call is only possible via the nil-safe ListenHandle.Stop path.
func (l *HostLoop) Stop() {}
