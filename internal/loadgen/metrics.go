package loadgen

import (
	"sync"
	"sync/atomic"
	"time"
)

// Metrics — run counters. The hot path is atomics; rare labelled counters
// (rejects by opcode, errors by kind) are under a mutex.
type Metrics struct {
	RoomsCreated  atomic.Int64
	TicketsIssued atomic.Int64
	JoinAttempts  atomic.Int64 // dials with a join ticket
	JoinSuccess   atomic.Int64 // first 0x01 FullState after a join dial

	FramesSent    atomic.Int64
	FramesRecv    atomic.Int64
	BytesRecv     atomic.Int64
	FullStateRecv atomic.Int64 // 0x01
	PatchRecv     atomic.Int64 // 0x02
	RpcRecv       atomic.Int64 // 0x07

	// Room notifications (0x80+).
	PeerDisconnectedRecv  atomic.Int64 // 0x80
	PeerReconnectedRecv   atomic.Int64 // 0x81
	RoomClosedRecv        atomic.Int64 // 0x82 (delivery before P30 — baseline)
	UnexpectedDisconnects atomic.Int64 // conn death without our close/0x82/ctx

	ReconnectAttempts atomic.Int64 // planned drops (RECONNECT_RATE)
	ReconnectSuccess  atomic.Int64 // first frame of a session after a resume dial
	ResumeRejects     atomic.Int64 // reject frames on a resume dial (slot/room dead)

	mu            sync.Mutex
	joinRejects   map[uint8]int64  // wire opcode → count (0x40–0x7F)
	inRoomErrors  map[uint8]int64  // wire opcode → count (0x60–0x6F)
	dialErrors    map[string]int64 // transport → count
	httpErrors    map[string]int64 // op (create|issue|list|metrics|netem) → count
	netemAssigned map[string]int64 // netem profile → clients (inc2)
	sendErrors    int64            // Send errors (excluding expected teardown)
	recvErrors    int64            // Recv errors that are not an expected drop
	decodeErrors  int64            // codec decode errors on state frames
	dialLatencyMs []float64        // all samples — sorted at report time
	joinLatencyMs []float64
}

func newMetrics() *Metrics {
	return &Metrics{
		joinRejects:   make(map[uint8]int64),
		inRoomErrors:  make(map[uint8]int64),
		dialErrors:    make(map[string]int64),
		httpErrors:    make(map[string]int64),
		netemAssigned: make(map[string]int64),
	}
}

func (m *Metrics) joinReject(op byte) {
	m.mu.Lock()
	m.joinRejects[op]++
	m.mu.Unlock()
}

func (m *Metrics) inRoomError(op byte) {
	m.mu.Lock()
	m.inRoomErrors[op]++
	m.mu.Unlock()
}

func (m *Metrics) dialError(transport string) {
	m.mu.Lock()
	m.dialErrors[transport]++
	m.mu.Unlock()
}

func (m *Metrics) httpError(op string) {
	m.mu.Lock()
	m.httpErrors[op]++
	m.mu.Unlock()
}

// netemAssign — a client was assigned a profile at spawn (shares are checked
// against NETEM_SHARE in the report, contract ±5%).
func (m *Metrics) netemAssign(profile string) {
	m.mu.Lock()
	m.netemAssigned[profile]++
	m.mu.Unlock()
}

func (m *Metrics) sendError() {
	m.mu.Lock()
	m.sendErrors++
	m.mu.Unlock()
}

func (m *Metrics) recvError() {
	m.mu.Lock()
	m.recvErrors++
	m.mu.Unlock()
}

func (m *Metrics) decodeError() {
	m.mu.Lock()
	m.decodeErrors++
	m.mu.Unlock()
}

func (m *Metrics) recordDial(d time.Duration) {
	m.mu.Lock()
	m.dialLatencyMs = append(m.dialLatencyMs, float64(d.Microseconds())/1000.0)
	m.mu.Unlock()
}

func (m *Metrics) recordJoin(d time.Duration) {
	m.mu.Lock()
	m.joinLatencyMs = append(m.joinLatencyMs, float64(d.Microseconds())/1000.0)
	m.mu.Unlock()
}

// --- snapshots for progress and the report -----------------------------------

func (m *Metrics) joinRejectsSnapshot() map[uint8]int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneU8(m.joinRejects)
}

func (m *Metrics) inRoomErrorsSnapshot() map[uint8]int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneU8(m.inRoomErrors)
}

func (m *Metrics) dialErrorsSnapshot() map[string]int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneS(m.dialErrors)
}

func (m *Metrics) httpErrorsSnapshot() map[string]int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneS(m.httpErrors)
}

func (m *Metrics) netemAssignedSnapshot() map[string]int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneS(m.netemAssigned)
}

func (m *Metrics) scalarErrorsSnapshot() (send, recv, decode int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sendErrors, m.recvErrors, m.decodeErrors
}

func (m *Metrics) latenciesSnapshot() (dial, join []float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]float64(nil), m.dialLatencyMs...),
		append([]float64(nil), m.joinLatencyMs...)
}

func cloneU8(src map[uint8]int64) map[uint8]int64 {
	out := make(map[uint8]int64, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func cloneS(src map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func mapSum(m map[string]int64) int64 {
	var s int64
	for _, v := range m {
		s += v
	}
	return s
}

func mapSumU8(m map[uint8]int64) int64 {
	var s int64
	for _, v := range m {
		s += v
	}
	return s
}
