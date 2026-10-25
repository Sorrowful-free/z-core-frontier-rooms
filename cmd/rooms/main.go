package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	admissionadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/admission"
	appconfig "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/config"
	httplimitsadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/httplimits"
	identityadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/identity"
	zaplog "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/zap"
	metricsadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/metrics"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	statepolicy "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/policy/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/registry"
	reservationadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/reservation"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/enet"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/ws"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/delivery/admin"
	deliveryenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/delivery/enet"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/delivery/http"
	deliveryws "github.com/Sorrowful-free/z-core-frontier-rooms/internal/delivery/ws"
	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
	fibzap "github.com/gofiber/contrib/v3/zap"
	"github.com/gofiber/fiber/v3"
	fibrecover "github.com/gofiber/fiber/v3/middleware/recover"
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
	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	z, err := cfg.Logging.NewLogger()
	if err != nil {
		return err
	}
	defer func() { _ = z.Sync() }()

	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	// Телеметрия (P19): prometheus-registry адаптера; портовый интерфейс
	// уходит в эмиттеры, Gatherer - в admin listener /metrics.
	metricsAdapter, err := metricsadapter.New(cfg.Metrics)
	if err != nil {
		return fmt.Errorf("metrics: %w", err)
	}
	m := portmetrics.Metrics(metricsAdapter)

	fiberApp := fiber.New()
	// Metrics middleware - самый внешний слой onion: duration покрывает
	// весь запрос, а статус видит финальный ответ (panic -> recover -> 500
	// тоже попадает в histogram).
	fiberApp.Use(http.NewMetricsMiddleware(m))
	fiberApp.Use(fibrecover.New())
	fiberApp.Use(fibzap.New(fibzap.Config{
		Logger: z,
		// path без query — ticket в GET /ws?token=... не попадает в access-лог
		Fields: []string{"ip", "latency", "status", "method", "path"},
	}))

	roomPolicyFactory := statepolicy.NewStateRoomPolicyFactory(zaplog.NewFrom(z, "rooms policy factory"), m)

	roomFactory := realtime.NewRoomFactory(zaplog.NewFrom(z, "rooms factory"), roomPolicyFactory, cfg.Transport.RoomIncomingQueueSize, m)
	peerFactory := realtime.NewPeerFactory(zaplog.NewFrom(z, "peer factory"), cfg.Transport.PeerOutboundQueueSize, m)

	roomRegistry := registry.NewRoomRegistry(appCtx, roomFactory, zaplog.NewFrom(z, "rooms registry"), m)

	wsConnectionFactory := ws.NewWsConnectionFactory(zaplog.NewFrom(z, "ws connection factory"), cfg.Transport)
	// HostLoop ещё не существует (создаётся внутри listener'а): nil + delivery
	// привяжет его через BindLoop при старте host loop'а.
	enetConnectionFactory := enet.NewEnetConnectionFactory(nil, zaplog.NewFrom(z, "enet connection factory"))

	admission := admissionadapter.NewAdmission(cfg.Admission)
	reservation := reservationadapter.NewReservation(zaplog.NewFrom(z, "reservation"), m)
	allocator := identityadapter.NewCounter()

	httpLimits, err := httplimitsadapter.New(cfg.HTTPLimits)
	if err != nil {
		return fmt.Errorf("http limits: %w", err)
	}

	leaveRoomUseCase := room.NewLeaveRoomUseCase(roomRegistry, reservation, zaplog.NewFrom(z, "leave room use case"))
	disconnectPeerUseCase := room.NewDisconnectPeerUseCase(roomRegistry, reservation, zaplog.NewFrom(z, "disconnect peer use case"))
	joinRoomUseCase := room.NewJoinRoomUseCase(peerFactory, roomRegistry, reservation, zaplog.NewFrom(z, "join room use case"))
	reconnectRoomUseCase := room.NewReconnectRoomUseCase(peerFactory, roomRegistry, reservation, zaplog.NewFrom(z, "reconnect room use case"), m)
	admitPeerUseCase := room.NewAdmitPeerUseCase(admission, joinRoomUseCase, reconnectRoomUseCase, zaplog.NewFrom(z, "admit peer use case"))

	// cfg.Enet уже провалидирован в LoadFromEnv; после копирования transport-лимитов - повторная Validate.
	enetCfg := cfg.Enet
	enetCfg.MaxIncomingFrameBytes = cfg.Transport.MaxIncomingFrameBytes
	enetCfg.EnetIncomingQueueSize = cfg.Transport.EnetIncomingQueueSize
	if err := enetCfg.Validate(); err != nil {
		return fmt.Errorf("enet config: %w", err)
	}
	// Handler до IssueTicket: он же HostRegistry (P18) - балансировка
	// ticket'ов по живым peer'ам host'ов (least-loaded) живёт внутри него.
	enetHandler := deliveryenet.NewRoomsHandler(
		appCtx,
		admitPeerUseCase,
		disconnectPeerUseCase,
		enetConnectionFactory,
		enetCfg,
		zaplog.NewFrom(z, "enet rooms handler"),
		m,
	)

	issueTicketUseCase := room.NewIssueTicketUseCase(roomRegistry, allocator, admission, reservation, enetHandler, zaplog.NewFrom(z, "issue ticket use case"))
	createUseCase := room.NewCreateUseCase(roomRegistry, allocator, reservation, httpLimits, issueTicketUseCase, zaplog.NewFrom(z, "create use case"))
	deleteUseCase := room.NewDeleteUseCase(roomRegistry, reservation, leaveRoomUseCase, zaplog.NewFrom(z, "delete use case"))
	getListUseCase := room.NewGetListUseCase(roomRegistry, zaplog.NewFrom(z, "get list use case"))
	sweepOrphanAdmittedUseCase := room.NewSweepOrphanAdmittedUseCase(
		roomRegistry,
		reservation,
		cfg.Reservation.ReconnectTTL,
		zaplog.NewFrom(z, "orphan admitted sweep"),
		m,
	)
	orphanSweep := room.StartOrphanAdmittedSweep(appCtx, sweepOrphanAdmittedUseCase, cfg.Reservation.OrphanSweepInterval, zaplog.NewFrom(z, "orphan admitted sweep"))

	var idleSweep *room.SweepHandle
	if cfg.Transport.PeerIdleTimeout > 0 {
		evictIdlePeersUseCase := room.NewEvictIdlePeersUseCase(roomRegistry, disconnectPeerUseCase, zaplog.NewFrom(z, "idle peer sweep"), m)
		idleSweep = room.StartIdlePeerSweep(appCtx, evictIdlePeersUseCase, cfg.Transport.PeerIdleSweepInterval, zaplog.NewFrom(z, "idle peer sweep"))
	}

	if cfg.HTTPAuth.Disabled {
		z.Warn("HTTP control plane auth is disabled — for local development only")
	}

	roomsHandler := http.NewRoomsHandler(createUseCase, issueTicketUseCase, deleteUseCase, getListUseCase, zaplog.NewFrom(z, "rooms handler"))
	roomsHandler.RegisterRoutes(fiberApp, cfg.HTTPAuth, httpLimits)

	wsHandler := deliveryws.NewRoomsHandler(appCtx, admitPeerUseCase, disconnectPeerUseCase, wsConnectionFactory, cfg.Transport, zaplog.NewFrom(z, "ws rooms handler"), m)
	wsHandler.RegisterRoutes(fiberApp)

	enetHandle := enetHandler.Listen()

	// Admin listener (P19): отдельный Fiber-инстанс с GET /metrics,
	// изолированный от публичного API. Пустой ADMIN_LISTEN_ADDR - выключен.
	// Bind - синхронно, до serve-горутины (P19b): bind failure виден
	// сразу как ошибка run, а race "Shutdown до Listen" исключён -
	// Listener на уже забинденном сокете выходит сам по закрытии.
	var adminApp *fiber.App
	var adminLn net.Listener
	if cfg.Metrics.AdminListenAddr != "" {
		adminLn, err = net.Listen("tcp", cfg.Metrics.AdminListenAddr)
		if err != nil {
			return fmt.Errorf("admin listener bind %q: %w", cfg.Metrics.AdminListenAddr, err)
		}
		adminApp = admin.NewMetricsApp(metricsAdapter.Gatherer(), zaplog.NewFrom(z, "admin listener"))
	}

	listenErr := make(chan error, 1)
	go func() {
		listenErr <- fiberApp.Listen(cfg.Server.HTTPAddr)
	}()

	adminListenErr := make(chan error, 1)
	if adminApp != nil {
		go func() {
			adminListenErr <- adminApp.Listener(adminLn)
		}()
		z.Info("admin listener starting", zap.String("addr", cfg.Metrics.AdminListenAddr))
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	var listenResult error
	listenDone := false
	var enetResult error
	enetDone := false
	var adminListenResult error
	adminListenDone := false
	// enetDoneCh зануляем после чистого выхода listener'а (stub build / ENet disabled):
	// закрытый Done иначе будет выигрывать select в бесконечном цикле.
	enetDoneCh := enetHandle.Done()
waitLoop:
	for {
		select {
		case sig := <-sigCh:
			z.Info("shutdown signal received", zap.String("signal", sig.String()))
			break waitLoop
		case err := <-listenErr:
			listenDone = true
			listenResult = err
			if err != nil && !errors.Is(err, fiber.ErrServiceUnavailable) {
				z.Error("fiber listen failed", zap.Error(err))
			}
			break waitLoop
		case err := <-adminListenErr:
			adminListenDone = true
			adminListenResult = err
			if err != nil && !errors.Is(err, fiber.ErrServiceUnavailable) {
				z.Error("admin listen failed", zap.Error(err))
			}
			break waitLoop
		case <-enetDoneCh:
			if err := enetHandle.Err(); err != nil && !errors.Is(err, context.Canceled) {
				enetDone = true
				enetResult = err
				z.Error("enet listen failed", zap.Error(err))
				break waitLoop
			}
			enetDoneCh = nil
		}
	}
	signal.Stop(sigCh)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := fiberApp.ShutdownWithContext(shutdownCtx); err != nil {
		z.Error("fiber shutdown", zap.Error(err))
	}
	if adminApp != nil {
		// Свой timeout-бюджет (P19b): shutdown основного app мог съесть
		// общий ctx целиком и оставить admin без времени на drain.
		adminCtx, adminCancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := adminApp.ShutdownWithContext(adminCtx); err != nil {
			z.Error("admin shutdown", zap.Error(err))
		}
		adminCancel()
	}

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
	appCancel()
	// Stop отменяет host loop и ждёт реального выхода: сессии закрываются,
	// Receive-горутины peer'ов просыпаются по closed-каналу.
	enetHandle.Stop()
	orphanSweep.Stop()
	idleSweep.Stop()

	// Однократное чтение listenErr: если select выше уже забрал результат,
	// повторный receive заблокировался бы навсегда.
	if !listenDone {
		listenResult = <-listenErr
	}
	// То же для admin listener'а, но только если он вообще был запущен -
	// иначе receive на пустом канале заблокировался бы навсегда.
	if adminApp != nil && !adminListenDone {
		adminListenResult = <-adminListenErr
	}
	var resultErr error
	if listenResult != nil && !errors.Is(listenResult, fiber.ErrServiceUnavailable) {
		resultErr = errors.Join(resultErr, listenResult)
	}
	if adminListenResult != nil && !errors.Is(adminListenResult, fiber.ErrServiceUnavailable) {
		resultErr = errors.Join(resultErr, adminListenResult)
	}
	// Финальный Err() listener'а: bind failure / падение host loop фейлят процесс.
	if !enetDone {
		if err := enetHandle.Err(); err != nil && !errors.Is(err, context.Canceled) {
			enetResult = err
		}
	}
	if enetResult != nil {
		resultErr = errors.Join(resultErr, enetResult)
	}
	if resultErr != nil {
		return errors.Join(resultErr, shutdownErr)
	}

	z.Info("shutdown complete")
	return nil
}
