package http

import (
	"fmt"
	"strconv"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/gofiber/fiber/v3"
)

func parseRoomIDParam(c fiber.Ctx, key string) (domain.RoomID, error) {
	raw := c.Params(key)
	if raw == "" {
		return domain.RoomIDInvalid, fmt.Errorf("missing path parameter %q", key)
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return domain.RoomIDInvalid, fmt.Errorf("invalid room id %q: %w", raw, err)
	}
	return parseRoomID(id)
}

func parseRoomID(id int64) (domain.RoomID, error) {
	roomID := domain.RoomID(id)
	if !roomID.IsValid() {
		return domain.RoomIDInvalid, fmt.Errorf("room id must be positive, got %d", id)
	}
	return roomID, nil
}

func parsePeerID(id int64) (domain.PeerID, error) {
	peerID := domain.PeerID(id)
	if !peerID.IsValid() {
		return domain.PeerIDInvalid, fmt.Errorf("peer id must be positive, got %d", id)
	}
	return peerID, nil
}

func validateCapacity(capacity int) error {
	if capacity <= 0 {
		return fmt.Errorf("capacity must be positive, got %d", capacity)
	}
	return nil
}
