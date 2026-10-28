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

// Payload probe IDs: one input value and one position component per client.
const (
	inputValueProbe = state.ValueId(1)
	posComponentID  = state.ComponentID(1)
	posValueID      = state.ValueId(1)
	entityTypeProbe = state.EntityTypeID(1)
)

// peer — scenario engine of a single room client (P27). Lifecycle:
//
//	dial (join ticket) → read loop (MANDATORY: fasthttp/websocket only
//	answers pings inside ReadMessage — a dead reader means idle eviction
//	via TRANSPORT_PEER_IDLE_TIMEOUT) + send loop per LOADGEN_MODE →
//	disconnect → resume with the reconnect token (if the client is in
//	RECONNECT_RATE) → … → closeReq (drain) — no resume.
//
// Join = the first 0x01 FullState frame; before it a one-byte OpCode
// 0x40–0x7F is a join/resume reject, after which the server closes the
// conn.
type peer struct {
	nick           string
	roomID         int64
	transport      string
	joinToken      string
	reconnectToken string
	enetAddr       string // host for the future ENet dial (inc 3)
	enetPort       uint16 // assigned enet_port from the ticket (0 — none)
	netemMark      uint32 // SO_MARK of the profile from NETEM_SHARE (0 — no marking)

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
	joined           atomic.Bool // admitted at least once (0x01 in the first session)
	reconnectEnabled atomic.Bool // picked by RECONNECT_RATE — resume after a drop
	closeReq         atomic.Bool // final drop without resume (drain / auth-kill)
	dropReq          atomic.Bool // planned drop of the reconnect cycle (not "unexpected")

	mu            sync.Mutex
	room          *state.RoomState // local replica — for peerID/isMaster
	sawRoomClosed bool             // 0x82 in the current session → conn close expected
	joinRejected  bool             // reject frame in the current session

	done chan struct{} // closed on exit from run
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
		netemMark:      spec.netemMark,
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

// peerSpec — client spawn parameters (fleet builds them from ticket responses).
type peerSpec struct {
	nick           string
	roomID         int64
	transport      string
	joinToken      string
	reconnectToken string
	enetAddr       string
	enetPort       uint16
	netemMark      uint32
}

// run — outer session loop: dial → session → (resume | exit).
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
		// Reject = the slot/room is dead — further resumes are pointless and
		// only storm admit (a 0x43 storm after the room's death).
		if rejected || p.closeReq.Load() || ctx.Err() != nil || !p.reconnectEnabled.Load() {
			return
		}
		if p.reconnectToken == "" {
			return
		}
		// Backoff before resume — don't hammer admit in the narrow window
		// after a drop.
		select {
		case <-ctx.Done():
			return
		case <-time.After(resumeBackoff):
		}
		token, resumed = p.reconnectToken, true
	}
}

// dial — dialDataConn + metrics (join attempt/latency only for the first
// session; a resume error is the same dial_errors{transport}).
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
		Mark:     p.netemMark,
	})
	if err != nil {
		p.m.dialError(p.transport)
		return nil, err
	}
	p.m.recordDial(time.Since(start))
	return conn, nil
}

// session — one lifetime of a connection: read loop (blocking) + send loop.
// Exit — a Recv error (drop/close) or ctx.
func (p *peer) session(ctx context.Context, conn DataConn, resumed bool, sessionStart time.Time) {
	p.connMu.Lock()
	p.conn = conn
	p.connMu.Unlock()
	// The "closeReq/dropReq arrived during dial" race: flag-driven close saw
	// conn=nil and was a no-op — we follow through here, otherwise the fresh
	// connection survives the drain and leaves a peer/slot on the server.
	if p.closeReq.Load() || p.dropReq.Load() || ctx.Err() != nil {
		_ = conn.Close()
	}
	p.mu.Lock()
	p.sawRoomClosed = false
	p.joinRejected = false
	p.mu.Unlock()
	// The server cleans the peer's Inputs/Entities on disconnect (OnLeave
	// removes the records) — the new session's patches are built from an
	// empty prev, otherwise Added/Updated diverge from the server state.
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

	gotFull := false // 0x01 in this session (join/resume rejects are frames before it)
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
			continue // the server will close the conn — exit on a Recv error
		}
		if !gotFull && resumed {
			// The first non-reject frame of a resume session — admit passed.
			p.m.ReconnectSuccess.Add(1)
		}
		gotFull = p.handleFrame(op, payload, sessionStart) || gotFull
	}
}

// isRejectOp — join/admit/reservation reject frames (0x40–0x7F, empty
// payload): they arrive as the only frame before admit. 0x60–0x6F overlap
// the range but are unreachable before admit — the distinction is by
// "first frame".
func isRejectOp(op domain.OpCode) bool {
	return op >= domain.OpEmptyToken && op <= domain.OpReservationInternal
}

// handleFrame — decode/accounting of an incoming frame. Returns true if it
// was a 0x01 FullState (the "admit passed" boundary).
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
		// The server killed the room (P31/P33): room close, DELETE,
		// no-migration deploy — the server closes the conn next; this is an
		// expected teardown, not an unexpected disconnect.
		p.m.RoomClosedRecv.Add(1)
		p.mu.Lock()
		p.sawRoomClosed = true
		p.mu.Unlock()
	default:
		p.m.inRoomError(byte(op))
	}
	return false
}

// onRecvErr — classification of the end of a session.
func (p *peer) onRecvErr(ctx context.Context, gotFull bool) {
	p.mu.Lock()
	rejected, roomClosed := p.joinRejected, p.sawRoomClosed
	p.mu.Unlock()
	switch {
	case ctx.Err() != nil, p.closeReq.Load(), p.dropReq.Swap(false), rejected, roomClosed:
		// Expected drops: shutdown, drain, a planned reconnect drop, a close
		// after a reject frame, a close after 0x82 (the room is dead).
	default:
		if gotFull {
			p.m.UnexpectedDisconnects.Add(1)
		} else {
			p.m.recvError()
		}
	}
}

// controlLoop — background client events: planned drops of the reconnect
// stress and a forced close on ctx (session wakes up with a Recv error).
func (p *peer) controlLoop(ctx context.Context) {
	if p.reconnectEnabled.Load() {
		// The first drop is at a random point in the window so clients don't
		// fire as a wave; afterwards — the RECONNECT_EVERY period.
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
					// conn may be nil (mid-dial/between sessions): dropReq is
					// followed through by the check at the start of session.
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

// closeForGood — abrupt drop without resume: the fleet's drain phase.
func (p *peer) closeForGood() {
	p.closeReq.Store(true)
	p.closeConn()
}

// --- replica state: own peerID and isMaster --------------------------------

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
		return // patch before the first full — a full-state resync heals the replica
	}
	if err := p.room.ApplyPatch(*patch); err != nil {
		p.m.decodeError()
		return
	}
	p.observeLocked()
}

// observeLocked — peerID by nick and the isMaster flag for the send loop.
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

// sendLoop — PatchInput for all clients (INPUT_HZ), PatchEntities while the
// server keeps us as master (ENTITIES_HZ). mode=occupancy — nothing is sent.
// peerID is valid only after admit + the first full state — before that
// frames are pointless (the peer is not in the room yet).
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

// sendInput — PatchInput (0x06) with the value "nick:<rand>:<tick>";
// MakePatch from prev → Added on the session's first frame, Updated after.
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

// sendEntities — PatchEntities (0x04): entity id = own peer id, Owner = own
// peer id, position is a u64 counter (same as in the match_e2e driver).
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
