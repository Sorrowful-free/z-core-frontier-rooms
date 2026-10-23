//go:build enet && cgo

package integration_test

import (
	"encoding/base64"
	"sync/atomic"
	"testing"
	"time"

	adapterenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/enet"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/env"
	libenet "github.com/codecat/go-enet"
)

// ENet-варианты e2e «живого матча» (план docs/plans/P12-e2e-enet-mixed.md),
// ~115s реального времени каждый:
//
//	TestE2E_LiveMatch_ENet  — 4 ENet-клиента; dropout = честное молчание
//	                          libenet (стоп host.Service → нет ack → серверный
//	                          peer timeout (измерено ~10s на localhost, зависит от
//	                          темпа исходящего reliable-трафика) → EventDisconnect →
//	                          LeaveRoom → миграция). Bound ожидания эвикта — 70s.
//	TestE2E_LiveMatch_Mixed — host на ENet (тот же timeout-дроп), второй ENet
//	                          + 2 WS slave'а: кросс-транспортное покрытие
//	                          entities/input/маграций по общему wire-протоколу.
//
// Общий клиентский слой и сценарный драйвер — match_e2e_client_test.go;
// WS-вариант — match_e2e_test.go.

var _ matchClient = (*enetClient)(nil)

// enetClient - клиент комнаты на in-test libenet host. Host'ом владеет
// adapterenet.HostLoop (P15): pump гоняет Service командами через его очередь,
// остальные libenet-вызовы - Call/Do в тот же loop. Per-host сериализация:
// конвоя P12 на общем mutex больше нет, Service(1) остаётся ради темпа pump'а.
type enetClient struct {
	matchClientBase
	loop     *adapterenet.HostLoop
	enetPeer libenet.Peer
	token    []byte // admit: первый пакет после EventConnect — сырой (decoded) ticket

	connected atomic.Bool // EventConnect получен - handshake завершён
	pumpStop  atomic.Bool // stop(): pump выходит без ожидания EventDisconnect
}

func newEnetClient(t *testing.T, e *env.Env, nick, token string) *enetClient {
	t.Helper()

	// Admit-пакет несёт сырой ticket: JoinRoom ждёт бинарный токен
	// (WS-ветка тоже делает base64 decode query-параметра перед JoinRoom).
	ticket, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("[%s] decode ticket token: %v", nick, err)
	}

	loop, err := adapterenet.NewHostLoop(nil, 1, 2)
	if err != nil {
		t.Fatalf("[%s] enet host: %v", nick, err)
	}
	c := &enetClient{
		matchClientBase: newMatchClientBase(t, nick),
		token:           ticket,
		loop:            loop,
	}
	// Страховка на случай t.Fatal до defer stop(): Stop идемпотентен
	// (drain + host.Destroy + ожидание выхода loop-горутины).
	t.Cleanup(loop.Stop)

	err = loop.Do(func() error {
		var err error
		c.enetPeer, err = loop.Host().Connect(libenet.NewAddress("127.0.0.1", e.EnetPort()), 2, 0)
		return err
	})
	if err != nil {
		t.Fatalf("[%s] enet connect: %v", nick, err)
	}

	go c.eventLoop()
	go c.sendLoop(c.writeFrame)
	go c.statsLoop()

	// Ждём EventConnect, пока вернём: сериализуем handshake'и клиентов —
	// параллельные CONNECT иногда теряют дейтаграммы/ответы и достаются
	// libenet-ретраями с экспоненциальным backoff'ом (~6s на наблюдениях).
	waitFor(t, 15*time.Second, nick+" enet connect", func() bool {
		return c.connected.Load()
	})
	return c
}

// eventLoop - pump клиента: Service(1) + dispatch событий. Admit-кадр (сырой
// ticket) шлём на EventConnect; дальше кадры OpCode+payload идут в общий
// handleFrame. Выход — EventDisconnect (любая причина) либо pumpStop/silent.
func (c *enetClient) eventLoop() {
	defer close(c.pumpDone)
	admitted := false
	for {
		if c.silent.Load() {
			c.connDead.Store(true)
			c.t.Logf("[%s] service stopped — silent mode, UDP socket stays bound", c.nick)
			return
		}
		if c.pumpStop.Load() {
			return
		}
		// Service(1) командой в свой HostLoop: pump двигает клиентский service
		// сам; per-host очередь - серверный loop и соседние клиенты не душат.
		ev, err := adapterenet.Call(c.loop, func() libenet.Event { return c.loop.Host().Service(1) })
		if err != nil {
			c.connDead.Store(true)
			return
		}
		switch ev.GetType() {
		case libenet.EventNone:
			continue
		case libenet.EventConnect:
			c.connected.Store(true)
			if admitted {
				continue
			}
			admitted = true
			err := c.loop.Do(func() error {
				return c.enetPeer.SendBytes(c.token, 0, libenet.PacketFlagReliable)
			})
			if err != nil {
				c.connDead.Store(true)
				c.t.Logf("[%s] admit send failed: %v", c.nick, err)
				return
			}
			c.t.Logf("[%s] enet connected, admit sent", c.nick)
		case libenet.EventReceive:
			data, err := adapterenet.Call(c.loop, func() []byte { return ev.GetPacket().GetData() })
			if err != nil {
				c.connDead.Store(true)
				return
			}
			_ = c.loop.Do(func() error { ev.GetPacket().Destroy(); return nil })
			if len(data) == 0 {
				continue
			}
			c.handleFrame(domain.OpCode(data[0]), data[1:])
		case libenet.EventDisconnect:
			c.connDead.Store(true)
			switch {
			case c.leftClean.Load():
				c.t.Logf("[%s] enet disconnected (planned)", c.nick)
			case c.silent.Load():
				c.t.Logf("[%s] enet disconnected while silent", c.nick)
			default:
				c.t.Logf("[%s] enet disconnected (data=%d)", c.nick, ev.GetData())
			}
			return
		}
	}
}

