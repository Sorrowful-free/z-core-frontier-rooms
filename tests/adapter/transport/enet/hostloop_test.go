//go:build enet && cgo

package enet_test

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	adapterenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/enet"
)

// HostLoop: контракт очереди и shutdown (P15). Все прогоны — на реальном
// libenet host с addr=nil (клиентский, без listen-порта).

func mustLoop(t *testing.T) *adapterenet.HostLoop {
	t.Helper()
	loop, err := adapterenet.NewHostLoop(nil, 8, 2)
	if err != nil {
		t.Fatalf("NewHostLoop: %v", err)
	}
	return loop
}

func TestHostLoopCallReturnsResult(t *testing.T) {
	t.Parallel()

	loop := mustLoop(t)
	defer loop.Stop()

	got, err := adapterenet.Call(loop, func() int { return 42 })
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if got != 42 {
		t.Fatalf("Call = %d, want 42", got)
	}
}

func TestHostLoopDoPropagatesError(t *testing.T) {
	t.Parallel()

	loop := mustLoop(t)
	defer loop.Stop()

	want := errors.New("fn failed")
	if err := loop.Do(func() error { return want }); !errors.Is(err, want) {
		t.Fatalf("Do err = %v, want %v", err, want)
	}
}

func TestHostLoopStopRejectsBlockingSubmit(t *testing.T) {
	t.Parallel()

	loop := mustLoop(t)
	loop.Stop()

	if err := loop.Do(func() error { return nil }); !errors.Is(err, adapterenet.ErrHostLoopStopped) {
		t.Fatalf("Do after Stop = %v, want ErrHostLoopStopped", err)
	}
	if _, err := adapterenet.Call(loop, func() int { return 1 }); !errors.Is(err, adapterenet.ErrHostLoopStopped) {
		t.Fatalf("Call after Stop = %v, want ErrHostLoopStopped", err)
	}
	// Идемпотентность: повторный Stop возвращается, не паникует.
	loop.Stop()
}

// Принятые до Stop команды исполняются в drain, не теряются и не висят:
// submit → Stop → обработанные счётчики должны дойти до 0 потерь.
func TestHostLoopStopDrainsAccepted(t *testing.T) {
	t.Parallel()

	loop := mustLoop(t)

	var ran atomic.Int32
	const n = 16
	done := make(chan struct{}, n)
	for range n {
		go func() {
			if err := loop.Do(func() error {
				ran.Add(1)
				done <- struct{}{}
				return nil
			}); err != nil {
				done <- struct{}{} // Stop-отказ тоже освобождает ждущего
			}
		}()
	}
	// Даём части команд попасть в очередь до Stop — не барьер, гонка сама
	// часть сценария: важно, что ПРИНЯТЫЕ исполняются, отвергнутые — ошибка.
	time.Sleep(20 * time.Millisecond)
	loop.Stop()

	for range n {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("blocking caller hung after Stop")
		}
	}
	if loop.Done() == nil {
		t.Fatal("Done channel nil")
	}
	select {
	case <-loop.Done():
	default:
		t.Fatal("Done not closed after Stop returned")
	}
	t.Logf("drained %d of %d submitted (rest rejected after Stop)", ran.Load(), n)
}

// Сериализация: команды одного loop'а исполняются по одной — параллельных
// исполнений не быть (прямая проверка свойства, ради которого loop существует).
func TestHostLoopSerializesCommands(t *testing.T) {
	t.Parallel()

	loop := mustLoop(t)
	defer loop.Stop()

	var inFlight atomic.Int32
	var maxSeen atomic.Int32
	const n = 32
	var wgChan = make(chan struct{}, n)
	for range n {
		go func() {
			err := loop.Do(func() error {
				cur := inFlight.Add(1)
				for {
					old := maxSeen.Load()
					if cur <= old || maxSeen.CompareAndSwap(old, cur) {
						break
					}
				}
				time.Sleep(time.Millisecond)
				inFlight.Add(-1)
				wgChan <- struct{}{}
				return nil
			})
			if err != nil {
				wgChan <- struct{}{}
			}
		}()
	}
	for range n {
		select {
		case <-wgChan:
		case <-time.After(5 * time.Second):
			t.Fatal("command hung")
		}
	}
	if maxSeen.Load() != 1 {
		t.Fatalf("max concurrent executions = %d, want 1", maxSeen.Load())
	}
}
