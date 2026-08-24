package errors_test

import (
	"errors"
	"testing"

	deliveryerrors "github.com/Sorrowful-free/z-core-frontier-rooms/internal/delivery/errors"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

func TestInRoomErrorOpCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want domain.OpCode
	}{
		{"in-room internal", domain.ErrInRoomInternal, domain.OpInRoomInternal},
		{"not master", domain.ErrNotMaster, domain.OpInRoomNotMaster},
		{"invalid payload", domain.ErrInRoomInvalidPayload, domain.OpInRoomInvalidPayload},
		{"invalid rpc target", domain.ErrInvalidRpcTarget, domain.OpInRoomInvalidRpc},
		{"rpc peer not found", domain.ErrRpcTargetPeerNotFound, domain.OpInRoomRpcPeerNotFound},
		{"no master", domain.ErrNoMaster, domain.OpInRoomNoMaster},
		{"unknown opcode", domain.ErrInRoomUnknownOpcode, domain.OpInRoomUnknownOpcode},
		{"unknown defaults in-room internal", errors.New("other"), domain.OpInRoomInternal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.InRoomErrorOpCode(tt.err); got != tt.want {
				t.Fatalf("domain.InRoomErrorOpCode() = %#x, want %#x", got, tt.want)
			}
			if got := deliveryerrors.InRoomErrorOpCode(tt.err); got != tt.want {
				t.Fatalf("delivery.InRoomErrorOpCode() = %#x, want %#x", got, tt.want)
			}
		})
	}
}
