package integration_test

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/codec"
	adaptertransport "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/env"
	fwebsocket "github.com/fasthttp/websocket"
)

// Сценарий e2e «живого матча» (план docs/plans/P11-e2e-match.md), ~60s
// реального времени:
//
//	host (master) + 3 slave по WS. Все шлют PatchInput ~10 Гц; клиент,
//	увидевший is_master на себе, дополнительно шлёт PatchEntities ~10 Гц
//	(entity id = peer id, Owner = свой peer id, позиция — u64 counter).
//
//	t=15s  host замолкает: read-pump останавливается, TCP-conn остаётся
//	       открытым → клиент не отвечает на ping → lastSeen стареет →
//	       idle sweep эвиктит за ~6s → миграция мастера на slave.
//	t=40s  текущий мастер делает clean leave → миграция №3.
//	t=60s  финал: ≥2 клиента живы, patches текут, peers в registry = 2.
//
// Ассерты едут по наблюдениям wire-кадров клиентов (masterSeq из is_master,
// entity Owner, Inputs), а не по внутренностям сервера.

const (
	clientSendInterval = 50 * time.Millisecond

	inputValueProbe = state.ValueId(1)
	posComponentID  = state.ComponentID(1)
	posValueID      = state.ValueId(1)
	entityTypeProbe = state.EntityTypeID(1)
)

// wsClient — минимальный клиент комнаты на goroutine'ах. Read-pump декодирует
// server→client кадры (0x01/0x02 state, 0x07 rpc, 0x60-0x6F in-room errors) и
// поддерживает локальную копию RoomState; send-loop шлёт PatchInput всегда и
// PatchEntities — пока за клиентом флаг is_master. Все наблюдения — под mu,
// флаги — atomic; t.Logf из pump'ов безопасен (testing.T потокобезопасен).
type wsClient struct {
	t    *testing.T
	nick string
	conn *fwebsocket.Conn

	roomCodec     *codec.RoomStateCodec
	entitiesCodec *codec.EntitiesStateCodec
	inputCodec    *codec.InputStateCodec
	rnd           *rand.Rand // случайный input-пейлоад, seed по нику — воспроизводимо

	peerID    atomic.Uint32
	isMaster  atomic.Bool
	silent    atomic.Bool // host dropout: не читаем и не шлём, TCP жив
	connDead  atomic.Bool // read-pump увидел ошибку/закрытие conn
	leftClean atomic.Bool // запланированный уход/stop — ошибка чтения ожидаема

	quit     chan struct{} // закрытие останавливает send-loop
	quitOnce sync.Once
	pumpDone chan struct{}
	sendDone chan struct{}

	mu           sync.Mutex
	room         *state.RoomState         // локальная реплика (nil до первого full)
	masterSeq    []domain.PeerID          // последовательность держателей is_master
	peerNicks    map[domain.PeerID]string // nick lookup, сохраняется после leave
	inputNicks   map[string]bool          // чей input встречался в state (payload-проверка)
	ownInputSeen bool                     // свой input вернулся через broadcast
	entityOwners map[domain.PeerID]bool   // distinct Owner в entities (мастера-отправители)
	entityPos    map[domain.PeerID]string // Owner → последняя позиция (для лога первого кадра)
	patchCount   int
	lastFrameAt  time.Time
}

func newWSClient(t *testing.T, e *env.Env, nick, token string) *wsClient {
	t.Helper()

	seed := fnv.New64a()
	_, _ = seed.Write([]byte(nick))
	c := &wsClient{
		t:             t,
		nick:          nick,
		roomCodec:     codec.NewRoomStateCodec(),
		rnd:           rand.New(rand.NewPCG(seed.Sum64(), 0x9e3779b97f4a7c15)),
		entitiesCodec: codec.NewEntitiesStateCodec(),
		inputCodec:    codec.NewInputStateCodec(),
		quit:          make(chan struct{}),
		pumpDone:      make(chan struct{}),
		sendDone:      make(chan struct{}),
		peerNicks:     make(map[domain.PeerID]string),
		inputNicks:    make(map[string]bool),
		entityOwners:  make(map[domain.PeerID]bool),
		entityPos:     make(map[domain.PeerID]string),
	}
	c.conn = e.DialWS(t, "token="+token)
	go c.readPump()
	go c.sendLoop()
	go c.statsLoop()
	return c
}

