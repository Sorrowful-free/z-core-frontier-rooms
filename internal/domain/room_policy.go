package domain

import "fmt"

// RoomPolicyKind — политика жизненного цикла комнаты (P29), выбирается
// создателем в POST /rooms (поле "policy"). Свойство комнаты, а не per-peer
// credential: в ticket не входит, действует на всех peer'ов комнаты.
type RoomPolicyKind string

const (
	// RoomPolicyKindState — дефолт: StateRoomPolicy, уход master'а ведёт к
	// перевыборам (host migration среди оставшихся peer'ов).
	RoomPolicyKindState RoomPolicyKind = "state"
	// RoomPolicyKindAuthoritativeHost — уход master'а убивает комнату:
	// перевыборов нет, policy отвечает ErrRoomShouldClose и usecase делает
	// teardown (OpRoomClosed оставшимся → disconnect всех → unregister +
	// revoke слотов).
	RoomPolicyKindAuthoritativeHost RoomPolicyKind = "authoritative-host"
)

// ParseRoomPolicyKind валидирует значение поля "policy" запроса create:
// пустая строка и "state" → дефолт; неизвестное значение → ErrInvalidPolicy.
func ParseRoomPolicyKind(s string) (RoomPolicyKind, error) {
	switch kind := RoomPolicyKind(s); kind {
	case "", RoomPolicyKindState:
		return RoomPolicyKindState, nil
	case RoomPolicyKindAuthoritativeHost:
		return kind, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidPolicy, s)
	}
}
