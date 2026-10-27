package loadgen

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

// Report — итог прогона (report.json по LOADGEN_REPORT). Без pass/fail-
// вердикта: чистый сбор; drain-дедлайн — событие (drain_complete=false +
// rooms_dump), не exit code.
type Report struct {
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
	Interrupted bool      `json:"interrupted"` // прогон оборван ctx (SIGINT)

	Config reportConfig `json:"config"`

	RoomsPlanned   int   `json:"rooms_planned"`
	RoomsCreated   int64 `json:"rooms_created"`
	ClientsPlanned int   `json:"clients_planned"`
	TicketsIssued  int64 `json:"tickets_issued"`

	JoinAttempts int64            `json:"join_attempts"`
	JoinSuccess  int64            `json:"join_success"`
	JoinRejects  map[string]int64 `json:"join_rejects,omitempty"`
	DialErrors   map[string]int64 `json:"dial_errors,omitempty"`
	HTTPErrors   map[string]int64 `json:"http_errors,omitempty"`

	DialLatencyMs latencyStats `json:"dial_latency_ms"`
	JoinLatencyMs latencyStats `json:"join_latency_ms"`

	FramesSent    int64 `json:"frames_sent"`
	FramesRecv    int64 `json:"frames_recv"`
	BytesRecv     int64 `json:"bytes_recv"`
	FullStateRecv int64 `json:"full_state_recv"`
	PatchRecv     int64 `json:"patch_state_recv"`
	RpcRecv       int64 `json:"rpc_recv"`

	InRoomErrors map[string]int64 `json:"in_room_errors,omitempty"`
	SendErrors   int64            `json:"send_errors"`
	RecvErrors   int64            `json:"recv_errors"`
	DecodeErrors int64            `json:"decode_errors"`

	ReconnectAttempts int64 `json:"reconnect_attempts"`
	ReconnectSuccess  int64 `json:"reconnect_success"`
	ResumeRejects     int64 `json:"resume_rejects"`

	PeerDisconnectedRecv  int64 `json:"peer_disconnected_recv"`
	PeerReconnectedRecv   int64 `json:"peer_reconnected_recv"`
	RoomClosedRecv        int64 `json:"room_closed_received"`
	AuthRoomsDied         int64 `json:"auth_rooms_died"`
	UnexpectedDisconnects int64 `json:"unexpected_disconnects"`

	// NetemClients — фактическое распределение клиентов по профилям
	// (сверка с config.netem_share, контракт ±5%); nil — share не задан.
	NetemClients map[string]int64 `json:"netem_clients,omitempty"`

	DrainComplete bool            `json:"drain_complete"`
	DrainElapsedS float64         `json:"drain_elapsed_s"`
	ActiveRooms   float64         `json:"server_active_rooms_final"`
	ActivePeers   float64         `json:"server_active_peers_final"`
	ActiveSlots   float64         `json:"server_active_slots_final"`
	RoomsDump     json.RawMessage `json:"rooms_dump,omitempty"`

	// ServerMetrics — raw prometheus exposition до/после прогона; заполняет
	// оркестратор (scripts/loadtest-run.sh мерджит снимки в отчёт).
	ServerMetrics json.RawMessage `json:"server_metrics,omitempty"`
}

type reportConfig struct {
	HTTPAddr       string  `json:"http_addr"`
	WSAddr         string  `json:"ws_addr"`
	AdminAddr      string  `json:"admin_addr"`
	Rooms          int     `json:"rooms"`
	Mode           string  `json:"mode"`
	InputHz        float64 `json:"input_hz"`
	EntitiesHz     float64 `json:"entities_hz"`
	Ramp           float64 `json:"ramp"`
	DurationS      float64 `json:"duration_s"`
	DrainTimeoutS  float64 `json:"drain_timeout_s"`
	ReconnectRate  float64 `json:"reconnect_rate"`
	ReconnectEvery float64 `json:"reconnect_every_s"`
	// NetemShare — сконфигурированные доли профилей (LOADGEN_NETEM_SHARE).
	NetemShare map[string]int `json:"netem_share,omitempty"`
}

// latencyStats — перцентили по всем замерам (выборка мала: ≤ clients count).
type latencyStats struct {
	Count int     `json:"count"`
	P50   float64 `json:"p50"`
	P95   float64 `json:"p95"`
	P99   float64 `json:"p99"`
	Max   float64 `json:"max"`
}

func percentile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(q * float64(len(sorted)-1))
	return sorted[idx]
}

