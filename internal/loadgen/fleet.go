package loadgen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Fleet — a fleet of virtual clients: ramp room creation via REST, sustain,
// drain with server-side polling, report collection. The server is an
// external address (LOADGEN_*_ADDR), not a subprocess.
type Fleet struct {
	cfg *Config
	m   *Metrics
	hc  *http.Client
	rnd *rand.Rand
	log io.Writer

	peersMu sync.Mutex
	peers   []*peer

	nickSeq atomic.Int64
	started time.Time // write-once in Run before goroutines start
}

// roomPlan — a scheduled room: sizes from ROOM_MIX.
type roomPlan struct {
	capacity  int
	occupancy int
}

// NewFleet builds the fleet. log — live progress (stdout from cmd/loadgen).
// Control-plane HTTP is marked too (clients behind a bad network also on
// the control plane): each new Transport conn gets an SO_MARK from a
// weighted pick of NETEM_SHARE; an empty share means an unmarked dial.
func NewFleet(cfg *Config, log io.Writer) *Fleet {
	return &Fleet{
		cfg: cfg,
		m:   newMetrics(),
		hc: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					mark, err := netemMarkFor(pickNetemGlobal(cfg.NetemShare))
					if err != nil {
						return nil, err
					}
					return markedDialer(mark).DialContext(ctx, network, addr)
				},
			},
		},
		rnd: rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())),
		log: log,
	}
}

// Run — the full run: ramp → sustain → drain → report. On ctx (SIGINT)
// ramp/sustain are interrupted, but drain and the report still run
// (partial).
func (f *Fleet) Run(ctx context.Context) *Report {
	started := time.Now()
	f.started = started
	plans := f.buildPlans()
	stopProgress := f.startProgress()
	defer close(stopProgress)

	f.ramp(ctx, plans)
	f.logf("ramp done in %s: rooms=%d tickets=%d clients=%d",
		time.Since(started).Round(time.Second),
		f.m.RoomsCreated.Load(), f.m.TicketsIssued.Load(), len(f.peersSnapshot()))

	f.sustain(ctx)

	drain := f.drain()

	rep := f.buildReport(started, plans, drain)
	rep.Interrupted = ctx.Err() != nil
	return rep
}

// pickWeighted — pick a value from a weighted mix (weights are not required
// to sum to 100).
func pickWeighted(rnd *rand.Rand, opts []weightedOption) string {
	total := 0
	for _, o := range opts {
		total += o.Weight
	}
	roll := rnd.IntN(total)
	for _, o := range opts {
		if roll < o.Weight {
			return o.Value
		}
		roll -= o.Weight
	}
	return opts[len(opts)-1].Value
}

// pickRoomMix — same for ROOM_MIX.
func pickRoomMix(rnd *rand.Rand, mix []RoomMixEntry) RoomMixEntry {
	total := 0
	for _, e := range mix {
		total += e.Weight
	}
	roll := rnd.IntN(total)
	for _, e := range mix {
		if roll < e.Weight {
			return e
		}
		roll -= e.Weight
	}
	return mix[len(mix)-1]
}

// buildPlans — a deterministic room plan: sizes are dealt weighted-randomly
// from ROOM_MIX.
func (f *Fleet) buildPlans() []roomPlan {
	plans := make([]roomPlan, 0, f.cfg.Rooms)
	for i := 0; i < f.cfg.Rooms; i++ {
		e := pickRoomMix(f.rnd, f.cfg.RoomMix)
		plans = append(plans, roomPlan{
			capacity:  e.Capacity,
			occupancy: e.Occupancy,
		})
	}
	return plans
}

// --- ramp ------------------------------------------------------------------

