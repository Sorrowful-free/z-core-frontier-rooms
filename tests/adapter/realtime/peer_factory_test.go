package realtime_test

import (
	"context"
	"testing"

	adapterrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/stdlib"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

func TestPeerFactory_CreatePeerSetsNickName(t *testing.T) {
	t.Parallel()

	logger := stdlib.New("peer-factory-test")
	factory := adapterrealtime.NewPeerFactory(logger, 0)
	room := adapterrealtime.NewRoom(
		context.Background(),
		domain.RoomID(1),
		nil,
		4,
		0,
		logger,
	)

	const nickName = "hero"
	peer, err := factory.CreatePeer(
		context.Background(),
		domain.PeerID(5),
		nickName,
		nil,
		room,
		logger,
	)
	if err != nil {
		t.Fatalf("CreatePeer: %v", err)
	}
	if peer.GetNickName() != nickName {
		t.Fatalf("nick = %q, want %q", peer.GetNickName(), nickName)
	}
}
