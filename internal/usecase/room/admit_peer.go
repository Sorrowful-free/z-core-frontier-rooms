package room

import (
	"context"
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/admission"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
)

// AdmitPeerUseCase — single entry point of the data plane (P20, T2): both
// handlers (ws `?token=`, enet first packet) hand the raw token here.
// Validate runs once, then the flow branches on claims.Kind: join flow
// (JoinRoomUseCase) or resume flow (ReconnectRoomUseCase, different
// rollback semantics).
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

// Admit validates the token and routes the flow by claims.Kind.
// enetHostID — the host that accepted the connection (strict binding, P18):
// a ticket with claims.HostID must match it, otherwise ErrWrongHost before
// any reservation slot is consumed. domain.HostIDUnbound — admit is not via
// ENet (WS): no check.
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
