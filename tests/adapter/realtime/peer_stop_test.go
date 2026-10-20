package realtime_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	adapterrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/mocks"
	"go.uber.org/mock/gomock"
)

func TestPeer_Stop_UnblocksReceive(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	conn := mocks.NewMockConnection(ctrl)

	// Receive блокируется до Close — имитация транспорта, ждущего кадр из сети.
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

	// nil room безопасен: входящая горутина получает только ошибку из Receive.
	peer := adapterrealtime.NewPeer(
		context.Background(),
		domain.PeerID(1),
		"nick",
		conn,
		nil,
		8,
		queueTestLogger{},
	)

	if err := peer.Start(); err != nil {
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
		t.Fatal("Stop hangs: Receive не разблокировался")
	}

	select {
	case <-closeCh:
	default:
		t.Fatal("connection Close was not called")
	}
}
