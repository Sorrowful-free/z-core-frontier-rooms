//go:build enet && cgo

package enet

import (
	"container/list"
	"errors"
	"sync"
	"time"

	libenet "github.com/codecat/go-enet"

	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
)

// ErrHostLoopStopped возвращается submit'ам, пришедшим после Stop: loop больше
// не принимает blocking-команды (Call/Do), а host уже на пути к Destroy.
var ErrHostLoopStopped = errors.New("enet host loop stopped")

// HostLoop владеет libenet.Host и сериализует ВСЕ обращения к нему: libenet не
// thread-safe, поэтому host/peer/packet трогает только loop-горутина -
// executor, разбирающий FIFO-очередь команд. Внешние горутины отправляют
// работу через Call/Do (blocking - для владельцев host'а: серверный драйвер
// service-итераций, клиентский pump, peer-операции из conn'ов) или enqueue
// (async - EnetConnection). Никакого глобального лока: изоляция per-host,
// конкурируют только submit'ы одного loop'а.
//
// Инвариант живучести: executor исполняет только libenet-вызовы, каждый из
// которых ограничен по времени (Service - serviceTimeoutMs). Blocking Call/Do
// поэтому не может deadlock'нуться: executor никогда не ждёт внешних
// горутин. Доменная работа, способная блокироваться (JoinRoom/LeaveRoom →
// dispatchLifecycle → ожидание room-горутины), обязана выполняться ВНЕ
// команд loop'а - иначе room-горутина, делающая blocking-вызов в loop
// (EnetConnection.Ping), замкнёт cross-deadlock executor↔room.
type HostLoop struct {
	host libenet.Host

	mu        sync.Mutex
	queue     *list.List // *hostLoopCommand, FIFO
	accepting bool       // false после Stop: blocking submit'ы отклоняются
	exited    bool       // loop-горутина ушла в Destroy: любые submit'ы отклоняются

	notify   chan struct{} // cap-1 будильник executor'а
	done     chan struct{} // закрыт после host.Destroy
	stopOnce sync.Once

	metrics portmetrics.Metrics
}

type hostLoopCommand struct {
	fn         func() error
	ack        chan struct{} // != nil у blocking-команд: закрывается после исполнения
	err        error
	enqueuedAt time.Time // момент постановки в очередь: сэмпл queue latency
}

// NewHostLoop создаёт libenet host (addr = NewListenAddress для сервера, nil
// для клиентского host'а) и запускает его loop-горутину. Host наружу не
// отдаётся как "живой" объект: вся работа с ним - через Call/Do/enqueue
// (Host() - только внутри таких команд). m - телеметрия ожидания команд в
// очереди (nil-safe, Noop).
func NewHostLoop(addr libenet.Address, peerLimit, channelLimit uint64, m portmetrics.Metrics) (*HostLoop, error) {
	host, err := libenet.NewHost(addr, peerLimit, channelLimit, 0, 0)
	if err != nil {
		return nil, err
	}
	l := &HostLoop{
		host:      host,
		queue:     list.New(),
		accepting: true,
		notify:    make(chan struct{}, 1),
		done:      make(chan struct{}),
		metrics:   portmetrics.OrNoop(m),
	}
	go l.run()
	return l, nil
}

// Host отдаёт owned libenet.Host для использования ВНУТРИ команд Call/Do (там
// вызов уже исполняется на loop-горутине). Любое другое использование нарушает
// confinement и является data race.
func (l *HostLoop) Host() libenet.Host {
	return l.host
}

// Call ставит fn в очередь loop'а и ждёт результата. Возвращает результат fn
// или ErrHostLoopStopped, если loop остановлен до submit'а.
func Call[T any](l *HostLoop, fn func() T) (T, error) {
	var result T
	if err := l.Do(func() error {
		result = fn()
		return nil
	}); err != nil {
		var zero T
		return zero, err
	}
	return result, nil
}

// Do - вариант Call для команд, возвращающих только ошибку.
func (l *HostLoop) Do(fn func() error) error {
	cmd := &hostLoopCommand{fn: fn, ack: make(chan struct{})}
	if err := l.submit(cmd, false); err != nil {
		return err
	}
	<-cmd.ack
	return cmd.err
}

// enqueue - async submit без ожидания исполнения. Безопасен с любой горутины.
// Принимает команды, пока loop-горутина не ушла в shutdown-финал (exited):
// graceful disconnect'ы из shutdown-пути обязаны доехать до очереди и
// исполниться в drain до Destroy.
func (l *HostLoop) enqueue(fn func()) error {
	return l.submit(&hostLoopCommand{
		fn: func() error {
			fn()
			return nil
		},
	}, true)
}

func (l *HostLoop) submit(cmd *hostLoopCommand, async bool) error {
	l.mu.Lock()
	// Blocking-команды отклоняются сразу после Stop; async (conn-операции)
	// - только когда executor уже финишировал.
	if l.exited || (!async && !l.accepting) {
		l.mu.Unlock()
		return ErrHostLoopStopped
	}
	cmd.enqueuedAt = time.Now()
	l.queue.PushBack(cmd)
	l.mu.Unlock()
	select {
	case l.notify <- struct{}{}:
	default:
	}
	return nil
}

// Stop прекращает приём blocking-команд, будит executor и ждёт его выхода:
// очередь разгребается (все принятые команды исполняются), host.Destroy.
// Повторный вызов идемпотентен и тоже ждёт реального завершения.
func (l *HostLoop) Stop() {
	l.stopOnce.Do(func() {
		l.mu.Lock()
		l.accepting = false
		l.mu.Unlock()
		select {
		case l.notify <- struct{}{}:
		default:
		}
	})
	<-l.done
}

// Done закрывается, когда loop-горутина завершилась (host уже разрушен).
func (l *HostLoop) Done() <-chan struct{} {
	return l.done
}

func (l *HostLoop) run() {
	defer close(l.done)
	for {
		cmd := l.next()
		if cmd == nil {
			break
		}
		l.exec(cmd)
	}
	// Финал: blocking submit'ы уже отклоняются (!accepting), async - режем
	// здесь под тем же mutex, чтобы ни одна команда не провисела без
	// обработчика, и добираем хвост очереди перед Destroy.
	l.mu.Lock()
	l.exited = true
	l.mu.Unlock()
	l.drain()
	l.host.Destroy()
}

func (l *HostLoop) exec(cmd *hostLoopCommand) {
	// Queue latency: enqueue → начало исполнения (P19b). Команды drain
	// учитываются так же - их ожидание тоже было реальным.
	l.metrics.EnetHostLoopQueueLatency(time.Since(cmd.enqueuedAt))
	cmd.err = cmd.fn()
	if cmd.ack != nil {
		close(cmd.ack)
	}
}

// next ждёт и забирает следующую команду; nil - очередь пуста и приём закрыт.
func (l *HostLoop) next() *hostLoopCommand {
	for {
		l.mu.Lock()
		if e := l.queue.Front(); e != nil {
			l.queue.Remove(e)
			l.mu.Unlock()
			return e.Value.(*hostLoopCommand)
		}
		if !l.accepting {
			l.mu.Unlock()
			return nil
		}
		l.mu.Unlock()
		<-l.notify
	}
}

// drain исполняет команды, стоящие в очереди (владелец - только loop-горутина).
func (l *HostLoop) drain() {
	for {
		l.mu.Lock()
		e := l.queue.Front()
		if e == nil {
			l.mu.Unlock()
			return
		}
		l.queue.Remove(e)
		l.mu.Unlock()
		l.exec(e.Value.(*hostLoopCommand))
	}
}
