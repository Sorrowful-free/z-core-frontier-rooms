// Package env — общий composition root для тестов delivery/integration:
// wiring in-memory адаптеров, use cases и HTTP/WS handlers поверх одного fiber.App.
package env

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	admissionadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/admission"
	httpauthadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/httpauth"
	httplimitsadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/httplimits"
	identityadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/identity"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/stdlib"
	adapterrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	statepolicy "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/policy/state"
	adapterregistry "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/registry"
	adapterreservation "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/reservation"
	adaptertransport "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport"
	transportws "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/ws"
	deliveryhttp "github.com/Sorrowful-free/z-core-frontier-rooms/internal/delivery/http"
	deliveryws "github.com/Sorrowful-free/z-core-frontier-rooms/internal/delivery/ws"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	portadmission "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/admission"
	portidentity "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/identity"
	portrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	portregistry "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
	portreservation "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/reservation"
	useroom "github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
	fwebsocket "github.com/fasthttp/websocket"
	"github.com/gofiber/fiber/v3"
)

// Env — white-box тестовое окружение: публичные поля дают прямой доступ
// к собранным портам для asserts (peers, слоты reservation и т.д.).
type Env struct {
	App         *fiber.App
	Registry    portregistry.RoomRegistry
	Reservation portreservation.Reservation
	Admission   portadmission.Admission
	Allocator   portidentity.Allocator

	wsMu       sync.Mutex
	wsListener net.Listener
	wsAddr     string
}

type options struct {
	httpAuth   httpauthadapter.HTTPAuthConfig
	httpLimits httplimitsadapter.HTTPLimitsConfig
	transport  adaptertransport.TransportConfig
}

// Option переопределяет дефолтный конфиг env (auth Disabled, limits zero, transport zero).
type Option func(*options)

// WithHTTPAuth задаёт конфиг API-key middleware REST control plane.
func WithHTTPAuth(cfg httpauthadapter.HTTPAuthConfig) Option {
	return func(o *options) {
		o.httpAuth = cfg
	}
}

// WithHTTPLimits задаёт конфиг rate/max-rooms лимитов REST control plane.
func WithHTTPLimits(cfg httplimitsadapter.HTTPLimitsConfig) Option {
	return func(o *options) {
		o.httpLimits = cfg
	}
}

// WithTransport задаёт конфиг data plane (WS read limit, очереди, idle).
func WithTransport(cfg adaptertransport.TransportConfig) Option {
	return func(o *options) {
		o.transport = cfg
	}
}

