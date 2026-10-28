//go:build enet && cgo

package env

// WithEnet brings up the production delivery/enet mux-listener (P18): one host
// per port from the list. With no arguments - a single host on a free UDP port;
// element 0 means auto (free port). The option lives only under `enet && cgo`:
// without the tag the listener would be a silent no-op (stub-run), and callers
// of ENet clients must be tagged anyway.
func WithEnet(ports ...uint16) Option {
	return func(o *options) {
		o.enet = true
		o.enetPorts = ports
	}
}

// EnetPort - the first UDP port of the environment's ENet listener; valid only
// with WithEnet (otherwise 0). For multi-host environments use EnetPorts or
// the assigned port from IssueTicketPort/CreateRoomPort.
func (e *Env) EnetPort() uint16 {
	if len(e.enetPorts) == 0 {
		return 0
	}
	return e.enetPorts[0]
}

// EnetPorts - all UDP ports of the environment's ENet hosts (one host per port, P18).
func (e *Env) EnetPorts() []uint16 {
	return e.enetPorts
}
