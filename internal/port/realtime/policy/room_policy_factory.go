package policy

type RoomPolicyFactory interface {
	CreateRoomPolicy() RoomPolicy
}
