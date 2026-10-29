// Package env — a shared composition root for delivery/integration tests:
// wiring of in-memory adapters, use cases and HTTP/WS handlers on top of a single fiber.App.
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
	useroom "github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
	fwebsocket "github.com/fasthttp/websocket"
	"github.com/gofiber/fiber/v3"
)

// Env — a white-box test environment: public fields give direct access
// to the assembled ports for asserts (peers, reservation slots, etc.).
type Env struct {
	App         *fiber.App
	Registry    portregistry.RoomRegistry
	Reservation portreservation.Reservation
	Admission   portadmission.Admission
	Allocator   portidentity.Allocator

	// Metrics - a real prometheus adapter (only with WithMetrics);
	// Admin - an admin Fiber app with GET /metrics on the same fiber.Test client.
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
	reconnectTTL      time.Duration // reconnect grace of admitted slots (P20/P24)
	statePolicy       statepolicy.StatePolicyConfig
	metrics           bool     // WithMetrics: real adapter/metrics + admin app
	enet              bool     // set by the tagged WithEnet option (env_enet.go)
	enetPorts         []uint16 // 0 element = auto (free port)
}

// Option overrides the default env config (auth Disabled, limits zero, transport zero).
type Option func(*options)

// WithHTTPAuth sets the API-key middleware config of the REST control plane.
func WithHTTPAuth(cfg httpauthadapter.HTTPAuthConfig) Option {
	return func(o *options) {
		o.httpAuth = cfg
	}
}

// WithHTTPLimits sets the rate/max-rooms limits config of the REST control plane.
func WithHTTPLimits(cfg httplimitsadapter.HTTPLimitsConfig) Option {
	return func(o *options) {
		o.httpLimits = cfg
	}
}

// WithTransport sets the data plane config (WS read limit, queues, idle).
func WithTransport(cfg adaptertransport.TransportConfig) Option {
	return func(o *options) {
		o.transport = cfg
	}
}

// WithMetrics enables production telemetry (P19b): a real
// metricsadapter.Metrics is injected into all metrics-bearing components
// (factories, registry, reservation, handlers, sweeps), the public app
// gets the HTTP metrics middleware, and Env.Admin receives an admin app
// with GET /metrics (the same fiber.Test client as Env.App).
func WithMetrics() Option {
	return func(o *options) {
		o.metrics = true
	}
}

// WithIdleSweep brings up the production idle-eviction pipeline
// (EvictIdlePeersUseCase + StartIdlePeerSweep, see cmd/rooms/main.go) with the
// given sweep interval. Without the option the sweep does not run. It works only
// together with transport.PeerIdleTimeout > 0 — otherwise IsAlive is always true
// and there is nothing to evict.
func WithIdleSweep(interval time.Duration) Option {
	return func(o *options) {
		o.idleSweepInterval = interval
	}
}

// WithStatePolicy sets the state room-policy config (P33): the process
// master-migration mode — AllowMasterMigration=false means the
// room dies when the master leaves (no-migration, ErrRoomShouldClose).
// Default — {AllowMasterMigration: true}.
func WithStatePolicy(cfg statepolicy.StatePolicyConfig) Option {
	return func(o *options) {
		o.statePolicy = cfg
	}
}

// WithReconnectTTL sets the reconnect grace of admitted slots without a peer
// in the room (P20/P24): the deadline gate of resume in ReconnectRoomUseCase
// computes expiry from the disconnect lastSeen + TTL. Without the option — the
// production default (adapterreservation.DefaultReconnectTTL). Zero — any resume
// of !HasPeer is expired. The orphan sweep is not running in env, so an expired
// grace lives until an explicit resume — the 0x74 branch is deterministic, no
// sweep option is needed.
func WithReconnectTTL(ttl time.Duration) Option {
	return func(o *options) {
		o.reconnectTTL = ttl
	}
}

