package room_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
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
	registry.EXPECT().CreateRoom(gomock.Any(), roomID, capacity, gomock.Nil(), domain.RoomPolicyKindState).Return(room, nil)
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewCreateUseCase(registry, allocator, reservation, limits, ticketIssuer, logger)
	result, err := uc.Create(context.Background(), capacity, "", nil, "host", domain.RoomPolicyKindState)
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
	registry.EXPECT().CreateRoom(gomock.Any(), roomID, capacity, gomock.Nil(), domain.RoomPolicyKindState).Return(room, nil)
	registry.EXPECT().DeleteRoom(gomock.Any(), roomID).Return(nil)
	reservation.EXPECT().UnregisterRoom(gomock.Any(), roomID).Return(nil)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewCreateUseCase(registry, allocator, reservation, limits, ticketIssuer, logger)
	_, err := uc.Create(context.Background(), capacity, "", nil, "host", domain.RoomPolicyKindState)
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
	registry.EXPECT().CreateRoom(gomock.Any(), roomID, capacity, gomock.Nil(), domain.RoomPolicyKindState).Return(room, nil)
	registry.EXPECT().DeleteRoom(gomock.Any(), roomID).Return(nil)
	reservation.EXPECT().UnregisterRoom(gomock.Any(), roomID).Return(unregisterErr)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewCreateUseCase(registry, allocator, reservation, limits, ticketIssuer, logger)
	_, err := uc.Create(context.Background(), capacity, "", nil, "host", domain.RoomPolicyKindState)
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
	_, err := uc.Create(ctx, 4, "", nil, "host", domain.RoomPolicyKindState)
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
	_, err := uc.Create(context.Background(), 4, "", nil, "host", domain.RoomPolicyKindState)
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
		CreateRoom(gomock.Any(), roomID, capacity, gomock.Nil(), domain.RoomPolicyKindState).
		Return(nil, domain.ErrRoomAlreadyExists)
	reservation.EXPECT().UnregisterRoom(gomock.Any(), roomID).Return(nil)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewCreateUseCase(registry, allocator, reservation, limits, unexpectedTicketIssuer(), logger)
	_, err := uc.Create(context.Background(), capacity, "", nil, "host", domain.RoomPolicyKindState)
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
		CreateRoom(gomock.Any(), roomID, capacity, gomock.Nil(), domain.RoomPolicyKindState).
		Return(nil, domain.ErrRoomAlreadyExists)
	reservation.EXPECT().
		UnregisterRoom(gomock.Any(), roomID).
		Return(domain.ErrReservationNotFound)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewCreateUseCase(registry, allocator, reservation, limits, unexpectedTicketIssuer(), logger)
	_, err := uc.Create(context.Background(), capacity, "", nil, "host", domain.RoomPolicyKindState)
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
	_, err := uc.Create(context.Background(), 4, "", nil, "host", domain.RoomPolicyKindState)
	if !errors.Is(err, domain.ErrRoomsLimitReached) {
		t.Fatalf("err = %v, want ErrRoomsLimitReached", err)
	}
}

func TestCreate_InvalidCapacity(t *testing.T) {
	t.Parallel()

	for _, capacity := range []int{0, -1, domain.MaxRoomCapacity + 1} {
		ctrl := gomock.NewController(t)
		registry := mocks.NewMockRoomRegistry(ctrl)
		allocator := mocks.NewMockAllocator(ctrl)
		reservation := mocks.NewMockReservation(ctrl)
		limits := mocks.NewMockLimits(ctrl)
		logger := mocks.NewMockLogger(ctrl)

		uc := useroom.NewCreateUseCase(registry, allocator, reservation, limits, unexpectedTicketIssuer(), logger)
		_, err := uc.Create(context.Background(), capacity, "", nil, "host", domain.RoomPolicyKindState)
		if !errors.Is(err, domain.ErrInvalidRoomCapacity) {
			t.Fatalf("capacity %d: err = %v, want ErrInvalidRoomCapacity", capacity, err)
		}
	}
}

// createMu сериализует лимит+регистрацию: из двух конкурентных Create
// проходит ровно один — второй видит уже созданную комнату в GetList.
func TestCreate_ConcurrentLimitRace(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	allocator := mocks.NewMockAllocator(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	limits := mocks.NewMockLimits(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	var created atomic.Int32
	registry.EXPECT().GetList(gomock.Any()).DoAndReturn(func(context.Context) ([]realtime.Room, error) {
		return make([]realtime.Room, int(created.Load())), nil
	}).AnyTimes()
	limits.EXPECT().AllowCreateRoom(gomock.Any()).DoAndReturn(func(current int) error {
		if current > 0 {
			return domain.ErrRoomsLimitReached
		}
		return nil
	}).AnyTimes()
	allocator.EXPECT().AllocateRoomID(gomock.Any()).Return(domain.RoomID(1), nil)
	reservation.EXPECT().RegisterRoom(gomock.Any(), domain.RoomID(1), 4, "").Return(nil)
	registry.EXPECT().CreateRoom(gomock.Any(), domain.RoomID(1), 4, gomock.Nil(), domain.RoomPolicyKindState).DoAndReturn(
		func(context.Context, domain.RoomID, int, domain.RoomAttributes, domain.RoomPolicyKind) (realtime.Room, error) {
			created.Add(1)
			return room, nil
		}).Times(1)
	ticketIssuer := &stubTicketIssuer{
		fn: func(context.Context, domain.RoomID, string, string) (useroom.IssueTicketResult, error) {
			return useroom.IssueTicketResult{Token: []byte("ticket")}, nil
		},
	}
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewCreateUseCase(registry, allocator, reservation, limits, ticketIssuer, logger)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = uc.Create(context.Background(), 4, "", nil, "host", domain.RoomPolicyKindState)
		}(i)
	}
	wg.Wait()

	var succeeded, limited int
	for _, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, domain.ErrRoomsLimitReached):
			limited++
		default:
			t.Fatalf("unexpected err = %v", err)
		}
	}
	if succeeded != 1 || limited != 1 {
		t.Fatalf("succeeded = %d, limited = %d, want 1/1", succeeded, limited)
	}
}
