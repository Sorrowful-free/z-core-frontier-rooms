package http

import (
	"errors"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/gofiber/fiber/v3"
)

const (
	codeInvalidRequest          = "invalid_request"
	codeInvalidRoomID           = "invalid_room_id"
	codeInvalidPeerID           = "invalid_peer_id"
	codeInvalidCapacity         = "invalid_capacity"
	codeRoomNotFound            = "room_not_found"
	codeRoomAlreadyExists       = "room_already_exists"
	codeReservationNotFound     = "reservation_not_found"
	codeReservationAlreadyExists = "reservation_already_exists"
	codeReservationFull         = "reservation_full"
	codeTicketSlotHeld          = "ticket_slot_held"
	codePeerAlreadyInRoom       = "peer_already_in_room"
	codeReservationNotReserved  = "reservation_not_reserved"
	codeReservationExpired      = "reservation_expired"
	codeReservationAlreadyAdmitted = "reservation_already_admitted"
	codeInvalidCredentials      = "invalid_credentials"
	codeInternal                = "internal_error"
)

func writeAPIError(c fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(errorResponse{
		Code:    code,
		Message: message,
	})
}

func writeBindError(c fiber.Ctx, err error) error {
	return writeAPIError(c, fiber.StatusBadRequest, codeInvalidRequest, err.Error())
}

func writeParseError(c fiber.Ctx, err error) error {
	return writeAPIError(c, fiber.StatusBadRequest, codeInvalidRequest, err.Error())
}

func writeUsecaseError(c fiber.Ctx, err error) error {
	status, code := statusAndCodeFromError(err)
	message := clientMessage(err, code)
	return writeAPIError(c, status, code, message)
}

func statusAndCodeFromError(err error) (int, string) {
	switch {
	case errors.Is(err, domain.ErrRoomNotFound):
		return fiber.StatusNotFound, codeRoomNotFound
	case errors.Is(err, domain.ErrReservationNotFound):
		return fiber.StatusNotFound, codeReservationNotFound
	case errors.Is(err, domain.ErrRoomAlreadyExists):
		return fiber.StatusConflict, codeRoomAlreadyExists
	case errors.Is(err, domain.ErrTicketSlotHeld):
		return fiber.StatusConflict, codeTicketSlotHeld
	case errors.Is(err, domain.ErrReservationAlreadyExists):
		return fiber.StatusConflict, codeReservationAlreadyExists
	case errors.Is(err, domain.ErrReservationFull):
		return fiber.StatusConflict, codeReservationFull
	case errors.Is(err, domain.ErrPeerAlreadyInRoom):
		return fiber.StatusConflict, codePeerAlreadyInRoom
	case errors.Is(err, domain.ErrReservationNotReserved):
		return fiber.StatusConflict, codeReservationNotReserved
	case errors.Is(err, domain.ErrReservationExpired):
		return fiber.StatusConflict, codeReservationExpired
	case errors.Is(err, domain.ErrReservationAlreadyAdmitted):
		return fiber.StatusConflict, codeReservationAlreadyAdmitted
	case errors.Is(err, domain.ErrInvalidCredentials):
		return fiber.StatusUnauthorized, codeInvalidCredentials
	default:
		return fiber.StatusInternalServerError, codeInternal
	}
}

func clientMessage(err error, code string) string {
	if code == codeInternal {
		return "internal server error"
	}
	return err.Error()
}
