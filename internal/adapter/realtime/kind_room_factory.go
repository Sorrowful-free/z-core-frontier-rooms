package realtime

import (
	"context"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

// KindRoomFactory — композитная realtime.RoomFactory (P29): dispatch
// CreateRoom по domain.RoomPolicyKind на конкретную leaf-фабрику, у каждой
// своя policy factory (state / authoritative-host). Port фабрик policy при
// этом параметра kind не несёт — выбор реализации решается здесь, а не внутри
// policy-пакета. Неизвестный kind проваливается в def (state) — валидация
// строки полиси живёт на HTTP-границе (ParseRoomPolicyKind).
type KindRoomFactory struct {
	def    realtime.RoomFactory
	byKind map[domain.RoomPolicyKind]realtime.RoomFactory
}

func NewKindRoomFactory(def realtime.RoomFactory, byKind map[domain.RoomPolicyKind]realtime.RoomFactory) *KindRoomFactory {
	return &KindRoomFactory{def: def, byKind: byKind}
}

func (f *KindRoomFactory) CreateRoom(ctx context.Context, id domain.RoomID, capacity int, attributes domain.RoomAttributes, policyKind domain.RoomPolicyKind) (realtime.Room, error) {
	leaf, ok := f.byKind[policyKind]
	if !ok {
		leaf = f.def
	}
	return leaf.CreateRoom(ctx, id, capacity, attributes, policyKind)
}

var _ realtime.RoomFactory = (*KindRoomFactory)(nil)