func latencyOf(samples []float64) latencyStats {
	if len(samples) == 0 {
		return latencyStats{}
	}
	s := append([]float64(nil), samples...)
	sort.Float64s(s)
	return latencyStats{
		Count: len(s),
		P50:   percentile(s, 0.50),
		P95:   percentile(s, 0.95),
		P99:   percentile(s, 0.99),
		Max:   s[len(s)-1],
	}
}

// opCounts — map[opcode]int64 → map["0xNN"]int64 для JSON.
func opCounts(src map[uint8]int64) map[string]int64 {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]int64, len(src))
	for op, n := range src {
		out[fmt.Sprintf("0x%02x", op)] = n
	}
	return out
}

func (f *Fleet) buildReport(started time.Time, plans []roomPlan, drain drainResult) *Report {
	m := f.m
	sendErr, recvErr, decodeErr := m.scalarErrorsSnapshot()
	dialLat, joinLat := m.latenciesSnapshot()

	clients := 0
	for _, p := range plans {
		clients += p.occupancy
	}

	return &Report{
		StartedAt:  started.UTC(),
		FinishedAt: time.Now().UTC(),

		Config: reportConfig{
			HTTPAddr:       f.cfg.HTTPAddr,
			WSAddr:         f.cfg.WSAddr,
			AdminAddr:      f.cfg.AdminAddr,
			Rooms:          f.cfg.Rooms,
			Mode:           f.cfg.Mode,
			InputHz:        f.cfg.InputHz,
			EntitiesHz:     f.cfg.EntitiesHz,
			Ramp:           f.cfg.Ramp,
			DurationS:      f.cfg.Duration.Seconds(),
			DrainTimeoutS:  f.cfg.DrainTimeout.Seconds(),
			ReconnectRate:  f.cfg.ReconnectRate,
			ReconnectEvery: f.cfg.ReconnectEvery.Seconds(),
			NetemShare:     netemShareMap(f.cfg.NetemShare),
		},

		RoomsPlanned:   len(plans),
		RoomsCreated:   m.RoomsCreated.Load(),
		ClientsPlanned: clients,
		TicketsIssued:  m.TicketsIssued.Load(),

		JoinAttempts: m.JoinAttempts.Load(),
		JoinSuccess:  m.JoinSuccess.Load(),
		JoinRejects:  opCounts(m.joinRejectsSnapshot()),
		DialErrors:   nilIfEmpty(m.dialErrorsSnapshot()),
		HTTPErrors:   nilIfEmpty(m.httpErrorsSnapshot()),

		DialLatencyMs: latencyOf(dialLat),
		JoinLatencyMs: latencyOf(joinLat),

		FramesSent:    m.FramesSent.Load(),
		FramesRecv:    m.FramesRecv.Load(),
		BytesRecv:     m.BytesRecv.Load(),
		FullStateRecv: m.FullStateRecv.Load(),
		PatchRecv:     m.PatchRecv.Load(),
		RpcRecv:       m.RpcRecv.Load(),

		InRoomErrors: opCounts(m.inRoomErrorsSnapshot()),
		SendErrors:   sendErr,
		RecvErrors:   recvErr,
		DecodeErrors: decodeErr,

		ReconnectAttempts: m.ReconnectAttempts.Load(),
		ReconnectSuccess:  m.ReconnectSuccess.Load(),
		ResumeRejects:     m.ResumeRejects.Load(),

		NetemClients: nilIfEmpty(m.netemAssignedSnapshot()),

		PeerDisconnectedRecv:  m.PeerDisconnectedRecv.Load(),
		PeerReconnectedRecv:   m.PeerReconnectedRecv.Load(),
		RoomClosedRecv:        m.RoomClosedRecv.Load(),
		AuthRoomsDied:         m.AuthRoomsDied.Load(),
		UnexpectedDisconnects: m.UnexpectedDisconnects.Load(),

		DrainComplete: drain.complete,
		DrainElapsedS: drain.elapsed.Seconds(),
		ActiveRooms:   drain.activeRooms,
		ActivePeers:   drain.activePeers,
		ActiveSlots:   drain.activeSlots,
		RoomsDump:     drain.roomsDump,
	}
}

// netemShareMap — []weightedOption → {"profile": weight} для отчёта.
func netemShareMap(opts []weightedOption) map[string]int {
	if len(opts) == 0 {
		return nil
	}
	out := make(map[string]int, len(opts))
	for _, o := range opts {
		out[o.Value] = o.Weight
	}
	return out
}

func nilIfEmpty(m map[string]int64) map[string]int64 {
	if len(m) == 0 {
		return nil
	}
	return m
}

// Write — атомарная запись report.json (tmp + rename).
func (r *Report) Write(path string) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
