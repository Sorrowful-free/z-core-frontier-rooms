package realtime

import (
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

type lifecycleOp byte

const (
	lifecycleJoin lifecycleOp = iota
	lifecycleLeave
	lifecycleReplace
)

type lifecycleRequest struct {
	op      lifecycleOp
	peer    realtime.Peer
	oldPeer realtime.Peer
	done    chan error
}

func (r *Room) runLifecycle(req lifecycleRequest) {
	var err error
	switch req.op {
	case lifecycleJoin:
		err = r.policy.OnJoin(req.peer)
	case lifecycleLeave:
		err = r.policy.OnLeave(req.peer)
	case lifecycleReplace:
		if leaveErr := r.policy.OnLeave(req.oldPeer); leaveErr != nil {
			err = fmt.Errorf("%w: %w", domain.ErrReplaceFailed, leaveErr)
		} else if joinErr := r.policy.OnJoin(req.peer); joinErr != nil {
			err = fmt.Errorf("%w: %w", domain.ErrReplaceFailed, joinErr)
		}
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
