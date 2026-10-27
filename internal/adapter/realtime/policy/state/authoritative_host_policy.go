package state

import (
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/codec"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	portpolicy "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime/policy"
)

// AuthoritativeHostPolicy — room-policy "authoritative-host" (P29): та же
// state-механика, что у StateRoomPolicy, но без master-миграции — уход
// master'а убивает комнату. OnLeave уходящего master'а не делегируется в
// StateRoomPolicy (перевыборы и remove-патч бессмысленны — комната умирает)
// и отвечает domain.ErrRoomShouldClose: usecase превращает sentinel в
// teardown (OpRoomClosed оставшимся → disconnect всех → unregister +
// revoke слотов). Уход non-master — обычный OnLeave StateRoomPolicy.
//
// Grace/reconnect для хоста не применяется: разрыв его транспорта идёт тем
// же OnLeave-путём (DisconnectPeer → Leave) → комната умирает сразу.
type AuthoritativeHostPolicy struct {
	*StateRoomPolicy
}

var _ portpolicy.RoomPolicy = (*AuthoritativeHostPolicy)(nil)

func NewAuthoritativeHostPolicy(inner *StateRoomPolicy) *AuthoritativeHostPolicy {
	return &AuthoritativeHostPolicy{StateRoomPolicy: inner}
}

func (p *AuthoritativeHostPolicy) OnLeave(peer realtime.Peer) error {
	// Машина ещё держит уходящего master'а (m.peer сбрасывается только в
	// dispatch) — проверки по peerID достаточно.
	if p.master.isMaster(peer.GetID()) {
		return fmt.Errorf("%w: %d", domain.ErrRoomShouldClose, peer.GetID())
	}
	return p.StateRoomPolicy.OnLeave(peer)
}

// AuthoritativeHostPolicyFactory — фабрика policy kind "authoritative-host"
// (P29): отдельная от StateRoomPolicyFactory, та же сборка кодеков и
// интервалов; dispatch по kind делает KindRoomFactory на уровне выше.
type AuthoritativeHostPolicyFactory struct {
	logger  logging.Logger
	metrics portmetrics.Metrics
}

func NewAuthoritativeHostPolicyFactory(logger logging.Logger, m portmetrics.Metrics) *AuthoritativeHostPolicyFactory {
	return &AuthoritativeHostPolicyFactory{
		logger:  logger,
		metrics: portmetrics.OrNoop(m),
	}
}

func (f *AuthoritativeHostPolicyFactory) CreateRoomPolicy() portpolicy.RoomPolicy {
	p := NewStateRoomPolicy(
		f.logger,
		codec.NewRoomStateCodec(),
		codec.NewEntitiesStateCodec(),
		codec.NewInputStateCodec(),
		codec.NewRpcStateCodec(),
		DefaultFullStateInterval,
		DefaultPatchStateInterval,
	)
	// Тот же полевой влейв metrics в master-машину, что у StateRoomPolicyFactory.
	p.master.metrics = f.metrics
	return NewAuthoritativeHostPolicy(p)
}

var _ portpolicy.RoomPolicyFactory = (*AuthoritativeHostPolicyFactory)(nil)
