package enet

import (
	"context"
	"sync"
	"sync/atomic"

	transportenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/enet"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	portadmission "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/admission"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport/enet"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
)

// enetHost - один ENet host (shard) процесса: свой UDP-порт и свой HostLoop.
// id = индекс+1 в cfg.ListenPorts (domain.HostID, уходит в claims ticket'а).
// peers - счётчик живых сессий на host'е для least-loaded балансировки
// (инкремент на EventConnect, декремент при закрытии сессии).
type enetHost struct {
	id    domain.HostID
	port  uint16
	peers atomic.Int64
}

// RoomsHandler - inbound ENet: mux над N host'ами (по одному HostLoop на порт
// из cfg.ListenPorts, P18), ticket в первом пакете, JoinRoom / LeaveRoom
// use cases. Strict binding: admit проверяет claims.HostID против host'а.
// Реализует port/transport/enet.HostRegistry для балансировки issue ticket'ов.
type RoomsHandler struct {
	ctx               context.Context
	joinRoomUseCase   *room.JoinRoomUseCase
	leaveRoomUseCase  *room.LeaveRoomUseCase
	connectionFactory enet.EnetConnectionFactory
	admission         portadmission.Admission
	logger            logging.Logger
	metrics           portmetrics.Metrics
	cfg               transportenet.EnetConfig
	hosts             []*enetHost
	assignRR          atomic.Uint64
}

func NewRoomsHandler(ctx context.Context, joinRoomUseCase *room.JoinRoomUseCase, leaveRoomUseCase *room.LeaveRoomUseCase, connectionFactory enet.EnetConnectionFactory, admission portadmission.Admission, cfg transportenet.EnetConfig, logger logging.Logger, m portmetrics.Metrics) *RoomsHandler {
	hosts := make([]*enetHost, len(cfg.ListenPorts))
	for i, port := range cfg.ListenPorts {
		hosts[i] = &enetHost{id: domain.HostID(i + 1), port: port}
	}
	return &RoomsHandler{
		ctx:               ctx,
		joinRoomUseCase:   joinRoomUseCase,
		leaveRoomUseCase:  leaveRoomUseCase,
		connectionFactory: connectionFactory,
		admission:         admission,
		logger:            logger,
		metrics:           portmetrics.OrNoop(m),
		cfg:               cfg,
		hosts:             hosts,
	}
}

// AssignHost реализует port/transport/enet.HostRegistry: least-loaded по
// живым peer'ам host'а, round-robin сдвиг стартовой позиции при равенстве
// (последовательные ticket'ы на пустых host'ах размазываются равномерно).
// Пустой список host'ов - ticket без привязки (zero HostAssignment).
func (h *RoomsHandler) AssignHost(ctx context.Context) (enet.HostAssignment, error) {
	if err := ctx.Err(); err != nil {
		return enet.HostAssignment{}, err
	}
	n := len(h.hosts)
	if n == 0 {
		return enet.HostAssignment{}, nil
	}
	start := int(h.assignRR.Add(1) % uint64(n))
	best := h.hosts[start]
	for k := 1; k < n; k++ {
		cur := h.hosts[(start+k)%n]
		if cur.peers.Load() < best.peers.Load() {
			best = cur
		}
	}
	return enet.HostAssignment{HostID: best.id, Port: best.port}, nil
}

var _ enet.HostRegistry = (*RoomsHandler)(nil)

// ListenHandle — lifecycle запущенного ENet listener'а.
// Done закрывается когда ВСЕ host-loop'ы вышли; после этого Err() отдаёт
// первую терминальную ошибку run: ошибка bind/host - для fail-fast на старте;
// context.Canceled - штатная остановка; nil - listener отключён (stub build
// без enet+cgo). Stop отменяет все host-loop'ы и ждёт их реального завершения
// (закрытие сессий + drain на каждом).
type ListenHandle struct {
	done   chan struct{}
	cancel context.CancelFunc
	err    error
}

// Done - канал, закрывающийся при выходе всех host loop'ов.
func (h *ListenHandle) Done() <-chan struct{} {
	return h.done
}

// Err — терминальный результат run; валиден только после Done.
func (h *ListenHandle) Err() error {
	return h.err
}

// Stop отменяет все host loop'ы и блокируется до их выхода. Teardown каждого
// идёт через HostLoop.Stop внутри run: сессии закрываются, graceful
// disconnect'ы флешатся service-итерациями, очередь команд доразбирается,
// host.Destroy.
func (h *ListenHandle) Stop() {
	h.cancel()
	<-h.done
}

// Listen запускает по run-горутине на каждый host (порт из cfg.ListenPorts) и
// возвращает handle для наблюдения за стартом (bind failure) и остановки.
// Первый упавший run отменяет общий ctx - fail-fast: частично поднятый mux
// считается failure (Err() = первая ошибка, выход остальных - context.Canceled
// и её не перезаписывает). Без тега enet+cgo каждый run - stub: warn + чистый
// выход (Err() == nil).
func (h *RoomsHandler) Listen() *ListenHandle {
	ctx, cancel := context.WithCancel(h.ctx)
	handle := &ListenHandle{done: make(chan struct{}), cancel: cancel}
	var wg sync.WaitGroup
	var errOnce sync.Once
	for _, host := range h.hosts {
		wg.Add(1)
		go func(host *enetHost) {
			defer wg.Done()
			// Ошибку не логируем здесь: терминальный результат читается через
			// ListenHandle.Err() и логируется в main.go с контекстом процесса.
			if err := h.run(ctx, host); err != nil {
				errOnce.Do(func() { handle.err = err })
				cancel()
			}
		}(host)
	}
	go func() {
		wg.Wait()
		close(handle.done)
	}()
	return handle
}