// goSilent имитирует зависший клиент: перестаём читать (ping'и сервера больше
// не получают pong — fasthttp/websocket отвечает на ping только внутри
// ReadMessage) и перестаём слать. TCP-соединение остаётся открытым: сервер
// должен выкинуть peer по idle timeout, а не по TCP-разрыву.
func (c *wsClient) goSilent() {
	c.silent.Store(true)
	c.stopSend()
	// Будим заблокированный ReadMessage — TCP при этом не закрывается.
	_ = c.conn.SetReadDeadline(time.Now())
}

// leaveClean — плановый уход мастера №2: закрываем conn, серверный Receive
// падает → LeaveRoom → вторая миграция.
func (c *wsClient) leaveClean() {
	c.leftClean.Store(true)
	c.stopSend()
	_ = c.conn.Close()
}

// stop — финальный teardown клиента (идемпотентно для silent/leftClean).
func (c *wsClient) stop() {
	c.leftClean.Store(true)
	c.stopSend()
	_ = c.conn.Close()
	<-c.pumpDone
	<-c.sendDone
}

func (c *wsClient) stopSend() {
	c.quitOnce.Do(func() { close(c.quit) })
}

// statsLoop — периодический дайджест наблюдаемого state: видно, что стейт
// реально течёт (позиции меняются, патчи приходят), а не только «первые»
// события. Печатается каждые 5s пока клиент жив.
func (c *wsClient) statsLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.pumpDone:
			return
		case <-ticker.C:
			c.logStats()
		}
	}
}

func (c *wsClient) logStats() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.room == nil {
		c.t.Logf("[%s] stats: no state yet (patches=%d)", c.nick, c.patchCount)
		return
	}
	master := "-"
	positions := make([]string, 0, len(c.entityPos))
	for pid, ps := range c.room.Peers {
		if ps.IsMaster {
			master = c.peerNicks[pid]
		}
	}
	for owner, pos := range c.entityPos {
		positions = append(positions, fmt.Sprintf("%s=%s", c.peerNicks[owner], pos))
	}
	c.t.Logf("[%s] stats: peers=%d master=%s entities={%s} inputsSeen=%d patches=%d me=%d",
		c.nick, len(c.room.Peers), master, strings.Join(positions, ","),
		len(c.inputNicks), c.patchCount, c.peerID.Load())
}

func (c *wsClient) sendDisabled() bool {
	return c.silent.Load() || c.connDead.Load() || c.leftClean.Load()
}

// --- read pump -------------------------------------------------------------

func (c *wsClient) readPump() {
	defer close(c.pumpDone)
	for {
		_, msg, err := c.conn.ReadMessage()
		if err != nil {
			c.connDead.Store(true)
			switch {
			case c.silent.Load():
				c.t.Logf("[%s] read stopped — silent mode, TCP stays open", c.nick)
			case c.leftClean.Load():
				c.t.Logf("[%s] connection closed (planned)", c.nick)
			default:
				c.t.Logf("[%s] disconnected: %v", c.nick, err)
			}
			return
		}
		if len(msg) == 0 {
			continue
		}
		c.handleFrame(domain.OpCode(msg[0]), msg[1:])
	}
}

func (c *wsClient) handleFrame(op domain.OpCode, payload []byte) {
	switch op {
	case state.OpCodeFullState:
		full, err := c.roomCodec.Decode(payload)
		if err != nil {
			c.t.Logf("[%s] full state decode failed: %v", c.nick, err)
			return
		}
		c.applyFull(full)
	case state.OpCodePatchState:
		patch, err := c.roomCodec.DecodePatch(payload)
		if err != nil {
			c.t.Logf("[%s] patch state decode failed: %v", c.nick, err)
			return
		}
		c.applyPatch(patch)
	case state.OpCodeRpc:
		c.t.Logf("[%s] rpc frame, %d bytes", c.nick, len(payload))
	default:
		if op >= 0x60 && op <= 0x6F {
			// In-room ошибки (напр. OpInRoomNotMaster) — лента событий, не фейл.
			c.t.Logf("[%s] in-room error frame: op=0x%02x", c.nick, byte(op))
			return
		}
		c.t.Logf("[%s] unknown frame op=0x%02x, %d bytes", c.nick, byte(op), len(payload))
	}
}

