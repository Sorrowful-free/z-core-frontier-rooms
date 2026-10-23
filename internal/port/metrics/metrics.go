// Package metrics — потребительский порт телеметрии процесса (P19).
//
// Плоский интерфейс типизированных сигналов (как `port/logging.Logger`):
// компоненты сообщают события доменными глаголами, адаптер
// (`internal/adapter/metrics`) мапит их на prometheus-метрики. Кардинальность
// label'ов ограничена типами параметров (wire OpCode).
package metrics

import (
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

// Metrics — порт экспорта сигналов наблюдаемости. Все методы безошибочны
// и неблокирующи с точки зрения контракта (реализация сама отвечает за
// thread-safety; prometheus-коллекторы конкурентно-безопасны).
type Metrics interface {
	// SetActiveRooms — gauge: текущее число живых комнат в registry.
	SetActiveRooms(rooms int)
	// AddActivePeers — gauge: дельта живых peer'ов суммарно по комнатам.
	AddActivePeers(delta int)

	// PeerJoined — counter: peer вошёл в комнату (lifecycle room loop).
	PeerJoined()
	// PeerLeft — counter: peer изъят из комнаты (lifecycle room loop).
	PeerLeft()

	// JoinRejected — counter: join/admit reject-кадр на wire, label —
	// OpCode отказа (bounded: конечное множество reject-opcode'ов).
	JoinRejected(op domain.OpCode)

	// RoomIncomingDropped — counter: кадр отброшен из переполненной
	// очереди room.incoming (ErrQueueFull).
	RoomIncomingDropped()
	// PeerOutboundDropped — counter: кадр отброшен из переполненной
	// очереди peer.outbound (ErrQueueFull).
	PeerOutboundDropped()
	// EnetIncomingDropped — counter: кадр отброшен из переполненной
	// очереди ENet-сессии (EnetIncomingQueueSize).
	EnetIncomingDropped()

	// RoomIncomingDepth — gauge: глубина очереди room.incoming.
	// Сэмпл `len(chan)` в точках enqueue/dequeue — приближение
	// (last-write-wins между комнатами), а не точная сумма.
	RoomIncomingDepth(n int)
	// PeerOutboundDepth — gauge: глубина очереди peer.outbound.
	// Сэмпл `len(chan)` в точках enqueue/dequeue — приближение.
	PeerOutboundDepth(n int)
	// RoomTickObserved — duration тика room loop (summary);
	// kind — "full" | "patch" (bounded, константы в adapter/realtime).
	RoomTickObserved(kind string, d time.Duration)
	// PeerSendError — counter: ошибка Send на живом соединении
	// (ожидаемый teardown с !IsAlive не считается).
	PeerSendError()

	// EnetHostPeers — gauge: живые ENet-сессии на host'е; label
	// hostID ограничен числом портов ENET_LISTEN_PORTS.
	EnetHostPeers(hostID domain.HostID, peers int)
	// EnetHostLoopQueueLatency — duration ожидания команды в очереди
	// HostLoop (enqueue → exec, summary; агрегировано по host'ам).
	EnetHostLoopQueueLatency(d time.Duration)

	// MasterElected — counter: комната получила нового master
	// (первичное избрание и перевыборы, master_machine).
	MasterElected()

	// ReservationReserved — counter: слот reservation занят (Reserve).
	ReservationReserved()
	// ReservationAdmitted — counter: слот переведён в admitted (Admit).
	ReservationAdmitted()
	// ReservationRevoked — counter: слот освобождён (любой реальный
	// delete: Revoke, expired в Admit, lazy sweep просроченных).
	ReservationRevoked()
	// ReservationActiveSlots — gauge: дельта живых слотов reservation
	// (reserve +1; каждое реальное удаление слота -1; tombstone
	// комнаты при UnregisterRoom -len(peers)).
	ReservationActiveSlots(delta int)

	// OrphanAdmittedRevoked — counter: orphan admitted слотов снято
	// за проход SweepOrphanAdmittedUseCase.
	OrphanAdmittedRevoked(n int)
	// IdlePeersEvicted — counter: idle peer'ов выведено за проход
	// EvictIdlePeersUseCase.
	IdlePeersEvicted(n int)
	// SweepRan — counter: проход фонового sweep (отдельно от
	// processed); sweep — "orphan_admitted" | "idle_peers".
	SweepRan(sweep string)

	// HTTPRequestObserved — duration HTTP-запроса публичного API
	// (histogram); route — шаблон маршрута Fiber (не raw path,
	// кардинальность ограничена роутером), status — HTTP-код.
	HTTPRequestObserved(route, method string, status int, d time.Duration)
}

// noopMetrics — реализация-заглушка: все сигналы отбрасываются.
type noopMetrics struct{}

func (noopMetrics) SetActiveRooms(int)                                     {}
func (noopMetrics) AddActivePeers(int)                                     {}
func (noopMetrics) PeerJoined()                                            {}
func (noopMetrics) PeerLeft()                                              {}
func (noopMetrics) JoinRejected(domain.OpCode)                             {}
func (noopMetrics) RoomIncomingDropped()                                   {}
func (noopMetrics) PeerOutboundDropped()                                   {}
func (noopMetrics) EnetIncomingDropped()                                   {}
func (noopMetrics) RoomIncomingDepth(int)                                  {}
func (noopMetrics) PeerOutboundDepth(int)                                  {}
func (noopMetrics) RoomTickObserved(string, time.Duration)                 {}
func (noopMetrics) PeerSendError()                                         {}
func (noopMetrics) EnetHostPeers(domain.HostID, int)                       {}
func (noopMetrics) EnetHostLoopQueueLatency(time.Duration)                 {}
func (noopMetrics) MasterElected()                                         {}
func (noopMetrics) ReservationReserved()                                   {}
func (noopMetrics) ReservationAdmitted()                                   {}
func (noopMetrics) ReservationRevoked()                                    {}
func (noopMetrics) ReservationActiveSlots(int)                             {}
func (noopMetrics) OrphanAdmittedRevoked(int)                              {}
func (noopMetrics) IdlePeersEvicted(int)                                   {}
func (noopMetrics) SweepRan(string)                                        {}
func (noopMetrics) HTTPRequestObserved(string, string, int, time.Duration) {}

// Noop — заглушка Metrics: принимает и отбрасывает все сигналы.
// Умолчание для компонентов, собранных без телеметрии (тесты).
func Noop() Metrics {
	return noopMetrics{}
}

// OrNoop нормализует nil к Noop — точка nil-безопасности DI:
// конструктор может принимать nil вместо реализации.
func OrNoop(m Metrics) Metrics {
	if m == nil {
		return noopMetrics{}
	}
	return m
}
