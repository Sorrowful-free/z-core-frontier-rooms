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
	metricsadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/metrics"
	adapterrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	statepolicy "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/policy/state"
	adapterregistry "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/registry"
	adapterreservation "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/reservation"
	adaptertransport "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport"
	adapterenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/enet"
	transportws "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/ws"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/delivery/admin"
	deliveryenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/delivery/enet"
	deliveryhttp "github.com/Sorrowful-free/z-core-frontier-rooms/internal/delivery/http"
	deliveryws "github.com/Sorrowful-free/z-core-frontier-rooms/internal/delivery/ws"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	portadmission "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/admission"
	portidentity "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/identity"
	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
	portrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	portregistry "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
	portreservation "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/reservation"
	portenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport/enet"
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

	// Metrics - реальный prometheus-адаптер (только с WithMetrics);
	// Admin - admin Fiber-app с GET /metrics на том же fiber.Test-клиенте.
	Metrics *metricsadapter.Metrics
	Admin   *fiber.App

	wsMu       sync.Mutex
	wsListener net.Listener
	wsAddr     string

	enetPorts  []uint16
	enetHandle *deliveryenet.ListenHandle
}

type options struct {
	httpAuth          httpauthadapter.HTTPAuthConfig
	httpLimits        httplimitsadapter.HTTPLimitsConfig
	transport         adaptertransport.TransportConfig
	idleSweepInterval time.Duration
	reconnectTTL      time.Duration // reconnect grace admitted-слотов (P20/P24)
	metrics           bool          // WithMetrics: реальный adapter/metrics + admin app
	enet              bool          // выставляется tagged-опцией WithEnet (env_enet.go)
	enetPorts         []uint16      // 0-элемент = auto (свободный порт)
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

// WithMetrics подключает продовую телеметрию (P19b): реальный
// metricsadapter.Metrics вливается во все метрико-ёмкие компоненты
// (фабрики, registry, reservation, handlers, sweeps), на публичный app
// ставится HTTP metrics middleware, а Env.Admin получает admin app
// с GET /metrics (тот же fiber.Test-клиент, что и Env.App).
func WithMetrics() Option {
	return func(o *options) {
		o.metrics = true
	}
}

// WithIdleSweep поднимает продовый idle-eviction пайплайн
// (EvictIdlePeersUseCase + StartIdlePeerSweep, см. cmd/rooms/main.go) с заданным
// интервалом sweep'а. Без опции sweep не запускается. Работает только вместе с
// transport.PeerIdleTimeout > 0 — иначе IsAlive всегда true и эвиктить нечего.
func WithIdleSweep(interval time.Duration) Option {
	return func(o *options) {
		o.idleSweepInterval = interval
	}
}

// WithReconnectTTL задаёт reconnect grace admitted-слотов без peer в room
// (P20/P24): deadline-гейт resume в ReconnectRoomUseCase считает expiry от
// lastSeen disconnect'а + TTL. Без опции — продовый дефолт
// (adapterreservation.DefaultReconnectTTL). Ноль — любой resume !HasPeer
// expired. Orphan sweep в env не поднят, так что просроченная grace живёт
// до явного resume — ветка 0x74 детерминирована, sweep-опция не нужна.
func WithReconnectTTL(ttl time.Duration) Option {
	return func(o *options) {
		o.reconnectTTL = ttl
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
			WSWriteDeadline:       adaptertransport.DefaultWSWriteDeadline,
			// Drain-дефолт как у прода: teardown-тесты (0x82 до close, P30)
			// опираются на гарантию «Send до Close».
			PeerCloseDrainTimeout: adaptertransport.DefaultPeerCloseDrainTimeout,
		},
		reconnectTTL: adapterreservation.DefaultReconnectTTL,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	limits, err := httplimitsadapter.New(cfg.httpLimits)
	if err != nil {
		t.Fatalf("httplimits.New: %v", err)
	}

	// Телеметрия opt-in (WithMetrics): реальный prometheus-адаптер;
	// без опции - nil → Noop в каждом конструкторе, как раньше.
	var metricsAdapter *metricsadapter.Metrics
	var m portmetrics.Metrics
	if cfg.metrics {
		metricsAdapter, err = metricsadapter.New(metricsadapter.MetricsConfig{})
		if err != nil {
			t.Fatalf("metrics.New: %v", err)
		}
		m = metricsAdapter
	}

	logger := stdlib.New("test-env")
	// Продовая фабрика policy (та же сборка, что cmd/rooms): metrics
	// попадает в master-машину полем внутри фабрики.
	roomFactory := adapterrealtime.NewKindRoomFactory(
		adapterrealtime.NewRoomFactory(logger, statepolicy.NewStateRoomPolicyFactory(logger, m), cfg.transport.RoomIncomingQueueSize, m),
		map[domain.RoomPolicyKind]portrealtime.RoomFactory{
			domain.RoomPolicyKindAuthoritativeHost: adapterrealtime.NewRoomFactory(
				logger, statepolicy.NewAuthoritativeHostPolicyFactory(logger, m), cfg.transport.RoomIncomingQueueSize, m),
		},
	)
	peerFactory := adapterrealtime.NewPeerFactory(logger, cfg.transport.PeerOutboundQueueSize, cfg.transport.PeerCloseDrainTimeout, m)

	registry := adapterregistry.NewRoomRegistry(context.Background(), roomFactory, logger, m)
	reservation := adapterreservation.NewReservation(logger, m)
	admission := admissionadapter.NewAdmission(admissionadapter.AdmissionConfig{
		Secret: []byte("test-secret"),
		TTL:    time.Hour,
	})
	allocator := identityadapter.NewCounter()
	connectionFactory := transportws.NewWsConnectionFactory(logger, cfg.transport)

	leaveUC := useroom.NewLeaveRoomUseCase(registry, reservation, logger)
	disconnectUC := useroom.NewDisconnectPeerUseCase(registry, reservation, logger)
	deleteUC := useroom.NewDeleteUseCase(registry, reservation, leaveUC, logger)
	getListUC := useroom.NewGetListUseCase(registry, logger)
	joinUC := useroom.NewJoinRoomUseCase(peerFactory, registry, reservation, logger)
	reconnectUC := useroom.NewReconnectRoomUseCase(peerFactory, registry, reservation, cfg.reconnectTTL, logger, m)
	admitUC := useroom.NewAdmitPeerUseCase(admission, joinUC, reconnectUC, logger)

	var enetHandler *deliveryenet.RoomsHandler
	var enetPorts []uint16
	if cfg.enet {
		// Продовый ENet mux listener (P18): по host'у на порт; 0-элементы
		// enetPorts - auto-свободные порты. Символы stub-safe (без тега enet
		// Listen - no-op), но cfg.enet ставит только tagged WithEnet → в
		// untagged-сборке блок мёртв.
		enetCfg := adapterenet.DefaultEnetConfig()
		enetCfg.ListenPorts = resolveEnetPorts(t, cfg.enetPorts)
		enetCfg.MaxIncomingFrameBytes = cfg.transport.MaxIncomingFrameBytes
		enetCfg.EnetIncomingQueueSize = cfg.transport.EnetIncomingQueueSize
		// ServiceTimeoutMs = 1: короткие service-итерации, чтобы серверный
		// HostLoop чаще разгребал очередь команд - шаг poll'ов тестов ~10ms,
		// лишний latency send-пути не нужен. Глобального apiMu-конвоя P12
		// больше нет: у каждого host'а своя очередь (P15).
		enetCfg.ServiceTimeoutMs = 1
		enetPorts = enetCfg.ListenPorts
		enetHandler = deliveryenet.NewRoomsHandler(context.Background(), admitUC, disconnectUC,
			adapterenet.NewEnetConnectionFactory(nil, logger), enetCfg, logger, m)
	}

	// Handler (когда ENet включён) - HostRegistry: strict binding ticket'ов
	// к назначенному host'у; nil - ticket'ы без привязки.
	var hostRegistry portenet.HostRegistry
	if enetHandler != nil {
		hostRegistry = enetHandler
	}
	issueUC := useroom.NewIssueTicketUseCase(registry, allocator, admission, reservation, hostRegistry, logger)
	createUC := useroom.NewCreateUseCase(registry, allocator, reservation, limits, issueUC, logger)

	var idleSweep *useroom.SweepHandle
	if cfg.idleSweepInterval > 0 {
		evictIdleUC := useroom.NewEvictIdlePeersUseCase(registry, disconnectUC, logger, m)
		idleSweep = useroom.StartIdlePeerSweep(context.Background(), evictIdleUC, cfg.idleSweepInterval, logger)
	}

	app := fiber.New()
	if m != nil {
		app.Use(deliveryhttp.NewMetricsMiddleware(m))
	}
	deliveryhttp.NewRoomsHandler(createUC, issueUC, deleteUC, getListUC, logger).
		RegisterRoutes(app, cfg.httpAuth, limits)
	deliveryws.NewRoomsHandler(context.Background(), admitUC, disconnectUC, connectionFactory, cfg.transport, logger, m).
		RegisterRoutes(app)

	var adminApp *fiber.App
	if metricsAdapter != nil {
		adminApp = admin.NewMetricsApp(metricsAdapter.Gatherer(), logger)
	}

	e := &Env{
		App:         app,
		Registry:    registry,
		Reservation: reservation,
		Admission:   admission,
		Allocator:   allocator,
		Metrics:     metricsAdapter,
		Admin:       adminApp,
		enetPorts:   enetPorts,
	}

	if enetHandler != nil {
		e.enetHandle = enetHandler.Listen()
		// Fail-fast на bind error: run() выходит сразу при ошибке NewHost.
		select {
		case <-e.enetHandle.Done():
			t.Fatalf("enet listener exited: %v", e.enetHandle.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}

	t.Cleanup(func() {
		// Sweep стопаем первым: RunOnce ходит в registry/leave-поток и не должен
		// гоняться за shutdown'ом. Handle nil-safe (interval <= 0 → nil).
		idleSweep.Stop()
		// ENet до shutdown app/registry: Stop ждёт выхода host loop'а —
		// EventDisconnect'ы при разрушении комнат не должны гнаться за ним.
		if e.enetHandle != nil {
			e.enetHandle.Stop()
		}
		e.closeWSListener()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if e.Admin != nil {
			_ = e.Admin.ShutdownWithContext(shutdownCtx)
		}
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

// freeUDPPort резервирует свободный UDP-порт (bind :0 → close) для ENet listener'а.
func freeUDPPort(t *testing.T) uint16 {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("reserve udp port: %v", err)
	}
	defer func() { _ = conn.Close() }()
	return uint16(conn.LocalAddr().(*net.UDPAddr).Port)
}

// resolveEnetPorts разрешает 0-элементы списка в свободные порты и проверяет
// дубликаты (явных и полученных auto-портов). Пустой список → один free порт.
func resolveEnetPorts(t *testing.T, ports []uint16) []uint16 {
	t.Helper()
	if len(ports) == 0 {
		ports = []uint16{0}
	}
	resolved := make([]uint16, len(ports))
	seen := make(map[uint16]bool, len(ports))
	for i, p := range ports {
		if p == 0 {
			for seen[p] || p == 0 {
				p = freeUDPPort(t)
			}
		}
		if seen[p] {
			t.Fatalf("duplicate enet listen port %d", p)
		}
		seen[p] = true
		resolved[i] = p
	}
	return resolved
}

// CreateRoom делает POST /rooms {"capacity":4,"nick_name":nick} и возвращает
// roomID и host ticket (token уже base64url — в WS query идёт как есть).
func (e *Env) CreateRoom(t *testing.T, nick string) (domain.RoomID, string) {
	t.Helper()

	roomID, token, _ := e.CreateRoomPort(t, nick)
	return roomID, token
}

// CreateRoomPort - то же, что CreateRoom, плюс назначенный ENet-порт тикета
// (P18: ответ issue несёт enet_port; 0 - env без ENet listener'а).
func (e *Env) CreateRoomPort(t *testing.T, nick string) (domain.RoomID, string, uint16) {
	t.Helper()
	return e.createRoomPost(t, map[string]any{
		"capacity":  4,
		"nick_name": nick,
	})
}

// CreateRoomWithPolicy — то же, что CreateRoom, с явным полем "policy"
// в теле (P29: "state" / "authoritative-host").
func (e *Env) CreateRoomWithPolicy(t *testing.T, nick, policy string) (domain.RoomID, string) {
	t.Helper()
	roomID, token, _ := e.createRoomPost(t, map[string]any{
		"capacity":  4,
		"nick_name": nick,
		"policy":    policy,
	})
	return roomID, token
}

func (e *Env) createRoomPost(t *testing.T, payload map[string]any) (domain.RoomID, string, uint16) {
	t.Helper()

	resp, body := e.Do(t, http.MethodPost, "/rooms", payload)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create room status = %d, body = %s", resp.StatusCode, body)
	}
	var created struct {
		Token    string `json:"token"`
		EnetPort uint16 `json:"enet_port"`
		Room     struct {
			ID int64 `json:"id"`
		} `json:"room"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("unmarshal create room: %v", err)
	}
	if created.Token == "" {
		t.Fatal("create room: empty token")
	}
	return domain.RoomID(created.Room.ID), created.Token, created.EnetPort
}

// IssueTicket делает POST /rooms/:id/tickets {"nick_name":nick} и возвращает
// joiner ticket. Комната без пароля — VerifyRoomPassword пропускает пустой.
func (e *Env) IssueTicket(t *testing.T, roomID domain.RoomID, nick string) string {
	t.Helper()

	token, _ := e.IssueTicketPort(t, roomID, nick)
	return token
}

// IssueTicketPort - то же, что IssueTicket, плюс назначенный ENet-порт тикета
// (0 - env без ENet listener'а).
func (e *Env) IssueTicketPort(t *testing.T, roomID domain.RoomID, nick string) (string, uint16) {
	t.Helper()

	token, _, port := e.IssueTicketReconnect(t, roomID, nick)
	return token, port
}

// IssueTicketReconnect - то же, что IssueTicketPort, плюс reconnect-токен
// (P20): credential resume под тем же peerID после разрыва.
func (e *Env) IssueTicketReconnect(t *testing.T, roomID domain.RoomID, nick string) (joinToken, reconnectToken string, enetPort uint16) {
	t.Helper()

	resp, body := e.Do(t, http.MethodPost, fmt.Sprintf("/rooms/%d/tickets", roomID), map[string]any{
		"nick_name": nick,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("issue ticket status = %d, body = %s", resp.StatusCode, body)
	}
	var ticket struct {
		Token          string `json:"token"`
		ReconnectToken string `json:"reconnect_token"`
		EnetPort       uint16 `json:"enet_port"`
	}
	if err := json.Unmarshal(body, &ticket); err != nil {
		t.Fatalf("unmarshal ticket: %v", err)
	}
	if ticket.Token == "" {
		t.Fatal("issue ticket: empty token")
	}
	if ticket.ReconnectToken == "" {
		t.Fatal("issue ticket: empty reconnect token")
	}
	return ticket.Token, ticket.ReconnectToken, ticket.EnetPort
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
