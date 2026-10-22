package room

import (
	"context"
	"sync"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
)

// SweepHandle — запущенный фоновый sweep; Stop дожидается реального завершения.
type SweepHandle struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Stop отменяет ctx sweep'а и ждёт выхода горутины (включая in-flight RunOnce).
// nil-safe: возвращаемый Start*Sweep nil при interval <= 0.
func (h *SweepHandle) Stop() {
	if h == nil {
		return
	}
	h.cancel()
	h.wg.Wait()
}

// startSweep запускает периодический run в фоне; interval <= 0 — no-op (nil handle).
// name/metric — префикс лог-сообщений и ключ счётчика обработанных записей.
func startSweep(
	ctx context.Context,
	interval time.Duration,
	logger logging.Logger,
	name string,
	metric string,
	run func(context.Context) (int, error),
) *SweepHandle {
	if interval <= 0 {
		return nil
	}

	sweepCtx, cancel := context.WithCancel(ctx)
	handle := &SweepHandle{cancel: cancel}
	handle.wg.Add(1)
	go func() {
		defer handle.wg.Done()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-sweepCtx.Done():
				return
			case <-ticker.C:
				processed, err := run(sweepCtx)
				if err != nil && sweepCtx.Err() == nil {
					logger.Error(name+" failed", "error", err)
					continue
				}
				if processed > 0 {
					logger.Info(name+" complete", metric, processed)
				}
			}
		}
	}()
	return handle
}
