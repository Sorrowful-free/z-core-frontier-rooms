package room_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	useroom "github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/mocks"
	"go.uber.org/mock/gomock"
)

func newAdmitFixture(ctrl *gomock.Controller) (
	*mocks.MockAdmission,
	*mocks.MockRoomRegistry,
	*mocks.MockPeerFactory,
	*mocks.MockReservation,
	*mocks.MockLogger,
) {
	return mocks.NewMockAdmission(ctrl),
		mocks.NewMockRoomRegistry(ctrl),
		mocks.NewMockPeerFactory(ctrl),
		mocks.NewMockReservation(ctrl),
		mocks.NewMockLogger(ctrl)
}

func newAdmitUseCase(
	admission *mocks.MockAdmission,
	registry *mocks.MockRoomRegistry,
	peerFactory *mocks.MockPeerFactory,
	reservation *mocks.MockReservation,
	logger *mocks.MockLogger,
) *useroom.AdmitPeerUseCase {
	joinUC := useroom.NewJoinRoomUseCase(peerFactory, registry, reservation, logger)
	reconnectUC := useroom.NewReconnectRoomUseCase(peerFactory, registry, reservation, testReconnectTTL, logger, nil)
	return useroom.NewAdmitPeerUseCase(admission, joinUC, reconnectUC, logger)
}

func TestAdmitPeer_ValidateError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	admission, registry, peerFactory, reservation, logger := newAdmitFixture(ctrl)

	admission.EXPECT().
		Validate(gomock.Any(), gomock.Any()).
		Return(domain.Claims{}, domain.ErrInvalidToken)

	uc := newAdmitUseCase(admission, registry, peerFactory, reservation, logger)
	_, session, err := uc.Admit(context.Background(), mocks.NewMockConnection(ctrl), []byte("token"), domain.HostIDAny)
	if !errors.Is(err, domain.ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
	if session.PeerID.IsValid() {
		t.Fatalf("session.PeerID = %v, want invalid", session.PeerID)
	}
}

// P18: ticket с hostID на чужом ENet host'е → ErrWrongHost до траты слота.
func TestAdmitPeer_WrongHost(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	admission, registry, peerFactory, reservation, logger := newAdmitFixture(ctrl)

	claims := newTestClaims(domain.RoomID(1), domain.PeerID(2), "player")
	claims.HostID = domain.HostID(2)

	admission.EXPECT().Validate(gomock.Any(), gomock.Any()).Return(claims, nil)
	logger.EXPECT().Warn(gomock.Any(), gomock.Any()).AnyTimes()

	uc := newAdmitUseCase(admission, registry, peerFactory, reservation, logger)
	_, _, err := uc.Admit(context.Background(), mocks.NewMockConnection(ctrl), []byte("t"), domain.HostID(1))
	if !errors.Is(err, domain.ErrWrongHost) {
		t.Fatalf("err = %v, want ErrWrongHost", err)
	}
}

// WS-admit (HostIDAny): host-bound ticket не проверяется — join-поток идёт дальше.
func TestAdmitPeer_WS_SkipsHostBinding(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	admission, registry, peerFactory, reservation, logger := newAdmitFixture(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer := mocks.NewMockPeer(ctrl)
	conn := mocks.NewMockConnection(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)
	claims := newTestClaims(roomID, peerID, "player")
	claims.HostID = domain.HostID(3)

	admission.EXPECT().Validate(gomock.Any(), gomock.Any()).Return(claims, nil)
	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().Admit(gomock.Any(), roomID, peerID).Return(nil)
	room.EXPECT().Context().Return(context.Background()).AnyTimes()
	peerFactory.EXPECT().CreatePeer(gomock.Any(), peerID, "player", conn, room, logger).Return(peer, nil)
	room.EXPECT().HasPeer(peerID).Return(false)
	room.EXPECT().Join(peer).Return(nil)
	peer.EXPECT().Start().Return(nil)
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	room.EXPECT().GetPeers().Return([]realtime.Peer{peer}).AnyTimes()
	room.EXPECT().GetAttributes().Return(nil).AnyTimes()
	peer.EXPECT().GetID().Return(peerID).AnyTimes()
	peer.EXPECT().GetNickName().Return("player").AnyTimes()
	peer.EXPECT().Ping().Return(int64(0)).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := newAdmitUseCase(admission, registry, peerFactory, reservation, logger)
	_, session, err := uc.Admit(context.Background(), conn, []byte("t"), domain.HostIDAny)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if session.Peer != peer {
		t.Fatal("session.Peer must be the created peer")
	}
}

// claims.Kind=reconnect → resume-поток: reservation.Reconnect вместо Admit,
// nick из слота, broadcast OpPeerReconnected.
func TestAdmitPeer_RoutesReconnectByKind(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	admission, registry, peerFactory, reservation, logger := newAdmitFixture(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer := mocks.NewMockPeer(ctrl)
	conn := mocks.NewMockConnection(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)
	claims := newTestClaims(roomID, peerID, "")
	claims.Kind = domain.ClaimsKindReconnect

	admission.EXPECT().Validate(gomock.Any(), gomock.Any()).Return(claims, nil)
	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().
		SlotDetails(gomock.Any(), roomID, peerID).
		Return(freshAdmittedSlot(peerID, "player"), nil)
	reservation.EXPECT().
		Reconnect(gomock.Any(), roomID, peerID).
		Return(domain.AdmittedSlot{PeerID: peerID, NickName: "player"}, nil)
	room.EXPECT().Context().Return(context.Background()).AnyTimes()
	peerFactory.EXPECT().CreatePeer(gomock.Any(), peerID, "player", conn, room, logger).Return(peer, nil)
	room.EXPECT().HasPeer(peerID).Return(false).Times(2)
	room.EXPECT().Join(peer).Return(nil)
	peer.EXPECT().Start().Return(nil)
	room.EXPECT().Send(gomock.Any()).Return(nil)
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	room.EXPECT().GetPeers().Return([]realtime.Peer{peer}).AnyTimes()
	room.EXPECT().GetAttributes().Return(nil).AnyTimes()
	peer.EXPECT().GetID().Return(peerID).AnyTimes()
	peer.EXPECT().GetNickName().Return("player").AnyTimes()
	peer.EXPECT().Ping().Return(int64(0)).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := newAdmitUseCase(admission, registry, peerFactory, reservation, logger)
	_, session, err := uc.Admit(context.Background(), conn, []byte("t"), domain.HostIDAny)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if session.Peer != peer {
		t.Fatal("session.Peer must be the created peer")
	}
}
