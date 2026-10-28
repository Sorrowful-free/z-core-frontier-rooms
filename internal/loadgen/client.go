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

// ErrEnetUnsupported — the ENet client is absent in this build (cgo +
// libenet only under `-tags enet`; stub — client_enet_stub.go,
// implementation — client_enet.go). The enet share of TRANSPORT_MIX in such
// a build fails on dial and is counted in dial_errors{transport=enet}.
var ErrEnetUnsupported = errors.New("enet client not supported in this build (ws only)")

// DataConn — transport-agnostic data-plane connection: frame = OpCode +
// payload (see domain.DecodeIncomingFrame / state opcodes). Implementations:
// WS (wsDataConn) and ENet (enetDataConn) — peer/fleet are transport
// independent.
type DataConn interface {
	// Send writes one [op|payload] frame.
	Send(op domain.OpCode, payload []byte) error
	// Recv blocks until the next frame or a drop. An error means the end of
	// the session.
	Recv() (domain.OpCode, []byte, error)
	// Close drops the connection (abrupt: the server sees a disconnect).
	Close() error
}

// DialParams — everything a transport needs to connect. ENet requires
// EnetAddr:EnetPort + the raw token as the first packet; WS — only WSAddr +
// token in the query.
type DialParams struct {
	WSAddr   string // host:port of the WS endpoint (/ws)
	EnetAddr string // host (no port) of the ENet hosts
	EnetPort uint16 // assigned port from the ticket response (0 — no binding)
	Token    string // join ticket or reconnect token (base64url, as in the response)
	// Mark — SO_MARK for tc netem sharding (inc2); 0 — no marking.
	// Ignored by ENet: the go-enet host owns the UDP socket and doesn't hand
	// out the fd — enet traffic goes unmarked (clean band).
	Mark uint32
}

// dialDataConn — transport selection point; dialEnet lives per-tag in
// client_enet.go / client_enet_stub.go.
func dialDataConn(ctx context.Context, transport string, p DialParams) (DataConn, error) {
	switch transport {
	case "ws":
		return dialWS(ctx, p.WSAddr, p.Token, p.Mark)
	case "enet":
		return dialEnet(ctx, p)
	default:
		return nil, fmt.Errorf("transport %s: unknown", transport)
	}
}

// wsDataConn — DataConn over a fasthttp/websocket client. A frame is a
// binary message: first byte is the OpCode, then the payload (mirrors the
// server-side ws adapter).
type wsDataConn struct {
	conn *fwebsocket.Conn
}

const wsClientWriteDeadline = 5 * time.Second

// dialWS — GET ws://<addr>/ws?token=<base64url-ticket>. The token goes as
// is — the server decodes base64url itself. mark>0 → SO_MARK on the socket
// (netem band).
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
