package loadgen

import (
	"sync"
	"sync/atomic"
	"time"
)

// Metrics — счётчики прогона. Горячий путь — атомики; редкие labelled
// счётчики (reject'ы по opcode, ошибки по виду) — под mutex.
type Metrics struct {
	RoomsCreated  atomic.Int64
	TicketsIssued atomic.Int64
	JoinAttempts  atomic.Int64 // dial'ы с join-ticket
	JoinSuccess   atomic.Int64 // первый 0x01 FullState после join-dial

	FramesSent    atomic.Int64
	FramesRecv    atomic.Int64
	BytesRecv     atomic.Int64
	FullStateRecv atomic.Int64 // 0x01
	PatchRecv     atomic.Int64 // 0x02
	RpcRecv       atomic.Int64 // 0x07

	// Комнатные уведомления (0x80+).
	PeerDisconnectedRecv  atomic.Int64 // 0x80
	PeerReconnectedRecv   atomic.Int64 // 0x81
	RoomClosedRecv        atomic.Int64 // 0x82 (доставка до P30 — baseline)
	AuthRoomsDied         atomic.Int64 // auth-комнат, где host убит сценарием
	UnexpectedDisconnects atomic.Int64 // смерть conn без нашего close/0x82/ctx

	ReconnectAttempts atomic.Int64 // запланированные разрывы (RECONNECT_RATE)
	ReconnectSuccess  atomic.Int64 // первый кадр сессии после resume-dial'а
	ResumeRejects     atomic.Int64 // reject-кадры на resume-dial (слот/комната мертвы)

	mu            sync.Mutex
	joinRejects   map[uint8]int64  // wire opcode → count (0x40–0x7F)
	inRoomErrors  map[uint8]int64  // wire opcode → count (0x60–0x6F)
	dialErrors    map[string]int64 // transport → count
	httpErrors    map[string]int64 // op (create|issue|list|metrics) → count
	sendErrors    int64            // ошибки Send (кроме ожидаемого teardown)
	recvErrors    int64            // ошибки Recv, не разрыв-ожидание
	decodeErrors  int64            // codec decode state-кадров
	dialLatencyMs []float64        // все замеры — сортировка на отчёте
	joinLatencyMs []float64
}

func newMetrics() *Metrics {
	return &Metrics{
		joinRejects:  make(map[uint8]int64),
		inRoomErrors: make(map[uint8]int64),
		dialErrors:   make(map[string]int64),
		httpErrors:   make(map[string]int64),
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

// --- snapshots для прогресса и отчёта ----------------------------------------

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
