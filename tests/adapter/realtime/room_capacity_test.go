package realtime_test

import (
	"context"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/stdlib"
	adapterrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

// GetCapacity клампит к int8/diапазону домена даже при невалидной capacity —
// защита state-кодека от значений, которые не влезают в wire int8.
func TestGetCapacity_Clamps(t *testing.T) {
	t.Parallel()

	logger := stdlib.New("test")
	policy := &sendOnJoinPolicy{}

	for _, tc := range []struct {
		name     string
		capacity int
		want     int8
	}{
		{"normal", 8, 8},
		{"max", domain.MaxRoomCapacity, int8(domain.MaxRoomCapacity)},
		{"above max", 255, int8(domain.MaxRoomCapacity)},
		{"way above", 1 << 20, int8(domain.MaxRoomCapacity)},
		{"negative", -5, -5},
		{"below int8", -300, -128},
	} {
		room := adapterrealtime.NewRoom(context.Background(), domain.RoomID(1), policy, tc.capacity, nil, 0, logger)
		if got := room.GetCapacity(); got != tc.want {
			t.Fatalf("%s: GetCapacity() = %d, want %d", tc.name, got, tc.want)
		}
	}
}
