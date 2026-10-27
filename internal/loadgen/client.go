package loadgen

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	fwebsocket "github.com/fasthttp/websocket"
)

// ErrEnetUnsupported — ENet-клиент в этой сборке отсутствует (inc 1: cgo +
// libenet только под `-tags enet`, реализация — inc 3, `client_enet.go`).
// Доля enet-клиентов из TRANSPORT_MIX падает на dial и считается в
// dial_errors{transport=enet}.
var ErrEnetUnsupported = errors.New("enet client not supported in this build (inc1: ws only)")

// DataConn — транспорт-агностичное соединение data plane: кадр = OpCode +
// payload (см. domain.DecodeIncomingFrame / state opcodes). Реализации: WS
// (wsDataConn) и ENet (inc 3) — peer/fleet от транспорта не зависят.
type DataConn interface {
	// Send пишет один кадр [op|payload].
	Send(op domain.OpCode, payload []byte) error
	// Recv блокируется до следующего кадра или разрыва. Ошибка — конец сессии.
	Recv() (domain.OpCode, []byte, error)
	// Close — разрыв соединения (abrupt: сервер увидит disconnect).
	Close() error
}

// DialParams — всё, что нужно транспорту для подключения. ENet (inc 3)
// потребуется EnetAddr:EnetPort + сырой токен первым пакетом; WS — только
// WSAddr + token в query.
type DialParams struct {
	WSAddr   string // host:port WS endpoint'а (/ws)
	EnetAddr string // host (без порта) ENet host'ов — inc 3
	EnetPort uint16 // назначенный порт из ticket-ответа (0 — нет привязки)
	Token    string // join-ticket или reconnect-токен (base64url, как в ответе)
	// Mark — SO_MARK для tc netem-шардинга (inc2); 0 — без маркировки.
	Mark uint32
}

// dialDataConn — точка выбора транспорта (inc 3 добавит case "enet").
func dialDataConn(ctx context.Context, transport string, p DialParams) (DataConn, error) {
	switch transport {
	case "ws":
		return dialWS(ctx, p.WSAddr, p.Token, p.Mark)
	case "enet":
		return nil, fmt.Errorf("transport enet: %w", ErrEnetUnsupported)
	default:
		return nil, fmt.Errorf("transport %s: unknown", transport)
	}
}

// wsDataConn — DataConn поверх fasthttp/websocket client. Кадр — binary
// message: первый байт OpCode, дальше payload (зеркало серверного ws-адаптера).
type wsDataConn struct {
	conn *fwebsocket.Conn
}

const wsClientWriteDeadline = 5 * time.Second

// dialWS — GET ws://<addr>/ws?token=<base64url-ticket>. Токен идёт как есть —
// сервер сам декодирует base64url. mark>0 → SO_MARK на сокете (netem-band).
func dialWS(ctx context.Context, addr, token string, mark uint32) (DataConn, error) {
	u := url.URL{Scheme: "ws", Host: addr, Path: "/ws", RawQuery: "token=" + token}
	dialer := fwebsocket.Dialer{
		NetDialContext:   markedDialer(mark).DialContext,
		HandshakeTimeout: 10 * time.Second,
	}
	conn, resp, err := dialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		return nil, fmt.Errorf("ws dial %s: %w (status %d)", u.Host, err, status)
	}
	return &wsDataConn{conn: conn}, nil
}

func (c *wsDataConn) Send(op domain.OpCode, payload []byte) error {
	_ = c.conn.SetWriteDeadline(time.Now().Add(wsClientWriteDeadline))
	return c.conn.WriteMessage(fwebsocket.BinaryMessage, append([]byte{byte(op)}, payload...))
}

func (c *wsDataConn) Recv() (domain.OpCode, []byte, error) {
	_, msg, err := c.conn.ReadMessage()
	if err != nil {
		return 0, nil, err
	}
	if len(msg) == 0 {
		return 0, nil, errors.New("empty frame")
	}
	return domain.OpCode(msg[0]), msg[1:], nil
}

func (c *wsDataConn) Close() error {
	return c.conn.Close()
}
