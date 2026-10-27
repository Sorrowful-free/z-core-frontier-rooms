package loadgen

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/codec"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

const (
	peerDialTimeout = 10 * time.Second
	resumeBackoff   = 300 * time.Millisecond
)

// Probe-id'ы payload'ов: один input-value и один position-component на клиента.
const (
	inputValueProbe = state.ValueId(1)
	posComponentID  = state.ComponentID(1)
	posValueID      = state.ValueId(1)
	entityTypeProbe = state.EntityTypeID(1)
)

// peer — сценарный движок одного клиента комнаты (P27). Жизненный цикл:
//
//	dial (join-ticket) → read-loop (ОБЯЗАТЕЛЕН: fasthttp/websocket отвечает
//	на ping только внутри ReadMessage — мёртвый reader = idle-eviction через
//	TRANSPORT_PEER_IDLE_TIMEOUT) + send-loop по LOADGEN_MODE →
//	разрыв → resume reconnect-токеном (если клиент в RECONNECT_RATE) → …
//	→ closeReq (drain/auth-kill) — без resume.
//
// Join = первый кадр 0x01 FullState; до него однобайтовый OpCode 0x40–0x7F —
// join/resume reject, дальше сервер закрывает conn.
type peer struct {
	nick           string
	roomID         int64
	transport      string
	joinToken      string
	reconnectToken string
	enetAddr       string // host для будущего ENet-dial (inc 3)
	enetPort       uint16 // назначенный enet_port из ticket (0 — нет)
	host           bool   // host-ticket: в auth-комнате его разрыв = смерть комнаты
	roomPolicy     string // domain.RoomPolicyKind комнаты (fleet ставит при спавне)

	cfg *Config
	m   *Metrics
	rnd *rand.Rand

	roomCodec     *codec.RoomStateCodec
	entitiesCodec *codec.EntitiesStateCodec
	inputCodec    *codec.InputStateCodec

	connMu sync.Mutex
	conn   DataConn

	peerID           atomic.Uint32
	isMaster         atomic.Bool
	joined           atomic.Bool // был admit хотя бы раз (0x01 в первой сессии)
	reconnectEnabled atomic.Bool // выбран в RECONNECT_RATE — resume после разрыва
	closeReq         atomic.Bool // финальный разрыв без resume (drain / auth-kill)
	dropReq          atomic.Bool // плановый разрыв reconnect-цикла (не «unexpected»)

	mu            sync.Mutex
	room          *state.RoomState // локальная реплика — для peerID/isMaster
	sawRoomClosed bool             // 0x82 в текущей сессии → close conn ожидаем
	joinRejected  bool             // reject-кадр в текущей сессии

	done chan struct{} // закрыт по выходу из run
}

func newPeer(cfg *Config, m *Metrics, spec peerSpec) *peer {
	seed := fnv.New64a()
	_, _ = seed.Write([]byte(spec.nick))
	rnd := rand.New(rand.NewPCG(seed.Sum64(), 0x9e3779b97f4a7c15))
	p := &peer{
		nick:           spec.nick,
		roomID:         spec.roomID,
		transport:      spec.transport,
		joinToken:      spec.joinToken,
		reconnectToken: spec.reconnectToken,
		enetAddr:       spec.enetAddr,
		enetPort:       spec.enetPort,
		host:           spec.host,
		cfg:            cfg,
		m:              m,
		rnd:            rnd,
		roomCodec:      codec.NewRoomStateCodec(),
		entitiesCodec:  codec.NewEntitiesStateCodec(),
		inputCodec:     codec.NewInputStateCodec(),
		done:           make(chan struct{}),
	}
	p.reconnectEnabled.Store(rnd.Float64() < cfg.ReconnectRate)
	return p
}

// peerSpec — параметры спавна клиента (fleet собирает из ticket-ответов).
type peerSpec struct {
	nick           string
	roomID         int64
	transport      string
	joinToken      string
	reconnectToken string
	enetAddr       string
	enetPort       uint16
	host           bool
}

