package realtime_test

import (
	"context"
	"errors"
	"strings"
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
	if err := peer.Deliver(ev); !errors.Is(err, domain.ErrQueueFull) {
		t.Fatalf("second Deliver (drop) err = %v, want ErrQueueFull", err)
	}
}

type noopConnection struct{}

func (noopConnection) Receive() (domain.Frame, error) { return domain.Frame{}, nil }
func (noopConnection) Send(domain.Frame) error        { return nil }
func (noopConnection) Close() error                   { return nil }
func (noopConnection) Ping() int64                    { return 0 }

func TestPeer_Deliver_ReturnsErrorWhenStopped(t *testing.T) {
	t.Parallel()

	logger := queueTestLogger{}
	peer := adapterrealtime.NewPeer(
		context.Background(),
		domain.PeerID(2),
		"nick",
		noopConnection{},
		nil,
		4,
		logger,
	)

	if err := peer.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	ev := events.PeerEvent{Frame: domain.Frame{OpCode: 0x01}}
	err := peer.Deliver(ev)
	if err == nil {
		t.Fatal("Deliver after Stop: want error")
	}
	if !strings.Contains(err.Error(), "peer stopped") {
		t.Fatalf("Deliver err = %v, want peer stopped", err)
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
		nil,
		1,
		logger,
	)

	ev := events.RoomEvent{PeerID: domain.PeerID(2), Frame: domain.Frame{OpCode: 0x01}}
	if err := room.Deliver(ev); err != nil {
		t.Fatalf("first Deliver: %v", err)
	}
	if err := room.Deliver(ev); !errors.Is(err, domain.ErrQueueFull) {
		t.Fatalf("second Deliver (drop) err = %v, want ErrQueueFull", err)
	}
}

func TestRoom_Deliver_ReturnsErrorWhenStopped(t *testing.T) {
	t.Parallel()

	logger := queueTestLogger{}
	room := adapterrealtime.NewRoom(
		context.Background(),
		domain.RoomID(3),
		noopPolicy{},
		4,
		nil,
		4,
		logger,
	)
	if err := room.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := room.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	ev := events.RoomEvent{PeerID: domain.PeerID(2), Frame: domain.Frame{OpCode: 0x01}}
	err := room.Deliver(ev)
	if err == nil {
		t.Fatal("Deliver after Stop: want error")
	}
	if !strings.Contains(err.Error(), "room stopped") {
		t.Fatalf("Deliver err = %v, want room stopped", err)
	}
}
