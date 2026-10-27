package realtime_test

import (
	"context"
	"errors"
	"testing"

	adapterrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

// recordRoomFactory — leaf-заглушка realtime.RoomFactory: отвечает своей
// sentinel-ошибкой и запоминает policy kind, с которым её вызвали.
type recordRoomFactory struct {
	marker error
	got    domain.RoomPolicyKind
}

func (f *recordRoomFactory) CreateRoom(_ context.Context, _ domain.RoomID, _ int, _ domain.RoomAttributes, kind domain.RoomPolicyKind) (realtime.Room, error) {
	f.got = kind
	return nil, f.marker
}

var (
	errStateLeaf = errors.New("state leaf")
	errAuthLeaf  = errors.New("auth host leaf")
)

// P29: KindRoomFactory диспатчит CreateRoom по policy kind на leaf-фабрику;
// неизвестный/пустой kind проваливается в дефолтную (state).
func TestKindRoomFactory_DispatchByPolicyKind(t *testing.T) {
	t.Parallel()

	stateLeaf := &recordRoomFactory{marker: errStateLeaf}
	authLeaf := &recordRoomFactory{marker: errAuthLeaf}
	factory := adapterrealtime.NewKindRoomFactory(stateLeaf, map[domain.RoomPolicyKind]realtime.RoomFactory{
		domain.RoomPolicyKindAuthoritativeHost: authLeaf,
	})

	for _, tc := range []struct {
		kind     domain.RoomPolicyKind
		wantLeaf error
	}{
		{domain.RoomPolicyKindState, errStateLeaf},
		{domain.RoomPolicyKindAuthoritativeHost, errAuthLeaf},
		{"", errStateLeaf},
		{"bogus", errStateLeaf},
	} {
		_, err := factory.CreateRoom(context.Background(), domain.RoomID(1), 4, nil, tc.kind)
		if !errors.Is(err, tc.wantLeaf) {
			t.Fatalf("kind %q → err %v, want leaf %v", tc.kind, err, tc.wantLeaf)
		}
	}
}
