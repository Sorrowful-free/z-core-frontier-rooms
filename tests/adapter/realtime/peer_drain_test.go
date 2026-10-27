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

// connCalls упорядоченно пишет вызовы методов conn'а — гарантия
// drain-aware Stop (P30) это порядок «Send закоммиченного кадра до Close».
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

// newRecordingConn — newBlockingConn (Receive блокируется до первого
// Close) + запись Send'ов в connCalls. Маркер «close» тест ставит
// сам по closeCh после возврата Stop: к этому моменту wg.Wait уже
// гарантировал смерть write-loop'а — поздних send нет.
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

// (а) Кадр, чей Deliver закоммитил до Stop, уходит Send'ом до conn.Close —
// это и есть контракт drain'а (прежний Stop терял его в select-гонке).
func TestPeer_Stop_DrainsCommittedFrameBeforeClose(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	conn, calls, closeCh := newRecordingConn(t, ctrl)

	// NewPeer без фабрики — пакетный дефолт drainTimeout (1s) > 0.
	peer := adapterrealtime.NewPeer(
		context.Background(),
		domain.PeerID(1),
		"nick",
		conn,
		8,
		queueTestLogger{},
	)
	// nil sink безопасен: входящая горутина получает только ошибку из Receive.
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

// (б) drainTimeout=0 — drain выключен: Stop не ждёт write-loop вообще.
// Send висит до Close conn'а — если бы Stop дожидался writeDone без
// дедлайна, тест завис бы на conn.Close.
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

// (в) Send заблокирован → drain не догоняет пустоту → по истечении
// drainTimeout Stop идёт на force conn.Close, прерывающий запись:
// Stop возвращается ≈за дедлайн, не висит на вечном Send.
func TestPeer_Stop_BlockedSend_ForceCloseAfterDrainTimeout(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	conn, closeCh := newBlockingConn(t, ctrl)
	conn.EXPECT().IsAlive().Return(false).AnyTimes()
	sendEntered := make(chan struct{})
	var sendOnce sync.Once
	conn.EXPECT().Send(gomock.Any()).DoAndReturn(func(domain.Frame) error {
		sendOnce.Do(func() { close(sendEntered) })
		<-closeCh // force-close из Stop прерывает заблокированную запись
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

	// Дожидаемся, что write-loop реально ушёл в заблокированный Send —
	// иначе drain завершился бы быстрее дедлайна и тест ничего не проверяет.
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
