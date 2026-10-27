package http

import (
	"errors"
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/gofiber/fiber/v3"
)

func parseRoomIDParam(c fiber.Ctx, key string) (domain.RoomID, error) {
	raw := c.Params(key)
	if raw == "" {
		return domain.RoomIDInvalid, fmt.Errorf("missing path parameter %q", key)
	}
	return domain.ParseRoomIDString(raw)
}

func roomIDParseAPIError(err error) (int, string) {
	if errors.Is(err, domain.ErrInvalidRoomID) {
		return fiber.StatusBadRequest, codeInvalidRoomID
	}
	return fiber.StatusBadRequest, codeInvalidRequest
}

func validateCapacity(capacity int) error {
	if capacity <= 0 {
		return fmt.Errorf("capacity must be positive, got %d", capacity)
	}
	if capacity > domain.MaxRoomCapacity {
		return fmt.Errorf("capacity must be at most %d, got %d", domain.MaxRoomCapacity, capacity)
	}
	return nil
}