// run — внешний цикл сессий: dial → session → (resume | exit).
func (p *peer) run(ctx context.Context) {
	defer close(p.done)
	go p.controlLoop(ctx)

	token := p.joinToken
	resumed := false
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		sessionStart := time.Now()
		conn, err := p.dial(ctx, token, resumed)
		if err != nil {
			return
		}
		p.session(ctx, conn, resumed, sessionStart)
		_ = conn.Close()

		p.mu.Lock()
		rejected := p.joinRejected
		p.mu.Unlock()
		// Reject = слот/комната мертвы — повторные resume бессмысленны и
		// лишь штормят admit (0x43-шторм после смерти auth-комнаты).
		if rejected || p.closeReq.Load() || ctx.Err() != nil || !p.reconnectEnabled.Load() {
			return
		}
		if p.reconnectToken == "" {
			return
		}
		// Backoff перед resume — не долбить admit в узком окне после разрыва.
		select {
		case <-ctx.Done():
			return
		case <-time.After(resumeBackoff):
		}
		token, resumed = p.reconnectToken, true
	}
}

// dial — dialDataConn + метрики (join attempt/latency только первой сессии;
// ошибка резюма — тот же dial_errors{transport}).
func (p *peer) dial(ctx context.Context, token string, resumed bool) (DataConn, error) {
	if !resumed {
		p.m.JoinAttempts.Add(1)
	}
	dctx, cancel := context.WithTimeout(ctx, peerDialTimeout)
	defer cancel()
	start := time.Now()
	conn, err := dialDataConn(dctx, p.transport, DialParams{
		WSAddr:   p.cfg.WSAddr,
		EnetAddr: p.enetAddr,
		EnetPort: p.enetPort,
		Token:    token,
	})
	if err != nil {
		p.m.dialError(p.transport)
		return nil, err
	}
	p.m.recordDial(time.Since(start))
	return conn, nil
}

// session — одна жизнь соединения: read-loop (блокирует) + send-loop.
// Выход — ошибка Recv (разрыв/close) или ctx.
func (p *peer) session(ctx context.Context, conn DataConn, resumed bool, sessionStart time.Time) {
	p.connMu.Lock()
	p.conn = conn
	p.connMu.Unlock()
	// Гонка «closeReq/dropReq пришёл во время dial»: закрытие по флагу нашло
	// conn=nil и было no-op — дожимаем здесь, иначе свежее соединение
	// переживёт drain и оставит peer/слот на сервере.
	if p.closeReq.Load() || p.dropReq.Load() || ctx.Err() != nil {
		_ = conn.Close()
	}
	p.mu.Lock()
	p.sawRoomClosed = false
	p.joinRejected = false
	p.mu.Unlock()
	// Сервер чистит Inputs/Entities peer'а на disconnect (OnLeave удаляет
	// записи) — патчи новой сессии строим от пустого prev, иначе Added/Updated
	// разъедутся с серверным state.
	prevInput := state.InputState{Values: state.NewMapState[state.ValueId, state.ValueState]()}
	prevEntities := state.NewMapState[state.EntityID, state.EntityState]()

	sendQuit := make(chan struct{})
	sendDone := make(chan struct{})
	go p.sendLoop(ctx, conn, &prevInput, &prevEntities, sendQuit, sendDone)
	defer func() {
		close(sendQuit)
		<-sendDone
		p.connMu.Lock()
		if p.conn == conn {
			p.conn = nil
		}
		p.connMu.Unlock()
	}()

	gotFull := false // 0x01 в этой сессии (join/resume reject — кадры до него)
	for {
		op, payload, err := conn.Recv()
		if err != nil {
			p.onRecvErr(ctx, gotFull)
			return
		}
		p.m.FramesRecv.Add(1)
		p.m.BytesRecv.Add(int64(len(payload) + 1))
		if !gotFull && isRejectOp(op) {
			p.mu.Lock()
			p.joinRejected = true
			p.mu.Unlock()
			p.m.joinReject(byte(op))
			if resumed {
				p.m.ResumeRejects.Add(1)
			}
			continue // сервер закроет conn — выход по ошибке Recv
		}
		if !gotFull && resumed {
			// Первый не-reject кадр resume-сессии — admit прошёл.
			p.m.ReconnectSuccess.Add(1)
		}
		gotFull = p.handleFrame(op, payload, sessionStart) || gotFull
	}
}

