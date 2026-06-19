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
	httpauthadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/httpauth"
	httplimitsadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/httplimits"
	porthttplimits "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/httplimits"
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

func newTestEnv(t *testing.T, httpAuth ...httpauthadapter.HTTPAuthConfig) *testEnv {
	return newTestEnvWithLimits(t, httplimitsadapter.HTTPLimitsConfig{}, httpAuth...)
}

func mustHTTPLimits(t *testing.T, cfg httplimitsadapter.HTTPLimitsConfig) porthttplimits.Limits {
	t.Helper()
	limits, err := httplimitsadapter.New(cfg)
	if err != nil {
		t.Fatalf("httplimits.New: %v", err)
	}
	return limits
}

func newTestEnvWithLimits(t *testing.T, httpLimits httplimitsadapter.HTTPLimitsConfig, httpAuth ...httpauthadapter.HTTPAuthConfig) *testEnv {
	t.Helper()

	authCfg := httpauthadapter.HTTPAuthConfig{Disabled: true}
	if len(httpAuth) > 0 {
		authCfg = httpAuth[0]
	}
	limits := mustHTTPLimits(t, httpLimits)

	logger := stdlib.New("http-test")
	roomPolicyFactory := statepolicy.NewStateRoomPolicyFactory(logger)
	roomFactory := adapterrealtime.NewRoomFactory(logger, roomPolicyFactory, 0)
	registry := adapterregistry.NewRoomRegistry(context.Background(), roomFactory, logger)
	reservation := adapterreservation.NewReservation(logger)
	admission := admissionadapter.NewAdmission(admissionadapter.AdmissionConfig{
		Secret: []byte("test-secret"),
		TTL:    time.Hour,
	})
	allocator := identityadapter.NewCounter()

	createUC := useroom.NewCreateUseCase(registry, allocator, reservation, limits, logger)
	issueUC := useroom.NewIssueTicketUseCase(registry, allocator, admission, reservation, logger)
	leaveUC := useroom.NewLeaveRoomUseCase(registry, reservation, logger)
	deleteUC := useroom.NewDeleteUseCase(registry, reservation, leaveUC, logger)
	getListUC := useroom.NewGetListUseCase(registry, logger)

	handler := deliveryhttp.NewRoomsHandler(createUC, issueUC, deleteUC, getListUC, logger)
	app := fiber.New()
	handler.RegisterRoutes(app, authCfg, limits)
	return &testEnv{app: app}
}

func (e *testEnv) do(t *testing.T, method, target string, body any) (*http.Response, []byte) {
	return e.doWithHeader(t, method, target, body, "", "")
}

func (e *testEnv) doAuthorized(t *testing.T, method, target string, body any, authorization string) (*http.Response, []byte) {
	return e.doWithHeader(t, method, target, body, "Authorization", authorization)
}

