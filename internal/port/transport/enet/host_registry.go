package enet

import (
	"context"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

// HostAssignment - ENet host choice for a ticket (P18): HostID goes into the
// claims (strict binding admit), Port goes into the issue ticket response —
// the client connects to it.
type HostAssignment struct {
	HostID domain.HostID
	Port   uint16
}

// HostRegistry - catalog of the process's ENet hosts (shards) for balancing
// ticket issues. Implemented by delivery/enet (RoomsHandler knows its hosts
// and the number of live peers on each); usecase/room always calls
// AssignHost on Issue — a zero HostAssignment produces an unbound ticket
// (hostID=0, fine for WS; rejected on ENet admit).
type HostRegistry interface {
	// AssignHost picks a host for a new ticket: least-loaded by the host's
	// live peers, round-robin tie-break on equality.
	AssignHost(ctx context.Context) (HostAssignment, error)
}
