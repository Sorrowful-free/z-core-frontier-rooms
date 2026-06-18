package realtime_test

import (
	"context"
	"testing"

	adapterrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
)

type queueTestLogger struct{}

func (queueTestLogger) Error(string, ...any) {}
func (queueTestLogger) Info(string, ...any)  {}
func (queueTestLogger) Debug(string, ...any) {}
func (queueTestLogger) Warn(string, ...any)  {}
func (queueTestLogger) Fatal(string, ...any) {}

func TestPeer_Deliver_DropsWhenOutboundFull(t *testing.T) {
	t.Parallel()

	logger := queueTestLogger{}
	peer := adapterrealtime.NewPeer(
		context.Background(),
		domain.PeerID(1),
		"nick",
		nil,
		nil,
		1,
		logger,
	)

	ev := events.PeerEvent{Frame: domain.Frame{OpCode: 0x01}}
	if err := peer.Deliver(ev); err != nil {
		t.Fatalf("first Deliver: %v", err)
	}
	if err := peer.Deliver(ev); err != nil {
		t.Fatalf("second Deliver (drop): %v, want nil", err)
	}
}

func TestRoom_Deliver_DropsWhenIncomingFull(t *testing.T) {
	t.Parallel()

	logger := queueTestLogger{}
	room := adapterrealtime.NewRoom(
		context.Background(),
		domain.RoomID(1),
		nil,
		4,
		1,
		logger,
	)

	ev := events.RoomEvent{PeerID: domain.PeerID(2), Frame: domain.Frame{OpCode: 0x01}}
	if err := room.Deliver(ev); err != nil {
		t.Fatalf("first Deliver: %v", err)
	}
	if err := room.Deliver(ev); err != nil {
		t.Fatalf("second Deliver (drop): %v, want nil", err)
	}
}
