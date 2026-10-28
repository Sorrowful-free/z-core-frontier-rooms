//go:build enet && cgo

package enet_test

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	adapterenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/enet"
)

// HostLoop: the queue and shutdown contract (P15). All runs use a real
// libenet host with addr=nil (client-side, no listen port).

func mustLoop(t *testing.T) *adapterenet.HostLoop {
	t.Helper()
	loop, err := adapterenet.NewHostLoop(nil, 8, 2, nil)
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
	// Idempotency: a repeated Stop returns, does not panic.
	loop.Stop()
}

// Commands accepted before Stop execute during the drain — not lost and
// not hanging: submit → Stop → the processed counters must reach 0 losses.
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
				done <- struct{}{} // a Stop rejection also releases the waiter
			}
		}()
	}
	// Let some commands reach the queue before Stop — not a barrier; the
	// race itself is part of the scenario: what matters is that ACCEPTED
	// commands execute, rejected ones error.
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

// Serialization: commands of one loop execute one at a time — there must
// be no parallel executions (a direct check of the property the loop
// exists for).
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
