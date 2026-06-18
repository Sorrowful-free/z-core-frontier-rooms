//go:build !enet || !cgo

package enet_test

import "testing"

func TestEnetSkippedWithoutBuildTag(t *testing.T) {
	t.Skip("ENET cgo tests: CGO_ENABLED=1 go test -tags enet ./tests/delivery/enet/...")
}
