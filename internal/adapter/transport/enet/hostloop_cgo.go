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

// ErrHostLoopStopped is returned to submits arriving after Stop: the loop no
// longer accepts blocking commands (Call/Do), and the host is already on its
// way to Destroy.
var ErrHostLoopStopped = errors.New("enet host loop stopped")

// HostLoop owns a libenet.Host and serializes ALL access to it: libenet is
// not thread-safe, so only the loop goroutine - an executor draining a FIFO
// command queue - touches host/peer/packet. External goroutines submit work
// via Call/Do (blocking - for host owners: the server-side service-iteration
// driver, the client pump, peer operations from conns) or enqueue (async -
// EnetConnection). No global lock: isolation is per-host, only submits to the
// same loop contend.
//
// Liveness invariant: the executor runs only libenet calls, each of which is
// time-bounded (Service - serviceTimeoutMs). Blocking Call/Do therefore
// cannot deadlock: the executor never waits on external goroutines. Domain
// work that can block (JoinRoom/LeaveRoom → dispatchLifecycle → waiting on
// the room goroutine) MUST run OUTSIDE loop commands - otherwise a room
// goroutine making a blocking call into the loop (EnetConnection.Ping) would
// close a cross-deadlock executor↔room.
type HostLoop struct {
	host libenet.Host

	mu        sync.Mutex
	queue     *list.List // *hostLoopCommand, FIFO
	accepting bool       // false after Stop: blocking submits are rejected
	exited    bool       // loop goroutine has gone into Destroy: all submits rejected

	notify   chan struct{} // cap-1 executor wakeup
	done     chan struct{} // closed after host.Destroy
	stopOnce sync.Once

	metrics portmetrics.Metrics
}

type hostLoopCommand struct {
	fn         func() error
	ack        chan struct{} // != nil for blocking commands: closed after execution
	err        error
	enqueuedAt time.Time // enqueue timestamp: queue latency sample
}

// NewHostLoop creates a libenet host (addr = NewListenAddress for a server,
// nil for a client host) and starts its loop goroutine. The host is not
// handed out as a "live" object: all work with it goes through
// Call/Do/enqueue (Host() - only inside such commands). m - command queue
// wait telemetry (nil-safe, Noop).
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

// Host returns the owned libenet.Host for use INSIDE Call/Do commands (where
// the call already runs on the loop goroutine). Any other use breaks
// confinement and is a data race.
func (l *HostLoop) Host() libenet.Host {
	return l.host
}

// Call enqueues fn on the loop and waits for the result. Returns fn's result
// or ErrHostLoopStopped if the loop was stopped before the submit.
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

// Do - variant of Call for commands returning only an error.
func (l *HostLoop) Do(fn func() error) error {
	cmd := &hostLoopCommand{fn: fn, ack: make(chan struct{})}
	if err := l.submit(cmd, false); err != nil {
		return err
	}
	<-cmd.ack
	return cmd.err
}

// enqueue - async submit without waiting for execution. Safe from any
// goroutine. Accepts commands until the loop goroutine enters the shutdown
// finale (exited): graceful disconnects from the shutdown path must reach
// the queue and execute during drain before Destroy.
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
	// Blocking commands are rejected right after Stop; async (conn
	// operations) - only once the executor has finished.
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

// Stop stops accepting blocking commands, wakes the executor, and waits for
// it to exit: the queue is drained (all accepted commands execute),
// host.Destroy. A repeated call is idempotent and also waits for real
// completion.
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

// Done is closed when the loop goroutine has finished (the host is already destroyed).
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
	// Finale: blocking submits are already rejected (!accepting); async ones
	// are cut off here under the same mutex so no command is left without a
	// handler, and we pick up the queue tail before Destroy.
	l.mu.Lock()
	l.exited = true
	l.mu.Unlock()
	l.drain()
	l.host.Destroy()
}

func (l *HostLoop) exec(cmd *hostLoopCommand) {
	// Queue latency: enqueue → start of execution (P19b). Drain commands
	// count the same way - their wait was real too.
	l.metrics.EnetHostLoopQueueLatency(time.Since(cmd.enqueuedAt))
	cmd.err = cmd.fn()
	if cmd.ack != nil {
		close(cmd.ack)
	}
}

// next waits for and takes the next command; nil - queue empty and intake closed.
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

// drain executes the commands standing in the queue (owner - only the loop goroutine).
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
