package http_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	admissionadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/admission"
	identityadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/identity"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/stdlib"
	adapterrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	statepolicy "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/policy/state"
	adapterregistry "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/registry"
	adapterreservation "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/reservation"
	deliveryhttp "github.com/Sorrowful-free/z-core-frontier-rooms/internal/delivery/http"
	useroom "github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
	"github.com/gofiber/fiber/v3"
)

type testEnv struct {
	app *fiber.App
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	logger := stdlib.New("http-test")
	roomPolicyFactory := statepolicy.NewStateRoomPolicyFactory(logger)
	roomFactory := adapterrealtime.NewRoomFactory(logger, roomPolicyFactory)
	registry := adapterregistry.NewRoomRegistry(context.Background(), roomFactory, logger)
	reservation := adapterreservation.NewReservation(logger)
	admission := admissionadapter.NewAdmission([]byte("test-secret"), time.Hour, "")
	allocator := identityadapter.NewCounter()

	createUC := useroom.NewCreateUseCase(registry, allocator, reservation, logger)
	issueUC := useroom.NewIssueTicketUseCase(registry, allocator, admission, reservation, logger)
	deleteUC := useroom.NewDeleteUseCase(registry, reservation, logger)
	getListUC := useroom.NewGetListUseCase(registry, logger)

	handler := deliveryhttp.NewRoomsHandler(createUC, issueUC, deleteUC, getListUC, logger)
	app := fiber.New()
	handler.RegisterRoutes(app)
	return &testEnv{app: app}
}

func (e *testEnv) do(t *testing.T, method, target string, body any) (*http.Response, []byte) {
	t.Helper()

	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(b)
	}

	req := httptest.NewRequest(method, target, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := e.app.Test(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, respBody
}

func TestHTTP_CreateGetListDeleteRoom(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	resp, body := env.do(t, http.MethodPost, "/rooms", map[string]any{
		"capacity": 2,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", resp.StatusCode, body)
	}

	var created struct {
		ID    int64 `json:"id"`
		Peers []any `json:"peers"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("unmarshal create: %v", err)
	}
	if created.ID <= 0 {
		t.Fatalf("id = %d, want positive server-assigned id", created.ID)
	}

	resp, body = env.do(t, http.MethodGet, "/rooms", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", resp.StatusCode, body)
	}

	var list struct {
		Rooms []struct {
			ID int64 `json:"id"`
		} `json:"rooms"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if len(list.Rooms) != 1 || list.Rooms[0].ID != created.ID {
		t.Fatalf("list = %+v, want one room id %d", list.Rooms, created.ID)
	}

	resp, body = env.do(t, http.MethodDelete, "/rooms/"+formatID(created.ID), nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d, body = %s", resp.StatusCode, body)
	}

	resp, body = env.do(t, http.MethodGet, "/rooms", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list after delete status = %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if len(list.Rooms) != 0 {
		t.Fatalf("rooms after delete = %+v, want empty", list.Rooms)
	}
}

func TestHTTP_IssueTicket(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	resp, body := env.do(t, http.MethodPost, "/rooms", map[string]any{
		"capacity": 4,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", resp.StatusCode, body)
	}

	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("unmarshal create: %v", err)
	}

	resp, body = env.do(t, http.MethodPost, "/rooms/"+formatID(created.ID)+"/tickets", map[string]any{})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("issue status = %d, body = %s", resp.StatusCode, body)
	}

	var ticket struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &ticket); err != nil {
		t.Fatalf("unmarshal ticket: %v", err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(ticket.Token)
	if err != nil {
		t.Fatalf("decode token: %v", err)
	}
	if len(raw) != 65 {
		t.Fatalf("token len = %d, want 65", len(raw))
	}
}

func TestHTTP_CreateAssignsDistinctRoomIDs(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	resp, body := env.do(t, http.MethodPost, "/rooms", map[string]any{"capacity": 1})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("first create status = %d, body = %s", resp.StatusCode, body)
	}
	var first struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(body, &first); err != nil {
		t.Fatalf("unmarshal first: %v", err)
	}

	resp, body = env.do(t, http.MethodPost, "/rooms", map[string]any{"capacity": 1})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("second create status = %d, body = %s", resp.StatusCode, body)
	}
	var second struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(body, &second); err != nil {
		t.Fatalf("unmarshal second: %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("room ids = %d, want distinct server-assigned ids", first.ID)
	}
}

func TestHTTP_InvalidRoomIDPath(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	cases := []struct {
		name string
		path string
	}{
		{name: "zero", path: "/rooms/0/tickets"},
		{name: "overflow uint32", path: "/rooms/4294967296/tickets"},
		{name: "negative", path: "/rooms/-1/tickets"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resp, body := env.do(t, http.MethodPost, tc.path, map[string]any{})
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400, body = %s", resp.StatusCode, body)
			}
			assertErrorCode(t, body, "invalid_room_id")
		})
	}
}

func TestHTTP_DeleteNotFound(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	resp, body := env.do(t, http.MethodDelete, "/rooms/404", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body = %s", resp.StatusCode, body)
	}
	assertErrorCode(t, body, "reservation_not_found")
}

func TestHTTP_IssueTicketRoomNotFound(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	resp, body := env.do(t, http.MethodPost, "/rooms/99/tickets", map[string]any{})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body = %s", resp.StatusCode, body)
	}
	assertErrorCode(t, body, "room_not_found")
}