func (c *wsClient) applyFull(full *state.RoomState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.room = full
	c.observeLocked()
}

func (c *wsClient) applyPatch(patch *state.RoomStatePatch) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.room == nil {
		c.t.Logf("[%s] patch before first full state — skipped", c.nick)
		return
	}
	if err := c.room.ApplyPatch(*patch); err != nil {
		// full-state resync каждые 4s вылечит локальную реплику.
		c.t.Logf("[%s] apply patch failed: %v", c.nick, err)
		return
	}
	c.patchCount++
	c.observeLocked()
}

// observeLocked — вся state-driven логика клиента: свой peer id по nick,
// is_master → send-loop начнёт слать entities, masterSeq, inputs, entities.
func (c *wsClient) observeLocked() {
	rs := c.room
	if rs == nil {
		return
	}
	c.lastFrameAt = time.Now()

	for pid, ps := range rs.Peers {
		c.peerNicks[pid] = ps.NickName
	}

	selfID := domain.PeerID(c.peerID.Load())
	if !selfID.IsValid() {
		for pid, ps := range rs.Peers {
			if ps.NickName == c.nick {
				c.peerID.Store(uint32(pid))
				selfID = pid
				c.t.Logf("[%s] joined as peer %d (room %d, capacity %d)", c.nick, pid, rs.ID, rs.Capacity)
			}
		}
	}

	for pid, ps := range rs.Peers {
		if !ps.IsMaster {
			continue
		}
		if len(c.masterSeq) == 0 || c.masterSeq[len(c.masterSeq)-1] != pid {
			c.masterSeq = append(c.masterSeq, pid)
			c.t.Logf("[%s] master is now %s (peer %d)", c.nick, c.peerNicks[pid], pid)
		}
	}

	if selfID.IsValid() {
		if ps, ok := rs.Peers[selfID]; ok && ps.IsMaster != c.isMaster.Load() {
			c.isMaster.Store(ps.IsMaster)
			if ps.IsMaster {
				c.t.Logf("[%s] became master — start entities stream", c.nick)
			} else {
				c.t.Logf("[%s] lost master — stop entities stream", c.nick)
			}
		}
		if in, ok := rs.Inputs[selfID]; ok && !c.ownInputSeen {
			if v, ok := in.Values[inputValueProbe]; ok && strings.HasPrefix(string(v), c.nick+":") {
				c.ownInputSeen = true
				c.t.Logf("[%s] my input observed in room state", c.nick)
			}
		}
	}

	for pid, in := range rs.Inputs {
		v, ok := in.Values[inputValueProbe]
		if !ok {
			continue
		}
		nick := c.peerNicks[pid]
		// payload проверяем по nick-префиксу — input точно этого клиента.
		if nick != "" && strings.HasPrefix(string(v), nick+":") && !c.inputNicks[nick] {
			c.inputNicks[nick] = true
			c.t.Logf("[%s] observed input of %s (peer %d)", c.nick, nick, pid)
		}
	}

	for eid, ent := range rs.Entities {
		pos := entityPosKey(ent)
		prev, seen := c.entityPos[ent.Owner]
		if !seen {
			c.entityOwners[ent.Owner] = true
			c.entityPos[ent.Owner] = pos
			c.t.Logf("[%s] first entity snapshot id=%d from owner %s (peer %d), pos %s",
				c.nick, eid, c.peerNicks[ent.Owner], ent.Owner, pos)
		} else if prev != pos {
			c.entityPos[ent.Owner] = pos
		}
	}
}

func entityPosKey(ent state.EntityState) string {
	comp, ok := ent.Components[posComponentID]
	if !ok {
		return ""
	}
	v, ok := comp.Values[posValueID]
	if !ok {
		return ""
	}
	return fmt.Sprintf("%x", []byte(v))
}