// isRejectOp — join/admit/reservation reject-кадры (0x40–0x7F, payload пуст):
// приходят единственным кадром до admit. 0x60–0x6F пересекаются по диапазону,
// но до admit недостижимы — различие по «first frame».
func isRejectOp(op domain.OpCode) bool {
	return op >= domain.OpEmptyToken && op <= domain.OpReservationInternal
}

// handleFrame — декод/учёт входящего кадра. Возвращает true, если это был
// 0x01 FullState (граница «admit прошёл»).
func (p *peer) handleFrame(op domain.OpCode, payload []byte, sessionStart time.Time) bool {
	switch op {
	case state.OpCodeFullState:
		p.m.FullStateRecv.Add(1)
		full, err := p.roomCodec.Decode(payload)
		if err != nil {
			p.m.decodeError()
			return true
		}
		p.applyFull(full)
		if !p.joined.Swap(true) {
			p.m.JoinSuccess.Add(1)
			p.m.recordJoin(time.Since(sessionStart))
		}
		return true
	case state.OpCodePatchState:
		p.m.PatchRecv.Add(1)
		patch, err := p.roomCodec.DecodePatch(payload)
		if err != nil {
			p.m.decodeError()
			return false
		}
		p.applyPatch(patch)
	case state.OpCodeRpc:
		p.m.RpcRecv.Add(1)
	case domain.OpPeerDisconnected:
		p.m.PeerDisconnectedRecv.Add(1)
	case domain.OpPeerReconnected:
		p.m.PeerReconnectedRecv.Add(1)
	case domain.OpRoomClosed:
		// authoritative-host: мастер ушёл → комната умирает, сервер закроет
		// conn следом — это ожидаемый teardown, не unexpected disconnect.
		p.m.RoomClosedRecv.Add(1)
		p.mu.Lock()
		p.sawRoomClosed = true
		p.mu.Unlock()
	default:
		p.m.inRoomError(byte(op))
	}
	return false
}

// onRecvErr — классификация конца сессии.
func (p *peer) onRecvErr(ctx context.Context, gotFull bool) {
	p.mu.Lock()
	rejected, roomClosed := p.joinRejected, p.sawRoomClosed
	p.mu.Unlock()
	switch {
	case ctx.Err() != nil, p.closeReq.Load(), p.dropReq.Swap(false), rejected, roomClosed:
		// Ожидаемые разрывы: shutdown, drain/auth-kill, плановый reconnect-drop,
		// close после reject-кадра, close после 0x82 (комната мертва).
	default:
		if gotFull {
			p.m.UnexpectedDisconnects.Add(1)
		} else {
			p.m.recvError()
		}
	}
}

// controlLoop — фоновые события клиента: плановые разрывы reconnect-стресса и
// принудительный close по ctx (session разбудится ошибкой Recv).
func (p *peer) controlLoop(ctx context.Context) {
	if p.reconnectEnabled.Load() {
		// Первый разрыв — в случайной точке окна, чтобы не стрелять волной;
		// дальше — период RECONNECT_EVERY.
		t := time.NewTimer(time.Duration(p.rnd.Float64() * float64(p.cfg.ReconnectEvery)))
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				p.closeConn()
				return
			case <-p.done:
				return
			case <-t.C:
				if p.joined.Load() {
					// conn может быть nil (mid-dial/между сессиями): dropReq
					// дожимается проверкой в начале session.
					p.m.ReconnectAttempts.Add(1)
					p.dropReq.Store(true)
					p.closeConn()
				}
				t.Reset(p.cfg.ReconnectEvery)
			}
		}
	}
	select {
	case <-ctx.Done():
		p.closeConn()
	case <-p.done:
	}
}

func (p *peer) closeConn() {
	p.connMu.Lock()
	defer p.connMu.Unlock()
	if p.conn != nil {
		_ = p.conn.Close()
	}
}

// closeForGood — abrupt-разрыв без resume: drain-фаза флота и убийство
// host-клиента auth-комнаты.
func (p *peer) closeForGood() {
	p.closeReq.Store(true)
	p.closeConn()
}

