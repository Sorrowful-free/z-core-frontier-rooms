package integration_test

import (
	"context"
	"errors"
	"testing"
	"time"

	statepolicy "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/policy/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/env"
)

// TestIntegration_NoMigration_RoomDiesWithHost — P33: в no-migration
// деплое (STATE_POLICY_ALLOW_MASTER_MIGRATION=false, здесь —
// env.WithStatePolicy) комната умирает вместе с хостом: разрыв master'а →
// выжившему уходит OpRoomClosed (0x82, payload пустой) и сервер закрывает
// соединение; комната unregister'ится, слоты reservation ревокаются (grace
// не применяется — resume невозможен).
func TestIntegration_NoMigration_RoomDiesWithHost(t *testing.T) {
	t.Parallel()

	e := env.New(t, env.WithStatePolicy(statepolicy.StatePolicyConfig{AllowMasterMigration: false}))

	roomID, hostToken := e.CreateRoom(t, "host")
	hostConn := e.DialWS(t, "token="+hostToken)
	e.WaitPeerCount(t, roomID, 1)

	joinerToken := e.IssueTicket(t, roomID, "joiner")
	joinerConn := e.DialWS(t, "token="+joinerToken)
	defer joinerConn.Close()
	e.WaitPeerCount(t, roomID, 2)

	// Разрыв хоста = смерть комнаты: перевыборов нет.
	if err := hostConn.Close(); err != nil {
		t.Fatalf("host conn close: %v", err)
	}

	payload := waitOp(t, joinerConn, domain.OpRoomClosed, 3*time.Second)
	if len(payload) != 0 {
		t.Fatalf("OpRoomClosed payload = %x, want empty", payload)
	}

	// После кадра сервер закрывает соединение выжившего.
	_ = joinerConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := joinerConn.ReadMessage(); err == nil {
		t.Fatal("joiner read after OpRoomClosed: want disconnect error")
	}

	if _, err := e.Registry.GetRoom(context.Background(), roomID); !errors.Is(err, domain.ErrRoomNotFound) {
		t.Fatalf("GetRoom err = %v, want ErrRoomNotFound", err)
	}
	// Комната unregister'нута из reservation целиком — слоты (включая
	// grace-меченные) отозваны, комнаты в store нет.
	if _, err := e.Reservation.ListAdmittedPeers(context.Background(), roomID); !errors.Is(err, domain.ErrReservationNotFound) {
		t.Fatalf("ListAdmittedPeers err = %v, want ErrReservationNotFound", err)
	}
}