// --- send loop -------------------------------------------------------------

func (c *wsClient) sendLoop() {
	defer close(c.sendDone)
	ticker := time.NewTicker(clientSendInterval)
	defer ticker.Stop()

	prevInput := state.InputState{Values: state.NewMapState[state.ValueId, state.ValueState]()}
	prevEntities := state.NewMapState[state.EntityID, state.EntityState]()
	var tick uint64

	for {
		select {
		case <-c.quit:
			return
		case <-ticker.C:
			if c.sendDisabled() {
				continue
			}
			tick++
			c.sendInput(tick, &prevInput)
			// State-driven: entities только пока сервер держит нас master'ом.
			if c.isMaster.Load() {
				c.sendEntities(tick, &prevEntities)
			}
		}
	}
}

// sendInput — PatchInput (0x06) со случайным значением "nick:<rand>" в
// inputValueProbe (20 Гц — state реально меняется каждый патч).
// Patch строится MakePatch от прошлого состояния → первый кадр выходит как
// Added, дальше Updated — серверный apply требует ровно это различие.
func (c *wsClient) sendInput(tick uint64, prev *state.InputState) {
	next := state.InputState{Values: state.MapState[state.ValueId, state.ValueState]{
		inputValueProbe: state.ValueState(fmt.Sprintf("%s:%x:%d", c.nick, c.rnd.Uint64(), tick)),
	}}
	patch, err := prev.MakePatch(&next)
	*prev = next
	if err != nil {
		c.t.Logf("[%s] input make patch: %v", c.nick, err)
		return
	}
	if patch == nil {
		return
	}
	payload, err := c.inputCodec.EncodePatch(patch)
	if err != nil {
		c.t.Logf("[%s] input encode: %v", c.nick, err)
		return
	}
	if err := c.writeFrame(state.OpCodePatchInput, payload); err != nil {
		c.connDead.Store(true)
		c.t.Logf("[%s] input write failed: %v", c.nick, err)
	}
}

// sendEntities — PatchEntities (0x04), entity id = свой peer id, Owner = свой
// peer id → по Owner на wire видно, какой мастер сейчас стримит позиции.
func (c *wsClient) sendEntities(tick uint64, prev *state.EntitiesState) {
	selfID := domain.PeerID(c.peerID.Load())
	if !selfID.IsValid() {
		return
	}
	pos := make([]byte, 8)
	binary.BigEndian.PutUint64(pos, tick)
	next := state.EntitiesState{
		state.EntityID(selfID): state.EntityState{
			EntityTypeID: entityTypeProbe,
			Owner:        selfID,
			Components: state.MapState[state.ComponentID, state.ComponentState]{
				posComponentID: {Values: state.MapState[state.ValueId, state.ValueState]{posValueID: pos}},
			},
		},
	}
	patch, err := state.MakeMapStatePatch(*prev, next,
		func(a, b state.EntityState) bool {
			return a.Equals(&b, func(c1, c2 state.ComponentState) bool {
				return c1.Equals(&c2, func(v1, v2 state.ValueState) bool { return v1.Equals(&v2) })
			})
		},
		func(a, b state.EntityState) (*state.EntityStatePatch, error) {
			return a.MakePatch(&b)
		})
	*prev = next
	if err != nil {
		c.t.Logf("[%s] entities make patch: %v", c.nick, err)
		return
	}
	if patch == nil {
		return
	}
	payload, err := c.entitiesCodec.EncodePatch(patch)
	if err != nil {
		c.t.Logf("[%s] entities encode: %v", c.nick, err)
		return
	}
	if err := c.writeFrame(state.OpCodePatchEntities, payload); err != nil {
		c.connDead.Store(true)
		c.t.Logf("[%s] entities write failed: %v", c.nick, err)
	}
}

func (c *wsClient) writeFrame(op domain.OpCode, payload []byte) error {
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.conn.WriteMessage(fwebsocket.BinaryMessage, append([]byte{byte(op)}, payload...))
}