// --- replica state: свой peerID и isMaster ---------------------------------

func (p *peer) applyFull(full *state.RoomState) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.room = full
	p.observeLocked()
}

func (p *peer) applyPatch(patch *state.RoomStatePatch) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.room == nil {
		return // patch до первого full — full-state resync вылечит реплику
	}
	if err := p.room.ApplyPatch(*patch); err != nil {
		p.m.decodeError()
		return
	}
	p.observeLocked()
}

// observeLocked — peerID по nick'у и флаг isMaster для send-loop.
func (p *peer) observeLocked() {
	rs := p.room
	if rs == nil {
		return
	}
	selfID := domain.PeerID(p.peerID.Load())
	if !selfID.IsValid() {
		for pid, ps := range rs.Peers {
			if ps.NickName == p.nick {
				p.peerID.Store(uint32(pid))
				selfID = pid
				break
			}
		}
	}
	if selfID.IsValid() {
		if ps, ok := rs.Peers[selfID]; ok {
			p.isMaster.Store(ps.IsMaster)
		}
	}
}

// --- send loop --------------------------------------------------------------

// sendLoop — PatchInput всем клиентам (INPUT_HZ), PatchEntities — пока сервер
// держит нас master'ом (ENTITIES_HZ). mode=occupancy — ничего не шлём.
// peerID валиден только после admit + первого full state — до этого кадры
// бессмысленны (peer ещё не в комнате).
func (p *peer) sendLoop(ctx context.Context, conn DataConn, prevInput *state.InputState,
	prevEntities *state.EntitiesState, quit, quitDone chan struct{}) {
	defer close(quitDone)
	if p.cfg.Mode == ModeOccupancy {
		<-quit
		return
	}
	inputTick := time.NewTicker(time.Duration(float64(time.Second) / p.cfg.InputHz))
	entitiesTick := time.NewTicker(time.Duration(float64(time.Second) / p.cfg.EntitiesHz))
	defer inputTick.Stop()
	defer entitiesTick.Stop()

	var tick uint64
	for {
		select {
		case <-quit:
			return
		case <-ctx.Done():
			return
		case <-inputTick.C:
			if !domain.PeerID(p.peerID.Load()).IsValid() {
				continue
			}
			tick++
			p.sendInput(conn, tick, prevInput)
		case <-entitiesTick.C:
			if !p.isMaster.Load() || !domain.PeerID(p.peerID.Load()).IsValid() {
				continue
			}
			p.sendEntities(conn, tick, prevEntities)
		}
	}
}

// sendInput — PatchInput (0x06) со значением "nick:<rand>:<tick>"; MakePatch
// от prev → Added на первом кадре сессии, дальше Updated.
func (p *peer) sendInput(conn DataConn, tick uint64, prev *state.InputState) {
	next := state.InputState{Values: state.MapState[state.ValueId, state.ValueState]{
		inputValueProbe: state.ValueState(fmt.Sprintf("%s:%x:%d", p.nick, p.rnd.Uint64(), tick)),
	}}
	patch, err := prev.MakePatch(&next)
	*prev = next
	if err != nil || patch == nil {
		return
	}
	payload, err := p.inputCodec.EncodePatch(patch)
	if err != nil {
		return
	}
	if err := p.write(conn, state.OpCodePatchInput, payload); err != nil {
		p.m.sendError()
	}
}

// sendEntities — PatchEntities (0x04): entity id = свой peer id, Owner = свой
// peer id, позиция — u64 counter (как в match_e2e драйвере).
func (p *peer) sendEntities(conn DataConn, tick uint64, prev *state.EntitiesState) {
	selfID := domain.PeerID(p.peerID.Load())
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
	if err != nil || patch == nil {
		return
	}
	payload, err := p.entitiesCodec.EncodePatch(patch)
	if err != nil {
		return
	}
	if err := p.write(conn, state.OpCodePatchEntities, payload); err != nil {
		p.m.sendError()
	}
}

func (p *peer) write(conn DataConn, op domain.OpCode, payload []byte) error {
	if err := conn.Send(op, payload); err != nil {
		return err
	}
	p.m.FramesSent.Add(1)
	return nil
}
