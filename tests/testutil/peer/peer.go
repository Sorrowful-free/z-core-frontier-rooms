// Package peer — параметризуемый тестовый realtime.Peer: конфигурируемые
// id/nick/ping/alive/deliverErr и захват доставленных PeerEvent
// (snapshot/clear/фильтр по opcode). Консолидирует hand-written capture-стабы
// (stubPeer/capturePeer/flakyPeer/mortalPeer и др., см. docs/plans/P16-test-hygiene.md).
//
// Семантика Deliver повторяет продовый peer: injected deliverErr возвращается
// первым, затем мёртвый транспорт (SetAlive(false)/Stop) отклоняет кадры
// ошибкой ErrPeerDead, живой peer захватывает кадр (если capture не выключен).
package peer

import (
	"errors"
	"sync"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	portrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

// ErrPeerDead возвращается Deliver мёртвого peer'а (SetAlive(false) или после
// Stop) — моделирует отказ транспорта, как у продового peer после Stop.
var ErrPeerDead = errors.New("peer transport dead")

// Peer — тестовый realtime.Peer с fault-инъекцией и захватом доставленных кадров.
type Peer struct {
	id   domain.PeerID
	nick string

	mu         sync.Mutex
	ping       int64
	alive      bool
	capture    bool
	deliverErr error
	delivered  []events.PeerEvent
}

// Option переопределяет дефолтный конфиг peer'а (ping=0, alive=true, capture on).
type Option func(*Peer)

// WithPing задаёт значение Ping() — используется в сценариях master election.
func WithPing(ping int64) Option {
	return func(p *Peer) {
		p.ping = ping
	}
}

// WithoutCapture выключает захват доставленных кадров — для no-op peer'ов
// в бенчмарках, где накопление delivered искажало бы память/тайминги.
func WithoutCapture() Option {
	return func(p *Peer) {
		p.capture = false
	}
}

// New создаёт живого peer'а с включённым захватом кадров.
func New(id domain.PeerID, nick string, opts ...Option) *Peer {
	p := &Peer{id: id, nick: nick, alive: true, capture: true}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *Peer) GetID() domain.PeerID { return p.id }
func (p *Peer) GetNickName() string  { return p.nick }

// Start — no-op: fake не гоняет read/write loop'ы, sink игнорируется.
func (p *Peer) Start(portrealtime.EventSink) error { return nil }
func (p *Peer) Ping() int64                        { p.mu.Lock(); defer p.mu.Unlock(); return p.ping }
func (p *Peer) IsAlive() bool                      { p.mu.Lock(); defer p.mu.Unlock(); return p.alive }

// Stop помечает peer мёртвым: дальнейший Deliver отклоняется ErrPeerDead —
// повторяет поведение продового peer («peer stopped»).
func (p *Peer) Stop() error {
	p.mu.Lock()
	p.alive = false
	p.mu.Unlock()
	return nil
}

// Deliver: injected deliverErr → мёртвый транспорт (ErrPeerDead) → захват кадра.
func (p *Peer) Deliver(ev events.PeerEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.deliverErr != nil {
		return p.deliverErr
	}
	if !p.alive {
		return ErrPeerDead
	}
	if p.capture {
		p.delivered = append(p.delivered, ev)
	}
	return nil
}

// SetPing меняет значение Ping() (бенч-сценарии ping-resync тика).
func (p *Peer) SetPing(ping int64) {
	p.mu.Lock()
	p.ping = ping
	p.mu.Unlock()
}

// SetAlive переключает живость транспорта: !alive → IsAlive false и Deliver
// отклоняется ErrPeerDead (модель «транспорт умер без leave»).
func (p *Peer) SetAlive(alive bool) {
	p.mu.Lock()
	p.alive = alive
	p.mu.Unlock()
}

// SetDeliverErr включает/снимает постоянную ошибку Deliver (nil — здоровый peer).
func (p *Peer) SetDeliverErr(err error) {
	p.mu.Lock()
	p.deliverErr = err
	p.mu.Unlock()
}

// Snapshot — копия захваченных кадров в порядке доставки.
func (p *Peer) Snapshot() []events.PeerEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]events.PeerEvent, len(p.delivered))
	copy(out, p.delivered)
	return out
}

// EventsByOpcode — захваченные кадры с заданным opcode.
func (p *Peer) EventsByOpcode(opcode domain.OpCode) []events.PeerEvent {
	return FilterByOpcode(p.Snapshot(), opcode)
}

// Clear сбрасывает захваченные кадры (отсчёт событий «с этого момента»).
func (p *Peer) Clear() {
	p.mu.Lock()
	p.delivered = nil
	p.mu.Unlock()
}

// FilterByOpcode — события из среза с заданным opcode (для кадров, захваченных
// вне peer'а — например, spy-реализацией room.Send).
func FilterByOpcode(peerEvents []events.PeerEvent, opcode domain.OpCode) []events.PeerEvent {
	var out []events.PeerEvent
	for _, ev := range peerEvents {
		if ev.Frame.OpCode == opcode {
			out = append(out, ev)
		}
	}
	return out
}

// Opcodes — opcode'ы событий среза (для диагностических сообщений).
func Opcodes(peerEvents []events.PeerEvent) []domain.OpCode {
	out := make([]domain.OpCode, len(peerEvents))
	for i, ev := range peerEvents {
		out[i] = ev.Frame.OpCode
	}
	return out
}

var _ portrealtime.Peer = (*Peer)(nil)
