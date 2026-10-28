package room

import (
	"context"
	"sync"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
)

// SweepHandle — a running background sweep; Stop waits for real completion.
type SweepHandle struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Stop cancels the sweep's ctx and waits for the goroutine to exit
// (including an in-flight RunOnce).
// nil-safe: Start*Sweep returns nil when interval <= 0.
func (h *SweepHandle) Stop() {
	if h == nil {
		return
	}
	h.cancel()
	h.wg.Wait()
}

// startSweep runs the periodic run in the background; interval <= 0 — no-op
// (nil handle). name/metric — log message prefix and the processed-records
// counter key.
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
