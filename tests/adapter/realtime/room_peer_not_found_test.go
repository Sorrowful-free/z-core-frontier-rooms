package realtime_test

import (
	"errors"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
)

func TestRoom_Leave_UnknownPeer_ReturnsErrPeerNotFound(t *testing.T) {
	t.Parallel()

	room := startTestRoom(t, 8)
	t.Cleanup(func() { _ = room.Stop() })

	err := room.Leave(&stubPeer{id: domain.PeerID(404)})
	if !errors.Is(err, domain.ErrPeerNotFound) {
		t.Fatalf("Leave err = %v, want ErrPeerNotFound", err)
	}
}

func TestRoom_GetPeer_UnknownPeer_ReturnsErrPeerNotFound(t *testing.T) {
	t.Parallel()

	room := startTestRoom(t, 8)
	t.Cleanup(func() { _ = room.Stop() })

	if _, err := room.GetPeer(domain.PeerID(404)); !errors.Is(err, domain.ErrPeerNotFound) {
		t.Fatalf("GetPeer err = %v, want ErrPeerNotFound", err)
	}
}

func TestRoom_Send_TargetPeerNotInRoom_ReturnsErrPeerNotFound(t *testing.T) {
	t.Parallel()

	room := startTestRoom(t, 8)
	t.Cleanup(func() { _ = room.Stop() })

	if err := room.Join(&stubPeer{id: domain.PeerID(1)}); err != nil {
		t.Fatalf("Join: %v", err)
	}

	err := room.Send(events.PeerEvent{
		PeerID: domain.PeerID(404),
		Frame:  domain.Frame{OpCode: 0x01},
	})
	if !errors.Is(err, domain.ErrPeerNotFound) {
		t.Fatalf("Send err = %v, want ErrPeerNotFound", err)
	}
}
