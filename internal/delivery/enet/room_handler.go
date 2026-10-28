package enet

import (
	"context"
	"sync"
	"sync/atomic"

	transportenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/enet"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport/enet"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
)

// enetHost - one ENet host (shard) of the process: its own UDP port and its
// own HostLoop. id = index+1 into cfg.ListenPorts (domain.HostID, goes into
// the ticket claims). peers - count of live sessions on the host for
// least-loaded balancing (increment on EventConnect, decrement on session
// close).
type enetHost struct {
	id    domain.HostID
	port  uint16
	peers atomic.Int64
}

// RoomsHandler - inbound ENet: mux over N hosts (one HostLoop per port from
// cfg.ListenPorts, P18), ticket in the first packet, AdmitPeer /
// DisconnectPeer use cases. Strict binding: the admit facade checks
// claims.HostID against the host that accepted the connection.
// Implements port/transport/enet.HostRegistry for issue-ticket balancing.
type RoomsHandler struct {
	ctx                   context.Context
	admitPeerUseCase      *room.AdmitPeerUseCase
	disconnectPeerUseCase *room.DisconnectPeerUseCase
	connectionFactory     enet.EnetConnectionFactory
	logger                logging.Logger
	metrics               portmetrics.Metrics
	cfg                   transportenet.EnetConfig
	hosts                 []*enetHost
	assignRR              atomic.Uint64
}

func NewRoomsHandler(ctx context.Context, admitPeerUseCase *room.AdmitPeerUseCase, disconnectPeerUseCase *room.DisconnectPeerUseCase, connectionFactory enet.EnetConnectionFactory, cfg transportenet.EnetConfig, logger logging.Logger, m portmetrics.Metrics) *RoomsHandler {
	hosts := make([]*enetHost, len(cfg.ListenPorts))
	for i, port := range cfg.ListenPorts {
		hosts[i] = &enetHost{id: domain.HostID(i + 1), port: port}
	}
	return &RoomsHandler{
		ctx:                   ctx,
		admitPeerUseCase:      admitPeerUseCase,
		disconnectPeerUseCase: disconnectPeerUseCase,
		connectionFactory:     connectionFactory,
		logger:                logger,
		metrics:               portmetrics.OrNoop(m),
		cfg:                   cfg,
		hosts:                 hosts,
	}
}

// AssignHost implements port/transport/enet.HostRegistry: least-loaded by
// live peers on the host, round-robin shift of the start position on ties
// (consecutive tickets on empty hosts are spread evenly).
// Empty host list - ticket without binding (zero HostAssignment).
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

// ListenHandle — lifecycle of a running ENet listener.
// Done is closed once ALL host loops have exited; after that Err() returns
// the first terminal run error: a bind/host error - for fail-fast at startup;
// context.Canceled - a normal stop; nil - listener disabled (stub build
// without enet+cgo). Stop cancels all host loops and waits for their real
// completion (session close + drain on each).
type ListenHandle struct {
	done   chan struct{}
	cancel context.CancelFunc
	err    error
}

// Done - channel closed when all host loops have exited.
func (h *ListenHandle) Done() <-chan struct{} {
	return h.done
}

// Err — terminal result of the run; valid only after Done.
func (h *ListenHandle) Err() error {
	return h.err
}

// Stop cancels all host loops and blocks until they exit. Teardown of each
// goes through HostLoop.Stop inside run: sessions are closed, graceful
// disconnects are flushed by service iterations, the command queue is
// drained, host.Destroy.
func (h *ListenHandle) Stop() {
	h.cancel()
	<-h.done
}

// Listen starts one run goroutine per host (port from cfg.ListenPorts) and
// returns a handle for observing startup (bind failure) and shutdown.
// The first failed run cancels the shared ctx - fail-fast: a partially
// started mux counts as a failure (Err() = the first error; the other exits
// are context.Canceled and do not overwrite it). Without the enet+cgo tag
// each run is a stub: warn + clean exit (Err() == nil).
func (h *RoomsHandler) Listen() *ListenHandle {
	ctx, cancel := context.WithCancel(h.ctx)
	handle := &ListenHandle{done: make(chan struct{}), cancel: cancel}
	var wg sync.WaitGroup
	var errOnce sync.Once
	for _, host := range h.hosts {
		wg.Add(1)
		go func(host *enetHost) {
			defer wg.Done()
			// The error is not logged here: the terminal result is read via
			// ListenHandle.Err() and logged in main.go with process context.
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
