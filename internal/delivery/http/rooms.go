package http

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
	"github.com/gofiber/fiber/v3"
)

type RoomsHandler struct {
	createUseCase  *room.CreateUseCase
	joinUseCase    *room.JoinUseCase
	deleteUseCase  *room.DeleteUseCase
	getListUseCase *room.GetListUseCase

	logger logging.Logger
}

func NewRoomsHandler(createUseCase *room.CreateUseCase, joinUseCase *room.JoinUseCase, deleteUseCase *room.DeleteUseCase, getListUseCase *room.GetListUseCase, logger logging.Logger) *RoomsHandler {
	return &RoomsHandler{
		createUseCase:  createUseCase,
		joinUseCase:    joinUseCase,
		deleteUseCase:  deleteUseCase,
		getListUseCase: getListUseCase,
		logger:         logger,
	}
}

func (h *RoomsHandler) CreateRoom(c *fiber.Ctx) error {

	return nil
}

func (h *RoomsHandler) JoinRoom(c *fiber.Ctx) error {
	return nil
}

func (h *RoomsHandler) DeleteRoom(c *fiber.Ctx) error {
	return nil
}

func (h *RoomsHandler) GetListRooms(c *fiber.Ctx) error {
	return nil
}

func (h *RoomsHandler) RegisterRoutes(app *fiber.App) {
	app.Post("/rooms", h.CreateRoom)
	app.Delete("/rooms/:id", h.DeleteRoom)
	app.Get("/rooms", h.GetListRooms)
}
