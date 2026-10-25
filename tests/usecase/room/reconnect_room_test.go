package room_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	useroom "github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/mocks"
	"go.uber.org/mock/gomock"
)

func newReconnectClaims(roomID domain.RoomID, peerID domain.PeerID) domain.Claims {
	claims := newTestClaims(roomID, peerID, "")
	claims.Kind = domain.ClaimsKindReconnect
	return claims
}

// Resume по отсутствующему peer'у: Reconnect → CreatePeer из слота → Join →
// broadcast OpPeerReconnected остальным. reservation.Admit не вызывается,
// слот не ревокается.
func TestReconnectRoom_JoinAbsentPeer(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	peerFactory := mocks.NewMockPeerFactory(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer := mocks.NewMockPeer(ctrl)
	conn := mocks.NewMockConnection(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)
	claims := newReconnectClaims(roomID, peerID)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().
		Reconnect(gomock.Any(), roomID, peerID).
		Return(domain.AdmittedSlot{PeerID: peerID, NickName: "player"}, nil)
	room.EXPECT().Context().Return(context.Background()).AnyTimes()
	peerFactory.EXPECT().CreatePeer(gomock.Any(), peerID, "player", conn, room, logger).Return(peer, nil)
	room.EXPECT().HasPeer(peerID).Return(false)
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

	uc := useroom.NewReconnectRoomUseCase(peerFactory, registry, reservation, logger, nil)
	summary, session, err := uc.ReconnectRoom(context.Background(), conn, claims)
	if err != nil {
		t.Fatalf("ReconnectRoom: %v", err)
	}
	if session.Peer != peer || session.PeerID != peerID || session.RoomID != roomID {
		t.Fatalf("session = %+v", session)
	}
	if summary.ID != roomID {
		t.Fatalf("summary.ID = %v, want %v", summary.ID, roomID)
	}
}

// Reconnect по живому peer'у — Replace (новейшее соединение побеждает).
func TestReconnectRoom_ReplaceLivePeer(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	peerFactory := mocks.NewMockPeerFactory(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer := mocks.NewMockPeer(ctrl)
	conn := mocks.NewMockConnection(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)
	claims := newReconnectClaims(roomID, peerID)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().
		Reconnect(gomock.Any(), roomID, peerID).
		Return(domain.AdmittedSlot{PeerID: peerID, NickName: "player"}, nil)
	room.EXPECT().Context().Return(context.Background()).AnyTimes()
	peerFactory.EXPECT().CreatePeer(gomock.Any(), peerID, "player", conn, room, logger).Return(peer, nil)
	room.EXPECT().HasPeer(peerID).Return(true)
	room.EXPECT().Replace(peer).Return(nil)
	peer.EXPECT().Start().Return(nil)
	room.EXPECT().Send(gomock.Any()).Return(nil)
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	room.EXPECT().GetPeers().Return([]realtime.Peer{peer}).AnyTimes()
	room.EXPECT().GetAttributes().Return(nil).AnyTimes()
	peer.EXPECT().GetID().Return(peerID).AnyTimes()
	peer.EXPECT().GetNickName().Return("player").AnyTimes()
	peer.EXPECT().Ping().Return(int64(0)).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewReconnectRoomUseCase(peerFactory, registry, reservation, logger, nil)
	_, session, err := uc.ReconnectRoom(context.Background(), conn, claims)
	if err != nil {
		t.Fatalf("ReconnectRoom: %v", err)
	}
	if session.Peer != peer {
		t.Fatal("session.Peer must be the created peer")
	}
}

// Reject resume: слота нет / не admitted → ErrReservationNotReserved;
// Revoke и Admit не вызываются.
func TestReconnectRoom_SlotNotAdmitted(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	peerFactory := mocks.NewMockPeerFactory(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().
		Reconnect(gomock.Any(), roomID, peerID).
		Return(domain.AdmittedSlot{}, domain.ErrReservationNotReserved)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewReconnectRoomUseCase(peerFactory, registry, reservation, logger, nil)
	_, _, err := uc.ReconnectRoom(context.Background(), mocks.NewMockConnection(ctrl), newReconnectClaims(roomID, peerID))
	if !errors.Is(err, domain.ErrReservationNotReserved) {
		t.Fatalf("err = %v, want ErrReservationNotReserved", err)
	}
}

// Гонка double-resume одного v4-токена: HasPeer ещё false, но победивший
// resume уже вставил peer'а — Join возвращает ErrPeerAlreadyInRoom
// (обёрнут ErrJoinDenied lifecycle'ом). Проигравший дожимает Replace
// (newest-wins): объект победителя останавливает old.Stop() внутри
// Replace — сироты вне peers map не остаётся.
func TestReconnectRoom_JoinRaceFallsBackToReplace(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	peerFactory := mocks.NewMockPeerFactory(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer := mocks.NewMockPeer(ctrl)
	conn := mocks.NewMockConnection(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)
	claims := newReconnectClaims(roomID, peerID)
	joinErr := fmt.Errorf("%w: %w", domain.ErrJoinDenied, domain.ErrPeerAlreadyInRoom)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().
		Reconnect(gomock.Any(), roomID, peerID).
		Return(domain.AdmittedSlot{PeerID: peerID, NickName: "player"}, nil)
	room.EXPECT().Context().Return(context.Background()).AnyTimes()
	peerFactory.EXPECT().CreatePeer(gomock.Any(), peerID, "player", conn, room, logger).Return(peer, nil)
	room.EXPECT().HasPeer(peerID).Return(false)
	room.EXPECT().Join(peer).Return(joinErr)
	room.EXPECT().Replace(peer).Return(nil)
	peer.EXPECT().Start().Return(nil)
	room.EXPECT().Send(gomock.Any()).Return(nil)
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	room.EXPECT().GetPeers().Return([]realtime.Peer{peer}).AnyTimes()
	room.EXPECT().GetAttributes().Return(nil).AnyTimes()
	peer.EXPECT().GetID().Return(peerID).AnyTimes()
	peer.EXPECT().GetNickName().Return("player").AnyTimes()
	peer.EXPECT().Ping().Return(int64(0)).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewReconnectRoomUseCase(peerFactory, registry, reservation, logger, nil)
	_, session, err := uc.ReconnectRoom(context.Background(), conn, claims)
	if err != nil {
		t.Fatalf("ReconnectRoom: %v", err)
	}
	if session.Peer != peer {
		t.Fatal("session.Peer must be the created peer")
	}
}

// Replace после проигранной гонки double-resume тоже может упасть —
// ошибка наружу, слот не ревокается.
func TestReconnectRoom_JoinRaceReplaceFailure(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	peerFactory := mocks.NewMockPeerFactory(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer := mocks.NewMockPeer(ctrl)
	conn := mocks.NewMockConnection(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)
	claims := newReconnectClaims(roomID, peerID)
	joinErr := fmt.Errorf("%w: %w", domain.ErrJoinDenied, domain.ErrPeerAlreadyInRoom)
	replaceErr := domain.ErrReplaceFailed

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().
		Reconnect(gomock.Any(), roomID, peerID).
		Return(domain.AdmittedSlot{PeerID: peerID, NickName: "player"}, nil)
	room.EXPECT().Context().Return(context.Background()).AnyTimes()
	peerFactory.EXPECT().CreatePeer(gomock.Any(), peerID, "player", conn, room, logger).Return(peer, nil)
	room.EXPECT().HasPeer(peerID).Return(false)
	room.EXPECT().Join(peer).Return(joinErr)
	room.EXPECT().Replace(peer).Return(replaceErr)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewReconnectRoomUseCase(peerFactory, registry, reservation, logger, nil)
	_, _, err := uc.ReconnectRoom(context.Background(), conn, claims)
	if !errors.Is(err, replaceErr) {
		t.Fatalf("err = %v, want ErrReplaceFailed", err)
	}
}

// Фейл после Reconnect (join отклонён): слот не ревокается.
func TestReconnectRoom_JoinFailureDoesNotRevoke(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	peerFactory := mocks.NewMockPeerFactory(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer := mocks.NewMockPeer(ctrl)
	conn := mocks.NewMockConnection(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)
	claims := newReconnectClaims(roomID, peerID)
	joinErr := errors.New("join denied")

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().
		Reconnect(gomock.Any(), roomID, peerID).
		Return(domain.AdmittedSlot{PeerID: peerID, NickName: "player"}, nil)
	room.EXPECT().Context().Return(context.Background()).AnyTimes()
	peerFactory.EXPECT().CreatePeer(gomock.Any(), peerID, "player", conn, room, logger).Return(peer, nil)
	room.EXPECT().HasPeer(peerID).Return(false)
	room.EXPECT().Join(peer).Return(joinErr)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewReconnectRoomUseCase(peerFactory, registry, reservation, logger, nil)
	_, _, err := uc.ReconnectRoom(context.Background(), conn, claims)
	if !errors.Is(err, joinErr) {
		t.Fatalf("err = %v, want joinErr", err)
	}
}
