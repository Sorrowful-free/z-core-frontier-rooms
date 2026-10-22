package ws_test

import (
	"encoding/base64"
	"testing"
	"time"

	adaptertransport "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/env"
	fwebsocket "github.com/fasthttp/websocket"
)

const wsReadTimeout = 2 * time.Second

func TestWS_Join_InvalidToken(t *testing.T) {
	t.Parallel()

	e := env.New(t)

	conn := e.DialWS(t, "token="+base64.RawURLEncoding.EncodeToString([]byte("garbage")))
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(wsReadTimeout))
	msgType, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read reject frame: %v", err)
	}
	if msgType != fwebsocket.BinaryMessage {
		t.Fatalf("message type = %d, want binary (%d)", msgType, fwebsocket.BinaryMessage)
	}
	if len(msg) != 1 || msg[0] != byte(domain.OpInvalidToken) {
		t.Fatalf("reject frame = %x, want [%02x]", msg, byte(domain.OpInvalidToken))
	}

	if msgType, msg, err := conn.ReadMessage(); err == nil {
		t.Fatalf("expected closed connection after reject, got type=%d msg=%x", msgType, msg)
	}
}

func TestWS_Join_MissingToken(t *testing.T) {
	t.Parallel()

	e := env.New(t)

	conn := e.DialWS(t, "")
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(wsReadTimeout))
	msgType, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read reject frame: %v", err)
	}
	if msgType != fwebsocket.BinaryMessage {
		t.Fatalf("message type = %d, want binary (%d)", msgType, fwebsocket.BinaryMessage)
	}
	if len(msg) != 1 || msg[0] != byte(domain.OpEmptyToken) {
		t.Fatalf("reject frame = %x, want [%02x]", msg, byte(domain.OpEmptyToken))
	}

	if msgType, msg, err := conn.ReadMessage(); err == nil {
		t.Fatalf("expected closed connection after reject, got type=%d msg=%x", msgType, msg)
	}
}

func TestWS_Join_MalformedTokenEncoding(t *testing.T) {
	t.Parallel()

	e := env.New(t)

	conn := e.DialWS(t, "token=*")
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(wsReadTimeout))
	msgType, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read reject frame: %v", err)
	}
	if msgType != fwebsocket.BinaryMessage {
		t.Fatalf("message type = %d, want binary (%d)", msgType, fwebsocket.BinaryMessage)
	}
	if len(msg) != 1 || msg[0] != byte(domain.OpInvalidToken) {
		t.Fatalf("reject frame = %x, want [%02x]", msg, byte(domain.OpInvalidToken))
	}

	if msgType, msg, err := conn.ReadMessage(); err == nil {
		t.Fatalf("expected closed connection after reject, got type=%d msg=%x", msgType, msg)
	}
}

// Oversized token (len > MaxIncomingFrameBytes): сервер закрывает соединение
// без reject-кадра — документируем текущее поведение (см. аудит: кандидат на
// явный reject OpCode).
func TestWS_Join_TokenTooLarge(t *testing.T) {
	t.Parallel()

	e := env.New(t, env.WithTransport(adaptertransport.TransportConfig{
		MaxIncomingFrameBytes: 16,
		RoomIncomingQueueSize: adaptertransport.DefaultRoomIncomingQueueSize,
		PeerOutboundQueueSize: adaptertransport.DefaultPeerOutboundQueueSize,
		EnetIncomingQueueSize: adaptertransport.DefaultEnetIncomingQueueSize,
	}))

	bigToken := base64.RawURLEncoding.EncodeToString(make([]byte, 64))
	conn := e.DialWS(t, "token="+bigToken)
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(wsReadTimeout))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("expected closed connection for oversized token")
	}
}

func TestWS_Join_WithIssuedTicket(t *testing.T) {
	t.Parallel()

	e := env.New(t)

	roomID, _ := e.CreateRoom(t, "host")
	joinerToken := e.IssueTicket(t, roomID, "joiner")

	// token из JSON уже base64url — в query идёт как есть.
	conn := e.DialWS(t, "token="+joinerToken)
	defer conn.Close()

	e.WaitPeerNick(t, roomID, "joiner", true)

	if err := conn.Close(); err != nil {
		t.Fatalf("conn close: %v", err)
	}
	e.WaitPeerNick(t, roomID, "joiner", false)
}
