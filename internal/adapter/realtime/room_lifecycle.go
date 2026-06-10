package realtime

import (
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

type lifecycleOp byte

const (
	lifecycleJoin lifecycleOp = iota
	lifecycleLeave
)

type lifecycleRequest struct {
	op   lifecycleOp
	peer realtime.Peer
	done chan error
}

func (r *Room) runLifecycle(req lifecycleRequest) {
	var err error
	switch req.op {
	case lifecycleJoin:
		err = r.policy.OnJoin(req.peer)
	case lifecycleLeave:
		err = r.policy.OnLeave(req.peer)
	default:
		err = fmt.Errorf("unknown lifecycle op: %d", req.op)
	}
	req.done <- err
}

func (r *Room) dispatchLifecycle(req lifecycleRequest) error {
	select {
	case <-r.ctx.Done():
		return fmt.Errorf("room stopped: %d", r.id)
	case r.lifecycle <- req:
	}
	select {
	case err := <-req.done:
		return err
	case <-r.ctx.Done():
		return fmt.Errorf("room stopped: %d", r.id)
	}
}
