//go:build enet && cgo

package enet

import (
	"io"
	"sync"
	"sync/atomic"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	libenet "github.com/codecat/go-enet"
)

// EnetConnection - transport.Connection поверх libenet peer'а. Все libenet-
// вызовы peer'а идут командами в очередь HostLoop владеющего host'а
// (hostloop_cgo.go): потребители transport.Connection libenet не видят,
// глобальной сериализации нет - только per-host.
//
// Send/Close - async enqueue: им не нужен результат, а FIFO-очередь сохраняет
// wire-порядок кадров и финального DisconnectLater. Ping - blocking Call:
// зовётся с room/policy-горутин, результат нужен синхронно, а ожидание безопасно
// - executor исполняет только ограниченные по времени libenet-вызовы и не ждёт
// доменных горутин (см. инвариант в hostloop_cgo.go).
type EnetConnection struct {
	loop      *HostLoop
	peer      libenet.Peer
	incoming  chan domain.Frame
	closed    chan struct{}
	closeOnce sync.Once
	alive     atomic.Bool
}

func NewEnetConnection(loop *HostLoop, peer libenet.Peer, incoming chan domain.Frame) *EnetConnection {
	c := &EnetConnection{
		loop:     loop,
		peer:     peer,
		incoming: incoming,
		closed:   make(chan struct{}),
	}
	c.alive.Store(true)
	return c
}

func (c *EnetConnection) Send(frame domain.Frame) error {
	if !c.alive.Load() {
		return io.EOF
	}
	if c.loop == nil {
		return io.EOF
	}
	payload := append([]byte{byte(frame.OpCode)}, frame.Payload...)
	return c.loop.enqueue(func() {
		// go-enet quirk: SendBytes → SendPacket игнорирует код возврата
		// enet_peer_send - отправка disconnecting/zombie peer молча падает
		// (nil error). Страховка - alive-флаг выше; разрыв всплывёт как
		// EventDisconnect → LeaveRoom на стороне host loop.
		_ = c.peer.SendBytes(payload, 0, packetFlagsFromDelivery(frame.Delivery))
	})
}

func (c *EnetConnection) Receive() (domain.Frame, error) {
	select {
	case <-c.closed:
		return domain.Frame{}, io.EOF
	case frame := <-c.incoming:
		return frame, nil
	}
}

func (c *EnetConnection) Ping() int64 {
	if c.peer == nil || c.loop == nil || !c.alive.Load() {
		return -1
	}
	rtt, err := Call(c.loop, func() int64 { return int64(c.peer.GetRoundTripTime()) })
	if err != nil {
		return -1
	}
	return rtt
}

func (c *EnetConnection) Close() error {
	c.closeOnce.Do(func() {
		c.alive.Store(false)
		// incoming не закрываем: peerSession.deliver шлёт из host-цикла вне нашего lifecycle → send-on-closed race (ADR 0003).
		close(c.closed)
		if c.peer != nil && c.loop != nil {
			// Graceful disconnect: DisconnectLater ждёт, пока queued outgoing-пакеты
			// (join reject, последние state-фреймы) уйдут на провод, и только потом шлёт
			// disconnect. DisconnectNow ресетил бы peer и выбрасывал очередь; plain
			// Disconnect тоже не гарантирует доставку уже стоящих в очереди команд.
			_ = c.loop.enqueue(func() { c.peer.DisconnectLater(0) })
		}
	})
	return nil
}

// IsAlive - false после Close(); zombie на ENet вычищает собственный таймаут транспорта (EventDisconnect → LeaveRoom).
func (c *EnetConnection) IsAlive() bool {
	return c.alive.Load()
}
