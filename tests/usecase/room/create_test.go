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

func newUnlimitedLimits(ctrl *gomock.Controller) *mocks.MockLimits {
	limits := mocks.NewMockLimits(ctrl)
	limits.EXPECT().AllowCreateRoom(gomock.Any()).Return(nil).AnyTimes()
	return limits
}

type stubTicketIssuer struct {
	fn func(ctx context.Context, roomID domain.RoomID, nickName string, password string) (useroom.IssueTicketResult, error)
}

func (s *stubTicketIssuer) IssueTicket(ctx context.Context, roomID domain.RoomID, nickName string, password string) (useroom.IssueTicketResult, error) {
	if s.fn == nil {
		return useroom.IssueTicketResult{}, errors.New("stub ticket issuer not configured")
	}
	return s.fn(ctx, roomID, nickName, password)
}

func unexpectedTicketIssuer() *stubTicketIssuer {
	return &stubTicketIssuer{
		fn: func(context.Context, domain.RoomID, string, string) (useroom.IssueTicketResult, error) {
			return useroom.IssueTicketResult{}, errors.New("unexpected IssueTicket call")
		},
	}
}

func TestCreate_Success(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	allocator := mocks.NewMockAllocator(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	limits := newUnlimitedLimits(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	const (
		roomID   = domain.RoomID(10)
		capacity = 8
	)

	ticketIssuer := &stubTicketIssuer{
		fn: func(_ context.Context, id domain.RoomID, nickName string, password string) (useroom.IssueTicketResult, error) {
			if id != roomID {
				t.Fatalf("issue roomID = %v, want %v", id, roomID)
			}
			if nickName != "host" {
				t.Fatalf("nickName = %q, want host", nickName)
			}
			if password != "" {
				t.Fatalf("password = %q, want empty", password)
			}
			return useroom.IssueTicketResult{
				Token: []byte("ticket"),
				Room:  useroom.RoomSummary{ID: roomID},
			}, nil
		},
	}

	registry.EXPECT().GetList(gomock.Any()).Return(nil, nil)
	allocator.EXPECT().AllocateRoomID(gomock.Any()).Return(roomID, nil)
	reservation.EXPECT().RegisterRoom(gomock.Any(), roomID, capacity, "").Return(nil)
	registry.EXPECT().CreateRoom(gomock.Any(), roomID, capacity, gomock.Nil()).Return(room, nil)
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewCreateUseCase(registry, allocator, reservation, limits, ticketIssuer, logger)
	result, err := uc.Create(context.Background(), capacity, "", nil, "host")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if result.Room.ID != roomID {
		t.Fatalf("result.Room.ID = %v, want %v", result.Room.ID, roomID)
	}
	if string(result.Token) != "ticket" {
		t.Fatalf("result.Token = %q, want ticket", result.Token)
	}
}

func TestCreate_IssueTicketErrorRollback(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	allocator := mocks.NewMockAllocator(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	limits := newUnlimitedLimits(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	issueErr := errors.New("issue failed")
	ticketIssuer := &stubTicketIssuer{
		fn: func(context.Context, domain.RoomID, string, string) (useroom.IssueTicketResult, error) {
			return useroom.IssueTicketResult{}, issueErr
		},
	}

	const (
		roomID   = domain.RoomID(3)
		capacity = 4
	)

	registry.EXPECT().GetList(gomock.Any()).Return(nil, nil)
	allocator.EXPECT().AllocateRoomID(gomock.Any()).Return(roomID, nil)
	reservation.EXPECT().RegisterRoom(gomock.Any(), roomID, capacity, "").Return(nil)
	registry.EXPECT().CreateRoom(gomock.Any(), roomID, capacity, gomock.Nil()).Return(room, nil)
	registry.EXPECT().DeleteRoom(gomock.Any(), roomID).Return(nil)
	reservation.EXPECT().UnregisterRoom(gomock.Any(), roomID).Return(nil)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewCreateUseCase(registry, allocator, reservation, limits, ticketIssuer, logger)
	_, err := uc.Create(context.Background(), capacity, "", nil, "host")
	if !errors.Is(err, issueErr) {
		t.Fatalf("err = %v, want issue error", err)
	}
}

func TestCreate_IssueTicketErrorRollbackJoinsUnregisterError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	allocator := mocks.NewMockAllocator(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	limits := newUnlimitedLimits(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	issueErr := errors.New("issue failed")
	unregisterErr := domain.ErrReservationNotFound
	ticketIssuer := &stubTicketIssuer{
		fn: func(context.Context, domain.RoomID, string, string) (useroom.IssueTicketResult, error) {
			return useroom.IssueTicketResult{}, issueErr
		},
	}

	const (
		roomID   = domain.RoomID(5)
		capacity = 4
	)

	registry.EXPECT().GetList(gomock.Any()).Return(nil, nil)
	allocator.EXPECT().AllocateRoomID(gomock.Any()).Return(roomID, nil)
	reservation.EXPECT().RegisterRoom(gomock.Any(), roomID, capacity, "").Return(nil)
	registry.EXPECT().CreateRoom(gomock.Any(), roomID, capacity, gomock.Nil()).Return(room, nil)
	registry.EXPECT().DeleteRoom(gomock.Any(), roomID).Return(nil)
	reservation.EXPECT().UnregisterRoom(gomock.Any(), roomID).Return(unregisterErr)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewCreateUseCase(registry, allocator, reservation, limits, ticketIssuer, logger)
	_, err := uc.Create(context.Background(), capacity, "", nil, "host")
	if !errors.Is(err, issueErr) {
		t.Fatalf("err = %v, want issue error", err)
	}
	if !errors.Is(err, unregisterErr) {
		t.Fatalf("err = %v, want joined unregister error", err)
	}
}

func TestCreate_CancelledContext(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	allocator := mocks.NewMockAllocator(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	limits := newUnlimitedLimits(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	uc := useroom.NewCreateUseCase(registry, allocator, reservation, limits, unexpectedTicketIssuer(), logger)
	_, err := uc.Create(ctx, 4, "", nil, "host")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestCreate_AllocatorError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	allocator := mocks.NewMockAllocator(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	limits := newUnlimitedLimits(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	allocateErr := errors.New("allocate room id failed")
	registry.EXPECT().GetList(gomock.Any()).Return(nil, nil)
	allocator.EXPECT().
		AllocateRoomID(gomock.Any()).
		Return(domain.RoomIDInvalid, allocateErr)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewCreateUseCase(registry, allocator, reservation, limits, unexpectedTicketIssuer(), logger)
	_, err := uc.Create(context.Background(), 4, "", nil, "host")
	if !errors.Is(err, allocateErr) {
		t.Fatalf("err = %v, want allocate error", err)
	}
}

func TestCreate_RegistryError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	allocator := mocks.NewMockAllocator(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	limits := newUnlimitedLimits(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	const (
		roomID   = domain.RoomID(1)
		capacity = 4
	)

	registry.EXPECT().GetList(gomock.Any()).Return(nil, nil)
	allocator.EXPECT().AllocateRoomID(gomock.Any()).Return(roomID, nil)
	reservation.EXPECT().RegisterRoom(gomock.Any(), roomID, capacity, "").Return(nil)
	registry.EXPECT().
		CreateRoom(gomock.Any(), roomID, capacity, gomock.Nil()).
		Return(nil, domain.ErrRoomAlreadyExists)
	reservation.EXPECT().UnregisterRoom(gomock.Any(), roomID).Return(nil)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewCreateUseCase(registry, allocator, reservation, limits, unexpectedTicketIssuer(), logger)
	_, err := uc.Create(context.Background(), capacity, "", nil, "host")
	if !errors.Is(err, domain.ErrRoomAlreadyExists) {
		t.Fatalf("err = %v, want ErrRoomAlreadyExists", err)
	}
}

func TestCreate_RegistryErrorJoinsUnregisterError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	allocator := mocks.NewMockAllocator(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	limits := newUnlimitedLimits(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	const (
		roomID   = domain.RoomID(1)
		capacity = 4
	)

	registry.EXPECT().GetList(gomock.Any()).Return(nil, nil)
	allocator.EXPECT().AllocateRoomID(gomock.Any()).Return(roomID, nil)
	reservation.EXPECT().RegisterRoom(gomock.Any(), roomID, capacity, "").Return(nil)
	registry.EXPECT().
		CreateRoom(gomock.Any(), roomID, capacity, gomock.Nil()).
		Return(nil, domain.ErrRoomAlreadyExists)
	reservation.EXPECT().
		UnregisterRoom(gomock.Any(), roomID).
		Return(domain.ErrReservationNotFound)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewCreateUseCase(registry, allocator, reservation, limits, unexpectedTicketIssuer(), logger)
	_, err := uc.Create(context.Background(), capacity, "", nil, "host")
	if !errors.Is(err, domain.ErrRoomAlreadyExists) {
		t.Fatalf("err = %v, want ErrRoomAlreadyExists", err)
	}
	if !errors.Is(err, domain.ErrReservationNotFound) {
		t.Fatalf("err = %v, want joined ErrReservationNotFound", err)
	}
}

func TestCreate_RoomsLimitReached(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	allocator := mocks.NewMockAllocator(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	limits := mocks.NewMockLimits(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	roomA := mocks.NewMockRoom(ctrl)
	roomB := mocks.NewMockRoom(ctrl)

	registry.EXPECT().GetList(gomock.Any()).Return([]realtime.Room{roomA, roomB}, nil)
	limits.EXPECT().AllowCreateRoom(2).Return(domain.ErrRoomsLimitReached)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewCreateUseCase(registry, allocator, reservation, limits, unexpectedTicketIssuer(), logger)
	_, err := uc.Create(context.Background(), 4, "", nil, "host")
	if !errors.Is(err, domain.ErrRoomsLimitReached) {
		t.Fatalf("err = %v, want ErrRoomsLimitReached", err)
	}
}
