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
// and the number of live peers on each); usecase/room calls AssignHost on
// Issue. nil registry in the usecase means unbound tickets (admit on any
// host).
type HostRegistry interface {
	// AssignHost picks a host for a new ticket: least-loaded by the host's
	// live peers, round-robin tie-break on equality.
	AssignHost(ctx context.Context) (HostAssignment, error)
}
