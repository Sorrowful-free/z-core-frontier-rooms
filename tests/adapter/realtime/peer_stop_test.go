package realtime_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	adapterrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/mocks"
	"go.uber.org/mock/gomock"
)

// newBlockingConn — Receive blocks until Close, simulating a transport waiting for a frame from the network.
// closeCh is closed by the first Close call.
func newBlockingConn(t *testing.T, ctrl *gomock.Controller) (*mocks.MockConnection, chan struct{}) {
	t.Helper()

	conn := mocks.NewMockConnection(ctrl)
	closeCh := make(chan struct{})
	var closeOnce sync.Once

	conn.EXPECT().Receive().DoAndReturn(func() (domain.Frame, error) {
		<-closeCh
		return domain.Frame{}, errors.New("connection closed")
	}).AnyTimes()
	conn.EXPECT().Close().DoAndReturn(func() error {
		closeOnce.Do(func() { close(closeCh) })
		return nil
	}).AnyTimes()

	return conn, closeCh
}

func TestPeer_Stop_UnblocksReceive(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	conn, closeCh := newBlockingConn(t, ctrl)

	peer := adapterrealtime.NewPeer(
		context.Background(),
		domain.PeerID(1),
		"nick",
		conn,
		8,
		queueTestLogger{},
	)

	// nil sink is safe: the inbound goroutine only gets the error from Receive.
	if err := peer.Start(nil); err != nil {
		t.Fatalf("Start: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- peer.Stop() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stop hangs: Receive did not unblock")
	}

	select {
	case <-closeCh:
	default:
		t.Fatal("connection Close was not called")
	}
}

func TestPeer_Stop_ConcurrentDeliver_NoPanic(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	conn, _ := newBlockingConn(t, ctrl)
	conn.EXPECT().Send(gomock.Any()).Return(nil).AnyTimes()

	peer := adapterrealtime.NewPeer(
		context.Background(),
		domain.PeerID(1),
		"nick",
		conn,
		8,
		queueTestLogger{},
	)
	// nil sink is safe: the inbound goroutine only gets the error from Receive.
	if err := peer.Start(nil); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ev := events.PeerEvent{Frame: domain.Frame{OpCode: 0x01}}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	const producers = 8

	for range producers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = peer.Deliver(ev)
				}
			}
		}()
	}

	time.Sleep(10 * time.Millisecond)

	if err := peer.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	close(stop)
	wg.Wait()

	if err := peer.Deliver(ev); err == nil {
		t.Fatal("Deliver after Stop must return a 'peer stopped' error, not panic")
	}
}
