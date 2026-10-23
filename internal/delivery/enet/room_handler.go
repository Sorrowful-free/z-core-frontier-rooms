package enet

import (
	"context"

	transportenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/enet"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport/enet"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
)

// RoomsHandler — inbound ENet: host loop, ticket в первом пакете, JoinRoom / LeaveRoom use cases.
type RoomsHandler struct {
	ctx               context.Context
	joinRoomUseCase   *room.JoinRoomUseCase
	leaveRoomUseCase  *room.LeaveRoomUseCase
	connectionFactory enet.EnetConnectionFactory
	logger            logging.Logger
	cfg               transportenet.EnetConfig
}

func NewRoomsHandler(ctx context.Context, joinRoomUseCase *room.JoinRoomUseCase, leaveRoomUseCase *room.LeaveRoomUseCase, connectionFactory enet.EnetConnectionFactory, cfg transportenet.EnetConfig, logger logging.Logger) *RoomsHandler {
	return &RoomsHandler{
		ctx:               ctx,
		joinRoomUseCase:   joinRoomUseCase,
		leaveRoomUseCase:  leaveRoomUseCase,
		connectionFactory: connectionFactory,
		logger:            logger,
		cfg:               cfg,
	}
}

// ListenHandle — lifecycle запущенного ENet listener'а.
// Done закрывается при выходе host loop; после этого Err() отдаёт результат run:
// ошибка bind/host — для fail-fast на старте; context.Canceled — штатная остановка;
// nil — listener отключён (stub build без enet+cgo). Stop отменяет loop и ждёт
// реального завершения (закрытие сессий + drain).
type ListenHandle struct {
	done   chan struct{}
	cancel context.CancelFunc
	err    error
}

// Done — канал, закрывающийся при выходе host loop.
func (h *ListenHandle) Done() <-chan struct{} {
	return h.done
}

// Err — терминальный результат run; валиден только после Done.
func (h *ListenHandle) Err() error {
	return h.err
}

// Stop отменяет host loop и блокируется до его выхода. Teardown идёт через
// HostLoop.Stop внутри run: сессии закрываются, graceful disconnect'ы
// флешатся service-итерациями, очередь команд доразбирается, host.Destroy.
func (h *ListenHandle) Stop() {
	h.cancel()
	<-h.done
}

// Listen запускает ENet host loop в отдельной горутине и возвращает handle для
// наблюдения за стартом (bind failure) и остановки. Без тега enet+cgo run —
// stub: сразу warn + чистый выход (Err() == nil).
func (h *RoomsHandler) Listen() *ListenHandle {
	ctx, cancel := context.WithCancel(h.ctx)
	handle := &ListenHandle{done: make(chan struct{}), cancel: cancel}
	go func() {
		defer close(handle.done)
		// Ошибку не логируем здесь: терминальный результат читается через
		// ListenHandle.Err() и логируется в main.go с контекстом процесса.
		handle.err = h.run(ctx)
	}()
	return handle
}
