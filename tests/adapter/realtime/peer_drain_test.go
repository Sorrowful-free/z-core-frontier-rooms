package realtime_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	adapterrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/mocks"
	"go.uber.org/mock/gomock"
)

// connCalls records conn method calls in order — the guarantee of
// drain-aware Stop (P30) is the order "Send of the committed frame before Close".
type connCalls struct {
	mu    sync.Mutex
	order []string
}

func (c *connCalls) record(name string) {
	c.mu.Lock()
	c.order = append(c.order, name)
	c.mu.Unlock()
}

func (c *connCalls) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.order)
}

// newRecordingConn — newBlockingConn (Receive blocks until the first
// Close) + recording Sends into connCalls. The test places the "close"
// marker itself via closeCh after Stop returns: by that point wg.Wait
// has already guaranteed the write loop's death — no late sends.
func newRecordingConn(t *testing.T, ctrl *gomock.Controller) (*mocks.MockConnection, *connCalls, chan struct{}) {
	t.Helper()

	conn, closeCh := newBlockingConn(t, ctrl)
	calls := &connCalls{}
	conn.EXPECT().Send(gomock.Any()).DoAndReturn(func(domain.Frame) error {
		calls.record("send")
		return nil
	}).AnyTimes()

	return conn, calls, closeCh
}

// (a) A frame whose Deliver committed before Stop goes out via Send before
// conn.Close — that is the drain contract (the previous Stop lost it in a select race).
func TestPeer_Stop_DrainsCommittedFrameBeforeClose(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	conn, calls, closeCh := newRecordingConn(t, ctrl)

	// NewPeer without a factory — package-default drainTimeout (1s) > 0.
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

	if err := peer.Deliver(events.PeerEvent{Frame: domain.Frame{OpCode: domain.OpRoomClosed}}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if err := peer.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	select {
	case <-closeCh:
	default:
		t.Fatal("conn.Close не вызван в Stop")
	}
	calls.record("close")

	if got, want := calls.snapshot(), []string{"send", "close"}; !slices.Equal(got, want) {
		t.Fatalf("conn calls = %v, want %v (Send до Close)", got, want)
	}
}

// (b) drainTimeout=0 — drain disabled: Stop does not wait for the write loop at all.
// Send blocks until conn Close — if Stop waited for writeDone without a
// deadline, the test would hang on conn.Close.
func TestPeer_Stop_DrainDisabled_DoesNotWaitForWriteLoop(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	conn, closeCh := newBlockingConn(t, ctrl)
	conn.EXPECT().IsAlive().Return(false).AnyTimes()
	conn.EXPECT().Send(gomock.Any()).DoAndReturn(func(domain.Frame) error {
		<-closeCh
		return errors.New("send on closed connection")
	}).AnyTimes()

	factory := adapterrealtime.NewPeerFactory(queueTestLogger{}, 8, 0, nil)
	peer, err := factory.CreatePeer(
		context.Background(),
		domain.PeerID(1),
		"nick",
		conn,
		nil,
	)
	if err != nil {
		t.Fatalf("CreatePeer: %v", err)
	}
	if err := peer.Start(nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := peer.Deliver(events.PeerEvent{Frame: domain.Frame{OpCode: 0x01}}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	start := time.Now()
	if err := peer.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Stop с drain=0 ждал write-loop: elapsed %v", elapsed)
	}
}

// (c) Send is blocked → drain never catches up to empty → once
// drainTimeout expires Stop proceeds to force conn.Close, interrupting the
// write: Stop returns in about the deadline, it does not hang on the eternal Send.
func TestPeer_Stop_BlockedSend_ForceCloseAfterDrainTimeout(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	conn, closeCh := newBlockingConn(t, ctrl)
	conn.EXPECT().IsAlive().Return(false).AnyTimes()
	sendEntered := make(chan struct{})
	var sendOnce sync.Once
	conn.EXPECT().Send(gomock.Any()).DoAndReturn(func(domain.Frame) error {
		sendOnce.Do(func() { close(sendEntered) })
		<-closeCh // force-close from Stop interrupts the blocked write
		return errors.New("send on closed connection")
	}).AnyTimes()

	const drainTimeout = 150 * time.Millisecond
	factory := adapterrealtime.NewPeerFactory(queueTestLogger{}, 8, drainTimeout, nil)
	peer, err := factory.CreatePeer(
		context.Background(),
		domain.PeerID(1),
		"nick",
		conn,
		nil,
	)
	if err != nil {
		t.Fatalf("CreatePeer: %v", err)
	}
	if err := peer.Start(nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := peer.Deliver(events.PeerEvent{Frame: domain.Frame{OpCode: domain.OpRoomClosed}}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	// Wait until the write loop has actually entered the blocked Send —
	// otherwise drain would finish before the deadline and the test would verify nothing.
	select {
	case <-sendEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("write-loop не дошёл до Send")
	}

	start := time.Now()
	if err := peer.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed < drainTimeout {
		t.Fatalf("Stop вернулся за %v раньше drain-дедлайна %v", elapsed, drainTimeout)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("Stop висит над заблокированным Send: %v (дедлайн %v)", elapsed, drainTimeout)
	}
}
