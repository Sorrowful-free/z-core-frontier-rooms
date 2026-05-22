package main

import (
	"context"
	"log"
	"os"
	"time"

	admissionadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/admission"
	zaplog "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/zap"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/registry"
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fiberApp := fiber.New()
	fiberApp.Use(fibzap.New(fibzap.Config{Logger: z}))

	roomFactory := realtime.NewRoomFactory(zaplog.NewFrom(z, "rooms factory"))
	roomHandlerFactory := realtime.NewRelayRoomHandlerFactory(zaplog.NewFrom(z, "rooms handler factory"))
	roomRegistry := registry.NewRoomRegistry(roomFactory, roomHandlerFactory, zaplog.NewFrom(z, "rooms registry"))
	peerFactory := realtime.NewPeerFactory(zaplog.NewFrom(z, "peer factory"))

	admission := admissionadapter.NewAdmission([]byte("dev-secret-change-me"), time.Hour, "")

	createUseCase := room.NewCreateUseCase(roomRegistry, zaplog.NewFrom(z, "create use case"))
	connectUseCase := room.NewConnectUseCase(admission, peerFactory, roomRegistry, zaplog.NewFrom(z, "connect use case"))
	joinUseCase := room.NewJoinUseCase(admission, zaplog.NewFrom(z, "join use case"))
	deleteUseCase := room.NewDeleteUseCase(roomRegistry, zaplog.NewFrom(z, "delete use case"))
	getListUseCase := room.NewGetListUseCase(roomRegistry, zaplog.NewFrom(z, "get list use case"))

	roomsHandler := http.NewRoomsHandler(createUseCase, joinUseCase, deleteUseCase, getListUseCase, zaplog.NewFrom(z, "rooms handler"))
	roomsHandler.RegisterRoutes(fiberApp)

	wsHandler := deliveryws.NewRoomsHandler(connectUseCase, ctx, zaplog.NewFrom(z, "ws rooms handler"))
	wsHandler.RegisterRoutes(fiberApp)

	enetHandler := deliveryenet.NewRoomsHandler(
		connectUseCase,
		ctx,
		deliveryenet.DefaultConfig(),
		zaplog.NewFrom(z, "enet rooms handler"),
	)

	enetHandler.Listen()
	return fiberApp.Listen(":3000")
}
