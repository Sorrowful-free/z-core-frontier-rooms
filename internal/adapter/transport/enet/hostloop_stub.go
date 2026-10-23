//go:build !enet || !cgo

package enet

// HostLoop - stub-аналог tagged HostLoop (hostloop_cgo.go): нужен только чтобы
// untagged-сигнатуры (NewEnetConnectionFactory, поля handler'ов) компилировались
// без libenet. Реального host'а и loop-горутины в stub-сборке нет.
type HostLoop struct{}

// Stop - no-op: в stub-сборке loop никогда не публикуется (run - stub),
// вызов возможен только по nil-safe пути ListenHandle.Stop.
func (l *HostLoop) Stop() {}
