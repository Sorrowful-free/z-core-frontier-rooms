package http

import (
	"encoding/base64"

	httpauthadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/httpauth"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	portadmission "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/admission"
	porthttplimits "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/httplimits"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	useroom "github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
	"github.com/gofiber/fiber/v3"
)

type RoomsHandler struct {
	createUseCase      *useroom.CreateUseCase
	issueTicketUseCase *useroom.IssueTicketUseCase
	deleteUseCase      *useroom.DeleteUseCase
	getListUseCase     *useroom.GetListUseCase
	leaveRoomUseCase   *useroom.LeaveRoomUseCase
	admission          portadmission.Admission
	logger             logging.Logger
}

func NewRoomsHandler(
	createUseCase *useroom.CreateUseCase,
	issueTicketUseCase *useroom.IssueTicketUseCase,
	deleteUseCase *useroom.DeleteUseCase,
	getListUseCase *useroom.GetListUseCase,
	leaveRoomUseCase *useroom.LeaveRoomUseCase,
	admission portadmission.Admission,
	logger logging.Logger,
) *RoomsHandler {
	return &RoomsHandler{
		createUseCase:      createUseCase,
		issueTicketUseCase: issueTicketUseCase,
		deleteUseCase:      deleteUseCase,
		getListUseCase:     getListUseCase,
		leaveRoomUseCase:   leaveRoomUseCase,
		admission:          admission,
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
	if err := domain.ValidateNickName(req.NickName); err != nil {
		return writeAPIError(c, fiber.StatusBadRequest, codeInvalidNickName, err.Error())
	}
	result, err := h.createUseCase.Create(c.Context(), req.Capacity, req.Password, domain.RoomAttributes(req.Attributes), req.NickName)
	if err != nil {
		h.logger.Error("http create room failed", "error", err)
		return writeUsecaseError(c, err)
	}

	return c.Status(fiber.StatusCreated).JSON(issueTicketToResponse(result))
}

func (h *RoomsHandler) IssueTicket(c fiber.Ctx) error {
	roomID, err := parseRoomIDParam(c, "id")
	if err != nil {
		status, code := roomIDParseAPIError(err)
		return writeAPIError(c, status, code, err.Error())
	}

	var req issueTicketRequest
	if err := c.Bind().JSON(&req); err != nil {
		return writeBindError(c, err)
	}
	if err := domain.ValidateNickName(req.NickName); err != nil {
		return writeAPIError(c, fiber.StatusBadRequest, codeInvalidNickName, err.Error())
	}

	result, err := h.issueTicketUseCase.IssueTicket(c.Context(), roomID, req.NickName, req.Password)
	if err != nil {
		h.logger.Error("http issue ticket failed", "error", err, "roomID", roomID)
		return writeUsecaseError(c, err)
	}

	return c.Status(fiber.StatusCreated).JSON(issueTicketToResponse(result))
}

func (h *RoomsHandler) DeleteRoom(c fiber.Ctx) error {
	roomID, err := parseRoomIDParam(c, "id")
	if err != nil {
		status, code := roomIDParseAPIError(err)
		return writeAPIError(c, status, code, err.Error())
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

// LeaveSelf — self-leave по reconnect-токену (P32): credential kind=2
// доказывает peerID+roomID из claims — клиент уходит только сам,
// kick чужих по HTTP не существует. Graceful leave через
// LeaveRoomUseCase — тот же путь, что у disconnect'а (room.Leave →
// drain-aware Stop → Revoke, пустая комната умирает). Идемпотентно:
// peer уже не в комнате → 204.
func (h *RoomsHandler) LeaveSelf(c fiber.Ctx) error {
	roomID, err := parseRoomIDParam(c, "id")
	if err != nil {
		status, code := roomIDParseAPIError(err)
		return writeAPIError(c, status, code, err.Error())
	}

	var req leaveRoomRequest
	if err := c.Bind().JSON(&req); err != nil {
		return writeBindError(c, err)
	}

	token, err := base64.RawURLEncoding.DecodeString(req.ReconnectToken)
	if err != nil {
		return writeAPIError(c, fiber.StatusUnauthorized, codeInvalidCredentials, "invalid reconnect token")
	}
	claims, err := h.admission.Validate(c.Context(), token)
	if err != nil || claims.Kind != domain.ClaimsKindReconnect || claims.RoomID != roomID {
		return writeAPIError(c, fiber.StatusUnauthorized, codeInvalidCredentials, "invalid reconnect token")
	}

	if err := h.leaveRoomUseCase.LeaveRoom(c.Context(), roomID, claims.PeerID); err != nil {
		h.logger.Error("http leave failed", "error", err, "roomID", roomID, "peerID", claims.PeerID)
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

func (h *RoomsHandler) RegisterRoutes(app *fiber.App, httpAuth httpauthadapter.HTTPAuthConfig, limits porthttplimits.Limits) {
	protected := app.Group("", NewAPIKeyMiddleware(httpAuth))
	protected.Post("/rooms", newCreateRateLimitMiddleware(limits), h.CreateRoom)
	protected.Post("/rooms/:id/tickets", newIssueRateLimitMiddleware(limits), h.IssueTicket)
	protected.Delete("/rooms/:id", newDeleteRateLimitMiddleware(limits), h.DeleteRoom)
	protected.Post("/rooms/:id/leave", newDeleteRateLimitMiddleware(limits), h.LeaveSelf)
	protected.Get("/rooms", newListRateLimitMiddleware(limits), h.GetListRooms)
}
