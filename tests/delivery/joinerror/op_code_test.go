package joinerror_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/delivery/joinerror"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

func TestOpCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want domain.OpCode
	}{
		{"empty token", domain.ErrEmptyToken, domain.OpEmptyToken},
		{"invalid token", domain.ErrInvalidToken, domain.OpInvalidToken},
		{"expired token", domain.ErrExpiredToken, domain.OpExpiredToken},
		{"room not found", domain.ErrRoomNotFound, domain.OpRoomNotFound},
		{"join denied", domain.ErrJoinDenied, domain.OpJoinDenied},
		{"replace failed", domain.ErrReplaceFailed, domain.OpReplaceFailed},
		{"peer not found", domain.ErrPeerNotFound, domain.OpPeerNotFound},
		{"peer start failed", domain.ErrPeerStartFailed, domain.OpPeerStartFailed},
		{
			"wrapped room not found",
			fmt.Errorf("registry: %w: %s", domain.ErrRoomNotFound, "42"),
			domain.OpRoomNotFound,
		},
		{
			"wrapped join denied",
			fmt.Errorf("%w: %w", domain.ErrJoinDenied, errors.New("handler")),
			domain.OpJoinDenied,
		},
		{"unknown", errors.New("something else"), domain.OpInternal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := joinerror.OpCode(tt.err); got != tt.want {
				t.Fatalf("OpCode() = %#x, want %#x", got, tt.want)
			}
		})
	}
}
