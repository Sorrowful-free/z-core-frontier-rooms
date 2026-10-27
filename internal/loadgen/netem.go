package loadgen

import (
	"fmt"
	"net"
	"syscall"
)

// Netem-профили (P27 inc2): каждому клиенту при спавне раздаётся профиль из
// LOADGEN_NETEM_SHARE (weighted pick), его dial'ы ставят SO_MARK=fwmark, а
// tc в контейнере роутит поток в band prio-qdisc с netem-импейрментом
// (см. deploy/loadgen/entrypoint.sh). Mark 0 — без маркировки (host-режим,
// share не задан): priomap отправляет немаркированный трафик в clean-band.
//
// fwmark == номер tc-band (1:1 clean, 1:2 bad, 1:3 ugly) — фильтры в
// entrypoint пишутся тривиально (handle N fw → flowid 1:N).
const (
	netemProfileClean = "clean"
	netemProfileBad   = "bad"
	netemProfileUgly  = "ugly"
)

// netemMarkFor — имя профиля → fwmark. Пустое имя (share не задан) → 0.
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

// mapNetem — валидатор имён для parseMix (LOADGEN_NETEM_SHARE).
func mapNetem(name string) (string, error) {
	if _, err := netemMarkFor(name); err != nil {
		return "", err
	}
	return name, nil
}

// markedDialer — net.Dialer, ставящий SO_MARK=mark на исходящий сокет
// (SO_MARK требует CAP_NET_ADMIN — в контейнере даёт cap_add: NET_ADMIN;
// на хосте без capability dial упадёт с EPERM и это считается dial_error,
// share предназначен только для контейнерного режима). mark=0 — Control
// не ставится, поведение идентично дефолтному dialer'у.
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
