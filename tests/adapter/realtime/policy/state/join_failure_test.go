package state_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

var errDeliverFailed = errors.New("deliver failed")

func TestOnJoin_FailedFullStateSend_LeavesNoPhantomPeer(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(30), 4)

	broken := newFlakyPeer(1, "broken")
	broken.setErr(errDeliverFailed)

	err := h.room.Join(broken)
	if !errors.Is(err, domain.ErrJoinDenied) {
		t.Fatalf("Join err = %v, want ErrJoinDenied", err)
	}
	if h.room.HasPeer(broken.id) {
		t.Fatal("failed peer must be rolled back from room peers")
	}

	if err := h.policy.OnTickPatchState(); err != nil {
		t.Fatalf("OnTickPatchState after failed join: %v (phantom peer leaked into state)", err)
	}

	joiner := newCapturePeer(2, "joiner")
	if err := h.room.Join(joiner); err != nil {
		t.Fatalf("retry Join after failed join: %v", err)
	}

	got := decodeOnlyFullState(t, h, joiner)
	if len(got.Peers) != 1 {
		t.Fatalf("peers = %#v, want only joiner (phantom peer leaked)", got.Peers)
	}
	self, ok := got.Peers[domain.PeerID(2)]
	if !ok || !self.IsMaster {
		t.Fatalf("joiner peer state = %#v ok=%v, want master of empty room", self, ok)
	}
}

func TestOnJoin_FailedJoinBroadcast_LeavesNoPhantomPeer(t *testing.T) {
	t.Parallel()

	h := newHarness(t, domain.RoomID(31), 4)
	master := newCapturePeer(1, "master")
	flaky := newFlakyPeer(2, "flaky")
	if err := h.room.Join(master); err != nil {
		t.Fatalf("Join master: %v", err)
	}
	if err := h.room.Join(flaky); err != nil {
		t.Fatalf("Join flaky: %v", err)
	}

	flaky.setErr(errDeliverFailed)
	joiner := newCapturePeer(3, "joiner")
	err := h.room.Join(joiner)
	if !errors.Is(err, domain.ErrJoinDenied) {
		t.Fatalf("Join err = %v, want ErrJoinDenied", err)
	}
	if h.room.HasPeer(joiner.id) {
		t.Fatal("failed joiner must be rolled back from room peers")
	}

	flaky.setErr(nil)
	if err := h.policy.OnTickPatchState(); err != nil {
		t.Fatalf("OnTickPatchState after failed join: %v (phantom peer leaked into state)", err)
	}

	master.clear()
	if err := h.policy.OnTickFullState(); err != nil {
		t.Fatalf("OnTickFullState after failed join: %v", err)
	}
	got := decodeOnlyFullState(t, h, master)
	if len(got.Peers) != 2 {
		t.Fatalf("peers = %#v, want {1,2} (phantom peer leaked)", got.Peers)
	}
	if _, ok := got.Peers[domain.PeerID(3)]; ok {
		t.Fatal("phantom joiner present in authoritative state")
	}
	if !got.Peers[domain.PeerID(1)].IsMaster {
		t.Fatal("master flag must stay on peer 1")
	}

	joiner.clear()
	if err := h.room.Join(joiner); err != nil {
		t.Fatalf("retry Join: %v", err)
	}
	retry := decodeOnlyFullState(t, h, joiner)
	if len(retry.Peers) != 3 {
		t.Fatalf("peers after retry = %#v, want {1,2,3}", retry.Peers)
	}
	if !retry.Peers[domain.PeerID(1)].IsMaster {
		t.Fatal("master flag must stay on peer 1 after retry")
	}
	if retry.Peers[domain.PeerID(3)].IsMaster {
		t.Fatal("retried joiner must not be master")
	}
}

func decodeOnlyFullState(t *testing.T, h *harness, peer *capturePeer) *state.RoomState {
	t.Helper()

	full := filterOpcode(peer.snapshot(), state.OpCodeFullState)
	if len(full) != 1 {
		t.Fatalf("peer %d full states = %d, want 1", peer.id, len(full))
	}
	got, err := h.roomCodec.Decode(full[0].Frame.Payload)
	if err != nil {
		t.Fatalf("Decode full state: %v", err)
	}
	return got
}

type flakyPeer struct {
	id   domain.PeerID
	nick string

	mu         sync.Mutex
	deliverErr error
}

func newFlakyPeer(id domain.PeerID, nick string) *flakyPeer {
	return &flakyPeer{id: id, nick: nick}
}

func (p *flakyPeer) setErr(err error) {
	p.mu.Lock()
	p.deliverErr = err
	p.mu.Unlock()
}

func (p *flakyPeer) GetID() domain.PeerID { return p.id }
func (p *flakyPeer) GetNickName() string  { return p.nick }
func (p *flakyPeer) Ping() int64          { return 0 }
func (p *flakyPeer) IsAlive() bool        { return true }
func (p *flakyPeer) Start() error         { return nil }
func (p *flakyPeer) Stop() error          { return nil }
func (p *flakyPeer) Deliver(ev events.PeerEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.deliverErr != nil {
		return p.deliverErr
	}
	return nil
}
