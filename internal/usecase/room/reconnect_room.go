package room

import (
	"context"
	"fmt"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/reservation"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
)

// ReconnectRoomUseCase — resume-поток за AdmitPeerUseCase (P20): воскрешение
// peer'а под тем же peerID в пределах grace admitted-слота. Отличия от
// join-потока: reservation.Admit не вызывается (слот уже admitted — атомарная
// проверка и refresh lastSeen в reservation.Reconnect), слот не ревокается
// ни при каком фейле (peer в grace), nick берётся из слота — в токене v4
// его нет. Grace-дедлайн (P24) проверяется до refresh'а по read-only
// SlotDetails — см. ReconnectRoom. Гонка double-resume одного токена
// дожимается Replace (newest-wins, Q10) — см. placePeer.
type ReconnectRoomUseCase struct {
	peerFactory  realtime.PeerFactory
	roomRegistry registry.RoomRegistry
	reservation  reservation.Reservation
	reconnectTTL time.Duration
	logging      logging.Logger
	metrics      portmetrics.Metrics
}

func NewReconnectRoomUseCase(
	peerFactory realtime.PeerFactory,
	roomRegistry registry.RoomRegistry,
	reservation reservation.Reservation,
	reconnectTTL time.Duration,
	logging logging.Logger,
	m portmetrics.Metrics,
) *ReconnectRoomUseCase {
	return &ReconnectRoomUseCase{
		peerFactory:  peerFactory,
		roomRegistry: roomRegistry,
		reservation:  reservation,
		reconnectTTL: reconnectTTL,
		logging:      logging,
		metrics:      portmetrics.OrNoop(m),
	}
}

func (uc *ReconnectRoomUseCase) ReconnectRoom(ctx context.Context, connection transport.Connection, claims domain.Claims) (RoomSummary, PeerSession, error) {
	if err := ctx.Err(); err != nil {
		return EmptyRoomSummary, PeerSession{}, err
	}

	room, err := uc.roomRegistry.GetRoom(ctx, claims.RoomID)
	if err != nil {
		return EmptyRoomSummary, PeerSession{}, err
	}

	// Read-only просмотр слота (P24): честный lastSeen ДО refresh'а —
	// deadline считается по нему, отклонённая попытка не продлевает grace.
	slot, err := uc.reservation.SlotDetails(ctx, claims.RoomID, claims.PeerID)
	if err != nil {
		uc.logging.Error("reconnect room: reservation slot details failed", "error", err, "roomID", claims.RoomID, "peerID", claims.PeerID)
		return EmptyRoomSummary, PeerSession{}, err
	}

	// Deadline-гейт (P24): admitted-слот без живого peer'а и с истёкшим
	// lastSeen + reconnectTTL → ErrReservationExpired — grace конечна и без
	// прохода sweep; предикат общий со sweep — см. graceExpired. Живой peer
	// (HasPeer) гейтом не затрагивается: admittedAt давно подключённого
	// peer'а старый законно, Replace (newest-wins, Q10) идёт без гейта.
	// reconnectTTL <= 0 → любой !HasPeer resume expired — TTL=0 запрещает
	// именно воскрешение, не перехват живого соединения.
	if !room.HasPeer(claims.PeerID) && graceExpired(slot.AdmittedAt, uc.reconnectTTL, time.Now()) {
		expiredErr := fmt.Errorf("%w: room %d peer %d", domain.ErrReservationExpired, claims.RoomID, claims.PeerID)
		uc.logging.Error("reconnect room: grace deadline expired", "error", expiredErr, "roomID", claims.RoomID, "peerID", claims.PeerID)
		return EmptyRoomSummary, PeerSession{}, expiredErr
	}

	// Атомарно под mutex комнаты reservation (T5): admitted-слот + refresh
	// lastSeen, иначе reject — гонка sweep-revoke ↔ resume закрыта.
	slot, err = uc.reservation.Reconnect(ctx, claims.RoomID, claims.PeerID)
	if err != nil {
		uc.logging.Error("reconnect room: reservation reconnect failed", "error", err, "roomID", claims.RoomID, "peerID", claims.PeerID)
		return EmptyRoomSummary, PeerSession{}, err
	}

	session, err := placePeer(uc.peerFactory, room, connection, claims.PeerID, slot.NickName, uc.logging, true)
	if err != nil {
		return EmptyRoomSummary, PeerSession{}, err
	}

	// OpPeerReconnected остальным (Q5): FIFO outbound каждого получателя
	// держит порядок «add-патч → opcode». Ошибка рассылки не роняет
	// resume — peer уже в комнате, reject закрыл бы живое соединение.
	if err := room.Send(events.PeerEvent{
		ExcludePeerID: claims.PeerID,
		Frame:         domain.NewPeerNotificationFrame(domain.OpPeerReconnected, claims.PeerID),
	}); err != nil {
		uc.logging.Error("reconnect room: broadcast reconnected failed", "error", err, "peerID", claims.PeerID)
	}

	uc.metrics.ReconnectSucceeded()
	roomSummary := NewRoomSummaryFromRoom(room)
	uc.logging.Info("reconnect room: reconnect success", "roomID", claims.RoomID, "peerID", claims.PeerID)
	return *roomSummary, session, nil
}
