package room_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	useroom "github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/mocks"
	"go.uber.org/mock/gomock"
)

// testReconnectTTL — дедлайн grace в тестах resume; свежий LastSeen
// проходит гейт, старый (now - 2*ttl) — отклоняется expired.
const testReconnectTTL = time.Minute

func newReconnectClaims(roomID domain.RoomID, peerID domain.PeerID) domain.Claims {
	claims := newTestClaims(roomID, peerID, "")
	claims.Kind = domain.ClaimsKindReconnect
	return claims
}

// freshAdmittedSlot — слот со свежим lastSeen: deadline-гейт (P24) пропускает.
func freshAdmittedSlot(peerID domain.PeerID, nick string) domain.AdmittedSlot {
	return domain.AdmittedSlot{PeerID: peerID, NickName: nick, LastSeen: time.Now()}
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
		SlotDetails(gomock.Any(), roomID, peerID).
		Return(freshAdmittedSlot(peerID, "player"), nil)
	reservation.EXPECT().
		Reconnect(gomock.Any(), roomID, peerID).
		Return(domain.AdmittedSlot{PeerID: peerID, NickName: "player"}, nil)
	room.EXPECT().Context().Return(context.Background()).AnyTimes()
	peerFactory.EXPECT().CreatePeer(gomock.Any(), peerID, "player", conn, logger).Return(peer, nil)
	room.EXPECT().HasPeer(peerID).Return(false).Times(2)
	room.EXPECT().Join(peer).Return(nil)
	peer.EXPECT().Start(room).Return(nil)
	room.EXPECT().Send(gomock.Any()).Return(nil)
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewReconnectRoomUseCase(peerFactory, registry, reservation, testReconnectTTL, logger, nil)
	session, err := uc.ReconnectRoom(context.Background(), conn, claims)
	if err != nil {
		t.Fatalf("ReconnectRoom: %v", err)
	}
	if session.Peer != peer || session.PeerID != peerID || session.RoomID != roomID {
		t.Fatalf("session = %+v", session)
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
		SlotDetails(gomock.Any(), roomID, peerID).
		Return(freshAdmittedSlot(peerID, "player"), nil)
	reservation.EXPECT().
		Reconnect(gomock.Any(), roomID, peerID).
		Return(domain.AdmittedSlot{PeerID: peerID, NickName: "player"}, nil)
	room.EXPECT().Context().Return(context.Background()).AnyTimes()
	peerFactory.EXPECT().CreatePeer(gomock.Any(), peerID, "player", conn, logger).Return(peer, nil)
	room.EXPECT().HasPeer(peerID).Return(true).Times(2)
	room.EXPECT().Replace(peer).Return(nil)
	peer.EXPECT().Start(room).Return(nil)
	room.EXPECT().Send(gomock.Any()).Return(nil)
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewReconnectRoomUseCase(peerFactory, registry, reservation, testReconnectTTL, logger, nil)
	session, err := uc.ReconnectRoom(context.Background(), conn, claims)
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
		SlotDetails(gomock.Any(), roomID, peerID).
		Return(domain.AdmittedSlot{}, domain.ErrReservationNotReserved)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewReconnectRoomUseCase(peerFactory, registry, reservation, testReconnectTTL, logger, nil)
	_, err := uc.ReconnectRoom(context.Background(), mocks.NewMockConnection(ctrl), newReconnectClaims(roomID, peerID))
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
		SlotDetails(gomock.Any(), roomID, peerID).
		Return(freshAdmittedSlot(peerID, "player"), nil)
	reservation.EXPECT().
		Reconnect(gomock.Any(), roomID, peerID).
		Return(domain.AdmittedSlot{PeerID: peerID, NickName: "player"}, nil)
	room.EXPECT().Context().Return(context.Background()).AnyTimes()
	peerFactory.EXPECT().CreatePeer(gomock.Any(), peerID, "player", conn, logger).Return(peer, nil)
	room.EXPECT().HasPeer(peerID).Return(false).Times(2)
	room.EXPECT().Join(peer).Return(joinErr)
	room.EXPECT().Replace(peer).Return(nil)
	peer.EXPECT().Start(room).Return(nil)
	room.EXPECT().Send(gomock.Any()).Return(nil)
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewReconnectRoomUseCase(peerFactory, registry, reservation, testReconnectTTL, logger, nil)
	session, err := uc.ReconnectRoom(context.Background(), conn, claims)
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
		SlotDetails(gomock.Any(), roomID, peerID).
		Return(freshAdmittedSlot(peerID, "player"), nil)
	reservation.EXPECT().
		Reconnect(gomock.Any(), roomID, peerID).
		Return(domain.AdmittedSlot{PeerID: peerID, NickName: "player"}, nil)
	room.EXPECT().Context().Return(context.Background()).AnyTimes()
	peerFactory.EXPECT().CreatePeer(gomock.Any(), peerID, "player", conn, logger).Return(peer, nil)
	room.EXPECT().HasPeer(peerID).Return(false).Times(2)
	room.EXPECT().Join(peer).Return(joinErr)
	room.EXPECT().Replace(peer).Return(replaceErr)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewReconnectRoomUseCase(peerFactory, registry, reservation, testReconnectTTL, logger, nil)
	_, err := uc.ReconnectRoom(context.Background(), conn, claims)
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
		SlotDetails(gomock.Any(), roomID, peerID).
		Return(freshAdmittedSlot(peerID, "player"), nil)
	reservation.EXPECT().
		Reconnect(gomock.Any(), roomID, peerID).
		Return(domain.AdmittedSlot{PeerID: peerID, NickName: "player"}, nil)
	room.EXPECT().Context().Return(context.Background()).AnyTimes()
	peerFactory.EXPECT().CreatePeer(gomock.Any(), peerID, "player", conn, logger).Return(peer, nil)
	room.EXPECT().HasPeer(peerID).Return(false).Times(2)
	room.EXPECT().Join(peer).Return(joinErr)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewReconnectRoomUseCase(peerFactory, registry, reservation, testReconnectTTL, logger, nil)
	_, err := uc.ReconnectRoom(context.Background(), conn, claims)
	if !errors.Is(err, joinErr) {
		t.Fatalf("err = %v, want joinErr", err)
	}
}

// P24: resume после lastSeen + reconnectTTL отклоняется дедлайн-гейтом до
// refresh'а — ErrReservationExpired (на wire — OpReservationExpired 0x74,
// маппинг покрыт в tests/delivery/errors). Reconnect не вызывается: rejected
// попытка не продлевает grace.
func TestReconnectRoom_ExpiredGraceRejected(t *testing.T) {
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

	stale := domain.AdmittedSlot{
		PeerID:   peerID,
		NickName: "player",
		LastSeen: time.Now().Add(-2 * testReconnectTTL),
	}
	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().SlotDetails(gomock.Any(), roomID, peerID).Return(stale, nil)
	room.EXPECT().HasPeer(peerID).Return(false)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewReconnectRoomUseCase(peerFactory, registry, reservation, testReconnectTTL, logger, nil)
	_, err := uc.ReconnectRoom(context.Background(), mocks.NewMockConnection(ctrl), newReconnectClaims(roomID, peerID))
	if !errors.Is(err, domain.ErrReservationExpired) {
		t.Fatalf("err = %v, want ErrReservationExpired", err)
	}
}

// P24: живой peer (HasPeer) дедлайном не гейтится — lastSeen давно
// подключённого peer'а старый законно; Replace newest-wins идёт без гейта.
func TestReconnectRoom_LivePeerNotGatedByDeadline(t *testing.T) {
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

	// LastSeen далеко за дедлайном — для живого peer'а это норма.
	stale := domain.AdmittedSlot{
		PeerID:   peerID,
		NickName: "player",
		LastSeen: time.Now().Add(-time.Hour),
	}
	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	reservation.EXPECT().SlotDetails(gomock.Any(), roomID, peerID).Return(stale, nil)
	reservation.EXPECT().
		Reconnect(gomock.Any(), roomID, peerID).
		Return(domain.AdmittedSlot{PeerID: peerID, NickName: "player"}, nil)
	room.EXPECT().Context().Return(context.Background()).AnyTimes()
	peerFactory.EXPECT().CreatePeer(gomock.Any(), peerID, "player", conn, logger).Return(peer, nil)
	room.EXPECT().HasPeer(peerID).Return(true).Times(2)
	room.EXPECT().Replace(peer).Return(nil)
	peer.EXPECT().Start(room).Return(nil)
	room.EXPECT().Send(gomock.Any()).Return(nil)
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewReconnectRoomUseCase(peerFactory, registry, reservation, testReconnectTTL, logger, nil)
	session, err := uc.ReconnectRoom(context.Background(), conn, claims)
	if err != nil {
		t.Fatalf("ReconnectRoom: %v", err)
	}
	if session.Peer != peer {
		t.Fatal("session.Peer must be the created peer")
	}
}

// P24: reconnectTTL = 0 («grace нет», ноль-семантизм sweep) — любой
// !HasPeer resume expired даже со свежим lastSeen.
func TestReconnectRoom_ZeroTTLResumeExpired(t *testing.T) {
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
		SlotDetails(gomock.Any(), roomID, peerID).
		Return(freshAdmittedSlot(peerID, "player"), nil)
	room.EXPECT().HasPeer(peerID).Return(false)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewReconnectRoomUseCase(peerFactory, registry, reservation, 0, logger, nil)
	_, err := uc.ReconnectRoom(context.Background(), mocks.NewMockConnection(ctrl), newReconnectClaims(roomID, peerID))
	if !errors.Is(err, domain.ErrReservationExpired) {
		t.Fatalf("err = %v, want ErrReservationExpired", err)
	}
}

// Гонка sweep ↔ resume (T5): SlotDetails ещё видел слот, но к моменту
// Reconnect sweep его отозвал — проигравший resume получает
// ErrReservationNotReserved, а не успех по протухшему снапшоту.
func TestReconnectRoom_SlotRevokedBySweepBetweenCheckAndReconnect(t *testing.T) {
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
		SlotDetails(gomock.Any(), roomID, peerID).
		Return(freshAdmittedSlot(peerID, "player"), nil)
	room.EXPECT().HasPeer(peerID).Return(false)
	reservation.EXPECT().
		Reconnect(gomock.Any(), roomID, peerID).
		Return(domain.AdmittedSlot{}, domain.ErrReservationNotReserved)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewReconnectRoomUseCase(peerFactory, registry, reservation, testReconnectTTL, logger, nil)
	_, err := uc.ReconnectRoom(context.Background(), mocks.NewMockConnection(ctrl), newReconnectClaims(roomID, peerID))
	if !errors.Is(err, domain.ErrReservationNotReserved) {
		t.Fatalf("err = %v, want ErrReservationNotReserved", err)
	}
}
