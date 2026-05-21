package main

import (
	"log"
	"os"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/stdlib"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/registry"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/delivery/http"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
	"github.com/gofiber/fiber/v3"
)

func main() {
	log.Println("z-core-frontier-rooms: starting")
	if err := run(); err != nil {
		log.Printf("fatal: %v", err)
		os.Exit(1)
	}
}

func run() error {
	// TODO: wire HTTP/WS/ENet listeners and room use cases.

	fiberApp := fiber.New()

	logger := stdlib.New("rooms")

	roomFactory := realtime.NewRoomFactory(logger)
	roomHandlerFactory := realtime.NewRelayRoomHandlerFactory(logger)
	roomRegistry := registry.NewRoomRegistry(roomFactory, roomHandlerFactory, logger)

	createUseCase := room.NewCreateUseCase(roomRegistry, logger)
	joinUseCase := room.NewJoinUseCase(nil, logger)
	deleteUseCase := room.NewDeleteUseCase(roomRegistry, logger)
	getListUseCase := room.NewGetListUseCase(roomRegistry, logger)

	roomsHandler := http.NewRoomsHandler(createUseCase, joinUseCase, deleteUseCase, getListUseCase, logger)
	roomsHandler.RegisterRoutes(fiberApp)

	return fiberApp.Listen(":3000")
}