func (e *testEnv) doWithHeader(t *testing.T, method, target string, body any, header, value string) (*http.Response, []byte) {
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
	if header != "" {
		req.Header.Set(header, value)
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

func TestHTTP_CreateRoomWithAttributes(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	resp, body := env.do(t, http.MethodPost, "/rooms", map[string]any{
		"capacity": 4,
		"attributes": map[string]any{
			"map":  "de_dust2",
			"mode": "deathmatch",
		},
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", resp.StatusCode, body)
	}

	var created struct {
		ID         int64          `json:"id"`
		Attributes map[string]any `json:"attributes"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("unmarshal create: %v", err)
	}
	if created.Attributes["map"] != "de_dust2" {
		t.Fatalf("attributes.map = %v, want de_dust2", created.Attributes["map"])
	}
	if created.Attributes["mode"] != "deathmatch" {
		t.Fatalf("attributes.mode = %v, want deathmatch", created.Attributes["mode"])
	}

	resp, body = env.do(t, http.MethodGet, "/rooms", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", resp.StatusCode, body)
	}

	var list struct {
		Rooms []struct {
			ID         int64          `json:"id"`
			Attributes map[string]any `json:"attributes"`
		} `json:"rooms"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if len(list.Rooms) != 1 {
		t.Fatalf("rooms count = %d, want 1", len(list.Rooms))
	}
	if list.Rooms[0].Attributes["map"] != "de_dust2" {
		t.Fatalf("list attributes.map = %v, want de_dust2", list.Rooms[0].Attributes["map"])
	}

	resp, body = env.do(t, http.MethodPost, "/rooms/"+formatID(created.ID)+"/tickets", map[string]any{
		"nick_name": "player",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("issue status = %d, body = %s", resp.StatusCode, body)
	}

	var ticket struct {
		Token string `json:"token"`
		Room  struct {
			ID         int64          `json:"id"`
			Attributes map[string]any `json:"attributes"`
			Peers      []any          `json:"peers"`
		} `json:"room"`
	}
	if err := json.Unmarshal(body, &ticket); err != nil {
		t.Fatalf("unmarshal ticket: %v", err)
	}
	if ticket.Room.ID != created.ID {
		t.Fatalf("ticket room.id = %d, want %d", ticket.Room.ID, created.ID)
	}
	if ticket.Room.Attributes["map"] != "de_dust2" {
		t.Fatalf("ticket attributes.map = %v, want de_dust2", ticket.Room.Attributes["map"])
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

	const nickName = "player"
	resp, body = env.do(t, http.MethodPost, "/rooms/"+formatID(created.ID)+"/tickets", map[string]any{
		"nick_name": nickName,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("issue status = %d, body = %s", resp.StatusCode, body)
	}

	var ticket struct {
		Token string `json:"token"`
		Room  struct {
			ID         int64          `json:"id"`
			Attributes map[string]any `json:"attributes"`
			Peers      []any          `json:"peers"`
		} `json:"room"`
	}
	if err := json.Unmarshal(body, &ticket); err != nil {
		t.Fatalf("unmarshal ticket: %v", err)
	}
	if ticket.Room.ID != created.ID {
		t.Fatalf("room.id = %d, want %d", ticket.Room.ID, created.ID)
	}
	if ticket.Room.Peers == nil {
		t.Fatalf("room.peers = nil, want empty slice")
	}
	raw, err := base64.RawURLEncoding.DecodeString(ticket.Token)
	if err != nil {
		t.Fatalf("decode token: %v", err)
	}
	wantTokenLen := 34 + len(nickName) + 32
	if len(raw) != wantTokenLen {
		t.Fatalf("token len = %d, want %d", len(raw), wantTokenLen)
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

func TestHTTP_IssueTicketInvalidNickName(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	resp, body := env.do(t, http.MethodPost, "/rooms", map[string]any{"capacity": 2})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", resp.StatusCode, body)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("unmarshal create: %v", err)
	}

	resp, body = env.do(t, http.MethodPost, "/rooms/"+formatID(created.ID)+"/tickets", map[string]any{
		"password": "secret",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", resp.StatusCode, body)
	}
	assertErrorCode(t, body, "invalid_nick_name")
}

func TestHTTP_IssueTicketRoomNotFound(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	resp, body := env.do(t, http.MethodPost, "/rooms/99/tickets", map[string]any{"nick_name": "player"})
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

func TestHTTP_CreateCapacityExceedsMax(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	resp, body := env.do(t, http.MethodPost, "/rooms", map[string]any{
		"capacity": 128,
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

	resp, body = env.do(t, http.MethodPost, roomPath, map[string]any{"nick_name": "player"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("first issue status = %d, body = %s", resp.StatusCode, body)
	}

	resp, body = env.do(t, http.MethodPost, roomPath, map[string]any{"nick_name": "player2"})
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
		"nick_name": "player",
		"password":  "wrong",
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("issue wrong pass status = %d, body = %s", resp.StatusCode, body)
	}
	assertErrorCode(t, body, "invalid_credentials")

	resp, body = env.do(t, http.MethodPost, roomPath+"/tickets", map[string]any{
		"nick_name": "player",
		"password":  "room-pass",
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
