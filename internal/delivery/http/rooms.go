package http

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	useroom "github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
	"github.com/gofiber/fiber/v3"
)

type RoomsHandler struct {
	createUseCase      *useroom.CreateUseCase
	issueTicketUseCase *useroom.IssueTicketUseCase
	deleteUseCase      *useroom.DeleteUseCase
	getListUseCase     *useroom.GetListUseCase
	logger             logging.Logger
}

func NewRoomsHandler(
	createUseCase *useroom.CreateUseCase,
	issueTicketUseCase *useroom.IssueTicketUseCase,
	deleteUseCase *useroom.DeleteUseCase,
	getListUseCase *useroom.GetListUseCase,
	logger logging.Logger,
) *RoomsHandler {
	return &RoomsHandler{
		createUseCase:      createUseCase,
		issueTicketUseCase: issueTicketUseCase,
		deleteUseCase:      deleteUseCase,
		getListUseCase:     getListUseCase,
		logger:             logger,
	}
}

func (h *RoomsHandler) CreateRoom(c fiber.Ctx) error {
	var req createRoomRequest
	if err := c.Bind().JSON(&req); err != nil {
		return writeBindError(c, err)
	}

	if err := validateCapacity(req.Capacity); err != nil {
		return writeAPIError(c, fiber.StatusBadRequest, codeInvalidCapacity, err.Error())
	}

	summary, err := h.createUseCase.Create(c.Context(), req.Capacity, req.Password)
	if err != nil {
		h.logger.Error("http create room failed", "error", err)
		return writeUsecaseError(c, err)
	}

	return c.Status(fiber.StatusCreated).JSON(roomToResponse(summary))
}

func (h *RoomsHandler) IssueTicket(c fiber.Ctx) error {
	roomID, err := parseRoomIDParam(c, "id")
	if err != nil {
		return writeAPIError(c, fiber.StatusBadRequest, codeInvalidRoomID, err.Error())
	}

	var req issueTicketRequest
	if err := c.Bind().JSON(&req); err != nil {
		return writeBindError(c, err)
	}

	token, err := h.issueTicketUseCase.IssueTicket(c.Context(), roomID, req.Password)
	if err != nil {
		h.logger.Error("http issue ticket failed", "error", err, "roomID", roomID)
		return writeUsecaseError(c, err)
	}

	return c.Status(fiber.StatusCreated).JSON(tokenToResponse(token))
}

func (h *RoomsHandler) DeleteRoom(c fiber.Ctx) error {
	roomID, err := parseRoomIDParam(c, "id")
	if err != nil {
		return writeAPIError(c, fiber.StatusBadRequest, codeInvalidRoomID, err.Error())
	}

	password := ""
	if len(c.Body()) > 0 {
		var req deleteRoomRequest
		if err := c.Bind().JSON(&req); err != nil {
			return writeBindError(c, err)
		}
		password = req.Password
	}

	if err := h.deleteUseCase.Delete(c.Context(), roomID, password); err != nil {
		h.logger.Error("http delete room failed", "error", err, "roomID", roomID)
		return writeUsecaseError(c, err)
	}

	return c.SendStatus(fiber.StatusNoContent)
}

func (h *RoomsHandler) GetListRooms(c fiber.Ctx) error {
	summaries, err := h.getListUseCase.GetList(c.Context())
	if err != nil {
		h.logger.Error("http get list rooms failed", "error", err)
		return writeUsecaseError(c, err)
	}

	return c.JSON(roomsToResponse(summaries))
}

func (h *RoomsHandler) RegisterRoutes(app *fiber.App) {
	app.Post("/rooms", h.CreateRoom)
	app.Post("/rooms/:id/tickets", h.IssueTicket)
	app.Delete("/rooms/:id", h.DeleteRoom)
	app.Get("/rooms", h.GetListRooms)
}
