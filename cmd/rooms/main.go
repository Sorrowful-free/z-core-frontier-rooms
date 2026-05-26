package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	admissionadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/admission"
	identityadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/identity"
	zaplog "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/zap"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/policy"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/registry"
	reservationadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/reservation"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/enet"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/ws"
	deliveryenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/delivery/enet"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/delivery/http"
	deliveryws "github.com/Sorrowful-free/z-core-frontier-rooms/internal/delivery/ws"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
	fibzap "github.com/gofiber/contrib/v3/zap"
	"github.com/gofiber/fiber/v3"
	"go.uber.org/zap"
)

func main() {
	log.Println("z-core-frontier-rooms: starting")
	if err := run(); err != nil {
		log.Printf("fatal: %v", err)
		os.Exit(1)
	}
}

func run() error {
	z, err := zap.NewDevelopment()
	if err != nil {
		return err
	}
	defer func() { _ = z.Sync() }()

	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	fiberApp := fiber.New()
	fiberApp.Use(fibzap.New(fibzap.Config{Logger: z}))

	roomPolicyFactory := policy.NewRelayRoomPolicyFactory(zaplog.NewFrom(z, "rooms policy factory"))
	roomFactory := realtime.NewRoomFactory(zaplog.NewFrom(z, "rooms factory"), roomPolicyFactory)
	peerFactory := realtime.NewPeerFactory(zaplog.NewFrom(z, "peer factory"))

	roomRegistry := registry.NewRoomRegistry(appCtx, roomFactory, zaplog.NewFrom(z, "rooms registry"))

	wsConnectionFactory := ws.NewWsConnectionFactory(zaplog.NewFrom(z, "ws connection factory"))
	enetConnectionFactory := enet.NewEnetConnectionFactory(zaplog.NewFrom(z, "enet connection factory"))

	admission := admissionadapter.NewAdmission([]byte("dev-secret-change-me"), time.Hour, "")
	reservation := reservationadapter.NewReservation(zaplog.NewFrom(z, "reservation"))
	allocator := identityadapter.NewCounter()

	createUseCase := room.NewCreateUseCase(roomRegistry, allocator, reservation, zaplog.NewFrom(z, "create use case"))
	issueTicketUseCase := room.NewIssueTicketUseCase(roomRegistry, allocator, admission, reservation, zaplog.NewFrom(z, "issue ticket use case"))
	joinRoomUseCase := room.NewJoinRoomUseCase(admission, peerFactory, roomRegistry, reservation, zaplog.NewFrom(z, "join room use case"))
	leaveRoomUseCase := room.NewLeaveRoomUseCase(roomRegistry, reservation, zaplog.NewFrom(z, "leave room use case"))
	deleteUseCase := room.NewDeleteUseCase(roomRegistry, reservation, zaplog.NewFrom(z, "delete use case"))
	getListUseCase := room.NewGetListUseCase(roomRegistry, zaplog.NewFrom(z, "get list use case"))

	roomsHandler := http.NewRoomsHandler(createUseCase, issueTicketUseCase, deleteUseCase, getListUseCase, zaplog.NewFrom(z, "rooms handler"))
	roomsHandler.RegisterRoutes(fiberApp)

	wsHandler := deliveryws.NewRoomsHandler(appCtx, joinRoomUseCase, leaveRoomUseCase, wsConnectionFactory, zaplog.NewFrom(z, "ws rooms handler"))
	wsHandler.RegisterRoutes(fiberApp)

	enetHandler := deliveryenet.NewRoomsHandler(
		appCtx,
		joinRoomUseCase,
		leaveRoomUseCase,
		enetConnectionFactory,
		deliveryenet.DefaultConfig(),
		zaplog.NewFrom(z, "enet rooms handler"),
	)
	enetHandler.Listen()

	listenErr := make(chan error, 1)
	go func() {
		listenErr <- fiberApp.Listen(":3000")
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		z.Info("shutdown signal received", zap.String("signal", sig.String()))
	case err := <-listenErr:
		if err != nil && !errors.Is(err, fiber.ErrServiceUnavailable) {
			return err
		}
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	var shutdownErr error
	summaries, listErr := getListUseCase.GetList(shutdownCtx)
	if listErr != nil {
		shutdownErr = errors.Join(shutdownErr, listErr)
	} else {
		for _, summary := range summaries {
			if err := deleteUseCase.DeleteForShutdown(shutdownCtx, summary.ID); err != nil {
				shutdownErr = errors.Join(shutdownErr, err)
			}
		}
	}
	if err := roomRegistry.Shutdown(shutdownCtx); err != nil {
		shutdownErr = errors.Join(shutdownErr, err)
	}
	if shutdownErr != nil {
		z.Error("shutdown cleanup", zap.Error(shutdownErr))
	}
	if err := fiberApp.ShutdownWithContext(shutdownCtx); err != nil {
		z.Error("fiber shutdown", zap.Error(err))
	}
	appCancel()

	if err := <-listenErr; err != nil && !errors.Is(err, fiber.ErrServiceUnavailable) {
		return err
	}

	z.Info("shutdown complete")
	return nil
}
