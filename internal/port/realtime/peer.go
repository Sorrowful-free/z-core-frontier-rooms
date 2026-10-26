package realtime

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
)

type Peer interface {
	GetID() domain.PeerID
	GetNickName() string
	Ping() int64
	// IsAlive — false: транспорт peer'а мёртв, peer подлежит idle eviction.
	IsAlive() bool

	// Start запускает read/write горутины peer'а; sink — mailbox той
	// комнаты, которая peer'а уже приняла. Контракт порядка
	// (Start-after-join): Start вызывается только после успешного
	// Join/Replace — sink это не «какая-то комната», а входная точка
	// комнаты-владельца. Peer сам проверить членство не может (о
	// комнатах он не знает — и это намеренно, P26): инвариант
	// caller-side. Sink живёт только в замыкании read-горутины и умирает
	// вместе с ней — peer не хранит ссылки на комнату.
	Start(sink EventSink) error

	// Stop — реальное завершение обеих горутин, не fire-and-forget:
	// cancel → conn.Close → read-loop выходит по ошибке Receive,
	// write-loop по ctx.Done, wg.Wait их ждёт. Порядок: Stop после
	// изъятия peer'а из комнаты (Leave/Replace/rollback) — остановленный
	// peer в реестре комнаты = zombie до idle-sweep. Fallback: conn
	// умер сам (сеть) → read-loop выходит по ошибке Receive без Stop;
	// write-loop висит на ctx.Done до Stop от evict-пути.
	Stop() error

	Deliver(peerEvent events.PeerEvent) error
}
