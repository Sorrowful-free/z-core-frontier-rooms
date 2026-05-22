package joinerror

import (
	"errors"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
)

// OpCode возвращает бинарный код ошибки join для клиента (без текстового payload).
func OpCode(err error) domain.OpCode {
	switch {
	case errors.Is(err, domain.ErrEmptyToken):
		return domain.OpEmptyToken
	case errors.Is(err, domain.ErrInvalidToken):
		return domain.OpInvalidToken
	case errors.Is(err, domain.ErrExpiredToken):
		return domain.OpExpiredToken
	case errors.Is(err, domain.ErrRoomNotFound):
		return domain.OpRoomNotFound
	case errors.Is(err, domain.ErrJoinDenied):
		return domain.OpJoinDenied
	case errors.Is(err, domain.ErrReplaceFailed):
		return domain.OpReplaceFailed
	case errors.Is(err, domain.ErrPeerNotFound):
		return domain.OpPeerNotFound
	case errors.Is(err, domain.ErrPeerStartFailed):
		return domain.OpPeerStartFailed
	default:
		return domain.OpInternal
	}
}

// Send пишет кадр ошибки join; ошибку Send игнорируем — соединение всё равно закрывается.
func Send(conn transport.Connection, err error) {
	_ = conn.Send(domain.Frame{
		OpCode:   OpCode(err),
		Delivery: domain.DeliveryReliable,
	})
}