func TestHTTP_CreateInvalidBody(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	req := httptest.NewRequest(http.MethodPost, "/rooms", bytes.NewReader([]byte("not-json")))
	req.Header.Set("Content-Type", "application/json")
	resp, err := env.app.Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", resp.StatusCode, body)
	}
}

func TestHTTP_CreateInvalidCapacity(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	resp, body := env.do(t, http.MethodPost, "/rooms", map[string]any{
		"capacity": 0,
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", resp.StatusCode, body)
	}
	assertErrorCode(t, body, "invalid_capacity")
}

func TestHTTP_IssueTicketReservationFull(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	resp, body := env.do(t, http.MethodPost, "/rooms", map[string]any{"capacity": 1})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", resp.StatusCode, body)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("unmarshal create: %v", err)
	}
	roomPath := "/rooms/" + formatID(created.ID) + "/tickets"

	resp, body = env.do(t, http.MethodPost, roomPath, map[string]any{})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("first issue status = %d, body = %s", resp.StatusCode, body)
	}

	resp, body = env.do(t, http.MethodPost, roomPath, map[string]any{})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("second issue status = %d, want 409, body = %s", resp.StatusCode, body)
	}
	assertErrorCode(t, body, "reservation_full")
}

func TestHTTP_RoomPasswordOnCreateIssueDelete(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	resp, body := env.do(t, http.MethodPost, "/rooms", map[string]any{
		"capacity": 2,
		"password": "room-pass",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", resp.StatusCode, body)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("unmarshal create: %v", err)
	}
	roomPath := "/rooms/" + formatID(created.ID)

	resp, body = env.do(t, http.MethodPost, roomPath+"/tickets", map[string]any{
		"password": "wrong",
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("issue wrong pass status = %d, body = %s", resp.StatusCode, body)
	}
	assertErrorCode(t, body, "invalid_credentials")

	resp, body = env.do(t, http.MethodPost, roomPath+"/tickets", map[string]any{
		"password": "room-pass",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("issue ok status = %d, body = %s", resp.StatusCode, body)
	}

	resp, body = env.do(t, http.MethodDelete, roomPath, map[string]any{"password": "wrong"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("delete wrong pass status = %d, body = %s", resp.StatusCode, body)
	}

	resp, body = env.do(t, http.MethodDelete, roomPath, map[string]any{"password": "room-pass"})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete ok status = %d, body = %s", resp.StatusCode, body)
	}
}

func formatID(id int64) string {
	return fmt.Sprintf("%d", id)
}

func assertErrorCode(t *testing.T, body []byte, want string) {
	t.Helper()
	var er struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &er); err != nil {
		t.Fatalf("unmarshal error body: %v", err)
	}
	if er.Code != want {
		t.Fatalf("code = %q, want %q, body = %s", er.Code, want, body)
	}
}
