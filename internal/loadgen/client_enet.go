//go:build enet && cgo

package loadgen

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"sync"

	adapterenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/enet"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	libenet "github.com/codecat/go-enet"
)

// enetClientFramesBuf — буфер входящих кадров pump→Recv. Ориентир —
// TRANSPORT_ENET_INCOMING_QUEUE серверной стороны; переполнение = честный
// backpressure на pump (Service стоит → серверный peer timeout), не дроп.
const enetClientFramesBuf = 1024

// enetDataConn — DataConn поверх клиентского libenet host'а (P27 inc3).
// Host'ом владеет adapterenet.HostLoop — ВСЕ libenet-вызовы идут командами
// через Call/Do (per-host сериализация, как у серверной стороны и e2e
// enetClient в tests/integration/match_e2e_enet_test.go). Pump-горутина
// гоняет Service(1) и раскладывает события: EventConnect → admit-пакет
// (сырой decoded ticket, channel 0 reliable), EventReceive → кадр в
// frames-канал, EventDisconnect → конец сессии (close(dead) → Recv EOF).
type enetDataConn struct {
	loop *adapterenet.HostLoop
	peer libenet.Peer

	frames    chan enetFrame
	connected chan struct{} // закрывает pump на EventConnect (handshake + admit ушёл)
	dead      chan struct{} // закрывает pump на выходе — Recv отдаёт ошибку
	err       error         // терминальная причина; пишется до close(dead) → видна читателю dead

	closeOnce sync.Once
}

type enetFrame struct {
	op      domain.OpCode
	payload []byte
}

// dialEnet — ENet-ветка dialDataConn: свой host (peerLimit=1) → Connect на
// EnetAddr:EnetPort (порт строго из ticket-ответа, P18 strict binding) →
// ожидание EventConnect с ctx-дедлайном → admit-пакетом идёт сырой ticket.
// EnetPort==0 — сервер собран/настроен без enet listeners → dial-ошибка.
func dialEnet(ctx context.Context, p DialParams) (DataConn, error) {
	if p.EnetPort == 0 {
		return nil, errors.New("enet dial: ticket has no enet_port binding")
	}
	// Admit = бинарный токен первым пакетом (WS-ветка отдаёт base64url в
	// query и сервер декодирует сам; на ENet декодирует клиент).
	ticket, err := base64.RawURLEncoding.DecodeString(p.Token)
	if err != nil {
		return nil, fmt.Errorf("enet dial: decode ticket token: %w", err)
	}
	loop, err := adapterenet.NewHostLoop(nil, 1, 2, nil)
	if err != nil {
		return nil, fmt.Errorf("enet dial: host: %w", err)
	}
	c := &enetDataConn{
		loop:      loop,
		frames:    make(chan enetFrame, enetClientFramesBuf),
		connected: make(chan struct{}),
		dead:      make(chan struct{}),
	}
	if err := loop.Do(func() error {
		var err error
		c.peer, err = loop.Host().Connect(libenet.NewAddress(p.EnetAddr, p.EnetPort), 2, 0)
		return err
	}); err != nil {
		loop.Stop()
		return nil, fmt.Errorf("enet dial %s:%d: %w", p.EnetAddr, p.EnetPort, err)
	}
	go c.pump(ticket)
	select {
	case <-c.connected:
		return c, nil
	case <-c.dead:
		loop.Stop()
		if c.err != nil {
			return nil, fmt.Errorf("enet dial %s:%d: %w", p.EnetAddr, p.EnetPort, c.err)
		}
		return nil, fmt.Errorf("enet dial %s:%d: disconnected during handshake", p.EnetAddr, p.EnetPort)
	case <-ctx.Done():
		// Handshake не уложился в dial-таймаут: Stop идемпотентен, pump
		// выйдет сам по ошибке Call после остановки loop'а.
		loop.Stop()
		return nil, ctx.Err()
	}
}

// pump — клиентский service-loop: Service(1) командой в свой HostLoop +
// dispatch событий. Первый EventConnect несёт admit (ticket) на channel 0
// reliable — зеркало e2e enetClient. Выход: EventDisconnect (любая причина)
// либо ошибка Call после loop.Stop (Close).
func (c *enetDataConn) pump(ticket []byte) {
	defer close(c.dead)
	admitted := false
	for {
		ev, err := adapterenet.Call(c.loop, func() libenet.Event { return c.loop.Host().Service(1) })
		if err != nil {
			return
		}
		switch ev.GetType() {
		case libenet.EventNone:
		case libenet.EventConnect:
			if admitted {
				continue
			}
			admitted = true
			if err := c.loop.Do(func() error {
				return c.peer.SendBytes(ticket, 0, libenet.PacketFlagReliable)
			}); err != nil {
				c.err = fmt.Errorf("admit send: %w", err)
				return
			}
			close(c.connected)
		case libenet.EventReceive:
			data, err := adapterenet.Call(c.loop, func() []byte { return ev.GetPacket().GetData() })
			_ = c.loop.Do(func() error { ev.GetPacket().Destroy(); return nil })
			if err != nil {
				return
			}
			if len(data) == 0 {
				continue
			}
			c.frames <- enetFrame{op: domain.OpCode(data[0]), payload: data[1:]}
		case libenet.EventDisconnect:
			c.err = fmt.Errorf("enet disconnected (data=%d)", ev.GetData())
			return
		}
	}
}

// Send — кадр [op|payload] на channel 0 reliable: то же, что сервер шлёт
// в EnetConnection.Send (packetFlagsFromDelivery → PacketFlagReliable).
func (c *enetDataConn) Send(op domain.OpCode, payload []byte) error {
	frame := append([]byte{byte(op)}, payload...)
	return c.loop.Do(func() error {
		return c.peer.SendBytes(frame, 0, libenet.PacketFlagReliable)
	})
}

// Recv — следующий кадр или разрыв. После dead дочитывает остаток буфера,
// затем отдаёт терминальную ошибку pump'а (или EOF на штатном Close).
func (c *enetDataConn) Recv() (domain.OpCode, []byte, error) {
	select {
	case f := <-c.frames:
		return f.op, f.payload, nil
	case <-c.dead:
	}
	select {
	case f := <-c.frames:
		return f.op, f.payload, nil
	default:
	}
	if c.err != nil {
		return 0, nil, c.err
	}
	return 0, nil, io.EOF
}

// Close — abrupt-разрыв: DisconnectNow (best-effort reset, сервер увидит
// disconnect), затем loop.Stop — drain очереди + host.Destroy + ожидание
// выхода loop-горутины. Идемпотентно (closeOnce + идемпотентный Stop).
func (c *enetDataConn) Close() error {
	c.closeOnce.Do(func() {
		_ = c.loop.Do(func() error { c.peer.DisconnectNow(0); return nil })
		c.loop.Stop()
	})
	return nil
}
