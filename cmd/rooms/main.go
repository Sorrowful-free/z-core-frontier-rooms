package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	admissionadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/admission"
	appconfig "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/config"
	httplimitsadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/httplimits"
	identityadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/identity"
	zaplog "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/zap"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	statepolicy "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/policy/state"
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

	roomPolicyFactory := statepolicy.NewStateRoomPolicyFactory(zaplog.NewFrom(z, "rooms policy factory"))
	roomFactory := realtime.NewRoomFactory(zaplog.NewFrom(z, "rooms factory"), roomPolicyFactory)
	peerFactory := realtime.NewPeerFactory(zaplog.NewFrom(z, "peer factory"))

	roomRegistry := registry.NewRoomRegistry(appCtx, roomFactory, zaplog.NewFrom(z, "rooms registry"))

	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	wsConnectionFactory := ws.NewWsConnectionFactory(zaplog.NewFrom(z, "ws connection factory"), cfg.Transport)
	enetConnectionFactory := enet.NewEnetConnectionFactory(zaplog.NewFrom(z, "enet connection factory"))

	admission := admissionadapter.NewAdmission(cfg.Admission)
	reservation := reservationadapter.NewReservation(zaplog.NewFrom(z, "reservation"))
	allocator := identityadapter.NewCounter()

	httpLimits, err := httplimitsadapter.New(cfg.HTTPLimits)
	if err != nil {
		return fmt.Errorf("http limits: %w", err)
	}

	createUseCase := room.NewCreateUseCase(roomRegistry, allocator, reservation, httpLimits, zaplog.NewFrom(z, "create use case"))
	issueTicketUseCase := room.NewIssueTicketUseCase(roomRegistry, allocator, admission, reservation, zaplog.NewFrom(z, "issue ticket use case"))
	leaveRoomUseCase := room.NewLeaveRoomUseCase(roomRegistry, reservation, zaplog.NewFrom(z, "leave room use case"))
	deleteUseCase := room.NewDeleteUseCase(roomRegistry, reservation, leaveRoomUseCase, zaplog.NewFrom(z, "delete use case"))
	joinRoomUseCase := room.NewJoinRoomUseCase(admission, peerFactory, roomRegistry, reservation, zaplog.NewFrom(z, "join room use case"))
	getListUseCase := room.NewGetListUseCase(roomRegistry, zaplog.NewFrom(z, "get list use case"))
	sweepOrphanAdmittedUseCase := room.NewSweepOrphanAdmittedUseCase(
		roomRegistry,
		reservation,
		cfg.Reservation.OrphanAdmittedTTL,
		zaplog.NewFrom(z, "orphan admitted sweep"),
	)
	room.StartOrphanAdmittedSweep(appCtx, sweepOrphanAdmittedUseCase, cfg.Reservation.OrphanSweepInterval, zaplog.NewFrom(z, "orphan admitted sweep"))

	if cfg.HTTPAuth.Disabled {
		z.Warn("HTTP control plane auth is disabled — for local development only")
	}

	roomsHandler := http.NewRoomsHandler(createUseCase, issueTicketUseCase, deleteUseCase, getListUseCase, zaplog.NewFrom(z, "rooms handler"))
	roomsHandler.RegisterRoutes(fiberApp, cfg.HTTPAuth, httpLimits)

	wsHandler := deliveryws.NewRoomsHandler(appCtx, joinRoomUseCase, leaveRoomUseCase, wsConnectionFactory, cfg.Transport, zaplog.NewFrom(z, "ws rooms handler"))
	wsHandler.RegisterRoutes(fiberApp)

	enetCfg := deliveryenet.DefaultConfig()
	enetCfg.MaxIncomingFrameBytes = cfg.Transport.MaxIncomingFrameBytes
	enetHandler := deliveryenet.NewRoomsHandler(
		appCtx,
		joinRoomUseCase,
		leaveRoomUseCase,
		enetConnectionFactory,
		enetCfg,
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
