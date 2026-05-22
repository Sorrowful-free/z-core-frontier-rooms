package registry_test

import (
	"context"
	"errors"
	"testing"

	adapterrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	adapterregistry "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/registry"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/stdlib"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

func TestGetRoomNotFoundMapsDomainError(t *testing.T) {
	t.Parallel()

	logger := stdlib.New("test")
	reg := adapterregistry.NewRoomRegistry(
		adapterrealtime.NewRoomFactory(logger),
		adapterrealtime.NewRelayRoomHandlerFactory(logger),
		logger,
	)

	_, err := reg.GetRoom(context.Background(), domain.RoomID(999))
	if !errors.Is(err, domain.ErrRoomNotFound) {
		t.Fatalf("GetRoom err = %v, want ErrRoomNotFound", err)
	}
}