// --- наблюдения для ассертов (mu) ------------------------------------------

func (c *wsClient) mastersSeen() []domain.PeerID {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]domain.PeerID(nil), c.masterSeq...)
}

func (c *wsClient) entityOwnersSeen() map[domain.PeerID]bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[domain.PeerID]bool, len(c.entityOwners))
	for k, v := range c.entityOwners {
		out[k] = v
	}
	return out
}

func (c *wsClient) inputNicksSeen() map[string]bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]bool, len(c.inputNicks))
	for k, v := range c.inputNicks {
		out[k] = v
	}
	return out
}

func (c *wsClient) ownInputObserved() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ownInputSeen
}

func (c *wsClient) lastFrame() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastFrameAt
}

// --- scenario helpers -------------------------------------------------------

func waitFor(t *testing.T, timeout time.Duration, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout %s waiting for: %s", timeout, desc)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Logf("[scenario] %s — ok", desc)
}

func sleepUntil(deadline time.Time) {
	if d := time.Until(deadline); d > 0 {
		time.Sleep(d)
	}
}

// currentMaster возвращает клиента, у которого сейчас is_master.
func currentMaster(t *testing.T, clients []*wsClient) *wsClient {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		for _, c := range clients {
			if c.isMaster.Load() && !c.sendDisabled() {
				return c
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("no live master client found")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestE2E_LiveMatch_HostMigration — см. шапку файла и docs/plans/P11-e2e-match.md.
func TestE2E_LiveMatch_HostMigration(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e live match ~60s real time — skipped under -short")
	}
	t.Parallel()

	e := env.New(t,
		env.WithTransport(adaptertransport.TransportConfig{
			RoomIncomingQueueSize: adaptertransport.DefaultRoomIncomingQueueSize,
			PeerOutboundQueueSize: adaptertransport.DefaultPeerOutboundQueueSize,
			EnetIncomingQueueSize: adaptertransport.DefaultEnetIncomingQueueSize,
			MaxIncomingFrameBytes: adaptertransport.DefaultMaxIncomingFrameBytes,
			WSWriteDeadline:       adaptertransport.DefaultWSWriteDeadline,
			// PeerIdleTimeout > wsPingInterval (2s): живые клиенты держатся
			// за pong → lastSeen; замолчавший отваливается за timeout+sweep.
			PeerIdleTimeout:       5 * time.Second,
			PeerIdleSweepInterval: time.Second,
		}),
		env.WithIdleSweep(time.Second),
	)

	start := time.Now()
	roomID, hostToken := e.CreateRoom(t, "host")
	host := newWSClient(t, e, "host", hostToken)
	s1 := newWSClient(t, e, "s1", e.IssueTicket(t, roomID, "s1"))
	s2 := newWSClient(t, e, "s2", e.IssueTicket(t, roomID, "s2"))
	s3 := newWSClient(t, e, "s3", e.IssueTicket(t, roomID, "s3"))
	clients := []*wsClient{host, s1, s2, s3}
	slaves := []*wsClient{s1, s2, s3}
	defer func() {
		for _, c := range clients {
			c.stop()
		}
	}()

	e.WaitPeerCount(t, roomID, 4)

	waitFor(t, 10*time.Second, "all clients discovered their peer id", func() bool {
		for _, c := range clients {
			if !domain.PeerID(c.peerID.Load()).IsValid() {
				return false
			}
		}
		return true
	})
	hostPeerID := domain.PeerID(host.peerID.Load())

	// Фаза 1: master=host стримит позиции, input всех текет в state.
	waitFor(t, 12*time.Second, "each slave observed entity positions from host", func() bool {
		for _, s := range slaves {
			if !s.entityOwnersSeen()[hostPeerID] {
				return false
			}
		}
		return true
	})
	waitFor(t, 10*time.Second, "each client sees own input in room state", func() bool {
		for _, c := range clients {
			if !c.ownInputObserved() {
				return false
			}
		}
		return true
	})

	// t=15s — host dropout: conn открыт, читать/слать перестаём.
	sleepUntil(start.Add(15 * time.Second))
	t.Logf("[scenario] host goes silent (TCP open, no reads, no sends)")
	host.goSilent()

	// Миграция №1: sweep эвиктит host (5s idle + 1s sweep) → is_master уезжает.
	waitFor(t, 15*time.Second, "master migrated off silent host", func() bool {
		for _, s := range slaves {
			if seq := s.mastersSeen(); len(seq) >= 2 && seq[len(seq)-1] != hostPeerID {
				return true
			}
		}
		return false
	})
	e.WaitPeerCount(t, roomID, 3)

	// Новый мастер state-driven: увидел is_master → начал слать свои entities.
	waitFor(t, 15*time.Second, "slave observed entity positions from second master", func() bool {
		for _, s := range slaves {
			for owner := range s.entityOwnersSeen() {
				if owner != hostPeerID {
					return true
				}
			}
		}
		return false
	})

	// t=40s — текущий мастер уходит чисто → миграция №2.
	sleepUntil(start.Add(40 * time.Second))
	m2 := currentMaster(t, slaves)
	t.Logf("[scenario] master %s (peer %d) leaves cleanly", m2.nick, m2.peerID.Load())
	m2.leaveClean()

	waitFor(t, 15*time.Second, "second master migration observed", func() bool {
		for _, s := range slaves {
			if s == m2 {
				continue
			}
			seq := s.mastersSeen()
			if len(seq) >= 3 {
				last := seq[len(seq)-1]
				if last != hostPeerID && last != domain.PeerID(m2.peerID.Load()) {
					return true
				}
			}
		}
		return false
	})
	e.WaitPeerCount(t, roomID, 2)

	// Фаза 3: жизнь под мастером №3 до конца минуты.
	sleepUntil(start.Add(60 * time.Second))

	var survivors []*wsClient
	for _, s := range slaves {
		if s != m2 {
			survivors = append(survivors, s)
		}
	}
	if len(survivors) != 2 {
		t.Fatalf("survivors = %d, want 2", len(survivors))
	}

	// (a) slaves видели позиции от host И от второго distinct master'а.
	for _, s := range survivors {
		owners := s.entityOwnersSeen()
		if !owners[hostPeerID] {
			t.Fatalf("[%s] entity positions from host not observed (owners %v)", s.nick, owners)
		}
		second := false
		for owner := range owners {
			if owner != hostPeerID {
				second = true
			}
		}
		if !second {
			t.Fatalf("[%s] no entity positions from a second master (owners %v)", s.nick, owners)
		}
	}

	// (b) input каждого slave доехал до чужого state (payload проверен по nick).
	for _, s := range slaves {
		seen := false
		for _, other := range clients {
			if other == s {
				continue
			}
			if other.inputNicksSeen()[s.nick] {
				seen = true
				break
			}
		}
		if !seen {
			t.Fatalf("input of %s not observed in any other client's room state", s.nick)
		}
	}

	// (c) две миграции через is_master: host → m2 → m3 на каждом выжившем.
	for _, s := range survivors {
		seq := s.mastersSeen()
		if len(seq) < 3 {
			t.Fatalf("[%s] master sequence %v: want >=3 holders (2 migrations)", s.nick, seq)
		}
		if seq[0] != hostPeerID {
			t.Fatalf("[%s] master sequence %v: first holder must be host peer %d", s.nick, seq, hostPeerID)
		}
	}

	// (d) финал: ≥2 клиента живы и кадры текут (patch-тик 50ms + input-трафик).
	live := 0
	for _, c := range clients {
		if !c.connDead.Load() {
			live++
		}
	}
	if live < 2 {
		t.Fatalf("live clients = %d, want >=2", live)
	}
	for _, s := range survivors {
		if c := s.connDead.Load(); c {
			t.Fatalf("[%s] survivor connection is dead", s.nick)
		}
		if since := time.Since(s.lastFrame()); since > 4*time.Second {
			t.Fatalf("[%s] no state frames for %s — stream stalled", s.nick, since)
		}
	}

	// (e) peers в registry сошлись с тем, что видят клиенты.
	e.WaitPeerCount(t, roomID, 2)
}
