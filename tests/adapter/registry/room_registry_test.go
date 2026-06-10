package registry_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/stdlib"
	adapterrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	statepolicy "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/policy/state"
	adapterregistry "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/registry"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

func newTestRegistry(t *testing.T) *adapterregistry.RoomRegistry {
	t.Helper()
	logger := stdlib.New("test")
	roomPolicyFactory := statepolicy.NewStateRoomPolicyFactory(logger)
	roomFactory := adapterrealtime.NewRoomFactory(logger, roomPolicyFactory)
	return adapterregistry.NewRoomRegistry(context.Background(), roomFactory, logger)
}

func TestGetRoomNotFoundMapsDomainError(t *testing.T) {
	t.Parallel()

	reg := newTestRegistry(t)

	_, err := reg.GetRoom(context.Background(), domain.RoomID(999))
	if !errors.Is(err, domain.ErrRoomNotFound) {
		t.Fatalf("GetRoom err = %v, want ErrRoomNotFound", err)
	}
}

func TestCreateRoomDuplicateID(t *testing.T) {
	t.Parallel()

	reg := newTestRegistry(t)
	const (
		id       = domain.RoomID(7)
		capacity = 8
	)

	if _, err := reg.CreateRoom(context.Background(), id, capacity); err != nil {
		t.Fatalf("first CreateRoom: %v", err)
	}
	t.Cleanup(func() { _ = reg.DeleteRoom(context.Background(), id) })

	_, err := reg.CreateRoom(context.Background(), id, capacity)
	if !errors.Is(err, domain.ErrRoomAlreadyExists) {
		t.Fatalf("second CreateRoom err = %v, want ErrRoomAlreadyExists", err)
	}
}