// New assembles the full environment and registers cleanup
// (listener close, app shutdown, registry shutdown — so room/peer goroutines don't linger).
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
			// Drain default same as production: teardown tests (0x82 before close, P30)
			// rely on the "Send before Close" guarantee.
			PeerCloseDrainTimeout: adaptertransport.DefaultPeerCloseDrainTimeout,
		},
		reconnectTTL: adapterreservation.DefaultReconnectTTL,
		statePolicy:  statepolicy.StatePolicyConfig{AllowMasterMigration: true},
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	limits, err := httplimitsadapter.New(cfg.httpLimits)
	if err != nil {
		t.Fatalf("httplimits.New: %v", err)
	}

	// Telemetry is opt-in (WithMetrics): a real prometheus adapter;
	// without the option - nil → Noop in every constructor, as before.
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
	// Production policy factory (same wiring as cmd/rooms): metrics and
	// the master-migration mode reach the policy as a field inside the factory.
	roomFactory := adapterrealtime.NewRoomFactory(logger,
		statepolicy.NewStateRoomPolicyFactory(logger, cfg.statePolicy, m),
		cfg.transport.RoomIncomingQueueSize, m)
	peerFactory := adapterrealtime.NewPeerFactory(logger, cfg.transport.PeerOutboundQueueSize, cfg.transport.PeerCloseDrainTimeout, m)

	registry := adapterregistry.NewRoomRegistry(context.Background(), roomFactory, logger, m)
	reservation := adapterreservation.NewReservation(logger, m)
	admission := admissionadapter.NewAdmission(admissionadapter.AdmissionConfig{
		Secret: []byte("test-secret"),
		TTL:    time.Hour,
	})
	allocator := identityadapter.NewCounter()
	connectionFactory := transportws.NewWsConnectionFactory(logger, cfg.transport)

	disconnectUC := useroom.NewDisconnectPeerUseCase(registry, reservation, logger)
	deleteUC := useroom.NewDeleteUseCase(registry, reservation, logger)
	getListUC := useroom.NewGetListUseCase(registry, logger)
	joinUC := useroom.NewJoinRoomUseCase(peerFactory, registry, reservation, logger)
	reconnectUC := useroom.NewReconnectRoomUseCase(peerFactory, registry, reservation, cfg.reconnectTTL, logger, m)
	admitUC := useroom.NewAdmitPeerUseCase(admission, joinUC, reconnectUC, logger)

	// The handler is constructed unconditionally (P39): it is also the
	// HostRegistry of IssueTicket and the use case no longer accepts a nil
	// registry. Without cfg.enet the handler has zero hosts → AssignHost
	// returns a zero assignment → unbound tickets, as before.
	enetCfg := adapterenet.DefaultEnetConfig()
	enetCfg.ListenPorts = nil
	var enetPorts []uint16
	if cfg.enet {
		// Production ENet mux listener (P18): one host per port; 0 elements of
		// enetPorts are auto-assigned free ports. Symbols are stub-safe (without
		// the enet tag Listen is a no-op), but cfg.enet is set only by the tagged
		// WithEnet → the block is dead in an untagged build.
		enetCfg.ListenPorts = resolveEnetPorts(t, cfg.enetPorts)
		enetCfg.MaxIncomingFrameBytes = cfg.transport.MaxIncomingFrameBytes
		enetCfg.EnetIncomingQueueSize = cfg.transport.EnetIncomingQueueSize
		// ServiceTimeoutMs = 1: short service iterations so the server
		// HostLoop drains the command queue more often — the test poll step is
		// ~10ms, extra send-path latency is unwanted. The global apiMu convoy of
		// P12 is gone: each host has its own queue (P15).
		enetCfg.ServiceTimeoutMs = 1
		enetPorts = enetCfg.ListenPorts
	}
	enetHandler := deliveryenet.NewRoomsHandler(context.Background(), admitUC, disconnectUC,
		adapterenet.NewEnetConnectionFactory(nil, logger), enetCfg, logger, m)

	issueUC := useroom.NewIssueTicketUseCase(registry, allocator, admission, reservation, enetHandler, logger)
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
	leaveUC := useroom.NewLeaveRoomUseCase(registry, reservation, logger)
	deliveryhttp.NewRoomsHandler(createUC, issueUC, deleteUC, getListUC, leaveUC, admission, logger).
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

	if cfg.enet {
		e.enetHandle = enetHandler.Listen()
		// Fail-fast on bind error: run() exits immediately on a NewHost error.
		select {
		case <-e.enetHandle.Done():
			t.Fatalf("enet listener exited: %v", e.enetHandle.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}

	t.Cleanup(func() {
		// The sweep is stopped first: RunOnce touches the registry/leave flow and
		// must not race the shutdown. Handle is nil-safe (interval <= 0 → nil).
		idleSweep.Stop()
		// ENet before app/registry shutdown: Stop waits for the host loop exit —
		// EventDisconnects from room teardown must not race it.
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

// Do performs an HTTP request via app.Test; a non-nil body is marshaled to JSON.
func (e *Env) Do(t *testing.T, method, target string, body any) (*http.Response, []byte) {
	t.Helper()
	return e.DoWithHeaders(t, method, target, body, "", "")
}

// DoWithHeaders — same as Do, plus one arbitrary header (for auth cases).
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

// DialWS brings up a real listener (lazy, one per Env) and dials /ws?query.
// query is an already URL-encoded tail (e.g. "token=<base64url>"), without '?'.
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
		// App.Listener blocks until closeWSListener/ShutdownWithContext in t.Cleanup.
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

// freeUDPPort reserves a free UDP port (bind :0 → close) for the ENet listener.
func freeUDPPort(t *testing.T) uint16 {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("reserve udp port: %v", err)
	}
	defer func() { _ = conn.Close() }()
	return uint16(conn.LocalAddr().(*net.UDPAddr).Port)
}

// resolveEnetPorts resolves 0 elements of the list into free ports and checks
// for duplicates (both explicit and auto-assigned ports). Empty list → one free port.
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

// CreateRoom does POST /rooms {"capacity":4,"nick_name":nick} and returns
// the roomID and host ticket (the token is already base64url — goes into the WS query as-is).
func (e *Env) CreateRoom(t *testing.T, nick string) (domain.RoomID, string) {
	t.Helper()

	roomID, token, _ := e.CreateRoomPort(t, nick)
	return roomID, token
}

// CreateRoomPort - same as CreateRoom, plus the ticket's assigned ENet port
// (P18: the issue response carries enet_port; 0 - env without an ENet listener).
func (e *Env) CreateRoomPort(t *testing.T, nick string) (domain.RoomID, string, uint16) {
	t.Helper()
	roomID, token, _, port := e.createRoomPost(t, map[string]any{
		"capacity":  4,
		"nick_name": nick,
	})
	return roomID, token, port
}

// CreateRoomReconnect - same as CreateRoom, plus the host's reconnect token
// (P20): resume under the same peerID within the grace window after a drop.
func (e *Env) CreateRoomReconnect(t *testing.T, nick string) (roomID domain.RoomID, joinToken, reconnectToken string) {
	t.Helper()
	roomID, token, reconnect, _ := e.createRoomPost(t, map[string]any{
		"capacity":  4,
		"nick_name": nick,
	})
	return roomID, token, reconnect
}

func (e *Env) createRoomPost(t *testing.T, payload map[string]any) (domain.RoomID, string, string, uint16) {
	t.Helper()

	resp, body := e.Do(t, http.MethodPost, "/rooms", payload)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create room status = %d, body = %s", resp.StatusCode, body)
	}
	var created struct {
		Token          string `json:"token"`
		ReconnectToken string `json:"reconnect_token"`
		EnetPort       uint16 `json:"enet_port"`
		Room           struct {
			ID int64 `json:"id"`
		} `json:"room"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("unmarshal create room: %v", err)
	}
	if created.Token == "" {
		t.Fatal("create room: empty token")
	}
	if created.ReconnectToken == "" {
		t.Fatal("create room: empty reconnect token")
	}
	return domain.RoomID(created.Room.ID), created.Token, created.ReconnectToken, created.EnetPort
}

// IssueTicket does POST /rooms/:id/tickets {"nick_name":nick} and returns
// the joiner ticket. The room has no password — VerifyRoomPassword accepts an empty one.
func (e *Env) IssueTicket(t *testing.T, roomID domain.RoomID, nick string) string {
	t.Helper()

	token, _ := e.IssueTicketPort(t, roomID, nick)
	return token
}

// IssueTicketPort - same as IssueTicket, plus the ticket's assigned ENet port
// (0 - env without an ENet listener).
func (e *Env) IssueTicketPort(t *testing.T, roomID domain.RoomID, nick string) (string, uint16) {
	t.Helper()

	token, _, port := e.IssueTicketReconnect(t, roomID, nick)
	return token, port
}

// IssueTicketReconnect - same as IssueTicketPort, plus the reconnect token
// (P20): credential resume under the same peerID after a drop.
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

// WaitPeerNick waits for the presence (want=true) or absence (want=false) of a
// peer with nick in the room. Join/leave happen asynchronously via the room
// lifecycle and transport.
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

// WaitPeerCount waits until the room has exactly want peers.
func (e *Env) WaitPeerCount(t *testing.T, roomID domain.RoomID, want int) {
	t.Helper()
	e.pollPeers(t, roomID, func(peers []portrealtime.Peer) bool {
		return len(peers) == want
	}, fmt.Sprintf("peer count, want %d", want))
}

// pollPeers — a shared poll engine waiting for room state via the peers list:
// deadline ~1s, step ~10ms, t.Fatalf on timeout.
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