func (f *Fleet) ramp(ctx context.Context, plans []roomPlan) {
	interval := time.Duration(float64(time.Second) / f.cfg.Ramp)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for i, plan := range plans {
		if ctx.Err() != nil {
			return // interrupted — don't create the rest, go to drain
		}
		if i > 0 {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
		f.spawnRoom(ctx, plan)
	}
}

// spawnRoom — POST /rooms (host ticket) → POST /rooms/:id/tickets ×
// (occupancy-1) → spawn peers with transports from TRANSPORT_MIX.
func (f *Fleet) spawnRoom(ctx context.Context, plan roomPlan) {
	hostNick := f.nextNick()
	hostTicket, err := f.createRoom(ctx, plan, hostNick)
	if err != nil {
		f.m.httpError("create")
		return
	}
	f.m.RoomsCreated.Add(1)
	roomID := hostTicket.Room.ID

	f.spawnPeer(ctx, peerSpec{
		nick:           hostNick,
		roomID:         roomID,
		transport:      pickWeighted(f.rnd, f.cfg.TransportMix),
		joinToken:      hostTicket.Token,
		reconnectToken: hostTicket.ReconnectToken,
		enetAddr:       f.cfg.EnetAddr,
		enetPort:       hostTicket.EnetPort,
	})

	for i := 1; i < plan.occupancy; i++ {
		nick := f.nextNick()
		ticket, err := f.issueTicket(ctx, roomID, nick)
		if err != nil {
			f.m.httpError("issue")
			continue
		}
		f.m.TicketsIssued.Add(1)
		f.spawnPeer(ctx, peerSpec{
			nick:           nick,
			roomID:         roomID,
			transport:      pickWeighted(f.rnd, f.cfg.TransportMix),
			joinToken:      ticket.Token,
			reconnectToken: ticket.ReconnectToken,
			enetAddr:       f.cfg.EnetAddr,
			enetPort:       ticket.EnetPort,
		})
	}
}

// pickNetemGlobal — weighted pick of a netem profile via the
// concurrency-safe package-level rand (the Transport's DialContext is
// called from its background goroutines — f.rnd must not be touched there;
// the same pick works for peer spawns too). Empty share → "" (no marking).
func pickNetemGlobal(opts []weightedOption) string {
	if len(opts) == 0 {
		return ""
	}
	total := 0
	for _, o := range opts {
		total += o.Weight
	}
	roll := rand.IntN(total)
	for _, o := range opts {
		if roll < o.Weight {
			return o.Value
		}
		roll -= o.Weight
	}
	return opts[len(opts)-1].Value
}

func (f *Fleet) spawnPeer(ctx context.Context, spec peerSpec) {
	profile := pickNetemGlobal(f.cfg.NetemShare)
	mark, err := netemMarkFor(profile)
	if err != nil { // names are validated in LoadConfigFromEnv — unreachable
		f.m.httpError("netem")
		return
	}
	spec.netemMark = mark
	if profile != "" {
		f.m.netemAssign(profile)
	}
	p := newPeer(f.cfg, f.m, spec)
	f.peersMu.Lock()
	f.peers = append(f.peers, p)
	f.peersMu.Unlock()
	go p.run(ctx)
}

func (f *Fleet) nextNick() string {
	return fmt.Sprintf("lg-%d", f.nickSeq.Add(1))
}

func (f *Fleet) peersSnapshot() []*peer {
	f.peersMu.Lock()
	defer f.peersMu.Unlock()
	return append([]*peer(nil), f.peers...)
}

// enetHost — default for LOADGEN_ENET_ADDR: the host part of the WS address
// (ENet lives on the same host as HTTP/WS).
func enetHost(wsAddr string) string {
	host, _, ok := strings.Cut(wsAddr, ":")
	if !ok || host == "" {
		return wsAddr
	}
	return host
}

// --- HTTP control plane ------------------------------------------------------

// ticketResponse — the issueTicketResponse fields (delivery/http/dto.go)
// the client needs: join + reconnect tokens, enet_port, room.
type ticketResponse struct {
	Token          string `json:"token"`
	ReconnectToken string `json:"reconnect_token"`
	EnetPort       uint16 `json:"enet_port"`
	Room           struct {
		ID int64 `json:"id"`
	} `json:"room"`
}

func (f *Fleet) createRoom(ctx context.Context, plan roomPlan, nick string) (*ticketResponse, error) {
	body := map[string]any{
		"capacity":  plan.capacity,
		"nick_name": nick,
	}
	var out ticketResponse
	if err := f.postJSON(ctx, "/rooms", body, http.StatusCreated, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (f *Fleet) issueTicket(ctx context.Context, roomID int64, nick string) (*ticketResponse, error) {
	var out ticketResponse
	err := f.postJSON(ctx, fmt.Sprintf("/rooms/%d/tickets", roomID),
		map[string]any{"nick_name": nick}, http.StatusCreated, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (f *Fleet) listRooms(ctx context.Context) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.httpURL("/rooms"), nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /rooms status %d: %s", resp.StatusCode, data)
	}
	return json.RawMessage(data), nil
}

func (f *Fleet) httpURL(path string) string {
	return "http://" + f.cfg.HTTPAddr + path
}

func (f *Fleet) postJSON(ctx context.Context, path string, body any, wantStatus int, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.httpURL(path), bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.hc.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != wantStatus {
		return fmt.Errorf("POST %s status %d: %s", path, resp.StatusCode, respBody)
	}
	return json.Unmarshal(respBody, out)
}

// --- sustain -----------------------------------------------------------------

// sustain — fleet lifetime until DURATION (or ctx).
func (f *Fleet) sustain(ctx context.Context) {
	timer := time.NewTimer(f.cfg.Duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// --- drain -------------------------------------------------------------------

// drainResult — outcome of the drain phase for the report.
type drainResult struct {
	complete    bool
	elapsed     time.Duration
	roomsDump   json.RawMessage
	activeRooms float64
	activePeers float64
	activeSlots float64
}

// drain — abrupt close of all connections (clients do not delete rooms —
// the server must collect empty rooms and slots itself), then poll admin
// /metrics until rooms_active_rooms + rooms_reservation_active_slots reach
// zero, within the DRAIN_TIMEOUT deadline. If it doesn't reach 0 → a GET
// /rooms dump goes into the report.
func (f *Fleet) drain() drainResult {
	start := time.Now()
	for _, p := range f.peersSnapshot() {
		p.closeForGood()
	}
	// Peer goroutines exit on closeReq — the conn is already closed; we wait
	// for them via done channels (bounded: Recv fails right after Close).
	waitDone := make(chan struct{})
	go func() {
		for _, p := range f.peersSnapshot() {
			<-p.done
		}
		close(waitDone)
	}()
	select {
	case <-waitDone:
	case <-time.After(15 * time.Second):
		f.logf("drain: peer goroutines did not all exit in 15s")
	}

	res := drainResult{}
	deadline := start.Add(f.cfg.DrainTimeout)
	for {
		rooms, peers, slots, err := f.scrapeGauges()
		if err != nil {
			f.m.httpError("metrics")
		} else {
			res.activeRooms, res.activePeers, res.activeSlots = rooms, peers, slots
			if rooms == 0 && slots == 0 {
				res.complete = true
				break
			}
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	res.elapsed = time.Since(start)
	if !res.complete {
		dumpCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		dump, err := f.listRooms(dumpCtx)
		cancel()
		if err != nil {
			f.m.httpError("list")
		} else {
			res.roomsDump = dump
		}
	}
	f.logf("drain: complete=%t in %s (active_rooms=%g, active_peers=%g, active_slots=%g)",
		res.complete, res.elapsed.Round(time.Second), res.activeRooms, res.activePeers, res.activeSlots)
	return res
}

// scrapeGauges — reads the prometheus exposition from ADMIN_LISTEN_ADDR and
// extracts rooms_active_rooms / rooms_active_peers /
// rooms_reservation_active_slots.
func (f *Fleet) scrapeGauges() (rooms, peers, slots float64, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+f.cfg.AdminAddr+"/metrics", nil)
	if err != nil {
		return 0, 0, 0, err
	}
	resp, err := f.hc.Do(req)
	if err != nil {
		return 0, 0, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, 0, err
	}
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		if v, ok := scrapeValue(line, "rooms_active_rooms"); ok {
			rooms = v
		}
		if v, ok := scrapeValue(line, "rooms_active_peers"); ok {
			peers = v
		}
		if v, ok := scrapeValue(line, "rooms_reservation_active_slots"); ok {
			slots = v
		}
	}
	return rooms, peers, slots, nil
}

// scrapeValue — "name{labels}? value" → value for the wanted metric.
func scrapeValue(line, name string) (float64, bool) {
	if !strings.HasPrefix(line, name) {
		return 0, false
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0, false
	}
	if fields[0] != name && !strings.HasPrefix(fields[0], name+"{") {
		return 0, false
	}
	v, err := strconv.ParseFloat(fields[len(fields)-1], 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// --- live progress -----------------------------------------------------------

func (f *Fleet) startProgress() chan struct{} {
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(f.cfg.LogEvery)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				f.printProgress()
			}
		}
	}()
	return stop
}

func (f *Fleet) printProgress() {
	m := f.m
	f.logf("clients=%d joined=%d recon=%d/%d in=%d out=%d err{http=%d dial=%d rej=%d} 0x82=%d dead=%d",
		len(f.peersSnapshot()), m.JoinSuccess.Load(),
		m.ReconnectSuccess.Load(), m.ReconnectAttempts.Load(),
		m.FramesRecv.Load(), m.FramesSent.Load(),
		mapSum(m.httpErrorsSnapshot()), mapSum(m.dialErrorsSnapshot()), mapSumU8(m.joinRejectsSnapshot()),
		m.RoomClosedRecv.Load(), m.UnexpectedDisconnects.Load())
}

func (f *Fleet) logf(format string, args ...any) {
	if f.log == nil {
		return
	}
	fmt.Fprintf(f.log, "[loadgen] +%s %s\n",
		time.Since(f.started).Round(time.Second), fmt.Sprintf(format, args...))
}
