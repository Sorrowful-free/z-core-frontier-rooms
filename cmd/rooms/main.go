package main

import (
	"log"
	"os"

	zaplog "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/zap"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/registry"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/delivery/http"
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
	// TODO: wire WS/ENet listeners and ConnectUseCase.

	z, err := zap.NewDevelopment()
	if err != nil {
		return err
	}
	defer func() { _ = z.Sync() }()

	fiberApp := fiber.New()
	fiberApp.Use(fibzap.New(fibzap.Config{Logger: z}))

	roomFactory := realtime.NewRoomFactory(zaplog.NewFrom(z, "rooms factory"))
	roomHandlerFactory := realtime.NewRelayRoomHandlerFactory(zaplog.NewFrom(z, "rooms handler factory"))
	roomRegistry := registry.NewRoomRegistry(roomFactory, roomHandlerFactory, zaplog.NewFrom(z, "rooms registry"))

	createUseCase := room.NewCreateUseCase(roomRegistry, zaplog.NewFrom(z, "create use case"))
	joinUseCase := room.NewJoinUseCase(nil, zaplog.NewFrom(z, "join use case"))
	deleteUseCase := room.NewDeleteUseCase(roomRegistry, zaplog.NewFrom(z, "delete use case"))
	getListUseCase := room.NewGetListUseCase(roomRegistry, zaplog.NewFrom(z, "get list use case"))

	roomsHandler := http.NewRoomsHandler(createUseCase, joinUseCase, deleteUseCase, getListUseCase, zaplog.NewFrom(z, "rooms handler"))
	roomsHandler.RegisterRoutes(fiberApp)

	return fiberApp.Listen(":3000")
}