// New собирает полное окружение и регистрирует cleanup
// (закрытие listener'а, shutdown app, shutdown registry — чтобы не висели горутины комнат/peer'ов).
func New(t *testing.T, opts ...Option) *Env {
	t.Helper()

	cfg := options{
		httpAuth: httpauthadapter.HTTPAuthConfig{Disabled: true},
		transport: adaptertransport.TransportConfig{
			RoomIncomingQueueSize: adaptertransport.DefaultRoomIncomingQueueSize,
			PeerOutboundQueueSize: adaptertransport.DefaultPeerOutboundQueueSize,
			EnetIncomingQueueSize: adaptertransport.DefaultEnetIncomingQueueSize,
			MaxIncomingFrameBytes: adaptertransport.DefaultMaxIncomingFrameBytes,
		},
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	limits, err := httplimitsadapter.New(cfg.httpLimits)
	if err != nil {
		t.Fatalf("httplimits.New: %v", err)
	}

	logger := stdlib.New("test-env")
	roomPolicyFactory := statepolicy.NewStateRoomPolicyFactory(logger)
	roomFactory := adapterrealtime.NewRoomFactory(logger, roomPolicyFactory, cfg.transport.RoomIncomingQueueSize)
	peerFactory := adapterrealtime.NewPeerFactory(logger, cfg.transport.PeerOutboundQueueSize)

	registry := adapterregistry.NewRoomRegistry(context.Background(), roomFactory, logger)
	reservation := adapterreservation.NewReservation(logger)
	admission := admissionadapter.NewAdmission(admissionadapter.AdmissionConfig{
		Secret: []byte("test-secret"),
		TTL:    time.Hour,
	})
	allocator := identityadapter.NewCounter()
	connectionFactory := transportws.NewWsConnectionFactory(logger, cfg.transport)

	issueUC := useroom.NewIssueTicketUseCase(registry, allocator, admission, reservation, logger)
	createUC := useroom.NewCreateUseCase(registry, allocator, reservation, limits, issueUC, logger)
	leaveUC := useroom.NewLeaveRoomUseCase(registry, reservation, logger)
	deleteUC := useroom.NewDeleteUseCase(registry, reservation, leaveUC, logger)
	getListUC := useroom.NewGetListUseCase(registry, logger)
	joinUC := useroom.NewJoinRoomUseCase(admission, peerFactory, registry, reservation, logger)

	app := fiber.New()
	deliveryhttp.NewRoomsHandler(createUC, issueUC, deleteUC, getListUC, logger).
		RegisterRoutes(app, cfg.httpAuth, limits)
	deliveryws.NewRoomsHandler(context.Background(), joinUC, leaveUC, connectionFactory, cfg.transport, logger).
		RegisterRoutes(app)

	e := &Env{
		App:         app,
		Registry:    registry,
		Reservation: reservation,
		Admission:   admission,
		Allocator:   allocator,
	}

	t.Cleanup(func() {
		e.closeWSListener()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = e.App.ShutdownWithContext(shutdownCtx)
		_ = e.Registry.Shutdown(shutdownCtx)
	})
	return e
}

// Do выполняет HTTP-запрос через app.Test; body != nil маршалится в JSON.
func (e *Env) Do(t *testing.T, method, target string, body any) (*http.Response, []byte) {
	t.Helper()
	return e.DoWithHeaders(t, method, target, body, "", "")
}

// DoWithHeaders — то же, что Do, плюс один произвольный заголовок (для auth-кейсов).
func (e *Env) DoWithHeaders(t *testing.T, method, target string, body any, header, value string) (*http.Response, []byte) {
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

	resp, err := e.App.Test(req)
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

// DialWS поднимает реальный listener (lazy, один на Env) и дозвонится на /ws?query.
// query — уже URL-encoded хвост (например "token=<base64url>"), без '?'.
func (e *Env) DialWS(t *testing.T, query string) *fwebsocket.Conn {
	t.Helper()

	url := "ws://" + e.wsListenAddr(t) + "/ws"
	if query != "" {
		url += "?" + query
	}
	conn, resp, err := fwebsocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("dial %s: %v (status %d)", url, err, status)
	}
	return conn
}

func (e *Env) wsListenAddr(t *testing.T) string {
	t.Helper()

	e.wsMu.Lock()
	defer e.wsMu.Unlock()

	if e.wsAddr != "" {
		return e.wsAddr
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	e.wsListener = ln
	e.wsAddr = ln.Addr().String()
	go func() {
		// App.Listener блокируется до closeWSListener/ShutdownWithContext в t.Cleanup.
		_ = e.App.Listener(ln)
	}()
	return e.wsAddr
}

func (e *Env) closeWSListener() {
	e.wsMu.Lock()
	defer e.wsMu.Unlock()
	if e.wsListener != nil {
		_ = e.wsListener.Close()
		e.wsListener = nil
	}
}

// CreateRoom делает POST /rooms {"capacity":4,"nick_name":nick} и возвращает
// roomID и host ticket (token уже base64url — в WS query идёт как есть).
func (e *Env) CreateRoom(t *testing.T, nick string) (domain.RoomID, string) {
	t.Helper()

	resp, body := e.Do(t, http.MethodPost, "/rooms", map[string]any{
		"capacity":  4,
		"nick_name": nick,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create room status = %d, body = %s", resp.StatusCode, body)
	}
	var created struct {
		Token string `json:"token"`
		Room  struct {
			ID int64 `json:"id"`
		} `json:"room"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("unmarshal create room: %v", err)
	}
	if created.Token == "" {
		t.Fatal("create room: empty token")
	}
	return domain.RoomID(created.Room.ID), created.Token
}

// IssueTicket делает POST /rooms/:id/tickets {"nick_name":nick} и возвращает
// joiner ticket. Комната без пароля — VerifyRoomPassword пропускает пустой.
func (e *Env) IssueTicket(t *testing.T, roomID domain.RoomID, nick string) string {
	t.Helper()

	resp, body := e.Do(t, http.MethodPost, fmt.Sprintf("/rooms/%d/tickets", roomID), map[string]any{
		"nick_name": nick,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("issue ticket status = %d, body = %s", resp.StatusCode, body)
	}
	var ticket struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &ticket); err != nil {
		t.Fatalf("unmarshal ticket: %v", err)
	}
	if ticket.Token == "" {
		t.Fatal("issue ticket: empty token")
	}
	return ticket.Token
}

// WaitPeerNick ждёт присутствия (want=true) или отсутствия (want=false) peer'а
// с nick в комнате. Join/leave идут асинхронно через lifecycle комнаты и transport.
func (e *Env) WaitPeerNick(t *testing.T, roomID domain.RoomID, nick string, want bool) {
	t.Helper()
	e.pollPeers(t, roomID, func(peers []portrealtime.Peer) bool {
		for _, peer := range peers {
			if peer.GetNickName() == nick {
				return want
			}
		}
		return !want
	}, fmt.Sprintf("peer %q presence, want %v", nick, want))
}

// WaitPeerCount ждёт, пока в комнате станет ровно want peers.
func (e *Env) WaitPeerCount(t *testing.T, roomID domain.RoomID, want int) {
	t.Helper()
	e.pollPeers(t, roomID, func(peers []portrealtime.Peer) bool {
		return len(peers) == want
	}, fmt.Sprintf("peer count, want %d", want))
}

// pollPeers — общий poll-движок ожидания состояния комнаты по списку peers:
// deadline ~1s, шаг ~10ms, t.Fatalf по таймауту.
func (e *Env) pollPeers(t *testing.T, roomID domain.RoomID, ok func(peers []portrealtime.Peer) bool, desc string) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for {
		room, err := e.Registry.GetRoom(context.Background(), roomID)
		if err != nil {
			t.Fatalf("get room %d: %v", roomID, err)
		}
		peers := room.GetPeers()
		if ok(peers) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("room %d: %s (peers = %d)", roomID, desc, len(peers))
		}
		time.Sleep(10 * time.Millisecond)
	}
}
