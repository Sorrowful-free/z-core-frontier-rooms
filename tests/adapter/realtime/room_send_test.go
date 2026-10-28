package realtime_test

import (
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	testpeer "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/peer"
)

func TestRoom_Send_ExcludePeerID(t *testing.T) {
	t.Parallel()

	room := startTestRoom(t, 8)
	t.Cleanup(func() { _ = room.Stop() })

	sender := testpeer.New(domain.PeerID(1), "sender")
	receiver := testpeer.New(domain.PeerID(2), "receiver")

	if err := room.Join(sender); err != nil {
		t.Fatalf("Join sender: %v", err)
	}
	if err := room.Join(receiver); err != nil {
		t.Fatalf("Join receiver: %v", err)
	}

	// PeerEvent.PeerID — the target peer; PeerIDInvalid (zero) means
	// broadcast, and ExcludePeerID removes the sender from the mailing.
	err := room.Send(events.PeerEvent{
		PeerID:        domain.PeerIDInvalid,
		ExcludePeerID: sender.GetID(),
		Frame:         domain.Frame{OpCode: 0x01},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got := len(sender.Snapshot()); got != 0 {
		t.Fatalf("sender delivered = %d, want 0 (excluded)", got)
	}
	if got := len(receiver.Snapshot()); got != 1 {
		t.Fatalf("receiver delivered = %d, want 1", got)
	}
}
