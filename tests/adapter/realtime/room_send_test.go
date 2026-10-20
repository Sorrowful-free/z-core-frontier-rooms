package realtime_test

import (
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
)

func TestRoom_Send_ExcludePeerID(t *testing.T) {
	t.Parallel()

	room := startTestRoom(t, 8)
	t.Cleanup(func() { _ = room.Stop() })

	sender := newErrorCapturePeer(domain.PeerID(1))
	receiver := newErrorCapturePeer(domain.PeerID(2))

	if err := room.Join(sender); err != nil {
		t.Fatalf("Join sender: %v", err)
	}
	if err := room.Join(receiver); err != nil {
		t.Fatalf("Join receiver: %v", err)
	}

	// PeerEvent.PeerID — целевой peer; PeerIDInvalid (zero) даёт broadcast,
	// а ExcludePeerID выкидывает отправителя из рассылки.
	err := room.Send(events.PeerEvent{
		PeerID:        domain.PeerIDInvalid,
		ExcludePeerID: sender.id,
		Frame:         domain.Frame{OpCode: 0x01},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got := len(sender.snapshot()); got != 0 {
		t.Fatalf("sender delivered = %d, want 0 (excluded)", got)
	}
	if got := len(receiver.snapshot()); got != 1 {
		t.Fatalf("receiver delivered = %d, want 1", got)
	}
}
