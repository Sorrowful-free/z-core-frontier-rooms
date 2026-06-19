package domain

// RoomAttributes — произвольные key-value аргументы комнаты (название карты, режим и т.п.).
type RoomAttributes map[string]any

func CloneRoomAttributes(src RoomAttributes) RoomAttributes {
	if len(src) == 0 {
		return nil
	}
	dst := make(RoomAttributes, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
