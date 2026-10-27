package room

import (
	"context"
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/admission"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
)

// AdmitPeerUseCase — единая точка входа data plane (P20, T2): оба handler'а
// (ws `?token=`, enet первый пакет) отдают сырой token сюда. Validate идёт
// один раз, ветвление — по claims.Kind: join-поток (JoinRoomUseCase) или
// resume-поток (ReconnectRoomUseCase, другая rollback-семантика).
type AdmitPeerUseCase struct {
	admission admission.Admission
	join      *JoinRoomUseCase
	reconnect *ReconnectRoomUseCase
	logging   logging.Logger
}

func NewAdmitPeerUseCase(
	admission admission.Admission,
	join *JoinRoomUseCase,
	reconnect *ReconnectRoomUseCase,
	logging logging.Logger,
) *AdmitPeerUseCase {
	return &AdmitPeerUseCase{
		admission: admission,
		join:      join,
		reconnect: reconnect,
		logging:   logging,
	}
}

// Admit валидирует token и направляет поток по claims.Kind.
// enetHostID — host, принявший соединение (strict binding, P18): тикет с
// claims.HostID обязан совпасть с ним, иначе ErrWrongHost до траты слота
// reservation. domain.HostIDUnbound — admit не по ENet (WS): проверки нет.
func (uc *AdmitPeerUseCase) Admit(
	ctx context.Context,
	connection transport.Connection,
	token []byte,
	enetHostID domain.HostID,
) (PeerSession, error) {
	if err := ctx.Err(); err != nil {
		return PeerSession{}, err
	}

	claims, err := uc.admission.Validate(ctx, token)
	if err != nil {
		return PeerSession{}, err
	}

	if enetHostID.IsValid() && claims.HostID != enetHostID {
		uc.logging.Warn("admit peer: ticket bound to another host", "wantHostID", claims.HostID, "hostID", enetHostID)
		return PeerSession{}, domain.ErrWrongHost
	}

	switch claims.Kind {
	case domain.ClaimsKindJoin:
		return uc.join.JoinRoom(ctx, connection, claims)
	case domain.ClaimsKindReconnect:
		return uc.reconnect.ReconnectRoom(ctx, connection, claims)
	default:
		return PeerSession{}, fmt.Errorf("%w: claims kind %d", domain.ErrInvalidToken, claims.Kind)
	}
}