// writeFrame — OpCode+payload в channel 0 reliable — то же, что шлёт сервер
// (EnetConnection.Send → packetFlagsFromDelivery → PacketFlagReliable).
func (c *enetClient) writeFrame(op domain.OpCode, payload []byte) error {
	frame := append([]byte{byte(op)}, payload...)
	return c.loop.Do(func() error {
		return c.enetPeer.SendBytes(frame, 0, libenet.PacketFlagReliable)
	})
}

// goSilent — честный dropout: event loop больше не вызывает host.Service →
// клиент не шлёт ack/ping, UDP-сокет остаётся открытым. Серверский libenet
// детектит мёртвого peer'а собственным таймаутом (~5–30s) → EventDisconnect →
// LeaveRoom → миграция. НЕ peer.DisconnectNow — это был бы clean-ish разрыв.
func (c *enetClient) goSilent() {
	c.silent.Store(true)
	c.stopSend()
}

// leaveClean — плановый уход мастера №2: graceful peer.Disconnect — сервер
// получает disconnect-команду → EventDisconnect → LeaveRoom. Pump продолжает
// Service до собственного EventDisconnect.
func (c *enetClient) leaveClean() {
	c.leftClean.Store(true)
	c.stopSend()
	_ = c.loop.Do(func() error { c.enetPeer.Disconnect(0); return nil })
}

// stop - финальный teardown: graceful disconnect (если pump ещё жив), bound на
// выход pump'а, затем loop.Stop → host.Destroy. Destroy только после pumpDone -
// Service на разрушенном host был бы UB.
func (c *enetClient) stop() {
	c.leftClean.Store(true)
	c.stopSend()
	select {
	case <-c.pumpDone:
	default:
		_ = c.loop.Do(func() error { c.enetPeer.Disconnect(0); return nil })
	}
	select {
	case <-c.pumpDone:
	case <-time.After(3 * time.Second):
		c.pumpStop.Store(true)
		<-c.pumpDone
	}
	<-c.sendDone
	c.loop.Stop()
}

// TestE2E_LiveMatch_ENet — 4 ENet-клиента, host dropout = молчание libenet.
func TestE2E_LiveMatch_ENet(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e live match ~115s real time — skipped under -short")
	}
	t.Parallel()

	e := env.New(t, env.WithEnet())

	roomID, hostToken := e.CreateRoom(t, "host")
	host := newEnetClient(t, e, "host", hostToken)
	s1 := newEnetClient(t, e, "e1", e.IssueTicket(t, roomID, "e1"))
	s2 := newEnetClient(t, e, "e2", e.IssueTicket(t, roomID, "e2"))
	s3 := newEnetClient(t, e, "e3", e.IssueTicket(t, roomID, "e3"))
	clients := []*enetClient{host, s1, s2, s3}
	defer func() {
		for _, c := range clients {
			c.stop()
		}
	}()

	runLiveMatch(t, e, roomID, host, []matchClient{s1, s2, s3}, liveMatchTiming{
		hostSilentAt:  15 * time.Second,
		evictBound:    70 * time.Second, // libenet peer timeout: ~10s измерено, bound с запасом под CI
		secondLeaveAt: 95 * time.Second,
		finishAt:      115 * time.Second,
	})
}

// TestE2E_LiveMatch_Mixed — host на ENet (timeout-дроп) + ENet и WS slave'ы в
// одной комнате: выжившие на обоих транспортах видят миграции, entities и input
// друг друга через общий wire-протокол.
func TestE2E_LiveMatch_Mixed(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e live match ~115s real time — skipped under -short")
	}
	t.Parallel()

	e := env.New(t, env.WithEnet())

	roomID, hostToken := e.CreateRoom(t, "host")
	// Host — первый joiner на ENet → начальный master; dropout — enet timeout.
	host := newEnetClient(t, e, "host", hostToken)
	e1 := newEnetClient(t, e, "e1", e.IssueTicket(t, roomID, "e1"))
	w1 := newWSClient(t, e, "w1", e.IssueTicket(t, roomID, "w1"))
	w2 := newWSClient(t, e, "w2", e.IssueTicket(t, roomID, "w2"))
	slaves := []matchClient{e1, w1, w2}
	defer func() {
		host.stop()
		for _, s := range slaves {
			s.stop()
		}
	}()

	runLiveMatch(t, e, roomID, host, slaves, liveMatchTiming{
		hostSilentAt:  15 * time.Second,
		evictBound:    70 * time.Second,
		secondLeaveAt: 95 * time.Second,
		finishAt:      115 * time.Second,
	})
}
