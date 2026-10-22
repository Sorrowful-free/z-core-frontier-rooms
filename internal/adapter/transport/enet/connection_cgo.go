//go:build enet && cgo

package enet

import (
	"io"
	"sync"
	"sync/atomic"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	libenet "github.com/codecat/go-enet"
)

// apiMu — единая точка сериализации вызовов libenet, трогающих C-состояние
// (host/peer/packet): библиотека не
// thread-safe, а host loop (delivery/enet) и peer outbound горутины (Send/Ping/Close)
// обращаются к ней конкурентно. Хост один на процесс → один mutex на пакет.
//
// Tradeoff: host.Service(ServiceTimeoutMs) тоже держит apiMu — в худшем случае
// вызовы из peer-горутин ждут ~ServiceTimeoutMs (10ms по умолчанию). Этот
// ограниченный latency-конвой — принятая цена простой модели сериализации;
// dispatch вызовов на host-loop горутину — возможный будущий редизайн.
var apiMu sync.Mutex

// Call выполняет fn под локом сериализации libenet и возвращает результат.
// КАЖДЫЙ вызов libenet (Host/Peer/Packet методы, NewHost, Initialize…) из любого
// пакета обязан идти через Call/Do — см. также internal/delivery/enet.
// Внутри fn — только один libenet-вызов, без вложенных Call/Do и без Go-колбэков,
// которые могут снова дотянуться до libenet (иначе дедлок).
func Call[T any](fn func() T) T {
	apiMu.Lock()
	defer apiMu.Unlock()
	return fn()
}

// Do — вариант Call для вызовов без результата.
func Do(fn func()) {
	apiMu.Lock()
	defer apiMu.Unlock()
	fn()
}

type EnetConnection struct {
	peer      libenet.Peer
	incoming  chan domain.Frame
	closed    chan struct{}
	closeOnce sync.Once
	alive     atomic.Bool
}

func NewEnetConnection(peer libenet.Peer, incoming chan domain.Frame) *EnetConnection {
	c := &EnetConnection{
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
	payload := append([]byte{byte(frame.OpCode)}, frame.Payload...)
	return Call(func() error {
		// go-enet quirk: SendBytes → SendPacket игнорирует код возврата
		// enet_peer_send — отправка disconnecting/zombie peer молча падает
		// (nil error). Страховка — alive-флаг выше; разрыв всплывёт как
		// EventDisconnect → LeaveRoom на стороне host loop.
		return c.peer.SendBytes(payload, 0, packetFlagsFromDelivery(frame.Delivery))
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
	if c.peer == nil || !c.alive.Load() {
		return -1
	}
	return Call(func() int64 { return int64(c.peer.GetRoundTripTime()) })
}

func (c *EnetConnection) Close() error {
	c.closeOnce.Do(func() {
		c.alive.Store(false)
		// incoming не закрываем: peerSession.deliver шлёт из host-цикла вне нашего lifecycle → send-on-closed race (ADR 0003).
		close(c.closed)
		if c.peer != nil {
			// Graceful disconnect: DisconnectLater ждёт, пока queued outgoing-пакеты
			// (join reject, последние state-фреймы) уйдут на провод, и только потом шлёт
			// disconnect. DisconnectNow ресетил бы peer и выбрасывал очередь; plain
			// Disconnect тоже не гарантирует доставку уже стоящих в очереди команд.
			Do(func() { c.peer.DisconnectLater(0) })
		}
	})
	return nil
}

// IsAlive — false после Close(); zombie на ENet вычищает собственный таймаут транспорта (EventDisconnect → LeaveRoom).
func (c *EnetConnection) IsAlive() bool {
	return c.alive.Load()
}
