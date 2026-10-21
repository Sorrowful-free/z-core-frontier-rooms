//go:build !enet || !cgo

package enet_test

import "testing"

func TestEnetSkippedWithoutBuildTag(t *testing.T) {
	// Flagless -> DeliveryUnreliable mapping (internal/delivery/enet/frame_cgo.go) is covered by
	// the CI enet smoke job; deliveryFromPacketFlags is unexported and needs cgo/libenet to test.
	t.Skip("ENET cgo tests: CGO_ENABLED=1 go test -tags enet ./tests/delivery/enet/...")
}
