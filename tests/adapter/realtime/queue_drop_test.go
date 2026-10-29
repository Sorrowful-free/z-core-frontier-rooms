package realtime_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	adapterrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/mocks"
	"go.uber.org/mock/gomock"
)

type queueTestLogger struct{}

func (queueTestLogger) Error(string, ...any) {}
func (queueTestLogger) Info(string, ...any)  {}
func (queueTestLogger) Debug(string, ...any) {}
func (queueTestLogger) Warn(string, ...any)  {}
func (queueTestLogger) Fatal(string, ...any) {}

func TestPeer_Deliver_DropsUnreliableWhenOutboundFull(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	// No expectations at all: any connection call — Close in particular —
	// fails the test; an unreliable frame must be dropped silently.
	conn := mocks.NewMockConnection(ctrl)

	logger := queueTestLogger{}
	peer := adapterrealtime.NewPeer(
		context.Background(),
		domain.PeerID(1),
		"nick",
		conn,
		1,
		logger,
	)

	ev := events.PeerEvent{Frame: domain.Frame{OpCode: 0x01, Delivery: domain.DeliveryUnreliable}}
	if err := peer.Deliver(ev); err != nil {
		t.Fatalf("first Deliver: %v", err)
	}
	if err := peer.Deliver(ev); !errors.Is(err, domain.ErrQueueFull) {
		t.Fatalf("second Deliver (drop) err = %v, want ErrQueueFull", err)
	}
}

// P37: a reliable frame is never silently dropped — on a full outbound
// queue the peer closes the connection (teardown escalation). DeliveryDefault
// counts as reliable (it maps to the reliable wire flag on ENet).
func TestPeer_Deliver_ClosesConnectionWhenOutboundFullReliable(t *testing.T) {
	t.Parallel()

	for _, delivery := range []domain.Delivery{domain.DeliveryDefault, domain.DeliveryReliable} {
		delivery := delivery
		t.Run(deliveryName(delivery), func(t *testing.T) {
			ctrl := gomock.NewController(t)
			closed := make(chan struct{})
			var closeOnce sync.Once
			conn := mocks.NewMockConnection(ctrl)
			conn.EXPECT().Close().DoAndReturn(func() error {
				closeOnce.Do(func() { close(closed) })
				return nil
			}).Times(1)

			peer := adapterrealtime.NewPeer(
				context.Background(),
				domain.PeerID(1),
				"nick",
				conn,
				1,
				queueTestLogger{},
			)

			ev := events.PeerEvent{Frame: domain.Frame{OpCode: 0x01, Delivery: delivery}}
			if err := peer.Deliver(ev); err != nil {
				t.Fatalf("first Deliver: %v", err)
			}
			if err := peer.Deliver(ev); !errors.Is(err, domain.ErrQueueFull) {
				t.Fatalf("second Deliver err = %v, want ErrQueueFull", err)
			}

			select {
			case <-closed:
			default:
				t.Fatal("reliable frame on a full outbound queue must close the connection")
			}
		})
	}
}

func deliveryName(d domain.Delivery) string {
	switch d {
	case domain.DeliveryDefault:
		return "default"
	case domain.DeliveryReliable:
		return "reliable"
	default:
		return "unreliable"
	}
}

// newIncomingConn — a connection whose Receive yields the frames pushed by
// the test and then blocks until Close (like a transport waiting on the
// network). closeCh is closed by the first Close call.
func newIncomingConn(t *testing.T, ctrl *gomock.Controller) (*mocks.MockConnection, chan domain.Frame, chan struct{}) {
	t.Helper()

	conn := mocks.NewMockConnection(ctrl)
	frames := make(chan domain.Frame, 8)
	closeCh := make(chan struct{})
	var closeOnce sync.Once

	conn.EXPECT().Receive().DoAndReturn(func() (domain.Frame, error) {
		select {
		case frame := <-frames:
			return frame, nil
		case <-closeCh:
			return domain.Frame{}, errors.New("connection closed")
		}
	}).AnyTimes()
	conn.EXPECT().Close().DoAndReturn(func() error {
		closeOnce.Do(func() { close(closeCh) })
		return nil
	}).AnyTimes()
	conn.EXPECT().Send(gomock.Any()).Return(nil).AnyTimes()

	return conn, frames, closeCh
}

// P37: the room incoming mailbox answers ErrQueueFull — the read loop drops
// an unreliable frame and keeps reading; a reliable frame escalates to
// conn.Close (the read loop exits into the transport teardown path).
func TestPeer_Incoming_RoomQueueFull_UnreliableDropsReliableCloses(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	conn, frames, closeCh := newIncomingConn(t, ctrl)

	var delivered atomic.Int32
	sink := mocks.NewMockRoom(ctrl)
	sink.EXPECT().Deliver(gomock.Any()).DoAndReturn(func(events.RoomEvent) error {
		delivered.Add(1)
		return domain.ErrQueueFull
	}).AnyTimes()

	peer := adapterrealtime.NewPeer(
		context.Background(),
		domain.PeerID(7),
		"nick",
		conn,
		8,
		queueTestLogger{},
	)
	if err := peer.Start(sink); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = peer.Stop() })

	// Unreliable: dropped, the connection stays alive and the read loop
	// keeps consuming.
	frames <- domain.Frame{OpCode: 0x01, Delivery: domain.DeliveryUnreliable}
	waitForCond(t, 2*time.Second, "the unreliable frame to reach the sink", func() bool {
		return delivered.Load() >= 1
	})
	select {
	case <-closeCh:
		t.Fatal("unreliable frame on a full room queue must not close the connection")
	default:
	}

	// Reliable: the read loop closes the connection and exits.
	frames <- domain.Frame{OpCode: 0x01, Delivery: domain.DeliveryReliable}
	select {
	case <-closeCh:
	case <-time.After(2 * time.Second):
		t.Fatal("reliable frame on a full room queue must close the connection")
	}
}

func waitForCond(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

type noopConnection struct{}

func (noopConnection) Receive() (domain.Frame, error) { return domain.Frame{}, nil }
func (noopConnection) Send(domain.Frame) error        { return nil }
func (noopConnection) Close() error                   { return nil }
func (noopConnection) Ping() int64                    { return 0 }
func (noopConnection) IsAlive() bool                  { return true }

func TestPeer_Deliver_ReturnsErrorWhenStopped(t *testing.T) {
	t.Parallel()

	logger := queueTestLogger{}
	peer := adapterrealtime.NewPeer(
		context.Background(),
		domain.PeerID(2),
		"nick",
		noopConnection{},
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
