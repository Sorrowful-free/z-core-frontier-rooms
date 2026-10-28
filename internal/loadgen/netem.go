package loadgen

import (
	"fmt"
	"net"
	"syscall"
)

// Netem profiles (P27 inc2): each client is dealt a profile from
// LOADGEN_NETEM_SHARE at spawn (weighted pick); its dials set
// SO_MARK=fwmark, and tc in the container routes the flow into a band of
// the prio qdisc with the netem impairment (see
// deploy/loadgen/entrypoint.sh). Mark 0 — no marking (host mode, share not
// set): priomap sends unmarked traffic to the clean band.
//
// fwmark == tc band number (1:1 clean, 1:2 bad, 1:3 ugly) — the filters in
// the entrypoint are trivial (handle N fw → flowid 1:N).
const (
	netemProfileClean = "clean"
	netemProfileBad   = "bad"
	netemProfileUgly  = "ugly"
)

// netemMarkFor — profile name → fwmark. Empty name (share not set) → 0.
func netemMarkFor(profile string) (uint32, error) {
	switch profile {
	case "":
		return 0, nil
	case netemProfileClean:
		return 1, nil
	case netemProfileBad:
		return 2, nil
	case netemProfileUgly:
		return 3, nil
	default:
		return 0, fmt.Errorf("unknown netem profile %q (want %s|%s|%s)",
			profile, netemProfileClean, netemProfileBad, netemProfileUgly)
	}
}

// mapNetem — name validator for parseMix (LOADGEN_NETEM_SHARE).
func mapNetem(name string) (string, error) {
	if _, err := netemMarkFor(name); err != nil {
		return "", err
	}
	return name, nil
}

// markedDialer — a net.Dialer that sets SO_MARK=mark on the outgoing socket
// (SO_MARK requires CAP_NET_ADMIN — the container grants it via cap_add:
// NET_ADMIN; on a host without the capability the dial fails with EPERM and
// that is counted as a dial_error — the share is meant for container mode
// only). mark=0 — Control is not installed, behaviour identical to the
// default dialer.
func markedDialer(mark uint32) *net.Dialer {
	d := &net.Dialer{}
	if mark == 0 {
		return d
	}
	d.Control = func(_, _ string, c syscall.RawConn) error {
		return setSockMark(c, mark)
	}
	return d
}
